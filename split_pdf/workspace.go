// Workspace-relative path resolution against a shared WORKSPACE_ROOT.
// Duplicated from ../core/workspace.go — this repo's convention is each
// service carries its own copy of shared logic rather than a shared module.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func filesDir(workspaceRoot, workspaceID string) string {
	return filepath.Join(workspaceRoot, workspaceID, "files")
}

// resolveWorkspacePath resolves relPath against {workspaceRoot}/{workspaceID}/files,
// rejecting absolute paths and any path (including via symlinks) that would
// escape that directory. If mustExist is true, the resolved path must
// already exist on disk.
func resolveWorkspacePath(workspaceRoot, workspaceID, relPath string, mustExist bool) (string, error) {
	base := filesDir(workspaceRoot, workspaceID)
	if _, err := os.Stat(base); err != nil {
		return "", fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("relative path must not be absolute: %s", relPath)
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve workspace files dir: %w", err)
	}

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
