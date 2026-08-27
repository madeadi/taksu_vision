import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableRow,
} from "@/components/ui/table"
import * as ocrApi from "@/lib/gemini-ocr-api"

export function GeminiConfig() {
  const [config, setConfig] = useState<ocrApi.RefreshConfigResponse | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const load = async () => {
    setRefreshing(true)
    try {
      const response = await ocrApi.refreshConfig()
      setConfig(response)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRefreshing(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleRefresh = async () => {
    await load()
    toast.success("Config reloaded")
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-8">
      <h1 className="text-2xl font-semibold">Configuration</h1>
      <p className="text-sm text-muted-foreground">
        Edit <code className="font-mono">config.yaml</code> on disk (see the service's
        README), then click Refresh to apply changes without restarting the server. The
        Gemini API key is never shown here.
      </p>

      {config && (
        <Table>
          <TableBody>
            <TableRow>
              <TableCell className="font-medium">Model</TableCell>
              <TableCell>{config.model}</TableCell>
            </TableRow>
            <TableRow>
              <TableCell className="font-medium">Max concurrency</TableCell>
              <TableCell>{config.max_concurrency}</TableCell>
            </TableRow>
            <TableRow>
              <TableCell className="font-medium">Workspace root</TableCell>
              <TableCell className="font-mono text-sm">{config.workspace_root}</TableCell>
            </TableRow>
            <TableRow>
              <TableCell className="font-medium">Save results dir</TableCell>
              <TableCell className="font-mono text-sm">{config.save_results_dir}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      )}

      <Button onClick={handleRefresh} disabled={refreshing} className="self-start">
        {refreshing ? "Refreshing…" : "Refresh"}
      </Button>
    </div>
  )
}
