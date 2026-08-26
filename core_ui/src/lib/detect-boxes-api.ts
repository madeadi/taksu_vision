// Typed client for detect_boxes's HTTP API (see detect_boxes/README.md):
// run detection over images already uploaded to a core workspace, manage
// the trained-weights registry, and start/track training jobs.
const API_BASE_URL = (
  (import.meta.env.VITE_DETECT_BOXES_API_URL as string | undefined) ??
  "http://localhost:8821"
).replace(/\/$/, "")

export type Point = [number, number]

export interface DetectBox {
  box_index: number
  yolo_conf: number
  xyxy: [number, number, number, number]
  polygon: Point[]
  is_obb: boolean
}

export interface DetectImageResult {
  image_name: string
  boxes: DetectBox[]
  skip_reason: string | null
}

export interface DetectResponse {
  output: { images: DetectImageResult[]; n_skipped: number }
  success: boolean
  error?: string
}

export interface Weight {
  name: string
  description: string
  path: string
}

export interface TrainJob {
  job_id: string
  status: "pending" | "running" | "succeeded" | "failed"
  start_at?: string
  finished_at?: string
  pid?: number
  metrics?: Record<string, number>
  weights_out?: string
  error?: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, init)
  if (!res.ok) {
    const body = await res.text().catch(() => "")
    throw new Error(`${init?.method ?? "GET"} ${path} failed: ${res.status} ${body}`)
  }
  return res.json() as Promise<T>
}

export function listWeights(): Promise<Weight[]> {
  return request<{ weights: Weight[] }>("/weights").then((r) => r.weights)
}

export function deleteWeight(name: string): Promise<void> {
  return request<unknown>(`/weights/${encodeURIComponent(name)}`, {
    method: "DELETE",
  }).then(() => undefined)
}

/**
 * `/tasks` and `/train` only accept weights/base-weights paths relative to
 * the target workspace's files dir, but the weights registry stores each
 * entry's absolute filesystem path (see detect_boxes/weights.py) with no
 * record of which workspace it was trained in. A registered weight is only
 * usable in a given workspace if it happens to physically live under that
 * workspace's own files dir — which is the common case when training and
 * trying/retraining happen in the same workspace. Returns null when the
 * weight isn't reachable from `workspaceId`.
 */
export function resolveWeightsPathForWorkspace(
  weight: Weight,
  workspaceId: string,
): string | null {
  const marker = `/${workspaceId}/files/`
  const idx = weight.path.indexOf(marker)
  return idx === -1 ? null : weight.path.slice(idx + marker.length)
}

export interface RunDetectParams {
  workspaceId: string
  imagesDir: string
  filenames?: string[]
  confidence?: number
  blurThreshold?: number
  weightsPath?: string
}

export function runDetect(params: RunDetectParams): Promise<DetectResponse> {
  const query = new URLSearchParams({
    workspace_id: params.workspaceId,
    images_dir: params.imagesDir,
    confidence: String(params.confidence ?? 0.25),
    blur_threshold: String(params.blurThreshold ?? 0),
  })
  if (params.weightsPath) query.set("weights_path", params.weightsPath)
  return request<DetectResponse>(`/tasks?${query}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ filenames: params.filenames ?? null }),
  })
}

export interface StartTrainParams {
  workspaceId: string
  imagesDir: string
  labelsPath: string
  weightsOutPath: string
  baseWeightsPath?: string
  classNames?: string[]
  epochs?: number
  imgsz?: number
  batch?: number
  patience?: number
  valSplit?: number
}

export interface StartTrainResponse {
  output: { job_id: string; status: string; class_names: string[] }
  success: boolean
}

export function startTrain(params: StartTrainParams): Promise<StartTrainResponse> {
  const query = new URLSearchParams({
    workspace_id: params.workspaceId,
    images_dir: params.imagesDir,
    labels_path: params.labelsPath,
    weights_out_path: params.weightsOutPath,
    epochs: String(params.epochs ?? 150),
    imgsz: String(params.imgsz ?? 1024),
    batch: String(params.batch ?? 4),
    patience: String(params.patience ?? 30),
    val_split: String(params.valSplit ?? 0.2),
  })
  if (params.baseWeightsPath) query.set("base_weights_path", params.baseWeightsPath)
  return request<StartTrainResponse>(`/train?${query}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ class_names: params.classNames ?? null }),
  })
}

export function listTrainJobs(workspaceId: string): Promise<TrainJob[]> {
  return request<{ output: { jobs: TrainJob[] } }>(
    `/train?workspace_id=${encodeURIComponent(workspaceId)}`,
  ).then((r) => r.output.jobs)
}
