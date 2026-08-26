import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { BoxCanvas, classColorClass, type CanvasBox } from "@/components/box-annotator"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { WorkspacePicker } from "@/components/workspace-picker"
import * as api from "@/lib/api"
import * as detectApi from "@/lib/detect-boxes-api"
import type { Point } from "@/lib/detect-boxes-api"

const IMAGES_DIR = "train_images"
const DEFAULT_MODEL = "__default__"
const DEFAULT_CLASS = "object"

interface AnnotationBox {
  id: string
  className: string
  points: Point[]
}

interface UploadedImage {
  name: string
  path: string
}

function slugify(value: string): string {
  return (
    value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "model"
  )
}

export function DetectTrainWizard({
  onDone,
  onCancel,
  onStarted,
}: {
  onDone: () => void
  onCancel: () => void
  onStarted: (workspaceId: string) => void
}) {
  const [step, setStep] = useState(0)
  const [workspaceId, setWorkspaceId] = useState<string | null>(null)
  const [pendingFiles, setPendingFiles] = useState<File[]>([])
  const [uploading, setUploading] = useState(false)
  const [uploadedImages, setUploadedImages] = useState<UploadedImage[] | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [weights, setWeights] = useState<detectApi.Weight[]>([])
  const [baseWeightName, setBaseWeightName] = useState(DEFAULT_MODEL)

  const [labels, setLabels] = useState<Record<string, AnnotationBox[]>>({})
  const [classNames, setClassNames] = useState<string[]>([DEFAULT_CLASS])
  const [activeClass, setActiveClass] = useState(DEFAULT_CLASS)
  const [newClass, setNewClass] = useState("")
  const [selectedBoxId, setSelectedBoxId] = useState<string | null>(null)
  const [imageIndex, setImageIndex] = useState(0)
  const [prefilling, setPrefilling] = useState(false)

  const [modelName, setModelName] = useState("")
  const [epochs, setEpochs] = useState(150)
  const [imgsz, setImgsz] = useState(1024)
  const [batch, setBatch] = useState(4)
  const [patience, setPatience] = useState(30)
  const [valSplit, setValSplit] = useState(0.2)
  const [starting, setStarting] = useState(false)

  useEffect(() => {
    detectApi
      .listWeights()
      .then(setWeights)
      .catch((err: Error) => toast.error(err.message))
  }, [])

  const resolvedBaseWeightsPath =
    baseWeightName === DEFAULT_MODEL || !workspaceId
      ? undefined
      : (() => {
          const weight = weights.find((w) => w.name === baseWeightName)
          return weight
            ? (detectApi.resolveWeightsPathForWorkspace(weight, workspaceId) ?? undefined)
            : undefined
        })()

  const handleUploadImages = async () => {
    if (!workspaceId || pendingFiles.length === 0) return
    setUploading(true)
    try {
      const entries = await api.uploadFiles(workspaceId, pendingFiles, IMAGES_DIR)
      setUploadedImages(
        entries.map((entry) => ({ name: entry.path.split("/").pop()!, path: entry.path })),
      )
      setStep(1)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setUploading(false)
    }
  }

  const totalBoxes = Object.values(labels).reduce((sum, boxes) => sum + boxes.length, 0)

  const handlePrefill = async () => {
    if (!workspaceId || !uploadedImages) return
    setPrefilling(true)
    try {
      const response = await detectApi.runDetect({
        workspaceId,
        imagesDir: IMAGES_DIR,
        filenames: uploadedImages.map((i) => i.name),
        weightsPath: resolvedBaseWeightsPath,
      })
      if (!response.success) {
        toast.error(response.error ?? "Detection failed")
        return
      }
      setLabels((prev) => {
        const next = { ...prev }
        for (const image of response.output.images) {
          if (next[image.image_name]?.length) continue // don't clobber manual edits
          if (image.boxes.length === 0) continue
          next[image.image_name] = image.boxes.map((box) => ({
            id: crypto.randomUUID(),
            className: activeClass,
            points: box.polygon,
          }))
        }
        return next
      })
      toast.success("Prefilled boxes from detection — review and correct them")
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setPrefilling(false)
    }
  }

  const handleAddClass = () => {
    const name = newClass.trim()
    if (!name || classNames.includes(name)) return
    setClassNames((prev) => [...prev, name])
    setActiveClass(name)
    setNewClass("")
  }

  const handleChipClick = (name: string) => {
    if (selectedBoxId && uploadedImages) {
      const currentName = uploadedImages[imageIndex].name
      setLabels((prev) => ({
        ...prev,
        [currentName]: (prev[currentName] ?? []).map((b) =>
          b.id === selectedBoxId ? { ...b, className: name } : b,
        ),
      }))
    } else {
      setActiveClass(name)
    }
  }

  const handleDraw = (points: Point[]) => {
    if (!uploadedImages) return
    const currentName = uploadedImages[imageIndex].name
    const box: AnnotationBox = { id: crypto.randomUUID(), className: activeClass, points }
    setLabels((prev) => ({ ...prev, [currentName]: [...(prev[currentName] ?? []), box] }))
  }

  const handleDeleteBox = (id: string) => {
    if (!uploadedImages) return
    const currentName = uploadedImages[imageIndex].name
    setLabels((prev) => ({
      ...prev,
      [currentName]: (prev[currentName] ?? []).filter((b) => b.id !== id),
    }))
    setSelectedBoxId(null)
  }

  const handleStart = async () => {
    if (!workspaceId || !uploadedImages) return
    if (!modelName.trim()) {
      toast.error("Give the trained model a name")
      return
    }
    setStarting(true)
    try {
      const labelsPayload: Record<string, { polygon: Point[]; class: number }[]> = {}
      for (const image of uploadedImages) {
        labelsPayload[image.name] = (labels[image.name] ?? []).map((box) => ({
          polygon: box.points,
          class: classNames.indexOf(box.className),
        }))
      }
      const labelsFile = new File(
        [JSON.stringify(labelsPayload)],
        "labels.json",
        { type: "application/json" },
      )
      const [labelsEntry] = await api.uploadFiles(workspaceId, [labelsFile], "train_meta")
      const weightsOutPath = `weights/${slugify(modelName)}.pt`

      const response = await detectApi.startTrain({
        workspaceId,
        imagesDir: IMAGES_DIR,
        labelsPath: labelsEntry.path,
        weightsOutPath,
        baseWeightsPath: resolvedBaseWeightsPath,
        classNames,
        epochs,
        imgsz,
        batch,
        patience,
        valSplit,
      })
      toast.success(`Training started (job ${response.output.job_id})`)
      onStarted(workspaceId)
      onDone()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setStarting(false)
    }
  }

  const currentImage = uploadedImages?.[imageIndex]
  const currentBoxes = currentImage ? (labels[currentImage.name] ?? []) : []
  const canvasBoxes: CanvasBox[] = currentBoxes.map((box) => ({
    id: box.id,
    points: box.points,
    label: box.className,
    colorClass: classColorClass(classNames.indexOf(box.className)),
  }))

  return (
    <div className="flex flex-col gap-4 rounded-xl border p-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-medium">Train a new model</h2>
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Step {step + 1} of 4:{" "}
        {["Upload images", "Base model", "Annotate", "Configure & start"][step]}
      </p>

      {step === 0 &&
        (uploadedImages ? (
          <div className="flex flex-col gap-3">
            <p className="text-sm">
              {uploadedImages.length} image(s) uploaded to workspace{" "}
              <span className="font-mono">{workspaceId}</span>.
            </p>
            <div className="flex gap-2">
              <Button onClick={() => setStep(1)}>Continue</Button>
              <Button
                variant="ghost"
                onClick={() => {
                  setUploadedImages(null)
                  setPendingFiles([])
                }}
              >
                Replace images
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Workspace</span>
              <WorkspacePicker value={workspaceId} onChange={setWorkspaceId} />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Raw training images</span>
              <div className="flex flex-wrap items-center gap-2">
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/*"
                  multiple
                  className="hidden"
                  onChange={(e) => setPendingFiles(Array.from(e.target.files ?? []))}
                />
                <Button variant="outline" onClick={() => fileInputRef.current?.click()}>
                  Choose files
                </Button>
                <span className="text-sm text-muted-foreground">
                  {pendingFiles.length === 0
                    ? "No files selected"
                    : `${pendingFiles.length} file(s) selected`}
                </span>
              </div>
            </div>
            <Button
              onClick={handleUploadImages}
              disabled={!workspaceId || pendingFiles.length === 0 || uploading}
              className="self-start"
            >
              {uploading ? "Uploading…" : "Upload & continue"}
            </Button>
          </div>
        ))}

      {step === 1 && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Base model</span>
            <Select value={baseWeightName} onValueChange={(v) => v && setBaseWeightName(v)}>
              <SelectTrigger className="max-w-sm">
                <SelectValue>
                  {(value: string) => (value === DEFAULT_MODEL ? "Default (server model)" : value)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DEFAULT_MODEL}>Default (server model)</SelectItem>
                {weights.map((w) => {
                  const usable = workspaceId
                    ? detectApi.resolveWeightsPathForWorkspace(w, workspaceId) !== null
                    : true
                  return (
                    <SelectItem key={w.name} value={w.name} disabled={!usable}>
                      {w.name} — {w.description}
                      {!usable && " (not available in this workspace)"}
                    </SelectItem>
                  )
                })}
              </SelectContent>
            </Select>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setStep(0)}>
              Back
            </Button>
            <Button onClick={() => setStep(2)}>Next</Button>
          </div>
        </div>
      )}

      {step === 2 && uploadedImages && currentImage && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">Classes:</span>
            {classNames.map((name) => (
              <button
                key={name}
                type="button"
                onClick={() => handleChipClick(name)}
                className={`rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors ${
                  name === activeClass && !selectedBoxId
                    ? "border-transparent bg-primary text-primary-foreground"
                    : "hover:bg-muted"
                }`}
              >
                {name}
              </button>
            ))}
            <Input
              placeholder="New class…"
              value={newClass}
              onChange={(e) => setNewClass(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleAddClass()}
              className="h-6 w-28"
            />
            <Button size="xs" variant="outline" onClick={handleAddClass}>
              Add
            </Button>
            <Button size="xs" variant="outline" onClick={handlePrefill} disabled={prefilling}>
              {prefilling ? "Detecting…" : "Prefill via detection"}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            {selectedBoxId
              ? "Click a class above to retag the selected box, or the ✕ to delete it."
              : "Drag on the image to draw a box for the active class. Click a box to select it."}
          </p>

          <BoxCanvas
            imageUrl={api.fileDownloadUrl(workspaceId!, currentImage.path)}
            boxes={canvasBoxes}
            selectedId={selectedBoxId}
            onSelect={setSelectedBoxId}
            onDraw={handleDraw}
            onDelete={handleDeleteBox}
          />

          <div className="flex items-center justify-between">
            <Button
              size="sm"
              variant="outline"
              disabled={imageIndex === 0}
              onClick={() => {
                setImageIndex((i) => i - 1)
                setSelectedBoxId(null)
              }}
            >
              ← Previous
            </Button>
            <span className="text-sm text-muted-foreground">
              Image {imageIndex + 1} / {uploadedImages.length} — {currentBoxes.length} box(es) —{" "}
              {totalBoxes} total
            </span>
            <Button
              size="sm"
              variant="outline"
              disabled={imageIndex === uploadedImages.length - 1}
              onClick={() => {
                setImageIndex((i) => i + 1)
                setSelectedBoxId(null)
              }}
            >
              Next →
            </Button>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setStep(1)}>
              Back
            </Button>
            <Button onClick={() => setStep(3)} disabled={totalBoxes === 0}>
              Next
            </Button>
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium">Model name</span>
            <Input
              placeholder="e.g. boxes-v2"
              value={modelName}
              onChange={(e) => setModelName(e.target.value)}
              className="max-w-sm"
            />
          </div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Epochs</span>
              <Input
                type="number"
                value={epochs}
                onChange={(e) => setEpochs(Number(e.target.value))}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Image size</span>
              <Input
                type="number"
                value={imgsz}
                onChange={(e) => setImgsz(Number(e.target.value))}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Batch</span>
              <Input
                type="number"
                value={batch}
                onChange={(e) => setBatch(Number(e.target.value))}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Patience</span>
              <Input
                type="number"
                value={patience}
                onChange={(e) => setPatience(Number(e.target.value))}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm font-medium">Val split</span>
              <Input
                type="number"
                min={0}
                max={1}
                step={0.05}
                value={valSplit}
                onChange={(e) => setValSplit(Number(e.target.value))}
              />
            </div>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setStep(2)}>
              Back
            </Button>
            <Button onClick={handleStart} disabled={starting}>
              {starting ? "Starting…" : "Start training"}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
