import { useState } from "react"
import { DetectTry } from "@/components/detect-try"
import { DetectWeights } from "@/components/detect-weights"
import { TopNav, type View } from "@/components/top-nav"

function App() {
  const [view, setView] = useState<View>("try")

  return (
    <div className="flex min-h-svh flex-col">
      <TopNav active={view} onNavigate={setView} />
      <main className="flex-1 overflow-y-auto">
        {view === "try" ? <DetectTry /> : <DetectWeights />}
      </main>
    </div>
  )
}

export default App
