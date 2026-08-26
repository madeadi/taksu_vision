import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { BoxCanvas, type CanvasBox } from "@/components/box-annotator"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { WorkspacePicker } from "@/components/workspace-picker"
import * as api from "@/lib/api"
import * as detectApi from "@/lib/detect-boxes-api"

const IMAGES_DIR = "try"
const DEFAULT_MODEL = "__default__"

export function DetectTry() {
  const [workspaceId, setWorkspaceId] = useState<string | null>(null)
  const [weights, setWeights] = useState<detectApi.Weight[]>([])
  const [weightName, setWeightName] = useState(DEFAULT_MODEL)
  const [files, setFiles] = useState<File[]>([])
  const [confidence, setConfidence] = useState(0.25)
  const [blurThreshold, setBlurThreshold] = useState(0)
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<detectApi.DetectResponse | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    detectApi
      .listWeights()
      .then(setWeights)
      .catch((err: Error) => toast.error(err.message))
  }, [])

  const handleRun = async () => {
    if (!workspaceId) {
      toast.error("Select a workspace first")
      return
    }
    if (files.length === 0) {
      toast.error("Select at least one image")
      return
    }

    let weightsPath: string | undefined
    if (weightName !== DEFAULT_MODEL) {
      const weight = weights.find((w) => w.name === weightName)
      const resolved =
        weight && detectApi.resolveWeightsPathForWorkspace(weight, workspaceId)
      if (!resolved) {
        toast.error(
          `Weight "${weightName}" isn't reachable from this workspace — it was trained elsewhere.`,
        )
        return
      }
      weightsPath = resolved
    }

    setRunning(true)
    setResult(null)
    try {
      await api.uploadFiles(workspaceId, files, IMAGES_DIR)
      const response = await detectApi.runDetect({
        workspaceId,
        imagesDir: IMAGES_DIR,
        filenames: files.map((f) => f.name),
        confidence,
        blurThreshold,
        weightsPath,
      })
      setResult(response)
      if (!response.success) toast.error(response.error ?? "Detection failed")
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-8">
      <h1 className="text-2xl font-semibold">Try detection</h1>

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

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Model</span>
            <Select value={weightName} onValueChange={(v) => v && setWeightName(v)}>
              <SelectTrigger>
                <SelectValue>
                  {(value: string) => (value === DEFAULT_MODEL ? "Default (server model)" : value)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DEFAULT_MODEL}>Default (server model)</SelectItem>
                {weights.map((w) => (
                  <SelectItem key={w.name} value={w.name}>
                    {w.name} — {w.description}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Confidence</span>
            <Input
              type="number"
              min={0}
              max={1}
              step={0.05}
              value={confidence}
              onChange={(e) => setConfidence(Number(e.target.value))}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Blur threshold</span>
            <Input
              type="number"
              min={0}
              step={1}
              value={blurThreshold}
              onChange={(e) => setBlurThreshold(Number(e.target.value))}
            />
          </div>
        </div>

        <Button onClick={handleRun} disabled={running} className="self-start">
          {running ? "Running…" : "Run detection"}
        </Button>
      </div>

      {running && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Skeleton className="aspect-video w-full" />
          <Skeleton className="aspect-video w-full" />
        </div>
      )}

      {result && (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            {result.output.images.length} image(s), {result.output.n_skipped} skipped
          </p>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {result.output.images.map((image) => {
              const canvasBoxes: CanvasBox[] = image.boxes.map((box) => ({
                id: String(box.box_index),
                points: box.polygon,
                label: `${(box.yolo_conf * 100).toFixed(0)}%`,
              }))
              return (
                <div key={image.image_name} className="flex flex-col gap-1.5">
                  <span className="truncate text-sm font-medium">{image.image_name}</span>
                  {image.skip_reason ? (
                    <p className="text-sm text-muted-foreground">Skipped: {image.skip_reason}</p>
                  ) : (
                    <BoxCanvas
                      imageUrl={api.fileDownloadUrl(
                        workspaceId!,
                        `${IMAGES_DIR}/${image.image_name}`,
                      )}
                      boxes={canvasBoxes}
                      readOnly
                    />
                  )}
                  <span className="text-xs text-muted-foreground">
                    {image.boxes.length} box(es)
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
