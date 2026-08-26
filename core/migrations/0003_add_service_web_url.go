// Migration: adds an optional "web_url" field to the "services" collection
// — a service's own web app URL, iframed by core_ui, distinct from its API
// "url" (which is health-checked). See 0002_create_services.go.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("services")
		if err != nil {
			return err
		}

		collection.Fields.Add(
			&core.URLField{
				Name:     "web_url",
				Required: false,
			},
		)

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("services")
		if err != nil {
			return err
		}

		collection.Fields.RemoveByName("web_url")

		return app.Save(collection)
	})
}
