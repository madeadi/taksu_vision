import { useState } from "react"
import { GeminiConfig } from "@/components/gemini-config"
import { GeminiLogs } from "@/components/gemini-logs"
import { GeminiTry } from "@/components/gemini-try"
import { TopNav, type View } from "@/components/top-nav"

function App() {
  const [view, setView] = useState<View>("try")

  return (
    <div className="flex min-h-svh flex-col">
      <TopNav active={view} onNavigate={setView} />
      <main className="flex-1 overflow-y-auto">
        {view === "try" && <GeminiTry />}
        {view === "config" && <GeminiConfig />}
        {view === "logs" && <GeminiLogs />}
      </main>
    </div>
  )
}

export default App
