// Background TTL sweep: periodically removes workspaces past their
// expires_at. Runs independently of the synchronous DELETE /workspaces/{id}
// endpoint in main.go.
package main

import (
	"log"
	"os"
	"time"
)

// startSweep runs sweepOnce every interval, forever, in the caller's
// goroutine (callers should invoke this via `go startSweep(...)`).
func startSweep(root string, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		sweepOnce(root, ttl)
	}
}

func sweepOnce(root string, ttl time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("sweep: read %s: %v", root, err)
		return
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		expiresAt, ok := expiryFor(root, id, ttl)
		if !ok || now.Before(expiresAt) {
			continue
		}
		if err := os.RemoveAll(workspaceDir(root, id)); err != nil {
			log.Printf("sweep: remove expired workspace %s: %v", id, err)
			continue
		}
		log.Printf("sweep: removed expired workspace %s (expired %s)", id, expiresAt.Format(time.RFC3339))
	}
}

// expiryFor returns the effective expiry time for a workspace: its recorded
// expires_at if metadata is readable, otherwise the workspace dir's mtime
// plus ttl as a conservative fallback so a corrupt/missing metadata file
// doesn't make a workspace linger forever.
func expiryFor(root, id string, ttl time.Duration) (time.Time, bool) {
	meta, err := loadWorkspaceMeta(root, id)
	if err == nil {
		return meta.ExpiresAt, true
	}
	info, statErr := os.Stat(workspaceDir(root, id))
	if statErr != nil {
		return time.Time{}, false
	}
	return info.ModTime().UTC().Add(ttl), true
}
