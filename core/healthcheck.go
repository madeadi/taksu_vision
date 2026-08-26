// Background service health check: every healthCheckInterval, pings
// {url}/health for every registered service and records whether it
// responded successfully (and, if so, updates last_seen_at). Runs
// independently of the HTTP API in main.go.
package main

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const healthCheckInterval = 30 * time.Second
const healthCheckTimeout = 5 * time.Second

var healthCheckClient = &http.Client{Timeout: healthCheckTimeout}

// startHealthCheck runs a health check pass immediately, then every
// interval thereafter, forever, in the caller's goroutine (callers should
// invoke this via `go startHealthCheck(...)`).
func startHealthCheck(app core.App) {
	checkAllServices(app)
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		checkAllServices(app)
	}
}

func checkAllServices(app core.App) {
	records, err := app.FindAllRecords(servicesCollection)
	if err != nil {
		log.Printf("healthcheck: list services: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, record := range records {
		wg.Add(1)
		go func(record *core.Record) {
			defer wg.Done()
			checkService(app, record)
		}(record)
	}
	wg.Wait()
}

func checkService(app core.App, record *core.Record) {
	url := strings.TrimRight(record.GetString("url"), "/") + "/health"

	online := false
	resp, err := healthCheckClient.Get(url)
	if err == nil {
		online = resp.StatusCode >= 200 && resp.StatusCode < 300
		resp.Body.Close()
	}

	record.Set("online", online)
	if online {
		record.Set("last_seen_at", time.Now().UTC())
	}
	if err := app.Save(record); err != nil {
		log.Printf("healthcheck: save status for %s: %v", record.GetString("name"), err)
	}
}
