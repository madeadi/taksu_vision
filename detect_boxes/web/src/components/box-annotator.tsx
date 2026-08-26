import { useEffect, useRef, useState } from "react"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import type { Point } from "@/lib/detect-boxes-api"

// Cycled by class index so the same class always gets the same color across
// images and across the annotate/try views.
const PALETTE = [
  "stroke-amber-500 fill-amber-500/15",
  "stroke-sky-500 fill-sky-500/15",
  "stroke-emerald-500 fill-emerald-500/15",
  "stroke-fuchsia-500 fill-fuchsia-500/15",
  "stroke-rose-500 fill-rose-500/15",
  "stroke-lime-500 fill-lime-500/15",
]

export function classColorClass(index: number): string {
  return PALETTE[((index % PALETTE.length) + PALETTE.length) % PALETTE.length]
}

export interface CanvasBox {
  id: string
  points: Point[]
  label: string
  colorClass?: string
}

export function BoxCanvas({
  imageUrl,
  boxes,
  readOnly = false,
  selectedId = null,
  onSelect,
  onDraw,
  onDelete,
}: {
  imageUrl: string
  boxes: CanvasBox[]
  readOnly?: boolean
  selectedId?: string | null
  onSelect?: (id: string | null) => void
  onDraw?: (points: Point[]) => void
  onDelete?: (id: string) => void
}) {
  const [size, setSize] = useState<[number, number] | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const [draft, setDraft] = useState<{ start: Point; end: Point } | null>(null)

  useEffect(() => {
    setSize(null)
    const img = new Image()
    img.onload = () => setSize([img.naturalWidth, img.naturalHeight])
    img.src = imageUrl
    return () => {
      img.onload = null
    }
  }, [imageUrl])

  const toImageCoords = (e: { clientX: number; clientY: number }): Point => {
    const svg = svgRef.current
    if (!svg || !size) return [0, 0]
    const rect = svg.getBoundingClientRect()
    return [
      ((e.clientX - rect.left) / rect.width) * size[0],
      ((e.clientY - rect.top) / rect.height) * size[1],
    ]
  }

  if (!size) return <Skeleton className="aspect-video w-full" />
  const [w, h] = size
  const strokeWidth = Math.max(w, h) / 300

  const handlePointerDown = (e: React.PointerEvent<SVGSVGElement>) => {
    if (readOnly || !onDraw) return
    // A click starting on an existing box is stopped at its own <g> handler
    // and never reaches here, so anything that does reach here is a
    // background click (typically hitting the <image>, not the <svg> itself).
    onSelect?.(null)
    const point = toImageCoords(e)
    setDraft({ start: point, end: point })
    svgRef.current?.setPointerCapture(e.pointerId)
  }
  const handlePointerMove = (e: React.PointerEvent<SVGSVGElement>) => {
    if (!draft) return
    setDraft({ start: draft.start, end: toImageCoords(e) })
  }
  const handlePointerUp = () => {
    if (!draft) return
    const [x1, y1] = draft.start
    const [x2, y2] = draft.end
    setDraft(null)
    const minX = Math.min(x1, x2)
    const maxX = Math.max(x1, x2)
    const minY = Math.min(y1, y2)
    const maxY = Math.max(y1, y2)
    if (maxX - minX < 4 || maxY - minY < 4) return // ignore accidental clicks/tiny drags
    onDraw?.([
      [minX, minY],
      [maxX, minY],
      [maxX, maxY],
      [minX, maxY],
    ])
  }

  return (
    <svg
      ref={svgRef}
      data-slot="box-canvas"
      viewBox={`0 0 ${w} ${h}`}
      className={cn(
        "aspect-auto w-full touch-none rounded-lg border bg-muted select-none",
        !readOnly && "cursor-crosshair",
      )}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
    >
      <image href={imageUrl} width={w} height={h} />
      {boxes.map((box) => {
        const xs = box.points.map((p) => p[0])
        const ys = box.points.map((p) => p[1])
        const minX = Math.min(...xs)
        const minY = Math.min(...ys)
        const maxX = Math.max(...xs)
        const isSelected = box.id === selectedId
        return (
          <g
            key={box.id}
            onPointerDown={(e) => {
              if (readOnly) return
              e.stopPropagation()
              onSelect?.(box.id)
            }}
          >
            <polygon
              points={box.points.map((p) => p.join(",")).join(" ")}
              className={cn(box.colorClass ?? PALETTE[0], !readOnly && "cursor-pointer")}
              strokeWidth={isSelected ? strokeWidth * 2.5 : strokeWidth}
            />
            <text
              x={minX}
              y={Math.max(minY - strokeWidth * 2, strokeWidth * 10)}
              fontSize={Math.max(w, h) / 45}
              className="fill-current font-medium"
              style={{ paintOrder: "stroke", stroke: "white", strokeWidth: strokeWidth * 3 }}
            >
              {box.label}
            </text>
            {!readOnly && isSelected && onDelete && (
              <text
                x={maxX}
                y={minY}
                fontSize={Math.max(w, h) / 30}
                textAnchor="end"
                className="cursor-pointer fill-destructive font-bold"
                style={{ paintOrder: "stroke", stroke: "white", strokeWidth: strokeWidth * 3 }}
                onPointerDown={(e) => {
                  e.stopPropagation()
                  onDelete(box.id)
                }}
              >
                ✕
              </text>
            )}
          </g>
        )
      })}
      {draft && (
        <rect
          x={Math.min(draft.start[0], draft.end[0])}
          y={Math.min(draft.start[1], draft.end[1])}
          width={Math.abs(draft.end[0] - draft.start[0])}
          height={Math.abs(draft.end[1] - draft.start[1])}
          className="fill-blue-500/10 stroke-blue-500"
          strokeWidth={strokeWidth}
          strokeDasharray={strokeWidth * 3}
        />
      )}
    </svg>
  )
}
