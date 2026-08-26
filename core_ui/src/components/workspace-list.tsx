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
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
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
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [creating, setCreating] = useState(false)
  // null while renaming nothing; the workspace being renamed otherwise.
  const [renaming, setRenaming] = useState<api.WorkspaceSummary | null>(null)
  const [renameValue, setRenameValue] = useState("")
  const [renameSubmitting, setRenameSubmitting] = useState(false)

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
      const workspace = await api.createWorkspace(name.trim())
      toast.success(`Created workspace ${workspace.name}`)
      setOpen(false)
      setName("")
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setCreating(false)
    }
  }

  const openRenameDialog = (ws: api.WorkspaceSummary) => {
    setRenaming(ws)
    setRenameValue(ws.name)
  }

  const handleRename = async () => {
    if (!renaming || !renameValue.trim()) return
    setRenameSubmitting(true)
    try {
      await api.renameWorkspace(renaming.workspace_id, renameValue.trim())
      toast.success(`Renamed to ${renameValue.trim()}`)
      setRenaming(null)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRenameSubmitting(false)
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
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button />}>Create workspace</DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create workspace</DialogTitle>
            </DialogHeader>
            <Input
              placeholder="Name (optional)"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <DialogFooter>
              <DialogClose render={<Button variant="outline" />}>
                Cancel
              </DialogClose>
              <Button onClick={handleCreate} disabled={creating}>
                {creating ? "Creating…" : "Create"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          open={renaming !== null}
          onOpenChange={(v) => !v && setRenaming(null)}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Rename workspace</DialogTitle>
            </DialogHeader>
            <Input
              placeholder="Name"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
            />
            <DialogFooter>
              <DialogClose render={<Button variant="outline" />}>
                Cancel
              </DialogClose>
              <Button
                onClick={handleRename}
                disabled={renameSubmitting || !renameValue.trim()}
              >
                {renameSubmitting ? "Saving…" : "Save"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
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
              <TableHead>Name</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {workspaces.map((ws) => (
              <TableRow key={ws.workspace_id} className="cursor-pointer">
                <TableCell
                  className="font-medium"
                  onClick={() => onSelect(ws.workspace_id)}
                >
                  {ws.name}
                </TableCell>
                <TableCell onClick={() => onSelect(ws.workspace_id)}>
                  {new Date(ws.created_at).toLocaleString()}
                </TableCell>
                <TableCell onClick={() => onSelect(ws.workspace_id)}>
                  {new Date(ws.expires_at).toLocaleString()}
                </TableCell>
                <TableCell className="flex justify-end gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => openRenameDialog(ws)}
                  >
                    Rename
                  </Button>
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
                          This permanently deletes {ws.name} ({ws.workspace_id}
                          ) and every file in it. This can't be undone.
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
