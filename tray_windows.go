//go:build windows

package main

import (
	"encoding/binary"

	"github.com/getlantern/systray"
)

func startTray() {
	systray.Run(onReady, func() {})
}

func quitApp() {
	systray.Quit()
}

func onReady() {
	systray.SetIcon(makeIcon())
	systray.SetTooltip("DriveKit Importer")

	mOpen := systray.AddMenuItem("Open", "Open DriveKit Importer in browser")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit DriveKit Importer")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser("http://localhost:7432")
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}

// makeIcon returns a minimal 16×16 ICO with the app's lime-green accent colour.
func makeIcon() []byte {
	const (
		w, h        = 16, 16
		pixBytes    = w * h * 4       // 32-bit BGRA
		andRowBytes = 4               // ceil(16/32)*4, one DWORD per row
		andBytes    = h * andRowBytes // 1-bpp AND mask
		imgBytes    = 40 + pixBytes + andBytes
		totalBytes  = 6 + 16 + imgBytes
	)

	b := make([]byte, totalBytes)
	le := binary.LittleEndian

	// ICONDIR
	le.PutUint16(b[0:], 0) // reserved
	le.PutUint16(b[2:], 1) // type = icon
	le.PutUint16(b[4:], 1) // image count

	// ICONDIRENTRY
	b[6] = w
	b[7] = h
	b[8] = 0 // color count (0 = full colour)
	b[9] = 0 // reserved
	le.PutUint16(b[10:], 1)
	le.PutUint16(b[12:], 32)
	le.PutUint32(b[14:], imgBytes)
	le.PutUint32(b[18:], 22) // image data starts at offset 6+16

	// BITMAPINFOHEADER
	img := b[22:]
	le.PutUint32(img[0:], 40)
	le.PutUint32(img[4:], w)
	le.PutUint32(img[8:], h*2) // height×2: XOR mask + AND mask stacked
	le.PutUint16(img[12:], 1)
	le.PutUint16(img[14:], 32)
	le.PutUint32(img[16:], 0) // BI_RGB
	le.PutUint32(img[20:], pixBytes)
	// remaining fields (pels/metre, colours used) stay zero

	// XOR mask — solid lime-green #d4ff47 (BGRA, bottom-up row order)
	pix := img[40:]
	for i := 0; i < w*h; i++ {
		pix[i*4+0] = 0x47 // B
		pix[i*4+1] = 0xff // G
		pix[i*4+2] = 0xd4 // R
		pix[i*4+3] = 0xff // A (fully opaque)
	}
	// AND mask stays all-zero (fully visible)

	return b
}
