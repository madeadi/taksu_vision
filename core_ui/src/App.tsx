import { useState } from "react"
import { DetectTry } from "@/components/detect-try"
import { DetectWeights } from "@/components/detect-weights"
import { ServiceList } from "@/components/service-list"
import { Sidebar, type View } from "@/components/sidebar"
import { WorkspaceDetail } from "@/components/workspace-detail"
import { WorkspaceList } from "@/components/workspace-list"

function App() {
  const [view, setView] = useState<View>("workspaces")
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<
    string | null
  >(null)

  const handleNavigate = (next: View) => {
    setSelectedWorkspaceId(null)
    setView(next)
  }

  return (
    <div className="flex min-h-svh">
      <Sidebar active={view} onNavigate={handleNavigate} />
      <main className="flex-1 overflow-y-auto">
        {view === "services" ? (
          <ServiceList />
        ) : view === "detect-try" ? (
          <DetectTry />
        ) : view === "detect-weights" ? (
          <DetectWeights />
        ) : selectedWorkspaceId ? (
          <WorkspaceDetail
            id={selectedWorkspaceId}
            onBack={() => setSelectedWorkspaceId(null)}
            onDeleted={() => setSelectedWorkspaceId(null)}
          />
        ) : (
          <WorkspaceList onSelect={setSelectedWorkspaceId} />
        )}
      </main>
    </div>
  )
}

export default App
