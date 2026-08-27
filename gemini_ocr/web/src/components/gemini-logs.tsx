import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import * as ocrApi from "@/lib/gemini-ocr-api"

const POLL_INTERVAL_MS = 4000

export function GeminiLogs() {
  const [lines, setLines] = useState<string[]>([])
  const [autoRefresh, setAutoRefresh] = useState(true)
  const preRef = useRef<HTMLPreElement>(null)

  const load = async () => {
    try {
      const response = await ocrApi.getLogs()
      setLines(response.lines)
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  useEffect(() => {
    load()
    if (!autoRefresh) return
    const id = setInterval(load, POLL_INTERVAL_MS)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRefresh])

  useEffect(() => {
    preRef.current?.scrollTo({ top: preRef.current.scrollHeight })
  }, [lines])

  return (
    <div className="mx-auto flex h-full w-full max-w-4xl flex-col gap-4 p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Logs</h1>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={load}>
            Refresh
          </Button>
          <Button variant={autoRefresh ? "default" : "outline"} onClick={() => setAutoRefresh((v) => !v)}>
            Auto-refresh: {autoRefresh ? "on" : "off"}
          </Button>
        </div>
      </div>
      <p className="text-sm text-muted-foreground">
        Most recent {lines.length} line(s) from this server's process, kept in memory (not
        persisted across restarts).
      </p>
      <pre
        ref={preRef}
        className="min-h-0 flex-1 overflow-auto rounded-xl border bg-muted p-4 font-mono text-xs whitespace-pre-wrap"
      >
        {lines.length > 0 ? lines.join("\n") : "No log lines yet."}
      </pre>
    </div>
  )
}
