//go:build ignore

package main

import (
	"encoding/binary"
	"os"
)

func main() {
	colors := map[string][4]byte{
		"assets/icon-daemon.ico": {0x3B, 0x82, 0xF6, 0xFF}, // blue
		"assets/icon-cli.ico":    {0x22, 0xC5, 0x5E, 0xFF}, // green
		"assets/icon-gui.ico":    {0xF5, 0x9E, 0x0B, 0xFF}, // amber/orange
	}

	os.Mkdir("assets", 0755)

	for path, c := range colors {
		writeICO(path, c)
	}
}

func writeICO(path string, color [4]byte) {
	// 32x32, 32-bit BGRA
	const w, h = 32, 32
	bmpSize := 40 + w*h*4 // BITMAPINFOHEADER + pixels
	icoSize := 6 + 16 + bmpSize

	buf := make([]byte, icoSize)

	// ICO header
	binary.LittleEndian.PutUint16(buf[0:], 0)   // reserved
	binary.LittleEndian.PutUint16(buf[2:], 1)   // type = ICO
	binary.LittleEndian.PutUint16(buf[4:], 1)   // count = 1 image

	// ICO entry
	buf[6] = w       // width (0 = 256)
	buf[7] = h       // height (0 = 256)
	buf[8] = 0       // color palette
	buf[9] = 0       // reserved
	binary.LittleEndian.PutUint16(buf[10:], 1)  // planes
	binary.LittleEndian.PutUint16(buf[12:], 32) // bpp
	binary.LittleEndian.PutUint32(buf[14:], uint32(bmpSize))
	binary.LittleEndian.PutUint32(buf[18:], 22) // offset to BMP data

	// BITMAPINFOHEADER (at offset 22)
	bmp := buf[22:]
	binary.LittleEndian.PutUint32(bmp[0:], 40)   // header size
	binary.LittleEndian.PutUint32(bmp[4:], w)     // width
	binary.LittleEndian.PutUint32(bmp[8:], h*2)   // height (doubled for ICO: top-down + bottom-up)
	binary.LittleEndian.PutUint16(bmp[12:], 1)    // planes
	binary.LittleEndian.PutUint16(bmp[14:], 32)   // bpp
	// rest of header is zeros

	// Pixel data (BGRA, bottom-up)
	pixels := bmp[40:]
	for y := 0; y < h; y++ {
		row := pixels[(h-1-y)*w*4 : (h-y)*w*4]
		for x := 0; x < w; x++ {
			px := row[x*4:]
			px[0] = color[0] // B
			px[1] = color[1] // G
			px[2] = color[2] // R
			px[3] = color[3] // A
		}
	}

	os.WriteFile(path, buf, 0644)
}
