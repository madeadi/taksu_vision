import { useCallback, useEffect, useRef, useState } from "react"
import { loadModels, runOcr, type OcrResult, type Pt } from "./ocr.ts"

function inQuad(p: Pt, q: Pt[]) {
  // Ray casting; the quad is a (possibly rotated) rectangle in image pixels.
  let inside = false
  for (let i = 0, j = q.length - 1; i < q.length; j = i++) {
    const [xi, yi] = q[i]
    const [xj, yj] = q[j]
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) inside = !inside
  }
  return inside
}

function App() {
  const [image, setImage] = useState<{ url: string; name: string } | null>(null)
  const [result, setResult] = useState<OcrResult | null>(null)
  const [status, setStatus] = useState("Loading models…")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const imgRef = useRef<HTMLImageElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    loadModels()
      .then(() => setStatus("Models ready"))
      .catch((e) =>
        setError(`Could not load models (run \`npm run models\`): ${e instanceof Error ? e.message : e}`),
      )
  }, [])

  const load = useCallback((file: File) => {
    if (!file.type.startsWith("image/")) return setError(`Not an image: ${file.name || file.type}`)
    setError(null)
    setResult(null)
    setImage((prev) => {
      if (prev) URL.revokeObjectURL(prev.url)
      return { url: URL.createObjectURL(file), name: file.name || "pasted image" }
    })
  }, [])

  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const f = [...(e.clipboardData?.files ?? [])].find((f) => f.type.startsWith("image/"))
      if (f) load(f)
    }
    window.addEventListener("paste", onPaste)
    return () => window.removeEventListener("paste", onPaste)
  }, [load])

  const onImageLoad = async () => {
    const img = imgRef.current
    if (!img) return
    setBusy(true)
    setError(null)
    try {
      setResult(await runOcr(img))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  // Draw detected boxes over the image.
  useEffect(() => {
    const img = imgRef.current
    const c = canvasRef.current
    if (!img || !c) return
    c.width = img.naturalWidth
    c.height = img.naturalHeight
    const ctx = c.getContext("2d")!
    ctx.clearRect(0, 0, c.width, c.height)
    ctx.lineWidth = Math.max(2, c.width / 400)
    ctx.strokeStyle = "#16a34a"
    result?.lines.forEach(({ quad }, idx) => {
      ctx.beginPath()
      quad.forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)))
      ctx.closePath()
      if (idx === hover) {
        ctx.fillStyle = "rgba(234, 88, 12, 0.3)"
        ctx.fill()
        ctx.save()
        ctx.strokeStyle = "#ea580c"
        ctx.lineWidth *= 1.5
        ctx.stroke()
        ctx.restore()
      } else ctx.stroke()
    })
  }, [result, hover])

  // Image -> list: find the box under the cursor (smallest wins when boxes overlap) and scroll its row into view.
  const onImageMove = (e: React.MouseEvent<HTMLDivElement>) => {
    const img = imgRef.current
    if (!img || !result) return
    const r = img.getBoundingClientRect()
    const p: Pt = [
      ((e.clientX - r.left) / r.width) * img.naturalWidth,
      ((e.clientY - r.top) / r.height) * img.naturalHeight,
    ]
    let best: number | null = null
    let bestArea = Infinity
    result.lines.forEach(({ quad }, i) => {
      if (!inQuad(p, quad)) return
      const area = Math.hypot(quad[1][0] - quad[0][0], quad[1][1] - quad[0][1]) * Math.hypot(quad[3][0] - quad[0][0], quad[3][1] - quad[0][1])
      if (area < bestArea) ((best = i), (bestArea = area))
    })
    if (best !== hover) {
      setHover(best)
      if (best !== null) listRef.current?.children[best]?.scrollIntoView({ block: "nearest" })
    }
  }

  const fullText = result?.lines.map((l) => l.text).join("\n") ?? ""

  return (
    <div className="mx-auto max-w-5xl p-6 space-y-4">
      <header>
        <h1 className="text-2xl font-semibold">PaddleOCR (ONNX, WASM)</h1>
        <p className="text-sm text-slate-500">
          PP-OCRv4 detection, angle classification and recognition running locally in your browser. {status}.
        </p>
      </header>

      <label
        onDragOver={(e) => (e.preventDefault(), setDragging(true))}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          const f = e.dataTransfer.files[0]
          if (f) load(f)
        }}
        className={`block cursor-pointer rounded-lg border-2 border-dashed p-8 text-center text-slate-500 ${
          dragging ? "border-green-500 bg-green-50" : "border-slate-300"
        }`}
      >
        Pick, drop or paste an image
        <input
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(e) => e.target.files?.[0] && load(e.target.files[0])}
        />
      </label>

      {error && <p className="rounded bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {busy && <p className="text-sm text-slate-500">Running OCR…</p>}
      {result && (
        <p className="text-sm text-slate-500">
          {result.lines.length} lines · detect {result.detMs.toFixed(0)} ms · recognize{" "}
          {result.recMs.toFixed(0)} ms
        </p>
      )}

      {image && (
        <div className="grid gap-4 md:grid-cols-2">
          <div className="relative self-start" onMouseMove={onImageMove} onMouseLeave={() => setHover(null)}>
            <img
              ref={imgRef}
              src={image.url}
              alt={image.name}
              onLoad={onImageLoad}
              className="block w-full rounded border"
            />
            <canvas ref={canvasRef} className="pointer-events-none absolute inset-0 h-full w-full" />
          </div>
          <div className="space-y-2">
            <ul ref={listRef} className="divide-y rounded border text-sm">
              {result?.lines.map((l, i) => (
                <li
                  key={i}
                  onMouseEnter={() => setHover(i)}
                  onMouseLeave={() => setHover(null)}
                  className={`flex justify-between gap-3 p-2 ${i === hover ? "bg-orange-100" : ""}`}
                >
                  <span className="break-all">{l.text}</span>
                  <span className="shrink-0 text-slate-400">{(l.score * 100).toFixed(0)}%</span>
                </li>
              ))}
            </ul>
            {fullText && (
              <button
                className="rounded bg-slate-800 px-3 py-1 text-sm text-white"
                onClick={() => navigator.clipboard.writeText(fullText)}
              >
                Copy text
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default App
