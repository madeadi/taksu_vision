import { useEffect, useState } from "react"
import { toast } from "sonner"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import * as api from "@/lib/api"

export function WorkspaceList({
  onSelect,
}: {
  onSelect: (id: string) => void
}) {
  const [workspaces, setWorkspaces] = useState<api.WorkspaceSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)

  const refresh = () => {
    setLoading(true)
    api
      .listWorkspaces()
      .then(setWorkspaces)
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(refresh, [])

  const handleCreate = async () => {
    setCreating(true)
    try {
      const workspace = await api.createWorkspace()
      toast.success(`Created workspace ${workspace.workspace_id}`)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      await api.deleteWorkspace(id)
      toast.success(`Deleted workspace ${id}`)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Workspaces</h1>
        <Button onClick={handleCreate} disabled={creating}>
          {creating ? "Creating…" : "Create workspace"}
        </Button>
      </div>

      {loading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : workspaces.length === 0 ? (
        <p className="text-muted-foreground py-8 text-center text-sm">
          No workspaces yet. Create one to get started.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Workspace ID</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {workspaces.map((ws) => (
              <TableRow key={ws.workspace_id} className="cursor-pointer">
                <TableCell
                  className="font-mono text-sm"
                  onClick={() => onSelect(ws.workspace_id)}
                >
                  {ws.workspace_id}
                </TableCell>
                <TableCell onClick={() => onSelect(ws.workspace_id)}>
                  {new Date(ws.created_at).toLocaleString()}
                </TableCell>
                <TableCell onClick={() => onSelect(ws.workspace_id)}>
                  {new Date(ws.expires_at).toLocaleString()}
                </TableCell>
                <TableCell>
                  <AlertDialog>
                    <AlertDialogTrigger
                      render={<Button variant="ghost" size="sm" />}
                    >
                      Delete
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>Delete workspace?</AlertDialogTitle>
                        <AlertDialogDescription>
                          This permanently deletes {ws.workspace_id} and every
                          file in it. This can't be undone.
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                          onClick={() => handleDelete(ws.workspace_id)}
                        >
                          Delete
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
