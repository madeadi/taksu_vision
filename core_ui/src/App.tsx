import { useState } from "react"
import { ServiceFrame } from "@/components/service-frame"
import { ServiceList } from "@/components/service-list"
import { Sidebar, type View } from "@/components/sidebar"
import { WorkspaceDetail } from "@/components/workspace-detail"
import { WorkspaceList } from "@/components/workspace-list"
import type { Service } from "@/lib/api"

function App() {
  const [view, setView] = useState<View>("workspaces")
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<
    string | null
  >(null)
  const [openService, setOpenService] = useState<Service | null>(null)

  const handleNavigate = (next: View) => {
    setSelectedWorkspaceId(null)
    setOpenService(null)
    setView(next)
  }

  const showingFrame = view === "services" && !!openService?.web_url

  return (
    <div className="flex min-h-svh">
      <Sidebar active={view} onNavigate={handleNavigate} />
      <main
        className={
          showingFrame ? "flex-1 overflow-hidden" : "flex-1 overflow-y-auto"
        }
      >
        {view === "services" ? (
          showingFrame ? (
            <ServiceFrame
              name={openService!.name}
              webUrl={openService!.web_url!}
              onBack={() => setOpenService(null)}
            />
          ) : (
            <ServiceList onOpen={setOpenService} />
          )
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
