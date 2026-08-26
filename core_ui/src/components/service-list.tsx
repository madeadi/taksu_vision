import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
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

// Matches the backend's health-check interval (see core/healthcheck.go) so
// the list reflects a status change without the user refreshing.
const POLL_INTERVAL_MS = 30_000

export function ServiceList({
  onOpen,
}: {
  onOpen: (service: api.Service) => void
}) {
  const [services, setServices] = useState<api.Service[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  // null while adding a new service; the service being edited otherwise.
  const [editing, setEditing] = useState<api.Service | null>(null)
  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const [webUrl, setWebUrl] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const refresh = () => {
    api
      .listServices()
      .then(setServices)
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    refresh()
    const interval = setInterval(refresh, POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [])

  const openAddDialog = () => {
    setEditing(null)
    setName("")
    setUrl("")
    setWebUrl("")
    setOpen(true)
  }

  const openEditDialog = (service: api.Service) => {
    setEditing(service)
    setName(service.name)
    setUrl(service.url)
    setWebUrl(service.web_url ?? "")
    setOpen(true)
  }

  const handleSubmit = async () => {
    if (!name.trim() || !url.trim()) return
    setSubmitting(true)
    try {
      if (editing) {
        await api.updateService(editing.id, name.trim(), url.trim(), webUrl.trim())
        toast.success(`Updated ${name.trim()}`)
      } else {
        await api.createService(name.trim(), url.trim(), webUrl.trim())
        toast.success(`Added ${name.trim()}`)
      }
      setOpen(false)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async (service: api.Service) => {
    try {
      await api.deleteService(service.id)
      toast.success(`Removed ${service.name}`)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Services</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button onClick={openAddDialog} />}>
            Add service
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{editing ? "Edit service" : "Add service"}</DialogTitle>
              <DialogDescription>
                Monitored by pinging its /health endpoint every 30 seconds.
                The web app URL is what core_ui iframes for the Dashboard
                button.
              </DialogDescription>
            </DialogHeader>
            <div className="flex flex-col gap-3">
              <Input
                placeholder="Name (e.g. split_pdf)"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              <Input
                placeholder="URL (e.g. http://localhost:8823)"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
              <Input
                placeholder="Web app URL (optional, e.g. http://localhost:8825)"
                value={webUrl}
                onChange={(e) => setWebUrl(e.target.value)}
              />
            </div>
            <DialogFooter>
              <DialogClose render={<Button variant="outline" />}>
                Cancel
              </DialogClose>
              <Button
                onClick={handleSubmit}
                disabled={submitting || !name.trim() || !url.trim()}
              >
                {submitting
                  ? editing
                    ? "Saving…"
                    : "Adding…"
                  : editing
                    ? "Save"
                    : "Add"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {loading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : services.length === 0 ? (
        <p className="text-muted-foreground py-8 text-center text-sm">
          No services yet. Add one to start monitoring it.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>URL</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last seen online</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {services.map((svc) => (
              <TableRow key={svc.id}>
                <TableCell className="font-medium">{svc.name}</TableCell>
                <TableCell className="font-mono text-sm">{svc.url}</TableCell>
                <TableCell>
                  <Badge variant={svc.online ? "default" : "destructive"}>
                    {svc.online ? "Online" : "Offline"}
                  </Badge>
                </TableCell>
                <TableCell>
                  {svc.last_seen_at
                    ? new Date(svc.last_seen_at).toLocaleString()
                    : "Never"}
                </TableCell>
                <TableCell className="flex justify-end gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!svc.web_url}
                    title={
                      svc.web_url
                        ? undefined
                        : "No web app URL set — edit this service to add one"
                    }
                    onClick={() => onOpen(svc)}
                  >
                    Dashboard
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => openEditDialog(svc)}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDelete(svc)}
                  >
                    Remove
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
