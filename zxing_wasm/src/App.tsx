import { useCallback, useEffect, useRef, useState } from "react"
import {
  prepareZXingModule,
  readBarcodes,
  type ReadResult,
  type ReadInputBarcodeFormat,
} from "zxing-wasm/reader"
// Bundle the .wasm with the app instead of fetching it from a CDN at runtime.
import wasmUrl from "zxing-wasm/reader/zxing_reader.wasm?url"

prepareZXingModule({
  overrides: {
    locateFile: (path: string, prefix: string) =>
      path.endsWith(".wasm") ? wasmUrl : prefix + path,
  },
  fireImmediately: false,
})

const FORMATS: { id: ReadInputBarcodeFormat; label: string }[] = [
  { id: "QRCode", label: "QR Code" },
  { id: "MicroQRCode", label: "Micro QR" },
  { id: "RMQRCode", label: "rMQR" },
  { id: "DataMatrix", label: "Data Matrix" },
  { id: "Aztec", label: "Aztec" },
  { id: "PDF417", label: "PDF417" },
  { id: "Code128", label: "Code 128" },
  { id: "EAN13", label: "EAN-13" },
]

const DEFAULT_FORMATS = new Set<ReadInputBarcodeFormat>([
  "QRCode",
  "MicroQRCode",
  "RMQRCode",
  "DataMatrix",
  "Aztec",
])

function App() {
  const [image, setImage] = useState<{ url: string; name: string } | null>(null)
  const [results, setResults] = useState<ReadResult[] | null>(null)
  const [elapsedMs, setElapsedMs] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [formats, setFormats] = useState(DEFAULT_FORMATS)
  const [tryHarder, setTryHarder] = useState(true)
  const [dragging, setDragging] = useState(false)
  const fileRef = useRef<File | null>(null)
  const imgRef = useRef<HTMLImageElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)

  const decode = useCallback(
    async (file: File) => {
      setBusy(true)
      setError(null)
      try {
        const t0 = performance.now()
        const found = await readBarcodes(file, {
          formats: [...formats],
          tryHarder,
          maxNumberOfSymbols: 0,
        })
        setElapsedMs(performance.now() - t0)
        setResults(found)
      } catch (e) {
        setResults(null)
        setError(e instanceof Error ? e.message : String(e))
      } finally {
        setBusy(false)
      }
    },
    [formats, tryHarder],
  )

  const load = useCallback(
    (file: File) => {
      if (!file.type.startsWith("image/")) {
        setError(`Not an image: ${file.name || file.type}`)
        return
      }
      fileRef.current = file
      setImage((prev) => {
        if (prev) URL.revokeObjectURL(prev.url)
        return { url: URL.createObjectURL(file), name: file.name || "pasted image" }
      })
      void decode(file)
    },
    [decode],
  )

  // Re-decode when options change.
  useEffect(() => {
    if (fileRef.current) void decode(fileRef.current)
  }, [decode])

  // Paste an image from the clipboard.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const file = [...(e.clipboardData?.files ?? [])].find((f) =>
        f.type.startsWith("image/"),
      )
      if (file) load(file)
    }
    window.addEventListener("paste", onPaste)
    return () => window.removeEventListener("paste", onPaste)
  }, [load])

  // Draw the detected symbol outlines over the image.
  const drawOverlay = useCallback(() => {
    const img = imgRef.current
    const canvas = canvasRef.current
    if (!img || !canvas) return
    canvas.width = img.naturalWidth
    canvas.height = img.naturalHeight
    const ctx = canvas.getContext("2d")!
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    const lw = Math.max(2, img.naturalWidth / 250)
    ;(results ?? []).forEach((r, i) => {
      const p = r.position
      ctx.beginPath()
      ctx.moveTo(p.topLeft.x, p.topLeft.y)
      ;[p.topRight, p.bottomRight, p.bottomLeft].forEach((pt) => ctx.lineTo(pt.x, pt.y))
      ctx.closePath()
      ctx.lineWidth = lw
      ctx.strokeStyle = r.isValid ? "#16a34a" : "#dc2626"
      ctx.stroke()
      ctx.fillStyle = ctx.strokeStyle
      ctx.font = `bold ${lw * 7}px sans-serif`
      ctx.fillText(String(i + 1), p.topLeft.x, p.topLeft.y - lw * 2)
    })
  }, [results])

  useEffect(drawOverlay, [drawOverlay])

  const toggleFormat = (id: ReadInputBarcodeFormat) =>
    setFormats((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <div className="mx-auto max-w-4xl space-y-4 p-6">
      <header>
        <h1 className="text-xl font-semibold">ZXing WASM</h1>
        <p className="text-sm text-neutral-500">
          Decode QR, Data Matrix, Aztec and more entirely in your browser with
          zxing-cpp compiled to WebAssembly. Nothing is uploaded.
        </p>
      </header>

      <label
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          const f = e.dataTransfer.files[0]
          if (f) load(f)
        }}
        className={`flex cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed p-8 text-sm text-neutral-500 transition ${
          dragging ? "border-neutral-900 bg-neutral-100" : "border-neutral-300"
        }`}
      >
        <span>Drop an image here, click to choose, or paste from the clipboard</span>
        <input
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) load(f)
            e.target.value = ""
          }}
        />
      </label>

      <fieldset className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
        {FORMATS.map((f) => (
          <label key={f.id} className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={formats.has(f.id)}
              onChange={() => toggleFormat(f.id)}
            />
            {f.label}
          </label>
        ))}
        <label className="ml-auto flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={tryHarder}
            onChange={(e) => setTryHarder(e.target.checked)}
          />
          Try harder
        </label>
      </fieldset>

      {error && (
        <p className="rounded bg-red-50 p-3 text-sm text-red-700">{error}</p>
      )}

      {image && (
        <div className="grid gap-4 md:grid-cols-2">
          <div className="relative self-start overflow-hidden rounded-lg border">
            <img
              ref={imgRef}
              src={image.url}
              alt={image.name}
              onLoad={drawOverlay}
              className="block w-full"
            />
            <canvas
              ref={canvasRef}
              className="pointer-events-none absolute inset-0 h-full w-full"
            />
          </div>

          <div className="space-y-3">
            <p className="text-sm text-neutral-500">
              {busy
                ? "Decoding…"
                : results
                  ? `${results.length} symbol${results.length === 1 ? "" : "s"} found${
                      elapsedMs != null ? ` in ${elapsedMs.toFixed(0)} ms` : ""
                    }`
                  : ""}
            </p>
            {results?.length === 0 && !busy && (
              <p className="text-sm">No barcode found.</p>
            )}
            {results?.map((r, i) => (
              <div key={i} className="rounded-lg border p-3 text-sm">
                <div className="mb-1 flex items-center gap-2 text-xs text-neutral-500">
                  <span className="font-semibold text-neutral-900">#{i + 1}</span>
                  <span>{r.format}</span>
                  {!r.isValid && <span className="text-red-600">{r.error}</span>}
                  <button
                    className="ml-auto underline"
                    onClick={() => void navigator.clipboard.writeText(r.text)}
                  >
                    Copy
                  </button>
                </div>
                <pre className="whitespace-pre-wrap break-all font-mono">{r.text}</pre>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default App
