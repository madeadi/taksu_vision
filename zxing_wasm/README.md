# zxing_wasm

Browser-only demo of [zxing-cpp](https://github.com/zxing-cpp/zxing-cpp)
compiled to WebAssembly, via the [`zxing-wasm`](https://github.com/Sec-ant/zxing-wasm)
package. Pick, drop or paste an image and it decodes QR / Micro QR / rMQR /
Data Matrix / Aztec (plus PDF417, Code 128, EAN-13 if enabled) locally, drawing
each detected symbol's outline over the image.

Unlike the other services this is a static app: no backend, no `/tasks`, no
workspace. Images never leave the browser. For server-side decoding of
workspace crops see `../read_qr_ocr`.

## Run

```bash
npm install
npm run dev      # http://localhost:8830
npm run build    # static output in dist/ (relative asset paths, host anywhere)
```

The `.wasm` file is bundled into the build (not loaded from a CDN), so the
built `dist/` works offline.
