// Migration: creates the "services" collection — the registry of services
// the workspace management UI monitors (name, URL, online status, last time
// a health check succeeded). See healthcheck.go for the background pinger
// that keeps "online"/"last_seen_at" up to date.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(func(app core.App) error {
		collection := core.NewBaseCollection("services")

		collection.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: true,
				Max:      100,
			},
			&core.URLField{
				Name:     "url",
				Required: true,
			},
			&core.BoolField{
				Name: "online",
			},
			&core.DateField{
				Name: "last_seen_at",
			},
		)

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("services")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
