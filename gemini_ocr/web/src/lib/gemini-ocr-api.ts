// Typed client for gemini_ocr's own API (see ../../../README.md): running
// extraction tasks, checking health, viewing/refreshing config, and
// reading recent logs.
const API_BASE_URL = (
  (import.meta.env.VITE_GEMINI_OCR_API_URL as string | undefined) ??
  "http://localhost:8820"
).replace(/\/$/, "")

export interface OCRResult {
  image: string
  image_name: string
  detected_text: string | null
  matched_patterns: string[] | null
  formatted_output: Record<string, unknown> | null
  input_tokens: number | null
  output_tokens: number | null
  cost_usd: number | null
  error: string | null
}

export interface TaskInput {
  images_dir: string
  filenames: string[] | null
  json_output_path: string | null
  model: string
  instruction: string
  formatted_output: Record<string, unknown> | null
  patterns: string[] | null
  remove_patterns: string[] | null
  max_concurrency: number
}

export interface TaskOutput {
  results: OCRResult[]
  n_processed: number
  n_failed: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number | null
}

export interface TaskResult {
  input: TaskInput
  output: TaskOutput
  start_at: string
  finished_at: string
  success: boolean
  error?: string
}

export interface HealthResponse {
  status: string
  model: string
  save_results_dir: string
}

export interface RefreshConfigResponse {
  success: boolean
  model: string
  max_concurrency: number
  workspace_root: string
  save_results_dir: string
}

export interface LogsResponse {
  lines: string[]
}

export interface RunOcrParams {
  workspaceId: string
  imagesDir: string
  filenames?: string[]
  model?: string
  maxConcurrency?: number
  jsonOutputPath?: string
  instruction?: string
  formattedOutput?: Record<string, unknown>
  patterns?: string[]
  removePatterns?: string[]
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, init)
  if (!res.ok) {
    const body = await res.text().catch(() => "")
    throw new Error(`${init?.method ?? "GET"} ${path} failed: ${res.status} ${body}`)
  }
  return res.json() as Promise<T>
}

export function getHealth(): Promise<HealthResponse> {
  return request<HealthResponse>("/health")
}

export function refreshConfig(): Promise<RefreshConfigResponse> {
  return request<RefreshConfigResponse>("/config/refresh", { method: "POST" })
}

export function getLogs(): Promise<LogsResponse> {
  return request<LogsResponse>("/logs")
}

export function runOcr(params: RunOcrParams): Promise<TaskResult> {
  const query = new URLSearchParams({
    workspace_id: params.workspaceId,
    images_dir: params.imagesDir,
  })
  if (params.model) query.set("model", params.model)
  if (params.maxConcurrency) query.set("max_concurrency", String(params.maxConcurrency))
  if (params.jsonOutputPath) query.set("json_output_path", params.jsonOutputPath)

  return request<TaskResult>(`/tasks?${query}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      filenames: params.filenames,
      instruction: params.instruction || undefined,
      formatted_output: params.formattedOutput,
      patterns: params.patterns,
      remove_patterns: params.removePatterns,
    }),
  })
}
