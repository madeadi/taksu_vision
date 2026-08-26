// Background TTL sweep: periodically removes workspaces past their
// expires_at. Runs independently of the synchronous DELETE /workspaces/{id}
// endpoint in main.go.
package main

import (
	"log"
	"os"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// startSweep runs sweepOnce every interval, forever, in the caller's
// goroutine (callers should invoke this via `go startSweep(...)`).
func startSweep(app core.App, root string, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		sweepOnce(app, root)
	}
}

func sweepOnce(app core.App, root string) {
	now := time.Now().UTC()
	records, err := app.FindRecordsByFilter(workspacesCollection, "expires_at <= {:now}", "", 0, 0, dbx.Params{"now": now})
	if err != nil {
		log.Printf("sweep: query expired workspaces: %v", err)
		return
	}
	for _, record := range records {
		id := record.GetString("workspace_id")
		if err := os.RemoveAll(workspaceDir(root, id)); err != nil {
			log.Printf("sweep: remove expired workspace %s: %v", id, err)
			continue
		}
		if err := app.Delete(record); err != nil {
			log.Printf("sweep: delete workspace record %s: %v", id, err)
			continue
		}
		log.Printf("sweep: removed expired workspace %s (expired %s)", id, record.GetDateTime("expires_at").Time().Format(time.RFC3339))
	}
}
