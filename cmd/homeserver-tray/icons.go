package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

const iconSize = 32

var (
	white  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	green  = color.RGBA{0x1e, 0x9e, 0x4a, 0xff}
	amber  = color.RGBA{0xf2, 0xa3, 0x00, 0xff}
	red    = color.RGBA{0xd9, 0x30, 0x25, 0xff}
	orange = color.RGBA{0xe8, 0x71, 0x0a, 0xff}
	grey   = color.RGBA{0x9a, 0xa0, 0xa6, 0xff}
)

// stateColor is the disc colour and badge glyph colour for a state.
func stateColor(s state) color.RGBA {
	switch s {
	case stRunning:
		return green
	case stBusy:
		return amber
	case stStopped:
		return red
	case stProblem:
		return orange
	}
	return grey
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, c)
		}
	}
}

// thickLine draws a line of the given half-width between two points.
func thickLine(img *image.RGBA, x0, y0, x1, y1, hw float64, c color.RGBA) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) * 2
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		fillCircle(img, x0+(x1-x0)*t, y0+(y1-y0)*t, hw, c)
	}
}

// drawIcon draws a house in a disc coloured for the state, with a badge in the
// bottom-right corner: tick (running), dots (starting/stopping), square
// (stopped), exclamation mark (not answering), dash (not installed).
func drawIcon(s state) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	c := stateColor(s)
	fillCircle(img, 15.5, 15.5, 15.5, c)
	for y := 7; y < 22; y++ { // roof triangle + body
		half := 11
		if y < 14 {
			half = (y - 7) * 2
		}
		for x := 15 - half; x < 15+half; x++ {
			if y >= 14 && (x < 8 || x > 21) {
				continue
			}
			img.Set(x, y, white)
		}
	}
	if s == stChecking {
		return img
	}
	// Badge: white ring, then a disc in the state colour, then a white glyph.
	fillCircle(img, 23.5, 23.5, 8.5, white)
	fillCircle(img, 23.5, 23.5, 7, c)
	switch s {
	case stRunning:
		thickLine(img, 19.5, 23.5, 22.5, 26.5, 1.2, white)
		thickLine(img, 22.5, 26.5, 28, 20.5, 1.2, white)
	case stBusy:
		for _, x := range []int{19, 23, 27} {
			fillRect(img, x-1, 23, x+1, 25, white)
		}
	case stStopped:
		fillRect(img, 20, 20, 27, 27, white)
	case stProblem:
		fillRect(img, 22, 18, 25, 24, white)
		fillRect(img, 22, 26, 25, 29, white)
	case stMissing:
		fillRect(img, 19, 22, 28, 25, white)
	}
	return img
}

// icoBytes wraps a 32x32 PNG in a one-image ICO container.
func icoBytes(img image.Image) []byte {
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, 1})
	ico.Write([]byte{iconSize, iconSize, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
	_ = binary.Write(&ico, binary.LittleEndian, uint32(pngBuf.Len()))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(pngBuf.Bytes())
	return ico.Bytes()
}

// icons renders every state once at startup.
func icons() map[state][]byte {
	m := map[state][]byte{}
	for _, s := range []state{stChecking, stRunning, stBusy, stStopped, stProblem, stMissing} {
		m[s] = icoBytes(drawIcon(s))
	}
	return m
}
