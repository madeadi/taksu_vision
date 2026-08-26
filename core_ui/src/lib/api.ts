// Typed client for core's workspace lifecycle API (see core/README.md).
const API_BASE_URL = (
  (import.meta.env.VITE_CORE_API_URL as string | undefined) ??
  "http://localhost:8824"
).replace(/\/$/, "")

export interface WorkspaceSummary {
  workspace_id: string
  created_at: string
  expires_at: string
}

export interface FileEntry {
  path: string
  size: number
}

export interface WorkspaceDetail extends WorkspaceSummary {
  files: FileEntry[]
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
  return request<WorkspaceSummary>("/workspaces", { method: "POST" })
}

export function getWorkspace(id: string): Promise<WorkspaceDetail> {
  return request<WorkspaceDetail>(`/workspaces/${encodeURIComponent(id)}`)
}

export function deleteWorkspace(id: string): Promise<void> {
  return request<unknown>(`/workspaces/${encodeURIComponent(id)}`, {
    method: "DELETE",
  }).then(() => undefined)
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

export function archiveDownloadUrl(id: string, dir?: string): string {
  const query = dir ? `?dir=${encodeURIComponent(dir)}` : ""
  return `${API_BASE_URL}/workspaces/${encodeURIComponent(id)}/archive${query}`
}

export interface Service {
  id: string
  name: string
  url: string
  online: boolean
  last_seen_at?: string
}

export function listServices(): Promise<Service[]> {
  return request<{ services: Service[] }>("/services").then((r) => r.services)
}

export function createService(name: string, url: string): Promise<Service> {
  return request<Service>("/services", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, url }),
  })
}

export function deleteService(id: string): Promise<void> {
  return request<unknown>(`/services/${encodeURIComponent(id)}`, {
    method: "DELETE",
  }).then(() => undefined)
}
