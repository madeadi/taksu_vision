// Workspace lifecycle: on-disk layout, creation, metadata, path resolution,
// listing, and deletion. No HTTP concerns — see main.go for handlers.
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const metaFileName = ".workspace.json"
const filesDirName = "files"

// WorkspaceMeta is the on-disk record of a workspace's lifecycle, stored as
// {workspaceDir}/.workspace.json.
type WorkspaceMeta struct {
	WorkspaceID string    `json:"workspace_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// FileEntry describes one file inside a workspace, path relative to its
// files/ dir.
type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// newWorkspaceID returns a random UUIDv4 string.
func newWorkspaceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func workspaceDir(root, id string) string {
	return filepath.Join(root, id)
}

func filesDir(root, id string) string {
	return filepath.Join(workspaceDir(root, id), filesDirName)
}

func metaPath(root, id string) string {
	return filepath.Join(workspaceDir(root, id), metaFileName)
}

// createWorkspace creates a new workspace directory (with its files/
// subdirectory) under root and records its creation/expiry metadata.
func createWorkspace(root string, ttl time.Duration) (WorkspaceMeta, error) {
	id, err := newWorkspaceID()
	if err != nil {
		return WorkspaceMeta{}, fmt.Errorf("generate workspace id: %w", err)
	}
	if err := os.MkdirAll(filesDir(root, id), 0o755); err != nil {
		return WorkspaceMeta{}, fmt.Errorf("create workspace dir: %w", err)
	}
	now := time.Now().UTC()
	meta := WorkspaceMeta{
		WorkspaceID: id,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	if err := writeWorkspaceMeta(root, id, meta); err != nil {
		return WorkspaceMeta{}, err
	}
	return meta, nil
}

func writeWorkspaceMeta(root, id string, meta WorkspaceMeta) error {
	f, err := os.Create(metaPath(root, id))
	if err != nil {
		return fmt.Errorf("write workspace metadata: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(meta)
}

// loadWorkspaceMeta reads a workspace's metadata. Returns an error if the
// workspace doesn't exist.
func loadWorkspaceMeta(root, id string) (WorkspaceMeta, error) {
	if _, err := os.Stat(filesDir(root, id)); err != nil {
		return WorkspaceMeta{}, fmt.Errorf("workspace not found: %s", id)
	}
	data, err := os.ReadFile(metaPath(root, id))
	if err != nil {
		// files/ exists but metadata is missing/unreadable: treat as expired
		// immediately so a corrupt workspace doesn't linger indefinitely.
		return WorkspaceMeta{}, fmt.Errorf("workspace metadata unreadable: %s: %w", id, err)
	}
	var meta WorkspaceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return WorkspaceMeta{}, fmt.Errorf("workspace metadata corrupt: %s: %w", id, err)
	}
	return meta, nil
}

// deleteWorkspace removes a workspace directory entirely.
func deleteWorkspace(root, id string) error {
	if _, err := os.Stat(workspaceDir(root, id)); err != nil {
		return fmt.Errorf("workspace not found: %s", id)
	}
	return os.RemoveAll(workspaceDir(root, id))
}

// listWorkspaceFiles walks a workspace's files/ dir (or a subdir of it, if
// dir is non-empty) and returns every regular file found, path relative to
// files/.
func listWorkspaceFiles(root, id, dir string) ([]FileEntry, error) {
	base, err := resolveWorkspacePath(root, id, dir, true)
	if err != nil {
		return nil, err
	}
	// Use the same symlink-resolved root resolveWorkspacePath returned for
	// base, so filepath.Rel below compares paths from the same realm.
	filesRoot, err := resolveWorkspacePath(root, id, "", true)
	if err != nil {
		return nil, err
	}
	entries := []FileEntry{}
	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filesRoot, path)
		if err != nil {
			return err
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// resolveWorkspacePath resolves relPath against {root}/{id}/files, rejecting
// absolute paths and any path (including via symlinks) that would escape the
// workspace's files/ directory. If mustExist is true, the resolved path must
// already exist on disk.
func resolveWorkspacePath(root, id, relPath string, mustExist bool) (string, error) {
	base := filesDir(root, id)
	if _, err := os.Stat(base); err != nil {
		return "", fmt.Errorf("workspace not found: %s", id)
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("relative path must not be absolute: %s", relPath)
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve workspace files dir: %w", err)
	}

	// Lexical check first (no filesystem access): filepath.Join cleans away
	// "." segments and collapses ".." lexically, so this alone catches
	// straightforward traversal like "../../etc".
	candidate := filepath.Join(base, relPath)
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if absCandidate != absBase && !strings.HasPrefix(absCandidate, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("relative path escapes workspace: %s", relPath)
	}

	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve workspace files dir: %w", err)
	}

	// Symlink-escape check: resolve symlinks on the deepest existing ancestor
	// of the candidate (the candidate itself may not exist yet, e.g. an
	// output dir/file that a caller is about to write).
	existingAncestor := absCandidate
	var remainder []string
	for {
		if _, err := os.Lstat(existingAncestor); err == nil {
			break
		}
		existingAncestor, remainder = filepath.Dir(existingAncestor), append([]string{filepath.Base(existingAncestor)}, remainder...)
		if existingAncestor == string(os.PathSeparator) || existingAncestor == "." {
			break
		}
	}
	realExisting, err := filepath.EvalSymlinks(existingAncestor)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if realExisting != realBase && !strings.HasPrefix(realExisting, realBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("relative path escapes workspace: %s", relPath)
	}
	resolved := filepath.Join(append([]string{realExisting}, remainder...)...)

	if mustExist {
		if _, err := os.Stat(resolved); err != nil {
			return "", fmt.Errorf("path not found in workspace: %s", relPath)
		}
	}
	return resolved, nil
}
