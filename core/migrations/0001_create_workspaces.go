// Migration: creates the "workspaces" collection, the SQLite-backed
// (PocketBase) source of truth for workspace metadata — replacing the old
// per-workspace .workspace.json file. Only metadata lives here; the actual
// workspace files stay on shared disk at
// $WORKSPACE_ROOT/{workspace_id}/files/, untouched by this DB.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

func init() {
	migrations.Register(func(app core.App) error {
		collection := core.NewBaseCollection("workspaces")

		collection.Fields.Add(
			&core.TextField{
				Name:     "workspace_id",
				Required: true,
				Max:      64,
			},
			&core.DateField{
				Name:     "created_at",
				Required: true,
			},
			&core.DateField{
				Name:     "expires_at",
				Required: true,
			},
		)

		collection.AddIndex("idx_workspaces_workspace_id", true, "workspace_id", "")

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("workspaces")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
