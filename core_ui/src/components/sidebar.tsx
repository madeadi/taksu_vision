import { cn } from "@/lib/utils"

export type View = "workspaces" | "services"

const NAV_ITEMS: { id: View; label: string }[] = [
  { id: "workspaces", label: "Workspaces" },
  { id: "services", label: "Services" },
]

export function Sidebar({
  active,
  onNavigate,
}: {
  active: View
  onNavigate: (view: View) => void
}) {
  return (
    <nav className="flex w-52 shrink-0 flex-col gap-1 border-r p-4">
      <span className="mb-3 px-2 text-sm font-semibold">Taksu Vision</span>
      {NAV_ITEMS.map((item) => (
        <button
          key={item.id}
          type="button"
          onClick={() => onNavigate(item.id)}
          className={cn(
            "rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted",
            active === item.id && "bg-muted font-medium",
          )}
        >
          {item.label}
        </button>
      ))}
    </nav>
  )
}
