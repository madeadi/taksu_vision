import { Button } from "@/components/ui/button"

export function ServiceFrame({
  name,
  webUrl,
  onBack,
}: {
  name: string
  webUrl: string
  onBack: () => void
}) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b p-2">
        <Button variant="ghost" size="sm" onClick={onBack}>
          ← Back
        </Button>
        <span className="text-sm font-medium">{name}</span>
      </div>
      <iframe
        src={webUrl}
        title={name}
        className="min-h-0 flex-1 border-0"
      />
    </div>
  )
}
