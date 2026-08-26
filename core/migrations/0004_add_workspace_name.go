// Migration: adds an optional "name" field to the "workspaces" collection —
// a human-friendly label for a workspace, distinct from its generated
// "workspace_id". See 0001_create_workspaces.go.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("workspaces")
		if err != nil {
			return err
		}

		collection.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: false,
				Max:      200,
			},
		)

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("workspaces")
		if err != nil {
			return err
		}

		collection.Fields.RemoveByName("name")

		return app.Save(collection)
	})
}
