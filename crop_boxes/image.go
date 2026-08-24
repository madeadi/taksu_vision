// Minimal image primitives (decode/encode, perspective warp, rotate, Sobel
// gradient) — a pure-Go stand-in for the handful of OpenCV calls cropper.py
// used (getPerspectiveTransform/warpPerspective/rotate/Sobel/cvtColor).
// Deliberately not a general imaging library: just what crop geometry needs.
package main

import (
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"

	_ "golang.org/x/image/bmp"
)

// Mat is an 8-bit RGB image, row-major, 3 bytes per pixel.
type Mat struct {
	Width, Height int
	Pix           []uint8
}

func newMat(width, height int) *Mat {
	return &Mat{Width: width, Height: height, Pix: make([]uint8, width*height*3)}
}

func (m *Mat) at(x, y int) (r, g, b uint8) {
	i := (y*m.Width + x) * 3
	return m.Pix[i], m.Pix[i+1], m.Pix[i+2]
}

func (m *Mat) set(x, y int, r, g, b uint8) {
	i := (y*m.Width + x) * 3
	m.Pix[i], m.Pix[i+1], m.Pix[i+2] = r, g, b
}

func loadMat(path string) (*Mat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	bounds := img.Bounds()
	m := newMat(bounds.Dx(), bounds.Dy())
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			m.set(x, y, uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}
	return m, nil
}

// saveJPEG writes m as a JPEG file at the given quality (0-100), creating
// parent directories as needed.
func saveJPEG(path string, m *Mat, quality int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, m.Width, m.Height))
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			r, g, b := m.at(x, y)
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
		}
	}
	return jpeg.Encode(f, img, &jpeg.Options{Quality: quality})
}

// point2 is a 2D point; kept distinct from image.Point since coordinates here
// are sub-pixel float64 (box corners), not integer pixel indices.
type point2 struct{ X, Y float64 }

// perspectiveTransform solves the 3x3 homography H (h[2][2]=1) mapping each
// src[i] to dst[i], equivalent to cv2.getPerspectiveTransform.
func perspectiveTransform(src, dst [4]point2) [3][3]float64 {
	// 8x8 linear system for h11..h32 (h33 fixed at 1); see cv2's
	// getPerspectiveTransform derivation.
	a := make([][]float64, 8)
	for i := range a {
		a[i] = make([]float64, 9) // last column is the RHS (augmented)
	}
	for i := 0; i < 4; i++ {
		x, y := src[i].X, src[i].Y
		X, Y := dst[i].X, dst[i].Y
		a[2*i][0], a[2*i][1], a[2*i][2] = x, y, 1
		a[2*i][3], a[2*i][4], a[2*i][5] = 0, 0, 0
		a[2*i][6], a[2*i][7] = -X*x, -X*y
		a[2*i][8] = X

		a[2*i+1][0], a[2*i+1][1], a[2*i+1][2] = 0, 0, 0
		a[2*i+1][3], a[2*i+1][4], a[2*i+1][5] = x, y, 1
		a[2*i+1][6], a[2*i+1][7] = -Y*x, -Y*y
		a[2*i+1][8] = Y
	}
	h := solveLinear(a)
	return [3][3]float64{
		{h[0], h[1], h[2]},
		{h[3], h[4], h[5]},
		{h[6], h[7], 1},
	}
}

// solveLinear solves an 8x8 linear system given as an 8x9 augmented matrix
// via Gaussian elimination with partial pivoting.
func solveLinear(a [][]float64) []float64 {
	n := len(a)
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[pivot][col]) {
				pivot = r
			}
		}
		a[col], a[pivot] = a[pivot], a[col]
		for r := col + 1; r < n; r++ {
			factor := a[r][col] / a[col][col]
			for c := col; c <= n; c++ {
				a[r][c] -= factor * a[col][c]
			}
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		sum := a[r][n]
		for c := r + 1; c < n; c++ {
			sum -= a[r][c] * x[c]
		}
		x[r] = sum / a[r][r]
	}
	return x
}

func invert3x3(m [3][3]float64) [3][3]float64 {
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	inv := 1 / det
	var r [3][3]float64
	r[0][0] = (m[1][1]*m[2][2] - m[1][2]*m[2][1]) * inv
	r[0][1] = (m[0][2]*m[2][1] - m[0][1]*m[2][2]) * inv
	r[0][2] = (m[0][1]*m[1][2] - m[0][2]*m[1][1]) * inv
	r[1][0] = (m[1][2]*m[2][0] - m[1][0]*m[2][2]) * inv
	r[1][1] = (m[0][0]*m[2][2] - m[0][2]*m[2][0]) * inv
	r[1][2] = (m[0][2]*m[1][0] - m[0][0]*m[1][2]) * inv
	r[2][0] = (m[1][0]*m[2][1] - m[1][1]*m[2][0]) * inv
	r[2][1] = (m[0][1]*m[2][0] - m[0][0]*m[2][1]) * inv
	r[2][2] = (m[0][0]*m[1][1] - m[0][1]*m[1][0]) * inv
	return r
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bilinearReplicate samples src at fractional (x, y), clamping out-of-bounds
// coordinates to the nearest edge pixel — matches cv2's BORDER_REPLICATE.
func bilinearReplicate(src *Mat, x, y float64) (r, g, b float64) {
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1, y1 := x0+1, y0+1
	fx, fy := x-float64(x0), y-float64(y0)

	x0c, x1c := clampInt(x0, 0, src.Width-1), clampInt(x1, 0, src.Width-1)
	y0c, y1c := clampInt(y0, 0, src.Height-1), clampInt(y1, 0, src.Height-1)

	r00, g00, b00 := src.at(x0c, y0c)
	r10, g10, b10 := src.at(x1c, y0c)
	r01, g01, b01 := src.at(x0c, y1c)
	r11, g11, b11 := src.at(x1c, y1c)

	lerp := func(a, b, t float64) float64 { return a + (b-a)*t }
	top := func(a, b uint8) float64 { return lerp(float64(a), float64(b), fx) }
	rVal := lerp(top(r00, r10), top(r01, r11), fy)
	gVal := lerp(top(g00, g10), top(g01, g11), fy)
	bVal := lerp(top(b00, b10), top(b01, b11), fy)
	return rVal, gVal, bVal
}

// warpPerspective renders src through homography h (mapping src -> dst
// coordinates) into an outW x outH image, sampling src with bilinear
// interpolation and edge-replicate borders — equivalent to
// cv2.warpPerspective(src, h, (outW, outH), flags=INTER_LINEAR,
// borderMode=BORDER_REPLICATE).
func warpPerspective(src *Mat, h [3][3]float64, outW, outH int) *Mat {
	inv := invert3x3(h)
	dst := newMat(outW, outH)
	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			fx := float64(x)
			fy := float64(y)
			w := inv[2][0]*fx + inv[2][1]*fy + inv[2][2]
			u := (inv[0][0]*fx + inv[0][1]*fy + inv[0][2]) / w
			v := (inv[1][0]*fx + inv[1][1]*fy + inv[1][2]) / w
			r, g, b := bilinearReplicate(src, u, v)
			dst.set(x, y, uint8(math.Round(clampFloat(r, 0, 255))), uint8(math.Round(clampFloat(g, 0, 255))), uint8(math.Round(clampFloat(b, 0, 255))))
		}
	}
	return dst
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// rotate90CW rotates m 90 degrees clockwise (cv2.ROTATE_90_CLOCKWISE).
func rotate90CW(m *Mat) *Mat {
	dst := newMat(m.Height, m.Width)
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			r, g, b := m.at(x, y)
			dst.set(m.Height-1-y, x, r, g, b)
		}
	}
	return dst
}

// rotate180 rotates m 180 degrees (cv2.ROTATE_180).
func rotate180(m *Mat) *Mat {
	dst := newMat(m.Width, m.Height)
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			r, g, b := m.at(x, y)
			dst.set(m.Width-1-x, m.Height-1-y, r, g, b)
		}
	}
	return dst
}

// grayscale converts m to single-channel intensity using the same weights as
// cv2.cvtColor(_, COLOR_BGR2GRAY): 0.299 R + 0.587 G + 0.114 B (channel
// order doesn't matter here since each weight is tied to its channel, not to
// storage order).
func grayscale(m *Mat) []float64 {
	out := make([]float64, m.Width*m.Height)
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			r, g, b := m.at(x, y)
			out[y*m.Width+x] = 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		}
	}
	return out
}

// reflect101 mirrors an out-of-range index without repeating the edge pixel,
// matching cv2's default border mode (BORDER_REFLECT_101) used by Sobel.
func reflect101(i, n int) int {
	if n == 1 {
		return 0
	}
	for i < 0 || i >= n {
		if i < 0 {
			i = -i
		}
		if i >= n {
			i = 2*(n-1) - i
		}
	}
	return i
}

// sobel computes the Sobel gradient magnitude sum (|Gx|+|Gy|) over a
// width x height single-channel image region, matching
// abs(cv2.Sobel(_,CV_32F,1,0,ksize=3)) + abs(cv2.Sobel(_,CV_32F,0,1,ksize=3)).
func sobelMagnitudeMean(gray []float64, width, height, offsetX int, fullWidth int) float64 {
	gx := [3][3]float64{{-1, 0, 1}, {-2, 0, 2}, {-1, 0, 1}}
	gy := [3][3]float64{{-1, -2, -1}, {0, 0, 0}, {1, 2, 1}}
	var sum float64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var sx, sy float64
			for ky := -1; ky <= 1; ky++ {
				for kx := -1; kx <= 1; kx++ {
					sampleX := reflect101(offsetX+x+kx, fullWidth)
					sampleY := reflect101(y+ky, height)
					v := gray[sampleY*fullWidth+sampleX]
					sx += gx[ky+1][kx+1] * v
					sy += gy[ky+1][kx+1] * v
				}
			}
			sum += math.Abs(sx) + math.Abs(sy)
		}
	}
	return sum / float64(width*height)
}
