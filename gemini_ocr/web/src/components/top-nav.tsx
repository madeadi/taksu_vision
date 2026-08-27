import { cn } from "@/lib/utils"

export type View = "try" | "config" | "logs"

const NAV_ITEMS: { id: View; label: string }[] = [
  { id: "try", label: "Try" },
  { id: "config", label: "Config" },
  { id: "logs", label: "Logs" },
]

export function TopNav({
  active,
  onNavigate,
}: {
  active: View
  onNavigate: (view: View) => void
}) {
  return (
    <nav className="flex items-center gap-4 border-b px-4 py-3">
      <span className="text-sm font-semibold">Gemini OCR</span>
      <div className="flex items-center gap-1">
        {NAV_ITEMS.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => onNavigate(item.id)}
            className={cn(
              "rounded-md px-3 py-1.5 text-sm transition-colors hover:bg-muted",
              active === item.id && "bg-muted font-medium",
            )}
          >
            {item.label}
          </button>
        ))}
      </div>
    </nav>
  )
}
