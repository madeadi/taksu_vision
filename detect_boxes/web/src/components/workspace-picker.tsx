import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import * as api from "@/lib/api"

export function WorkspacePicker({
  value,
  onChange,
}: {
  value: string | null
  onChange: (id: string) => void
}) {
  const [workspaces, setWorkspaces] = useState<api.WorkspaceSummary[]>([])
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    api.listWorkspaces().then(setWorkspaces).catch((err: Error) => toast.error(err.message))
  }, [])

  const handleCreate = async () => {
    setCreating(true)
    try {
      const workspace = await api.createWorkspace()
      toast.success(`Created workspace ${workspace.workspace_id}`)
      setWorkspaces((prev) => [...prev, workspace])
      onChange(workspace.workspace_id)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Select value={value} onValueChange={(v) => v && onChange(v)}>
        <SelectTrigger>
          <SelectValue placeholder="Select a workspace…" />
        </SelectTrigger>
        <SelectContent>
          {workspaces.map((ws) => (
            <SelectItem key={ws.workspace_id} value={ws.workspace_id}>
              {ws.workspace_id}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button variant="outline" onClick={handleCreate} disabled={creating}>
        {creating ? "Creating…" : "New workspace"}
      </Button>
    </div>
  )
}
