import { useEffect, useState } from "react"
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
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { DetectTrainWizard } from "@/components/detect-train-wizard"
import { WorkspacePicker } from "@/components/workspace-picker"
import * as detectApi from "@/lib/detect-boxes-api"

// A job still in flight is polled at this interval so status/metrics show up
// without the user refreshing.
const JOB_POLL_INTERVAL_MS = 5_000

function statusVariant(status: detectApi.TrainJob["status"]) {
  switch (status) {
    case "succeeded":
      return "default" as const
    case "failed":
      return "destructive" as const
    default:
      return "secondary" as const
  }
}

function JobsPanel({ workspaceId }: { workspaceId: string | null }) {
  const [jobs, setJobs] = useState<detectApi.TrainJob[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!workspaceId) {
      setJobs([])
      return
    }
    let cancelled = false
    const refresh = () => {
      setLoading(true)
      detectApi
        .listTrainJobs(workspaceId)
        .then((result) => !cancelled && setJobs(result))
        .catch((err: Error) => toast.error(err.message))
        .finally(() => !cancelled && setLoading(false))
    }
    refresh()
    const interval = setInterval(refresh, JOB_POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [workspaceId])

  if (!workspaceId) {
    return (
      <p className="text-sm text-muted-foreground">
        Select a workspace to see its training jobs.
      </p>
    )
  }
  if (loading && jobs.length === 0) return <Skeleton className="h-10 w-full" />
  if (jobs.length === 0) {
    return <p className="text-sm text-muted-foreground">No training jobs in this workspace yet.</p>
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Job</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Started</TableHead>
          <TableHead>Metrics / error</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {jobs.map((job) => (
          <TableRow key={job.job_id}>
            <TableCell className="font-mono text-xs">{job.job_id.slice(0, 8)}</TableCell>
            <TableCell>
              <Badge variant={statusVariant(job.status)}>{job.status}</Badge>
            </TableCell>
            <TableCell>{job.start_at ? new Date(job.start_at).toLocaleString() : "—"}</TableCell>
            <TableCell className="max-w-xs truncate text-xs text-muted-foreground">
              {job.status === "failed"
                ? job.error
                : job.metrics
                  ? Object.entries(job.metrics)
                      .map(([k, v]) => `${k}=${v}`)
                      .join(", ")
                  : "—"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function DetectWeights() {
  const [weights, setWeights] = useState<detectApi.Weight[]>([])
  const [loading, setLoading] = useState(true)
  const [training, setTraining] = useState(false)
  const [jobsWorkspaceId, setJobsWorkspaceId] = useState<string | null>(null)

  const refresh = () => {
    setLoading(true)
    detectApi
      .listWeights()
      .then(setWeights)
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(refresh, [])

  const handleDelete = async (name: string) => {
    try {
      await detectApi.deleteWeight(name)
      toast.success(`Removed ${name}`)
      refresh()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Weights</h1>
        {!training && <Button onClick={() => setTraining(true)}>Train new model</Button>}
      </div>

      {training && (
        <DetectTrainWizard
          onCancel={() => setTraining(false)}
          onStarted={(workspaceId) => setJobsWorkspaceId(workspaceId)}
          onDone={() => {
            setTraining(false)
            refresh()
          }}
        />
      )}

      <div className="flex flex-col gap-3">
        <h2 className="text-lg font-medium">Registry</h2>
        {loading ? (
          <Skeleton className="h-10 w-full" />
        ) : weights.length === 0 ? (
          <p className="text-muted-foreground py-8 text-center text-sm">
            No trained weights yet. Train a model to add one.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Path</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {weights.map((w) => (
                <TableRow key={w.name}>
                  <TableCell className="font-medium">{w.name}</TableCell>
                  <TableCell>{w.description}</TableCell>
                  <TableCell className="max-w-xs truncate font-mono text-xs">{w.path}</TableCell>
                  <TableCell>
                    <AlertDialog>
                      <AlertDialogTrigger render={<Button variant="ghost" size="sm" />}>
                        Remove
                      </AlertDialogTrigger>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>Remove {w.name}?</AlertDialogTitle>
                          <AlertDialogDescription>
                            This removes the registry entry only — the weights file on disk is
                            left in place.
                          </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>Cancel</AlertDialogCancel>
                          <AlertDialogAction onClick={() => handleDelete(w.name)}>
                            Remove
                          </AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <div className="flex flex-col gap-3">
        <h2 className="text-lg font-medium">Training jobs</h2>
        <WorkspacePicker value={jobsWorkspaceId} onChange={setJobsWorkspaceId} />
        <JobsPanel workspaceId={jobsWorkspaceId} />
      </div>
    </div>
  )
}
