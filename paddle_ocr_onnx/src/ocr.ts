// PP-OCRv4 (DBNet detector + angle classifier + SVTR/CTC recognizer) on onnxruntime-web (WASM).
// Detection post-processing (contours, rotated rects, perspective crops) uses opencv.js,
// loaded lazily, so tilted and vertical text is straightened before recognition.
import * as ort from "onnxruntime-web"
import ortWasmUrl from "onnxruntime-web/ort-wasm-simd-threaded.wasm?url"
import ortMjsUrl from "onnxruntime-web/ort-wasm-simd-threaded.mjs?url"

ort.env.wasm.wasmPaths = { wasm: ortWasmUrl, mjs: ortMjsUrl }

import type cvType from "@techstark/opencv-js"
type CV = typeof cvType
let cvPromise: Promise<CV> | null = null
function loadCv() {
  cvPromise ??= (async () => {
    const m = (await import("@techstark/opencv-js")).default as unknown
    let cv = (m instanceof Promise ? await m : m) as CV
    if (!cv.Mat) await new Promise<void>((r) => (cv.onRuntimeInitialized = () => r()))
    return cv
  })()
  return cvPromise
}

export type Pt = [number, number]
export interface OcrLine {
  /** Quadrilateral in source-image pixels, order: top-left, top-right, bottom-right, bottom-left. */
  quad: Pt[]
  text: string
  score: number // mean CTC confidence
  detScore: number
  rotated180: boolean // classifier flipped this crop
}
export interface OcrResult {
  lines: OcrLine[]
  detMs: number
  recMs: number
}

const BASE = import.meta.env.BASE_URL
const DET_LIMIT = 960 // max side fed to detector
const DET_THRESH = 0.3
const DET_BOX_THRESH = 0.6
const DET_UNCLIP = 1.6
const REC_H = 48
const CLS_W = 192
const CLS_THRESH = 0.9

type Models = {
  det: ort.InferenceSession
  cls: ort.InferenceSession
  rec: ort.InferenceSession
  chars: string[]
  cv: CV
}
let models: Promise<Models> | null = null

export function loadModels() {
  models ??= (async () => {
    const opts: ort.InferenceSession.SessionOptions = { executionProviders: ["wasm"] }
    const [det, cls, rec, keys, cv] = await Promise.all([
      ort.InferenceSession.create(`${BASE}models/det.onnx`, opts),
      ort.InferenceSession.create(`${BASE}models/cls.onnx`, opts),
      ort.InferenceSession.create(`${BASE}models/rec.onnx`, opts),
      fetch(`${BASE}models/keys.txt`).then((r) => r.text()),
      loadCv(),
    ])
    // CTC index 0 is the blank; the dictionary follows, then a trailing space.
    const chars = ["", ...keys.split("\n").filter((l: string) => l.length > 0), " "]
    return { det, cls, rec, chars, cv }
  })()
  models.catch(() => (models = null))
  return models
}

// CHW float tensor from RGBA bytes, emitted in BGR order (matches PaddleOCR's cv2-based
// pipeline): pixel -> (p/255 - mean[ch]) / std[ch].
function toTensor(d: Uint8Array | Uint8ClampedArray, w: number, h: number, mean: number[], std: number[]) {
  const out = new Float32Array(3 * w * h)
  const plane = w * h
  for (let i = 0; i < plane; i++) {
    const p = i * 4
    out[i] = (d[p + 2] / 255 - mean[0]) / std[0]
    out[plane + i] = (d[p + 1] / 255 - mean[1]) / std[1]
    out[2 * plane + i] = (d[p] / 255 - mean[2]) / std[2]
  }
  return out
}

function orderQuad(p: Pt[]): Pt[] {
  // tl = min(x+y), br = max(x+y), tr = min(y-x), bl = max(y-x)
  const by = (f: (q: Pt) => number, max: boolean) =>
    p.reduce((a, b) => ((f(b) > f(a)) === max ? b : a))
  return [by((q) => q[0] + q[1], false), by((q) => q[1] - q[0], false), by((q) => q[0] + q[1], true), by((q) => q[1] - q[0], true)]
}

async function detect(m: Models, src: any, iw: number, ih: number) {
  const { det, cv } = m
  const scale = Math.min(1, DET_LIMIT / Math.max(iw, ih))
  const w = Math.max(32, Math.round((iw * scale) / 32) * 32)
  const h = Math.max(32, Math.round((ih * scale) / 32) * 32)
  const small = new cv.Mat()
  cv.resize(src, small, new cv.Size(w, h), 0, 0, cv.INTER_AREA)
  const data = toTensor(small.data, w, h, [0.485, 0.456, 0.406], [0.229, 0.224, 0.225])
  small.delete()
  const out = await det.run({ [det.inputNames[0]]: new ort.Tensor("float32", data, [1, 3, h, w]) })
  const prob = out[det.outputNames[0]].data as Float32Array // [1,1,h,w]

  const probMat = cv.matFromArray(h, w, cv.CV_32F, Array.from(prob))
  const bitmap = new cv.Mat()
  cv.threshold(probMat, bitmap, DET_THRESH, 255, cv.THRESH_BINARY)
  bitmap.convertTo(bitmap, cv.CV_8U)
  const contours = new cv.MatVector()
  const hier = new cv.Mat()
  cv.findContours(bitmap, contours, hier, cv.RETR_LIST, cv.CHAIN_APPROX_SIMPLE)

  const boxes: { quad: Pt[]; score: number }[] = []
  for (let i = 0; i < contours.size(); i++) {
    const cnt = contours.get(i)
    const rect = cv.minAreaRect(cnt)
    const short = Math.min(rect.size.width, rect.size.height)
    if (short < 3) {
      cnt.delete()
      continue
    }
    // Box score: mean probability inside the (un-expanded) rotated rect.
    const pts = orderQuad(cv.RotatedRect.points(rect).map((q: { x: number; y: number }) => [q.x, q.y] as Pt))
    const bb = cv.boundingRect(cnt)
    const x0 = Math.max(0, bb.x), y0 = Math.max(0, bb.y)
    const x1 = Math.min(w, bb.x + bb.width), y1 = Math.min(h, bb.y + bb.height)
    const mask = cv.Mat.zeros(y1 - y0, x1 - x0, cv.CV_8U)
    const poly = cv.matFromArray(4, 1, cv.CV_32SC2, pts.flatMap(([x, y]) => [Math.round(x - x0), Math.round(y - y0)]))
    const pv = new cv.MatVector()
    pv.push_back(poly)
    cv.fillPoly(mask, pv, new cv.Scalar(1))
    const score = cv.mean(probMat.roi(new cv.Rect(x0, y0, x1 - x0, y1 - y0)), mask)[0]
    mask.delete(); poly.delete(); pv.delete(); cnt.delete()
    if (score < DET_BOX_THRESH) continue

    // DB "unclip": grow the shrunk text kernel by area*ratio/perimeter on every side.
    const { width: rw, height: rh } = rect.size
    const dist = (rw * rh * DET_UNCLIP) / (2 * (rw + rh))
    const grown = { center: rect.center, size: { width: rw + 2 * dist, height: rh + 2 * dist }, angle: rect.angle }
    if (Math.min(grown.size.width, grown.size.height) < 5) continue
    const quad = orderQuad(cv.RotatedRect.points(grown).map((q: { x: number; y: number }) => [(q.x / w) * iw, (q.y / h) * ih] as Pt))
    boxes.push({ quad, score })
  }
  probMat.delete(); bitmap.delete(); contours.delete(); hier.delete()

  // Reading order: top-to-bottom, then left-to-right within a line.
  const key = (q: Pt[]) => ({ x: q[0][0], y: q[0][1], h: Math.hypot(q[3][0] - q[0][0], q[3][1] - q[0][1]) })
  boxes.sort((a, b) => {
    const A = key(a.quad), B = key(b.quad)
    return Math.abs(A.y - B.y) < Math.min(A.h, B.h) / 2 ? A.x - B.x : A.y - B.y
  })
  return boxes
}

// Perspective-warp a quad to an upright rectangle; vertical crops (h/w >= 1.5) are turned 90 degrees.
function cropQuad(cv: CV, src: any, quad: Pt[]) {
  const d = (a: Pt, b: Pt) => Math.hypot(a[0] - b[0], a[1] - b[1])
  const [tl, tr, br, bl] = quad
  const cw = Math.max(1, Math.round(Math.max(d(tl, tr), d(bl, br))))
  const ch = Math.max(1, Math.round(Math.max(d(tl, bl), d(tr, br))))
  const from = cv.matFromArray(4, 1, cv.CV_32FC2, quad.flat())
  const to = cv.matFromArray(4, 1, cv.CV_32FC2, [0, 0, cw, 0, cw, ch, 0, ch])
  const M = cv.getPerspectiveTransform(from, to)
  const out = new cv.Mat()
  cv.warpPerspective(src, out, M, new cv.Size(cw, ch), cv.INTER_CUBIC, cv.BORDER_REPLICATE)
  from.delete(); to.delete(); M.delete()
  if (ch / cw >= 1.5) cv.rotate(out, out, cv.ROTATE_90_COUNTERCLOCKWISE)
  return out
}

function resizeToHeight(cv: CV, crop: any, h: number, maxW: number) {
  const w = Math.min(maxW, Math.max(8, Math.ceil((h * crop.cols) / crop.rows)))
  const out = new cv.Mat()
  cv.resize(crop, out, new cv.Size(w, h), 0, 0, cv.INTER_LINEAR)
  return out
}

// Returns true when the crop is upside down (180 degrees).
async function isFlipped(m: Models, crop: any) {
  const { cls, cv } = m
  const r = resizeToHeight(cv, crop, REC_H, CLS_W)
  const padded = new cv.Mat()
  cv.copyMakeBorder(r, padded, 0, 0, 0, CLS_W - r.cols, cv.BORDER_CONSTANT, new cv.Scalar(0, 0, 0, 255))
  const data = toTensor(padded.data, CLS_W, REC_H, [0.5, 0.5, 0.5], [0.5, 0.5, 0.5])
  r.delete(); padded.delete()
  const out = await cls.run({ [cls.inputNames[0]]: new ort.Tensor("float32", data, [1, 3, REC_H, CLS_W]) })
  const p = out[cls.outputNames[0]].data as Float32Array
  return p[1] > p[0] && p[1] > CLS_THRESH
}

async function recognize(m: Models, crop: any) {
  const { rec, chars, cv } = m
  const r = resizeToHeight(cv, crop, REC_H, 4000)
  const w = r.cols
  const data = toTensor(r.data, w, REC_H, [0.5, 0.5, 0.5], [0.5, 0.5, 0.5])
  r.delete()
  const out = await rec.run({ [rec.inputNames[0]]: new ort.Tensor("float32", data, [1, 3, REC_H, w]) })
  const t = out[rec.outputNames[0]]
  const [, steps, classes] = t.dims as number[]
  const logits = t.data as Float32Array

  // Greedy CTC: argmax per step, collapse repeats, drop blanks.
  let text = ""
  let prev = -1
  let confSum = 0
  let confN = 0
  for (let s = 0; s < steps; s++) {
    const o = s * classes
    let best = 0
    let max = -Infinity
    for (let k = 0; k < classes; k++) if (logits[o + k] > max) ((max = logits[o + k]), (best = k))
    if (best !== 0 && best !== prev) {
      text += chars[best] ?? ""
      confSum += max // the v4 rec model already emits softmax probabilities
      confN++
    }
    prev = best
  }
  return { text, score: confN ? confSum / confN : 0 }
}

/** Run OCR on a loaded image. */
export async function runOcr(img: HTMLImageElement | ImageBitmap): Promise<OcrResult> {
  const m = await loadModels()
  const { cv } = m
  const c = document.createElement("canvas")
  c.width = "naturalWidth" in img ? img.naturalWidth : img.width
  c.height = "naturalHeight" in img ? img.naturalHeight : img.height
  c.getContext("2d")!.drawImage(img, 0, 0)
  const src = cv.imread(c) // RGBA
  try {
    const t0 = performance.now()
    const boxes = await detect(m, src, c.width, c.height)
    const t1 = performance.now()
    const lines: OcrLine[] = []
    for (const { quad, score } of boxes) {
      const crop = cropQuad(cv, src, quad)
      const flipped = await isFlipped(m, crop)
      if (flipped) cv.rotate(crop, crop, cv.ROTATE_180)
      const r = await recognize(m, crop)
      crop.delete()
      if (r.text.trim()) lines.push({ quad, text: r.text, score: r.score, detScore: score, rotated180: flipped })
    }
    return { lines, detMs: t1 - t0, recMs: performance.now() - t1 }
  } finally {
    src.delete()
  }
}
