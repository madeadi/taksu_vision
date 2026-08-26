// Typed client for the subset of core's workspace lifecycle API (see
// core/README.md) this app needs: picking/creating a workspace and
// uploading/downloading files into it.
const API_BASE_URL = (
  (import.meta.env.VITE_CORE_API_URL as string | undefined) ??
  "http://localhost:8824"
).replace(/\/$/, "")

export interface WorkspaceSummary {
  workspace_id: string
  name: string
  created_at: string
  expires_at: string
}

export interface FileEntry {
  path: string
  size: number
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, init)
  if (!res.ok) {
    const body = await res.text().catch(() => "")
    throw new Error(`${init?.method ?? "GET"} ${path} failed: ${res.status} ${body}`)
  }
  return res.json() as Promise<T>
}

export function listWorkspaces(): Promise<WorkspaceSummary[]> {
  return request<{ workspaces: WorkspaceSummary[] }>("/workspaces").then(
    (r) => r.workspaces,
  )
}

export function createWorkspace(): Promise<WorkspaceSummary> {
  return request<WorkspaceSummary>("/workspaces", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  })
}

export async function uploadFiles(
  id: string,
  files: File[],
  dir?: string,
): Promise<FileEntry[]> {
  const form = new FormData()
  for (const file of files) form.append("files", file)
  const query = dir ? `?dir=${encodeURIComponent(dir)}` : ""
  const result = await request<{ files: FileEntry[] }>(
    `/workspaces/${encodeURIComponent(id)}/files${query}`,
    { method: "POST", body: form },
  )
  return result.files
}

export function fileDownloadUrl(id: string, path: string): string {
  return `${API_BASE_URL}/workspaces/${encodeURIComponent(id)}/files?path=${encodeURIComponent(path)}`
}
