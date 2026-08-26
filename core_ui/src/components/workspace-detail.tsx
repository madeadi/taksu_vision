import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import * as api from "@/lib/api"

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ["KB", "MB", "GB", "TB"]
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

export function WorkspaceDetail({
  id,
  onBack,
  onDeleted,
}: {
  id: string
  onBack: () => void
  onDeleted: () => void
}) {
  const [workspace, setWorkspace] = useState<api.WorkspaceDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const refresh = () => {
    setLoading(true)
    api
      .getWorkspace(id)
      .then(setWorkspace)
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(refresh, [id])

  const handleUpload = async (files: FileList | null) => {
    if (!files || files.length === 0) return
    setUploading(true)
    try {
      await api.uploadFiles(id, Array.from(files))
      toast.success(`Uploaded ${files.length} file(s)`)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  const handleDelete = async () => {
    try {
      await api.deleteWorkspace(id)
      toast.success(`Deleted workspace ${id}`)
      onDeleted()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-8">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onBack}>
          ← Back
        </Button>
      </div>

      {loading || !workspace ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle className="font-mono text-base">
                {workspace.workspace_id}
              </CardTitle>
              <CardDescription className="flex flex-wrap gap-x-6 gap-y-1 pt-1">
                <span>
                  Created {new Date(workspace.created_at).toLocaleString()}
                </span>
                <span>
                  Expires {new Date(workspace.expires_at).toLocaleString()}
                </span>
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap items-center gap-2">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => handleUpload(e.target.files)}
              />
              <Button
                onClick={() => fileInputRef.current?.click()}
                disabled={uploading}
              >
                {uploading ? "Uploading…" : "Upload files"}
              </Button>
              <Button
                variant="outline"
                render={<a href={api.archiveDownloadUrl(workspace.workspace_id)} />}
              >
                Download all (.zip)
              </Button>
              <AlertDialog>
                <AlertDialogTrigger
                  render={<Button variant="destructive" className="ml-auto" />}
                >
                  Delete workspace
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Delete workspace?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This permanently deletes {workspace.workspace_id} and
                      every file in it. This can't be undone.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction onClick={handleDelete}>
                      Delete
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </CardContent>
          </Card>

          <div className="flex items-center gap-2">
            <h2 className="text-lg font-medium">Files</h2>
            <Badge variant="secondary">{workspace.files.length}</Badge>
          </div>

          {workspace.files.length === 0 ? (
            <p className="text-muted-foreground py-8 text-center text-sm">
              No files uploaded yet.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Path</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead className="w-0" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {workspace.files.map((file) => (
                  <TableRow key={file.path}>
                    <TableCell className="font-mono text-sm">
                      {file.path}
                    </TableCell>
                    <TableCell>{formatBytes(file.size)}</TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        render={
                          <a
                            href={api.fileDownloadUrl(
                              workspace.workspace_id,
                              file.path,
                            )}
                          />
                        }
                      >
                        Download
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </>
      )}
    </div>
  )
}
