//go:build ignore

// Run with: go run genicon.go
// Outputs icon.ico, then embed it:
//   go run github.com/akavel/rsrc@latest -ico icon.ico -o rsrc_windows_amd64.syso

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
)

var (
	bgCol = color.RGBA{0x16, 0x16, 0x16, 0xff} // #161616
	fgCol = color.RGBA{0xd4, 0xff, 0x47, 0xff} // #d4ff47
)

type icoEntry struct {
	size  int
	data  []byte
	isPNG bool
}

func main() {
	sizes := []int{16, 32, 48, 256}

	var entries []icoEntry
	for _, s := range sizes {
		img := drawIcon(s)
		if s == 256 {
			var buf bytes.Buffer
			_ = png.Encode(&buf, img)
			entries = append(entries, icoEntry{s, buf.Bytes(), true})
		} else {
			entries = append(entries, icoEntry{s, encodeBMP(img), false})
		}
	}

	if err := os.WriteFile("icon.ico", buildICO(entries), 0644); err != nil {
		panic(err)
	}
}

// drawIcon renders an import-arrow icon at the given square size.
//
//	┌──────────────┐  top bar (source file)
//	      ││
//	      ││          shaft
//	▼▼▼▼▼▼▼▼▼▼▼▼▼  chevron arrowhead
func drawIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, bgCol)
		}
	}

	s := float64(size)
	p := func(v float64) int { return int(v * s) }

	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1 && y < size; y++ {
			for x := x0; x < x1 && x < size; x++ {
				if x >= 0 && y >= 0 {
					img.SetRGBA(x, y, fgCol)
				}
			}
		}
	}

	// top bar
	fill(p(0.12), p(0.07), p(0.88), p(0.25))
	// vertical shaft
	fill(p(0.38), p(0.25), p(0.62), p(0.56))
	// arrowhead — 6 stepped rows, each narrowing by ~6% per side
	fill(p(0.06), p(0.56), p(0.94), p(0.68))
	fill(p(0.12), p(0.68), p(0.88), p(0.75))
	fill(p(0.18), p(0.75), p(0.82), p(0.81))
	fill(p(0.25), p(0.81), p(0.75), p(0.87))
	fill(p(0.31), p(0.87), p(0.69), p(0.93))
	fill(p(0.38), p(0.93), p(0.62), p(1.00))

	return img
}

// encodeBMP encodes img as the BMP image-data portion of an ICO entry (no BITMAPFILEHEADER).
func encodeBMP(img *image.RGBA) []byte {
	size := img.Bounds().Max.X
	pixBytes := size * size * 4
	andRowBytes := ((size + 31) / 32) * 4
	andBytes := size * andRowBytes
	total := 40 + pixBytes + andBytes

	b := make([]byte, total)
	le := binary.LittleEndian

	le.PutUint32(b[0:], 40)
	le.PutUint32(b[4:], uint32(size))
	le.PutUint32(b[8:], uint32(size*2)) // height×2: XOR mask + AND mask
	le.PutUint16(b[12:], 1)
	le.PutUint16(b[14:], 32)
	le.PutUint32(b[20:], uint32(pixBytes))

	pix := b[40:]
	for y := 0; y < size; y++ {
		row := size - 1 - y // BMP rows are bottom-up
		for x := 0; x < size; x++ {
			c := img.RGBAAt(x, y)
			i := (row*size + x) * 4
			pix[i+0] = c.B
			pix[i+1] = c.G
			pix[i+2] = c.R
			pix[i+3] = c.A
		}
	}
	// AND mask stays all-zero (fully opaque)

	return b
}

func buildICO(entries []icoEntry) []byte {
	count := len(entries)
	dirSize := 6 + 16*count

	offsets := make([]int, count)
	off := dirSize
	for i, e := range entries {
		offsets[i] = off
		off += len(e.data)
	}

	var buf bytes.Buffer
	le := binary.LittleEndian

	// ICONDIR
	var hdr [6]byte
	le.PutUint16(hdr[0:], 0)
	le.PutUint16(hdr[2:], 1) // type = icon
	le.PutUint16(hdr[4:], uint16(count))
	buf.Write(hdr[:])

	// ICONDIRENTRY per image
	for i, e := range entries {
		var ent [16]byte
		w, h := byte(e.size), byte(e.size)
		if e.size >= 256 {
			w, h = 0, 0 // 256 is encoded as 0 in the ICO spec
		}
		ent[0] = w
		ent[1] = h
		le.PutUint16(ent[4:], 1)  // planes
		le.PutUint16(ent[6:], 32) // bpp
		le.PutUint32(ent[8:], uint32(len(e.data)))
		le.PutUint32(ent[12:], uint32(offsets[i]))
		buf.Write(ent[:])
	}

	for _, e := range entries {
		buf.Write(e.data)
	}

	return buf.Bytes()
}
