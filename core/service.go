// Service registry: services the UI monitors (name, URL, online status,
// last time a health check succeeded). No HTTP concerns — see main.go for
// handlers and healthcheck.go for the background pinger.
package main

import (
	"fmt"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const servicesCollection = "services"

// ServiceMeta is a monitored service's record, stored in the "services"
// PocketBase collection.
type ServiceMeta struct {
	ID         string
	Name       string
	URL        string
	WebURL     string
	Online     bool
	LastSeenAt time.Time // zero if never observed online
}

// createService registers a new service to monitor. webURL is optional —
// the service's own web app URL, iframed by core_ui (see AGENT.md).
func createService(app core.App, name, url, webURL string) (ServiceMeta, error) {
	collection, err := app.FindCollectionByNameOrId(servicesCollection)
	if err != nil {
		return ServiceMeta{}, fmt.Errorf("find services collection: %w", err)
	}

	record := core.NewRecord(collection)
	record.Set("name", name)
	record.Set("url", url)
	record.Set("web_url", webURL)
	if err := app.Save(record); err != nil {
		return ServiceMeta{}, fmt.Errorf("save service record: %w", err)
	}

	return recordToServiceMeta(record), nil
}

// updateService edits an existing service's registration (name, URL, and
// web app URL). webURL is optional, same as in createService.
func updateService(app core.App, id, name, url, webURL string) (ServiceMeta, error) {
	record, err := app.FindRecordById(servicesCollection, id)
	if err != nil {
		return ServiceMeta{}, fmt.Errorf("service not found: %s", id)
	}

	record.Set("name", name)
	record.Set("url", url)
	record.Set("web_url", webURL)
	if err := app.Save(record); err != nil {
		return ServiceMeta{}, fmt.Errorf("save service record: %w", err)
	}

	return recordToServiceMeta(record), nil
}

// listServices returns metadata for every monitored service, in no
// particular order.
func listServices(app core.App) ([]ServiceMeta, error) {
	records, err := app.FindAllRecords(servicesCollection)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	metas := make([]ServiceMeta, 0, len(records))
	for _, record := range records {
		metas = append(metas, recordToServiceMeta(record))
	}
	return metas, nil
}

// deleteService stops monitoring a service.
func deleteService(app core.App, id string) error {
	record, err := app.FindRecordById(servicesCollection, id)
	if err != nil {
		return fmt.Errorf("service not found: %s", id)
	}
	return app.Delete(record)
}

func recordToServiceMeta(record *core.Record) ServiceMeta {
	return ServiceMeta{
		ID:         record.Id,
		Name:       record.GetString("name"),
		URL:        record.GetString("url"),
		WebURL:     record.GetString("web_url"),
		Online:     record.GetBool("online"),
		LastSeenAt: record.GetDateTime("last_seen_at").Time(),
	}
}
