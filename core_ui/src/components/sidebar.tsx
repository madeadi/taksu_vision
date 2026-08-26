import { cn } from "@/lib/utils"

export type View = "workspaces" | "services" | "detect-try" | "detect-weights"

const NAV_GROUPS: { label?: string; items: { id: View; label: string }[] }[] = [
  {
    items: [
      { id: "workspaces", label: "Workspaces" },
      { id: "services", label: "Services" },
    ],
  },
  {
    label: "Detect Boxes",
    items: [
      { id: "detect-try", label: "Try" },
      { id: "detect-weights", label: "Weights" },
    ],
  },
]

export function Sidebar({
  active,
  onNavigate,
}: {
  active: View
  onNavigate: (view: View) => void
}) {
  return (
    <nav className="flex w-52 shrink-0 flex-col gap-4 border-r p-4">
      <span className="px-2 text-sm font-semibold">Taksu Vision</span>
      {NAV_GROUPS.map((group, i) => (
        <div key={i} className="flex flex-col gap-1">
          {group.label && (
            <span className="px-2 text-xs font-medium text-muted-foreground">{group.label}</span>
          )}
          {group.items.map((item) => (
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
        </div>
      ))}
    </nav>
  )
}
