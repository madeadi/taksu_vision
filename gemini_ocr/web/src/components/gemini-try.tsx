import { useRef, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { WorkspacePicker } from "@/components/workspace-picker"
import * as api from "@/lib/api"
import * as ocrApi from "@/lib/gemini-ocr-api"

const IMAGES_DIR = "ocr"

function linesToList(text: string): string[] | undefined {
  const lines = text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
  return lines.length > 0 ? lines : undefined
}

export function GeminiTry() {
  const [workspaceId, setWorkspaceId] = useState<string | null>(null)
  const [files, setFiles] = useState<File[]>([])
  const [instruction, setInstruction] = useState("")
  const [formattedOutputText, setFormattedOutputText] = useState("")
  const [patternsText, setPatternsText] = useState("")
  const [removePatternsText, setRemovePatternsText] = useState("")
  const [maxConcurrency, setMaxConcurrency] = useState("")
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<ocrApi.TaskResult | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const handleRun = async () => {
    if (!workspaceId) {
      toast.error("Select a workspace first")
      return
    }
    if (files.length === 0) {
      toast.error("Select at least one image")
      return
    }

    let formattedOutput: Record<string, unknown> | undefined
    if (formattedOutputText.trim()) {
      try {
        formattedOutput = JSON.parse(formattedOutputText) as Record<string, unknown>
      } catch {
        toast.error("formatted_output example isn't valid JSON")
        return
      }
    }

    setRunning(true)
    setResult(null)
    try {
      await api.uploadFiles(workspaceId, files, IMAGES_DIR)
      const response = await ocrApi.runOcr({
        workspaceId,
        imagesDir: IMAGES_DIR,
        filenames: files.map((f) => f.name),
        maxConcurrency: maxConcurrency ? Number(maxConcurrency) : undefined,
        instruction,
        formattedOutput,
        patterns: linesToList(patternsText),
        removePatterns: linesToList(removePatternsText),
      })
      setResult(response)
      if (!response.success) toast.error(response.error ?? "Extraction failed")
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-8">
      <h1 className="text-2xl font-semibold">Try extraction</h1>

      <div className="flex flex-col gap-3 rounded-xl border p-4">
        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">Workspace</span>
          <WorkspacePicker value={workspaceId} onChange={setWorkspaceId} />
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">Images</span>
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              multiple
              className="hidden"
              onChange={(e) => setFiles(Array.from(e.target.files ?? []))}
            />
            <Button variant="outline" onClick={() => fileInputRef.current?.click()}>
              Choose files
            </Button>
            <span className="text-sm text-muted-foreground">
              {files.length === 0
                ? "No files selected"
                : `${files.length} file(s): ${files.map((f) => f.name).join(", ")}`}
            </span>
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">Instruction</span>
          <Textarea
            placeholder="Read the ID card and extract the fields below. (Leave blank for a general-OCR default.)"
            value={instruction}
            onChange={(e) => setInstruction(e.target.value)}
            rows={2}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">formatted_output example (JSON)</span>
          <Textarea
            placeholder={'{"name": "string", "nik": "string"}'}
            value={formattedOutputText}
            onChange={(e) => setFormattedOutputText(e.target.value)}
            rows={3}
            className="font-mono text-sm"
          />
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Patterns (one per line)</span>
            <Textarea
              placeholder={"16-digit NIK number\n6-digit plate number"}
              value={patternsText}
              onChange={(e) => setPatternsText(e.target.value)}
              rows={3}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Remove patterns (one per line)</span>
            <Textarea
              placeholder={"WATERMARK\nCONFIDENTIAL"}
              value={removePatternsText}
              onChange={(e) => setRemovePatternsText(e.target.value)}
              rows={3}
            />
          </div>
        </div>

        <div className="flex flex-col gap-1.5 sm:w-1/2">
          <span className="text-sm font-medium">Max concurrency (optional)</span>
          <Input
            type="number"
            min={1}
            placeholder="4"
            value={maxConcurrency}
            onChange={(e) => setMaxConcurrency(e.target.value)}
          />
        </div>

        <Button onClick={handleRun} disabled={running} className="self-start">
          {running ? "Running…" : "Run extraction"}
        </Button>
      </div>

      {running && (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      )}

      {result && (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            {result.output.n_processed} processed, {result.output.n_failed} failed
            {result.output.total_cost_usd != null &&
              ` — $${result.output.total_cost_usd.toFixed(6)}`}
          </p>
          <div className="flex flex-col gap-3">
            {result.output.results.map((r) => (
              <div key={r.image} className="flex flex-col gap-2 rounded-xl border p-4">
                <div className="flex items-center justify-between">
                  <span className="truncate text-sm font-medium">{r.image_name}</span>
                  {(r.input_tokens != null || r.cost_usd != null) && (
                    <span className="text-xs text-muted-foreground">
                      {r.input_tokens ?? 0}/{r.output_tokens ?? 0} tok
                      {r.cost_usd != null && ` · $${r.cost_usd.toFixed(6)}`}
                    </span>
                  )}
                </div>

                {r.error ? (
                  <p className="text-sm text-destructive">{r.error}</p>
                ) : (
                  <>
                    <div className="flex flex-col gap-1">
                      <span className="text-xs font-medium text-muted-foreground">
                        detected_text
                      </span>
                      <p className="text-sm whitespace-pre-wrap">{r.detected_text || "—"}</p>
                    </div>

                    {r.matched_patterns && r.matched_patterns.length > 0 && (
                      <div className="flex flex-wrap gap-1.5">
                        {r.matched_patterns.map((p) => (
                          <Badge key={p} variant="secondary">
                            {p}
                          </Badge>
                        ))}
                      </div>
                    )}

                    {r.formatted_output && Object.keys(r.formatted_output).length > 0 && (
                      <pre className="overflow-x-auto rounded-lg bg-muted p-2 text-xs">
                        {JSON.stringify(r.formatted_output, null, 2)}
                      </pre>
                    )}
                  </>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
