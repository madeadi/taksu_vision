// Workspace lifecycle: on-disk layout, creation, metadata, path resolution,
// listing, and deletion. Metadata (workspace_id/created_at/expires_at) lives
// in the embedded PocketBase/SQLite "workspaces" collection (see
// migrations/0001_create_workspaces.go); actual files stay on shared disk.
// No HTTP concerns — see main.go for handlers.
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const filesDirName = "files"
const workspacesCollection = "workspaces"

// WorkspaceMeta is a workspace's lifecycle record, stored in the
// "workspaces" PocketBase collection.
type WorkspaceMeta struct {
	WorkspaceID string
	CreatedAt   time.Time
	ExpiresAt   time.Time
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

// createWorkspace creates a new workspace directory (with its files/
// subdirectory) under root and records its creation/expiry metadata in
// PocketBase.
func createWorkspace(app core.App, root string, ttl time.Duration) (WorkspaceMeta, error) {
	id, err := newWorkspaceID()
	if err != nil {
		return WorkspaceMeta{}, fmt.Errorf("generate workspace id: %w", err)
	}
	if err := os.MkdirAll(filesDir(root, id), 0o755); err != nil {
		return WorkspaceMeta{}, fmt.Errorf("create workspace dir: %w", err)
	}

	collection, err := app.FindCollectionByNameOrId(workspacesCollection)
	if err != nil {
		return WorkspaceMeta{}, fmt.Errorf("find workspaces collection: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	record := core.NewRecord(collection)
	record.Set("workspace_id", id)
	record.Set("created_at", now)
	record.Set("expires_at", expiresAt)
	if err := app.Save(record); err != nil {
		return WorkspaceMeta{}, fmt.Errorf("save workspace record: %w", err)
	}

	return WorkspaceMeta{WorkspaceID: id, CreatedAt: now, ExpiresAt: expiresAt}, nil
}

// loadWorkspaceMeta reads a workspace's metadata from PocketBase. Returns an
// error if the workspace doesn't exist.
func loadWorkspaceMeta(app core.App, id string) (WorkspaceMeta, error) {
	record, err := app.FindFirstRecordByFilter(workspacesCollection, "workspace_id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return WorkspaceMeta{}, fmt.Errorf("workspace not found: %s", id)
	}
	return recordToMeta(record), nil
}

// listWorkspaces returns metadata for every workspace, in no particular
// order.
func listWorkspaces(app core.App) ([]WorkspaceMeta, error) {
	records, err := app.FindAllRecords(workspacesCollection)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	metas := make([]WorkspaceMeta, 0, len(records))
	for _, record := range records {
		metas = append(metas, recordToMeta(record))
	}
	return metas, nil
}

func recordToMeta(record *core.Record) WorkspaceMeta {
	return WorkspaceMeta{
		WorkspaceID: record.GetString("workspace_id"),
		CreatedAt:   record.GetDateTime("created_at").Time(),
		ExpiresAt:   record.GetDateTime("expires_at").Time(),
	}
}

// deleteWorkspace removes a workspace's directory entirely and deletes its
// PocketBase record.
func deleteWorkspace(app core.App, root, id string) error {
	record, err := app.FindFirstRecordByFilter(workspacesCollection, "workspace_id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return fmt.Errorf("workspace not found: %s", id)
	}
	if err := os.RemoveAll(workspaceDir(root, id)); err != nil {
		return fmt.Errorf("remove workspace dir: %w", err)
	}
	return app.Delete(record)
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
