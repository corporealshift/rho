package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"

	"github.com/corporealshift/rho/protocol"
)

// The tray icon is drawn here rather than shipped as an asset: it carries the
// session state and the task fraction, so a fixed picture could not say what
// the tray is for. Both containers come out of the same drawing.

// iconEdge is the icon's width and height in pixels. Plasma asks for 22 and
// Windows for 16 or 32; one 32-pixel drawing covers them, and the ICO carries
// a 16-pixel copy so Windows does not have to shrink one.
const iconEdge = 32

// supersample is how many samples per edge each pixel is made of. Coverage
// from sampling is what keeps a 32-pixel ring from turning into staircases.
const supersample = 4

// palette colours the ring's progress and the centre mark by session state.
// These are mid-tones: a tray sits on a light panel or a dark one, and a
// colour that only works on one of them is invisible half the time.
var palette = map[protocol.SessionState]color.RGBA{
	protocol.StateRunning:   {0x3f, 0xb9, 0x50, 0xff},
	protocol.StateBlocked:   {0xd2, 0x99, 0x22, 0xff},
	protocol.StatePaused:    {0xa3, 0x71, 0xf7, 0xff},
	protocol.StateError:     {0xf8, 0x51, 0x49, 0xff},
	protocol.StateIdle:      {0x8b, 0x94, 0x9e, 0xff},
	protocol.StateCompleted: {0x58, 0xa6, 0xff, 0xff},
}

var (
	plateColor   = color.RGBA{0x1b, 0x20, 0x28, 0xff}
	plateEdge    = color.RGBA{0x4a, 0x55, 0x63, 0xff}
	trackColor   = color.RGBA{0x3f, 0x4a, 0x58, 0xff}
	offlineColor = color.RGBA{0x6e, 0x76, 0x81, 0xff}
)

func stateColor(v View) color.RGBA {
	if v.Err != "" {
		return offlineColor
	}
	if c, ok := palette[v.State]; ok {
		return c
	}
	return offlineColor
}

// IconPNG renders the tray icon as PNG bytes, the container the Linux tray
// takes.
func IconPNG(v View) []byte {
	return encodePNG(drawIcon(v, iconEdge))
}

// iconKey is what the drawing depends on. Two views with the same key draw the
// same icon, so a tray can skip re-encoding one it has already shown: the ring
// needs the task counts and the centre mark needs the state, and nothing else
// in the picture changes.
func iconKey(v View) string {
	done, total := Progress(v)
	return v.Err + "\x00" + string(v.State) + "\x00" + strconv.Itoa(done) + "/" + strconv.Itoa(total)
}

// IconICO renders the same icon as an .ico with a 16- and a 32-pixel entry.
// Windows loads tray icons through LoadImage, which wants an icon container;
// PNG-compressed entries are valid inside one since Vista.
func IconICO(v View) []byte {
	type entry struct {
		edge int
		data []byte
	}
	entries := []entry{{16, encodePNG(drawIcon(v, 16))}, {iconEdge, encodePNG(drawIcon(v, iconEdge))}}

	out := &bytes.Buffer{}
	// ICONDIR: reserved, type (1 = icon), count.
	_ = binary.Write(out, binary.LittleEndian, struct {
		Reserved, Type, Count uint16
	}{0, 1, uint16(len(entries))})
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		// ICONDIRENTRY. Width and height of 0 mean 256; nothing here is that big.
		_ = binary.Write(out, binary.LittleEndian, struct {
			Width, Height, Count, Reserved uint8
			Planes, BitCount               uint16
			Size, Offset                   uint32
		}{byte(e.edge), byte(e.edge), 0, 0, 1, 32, uint32(len(e.data)), uint32(offset)})
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return out.Bytes()
}

func encodePNG(img image.Image) []byte {
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, img); err != nil {
		// An in-memory encode of an image we built cannot fail; returning an
		// empty PNG is what a tray does with a drawing it cannot read.
		return nil
	}
	return buf.Bytes()
}

// drawIcon paints one size of the icon. Geometry is in 0..1 of the edge so the
// 16- and 32-pixel versions are the same picture.
func drawIcon(v View, edge int) *image.NRGBA {
	c := newCanvas(edge)
	col := stateColor(v)

	// A dark plate with a light edge: the tray background is whatever the user
	// has, and this is the one way to be legible on all of them.
	c.paint(roundedRect(0.015, 0.015, 0.985, 0.985, 0.245), plateEdge)
	c.paint(roundedRect(0.06, 0.06, 0.94, 0.94, 0.20), plateColor)

	// The ring is the task list: track for what is left, state colour for what
	// is done, swept clockwise from 12 o'clock.
	c.paint(annulus(0.5, 0.5, 0.30, 0.42), trackColor)
	if f := Fraction(v); f > 0 {
		c.paint(arc(0.5, 0.5, 0.30, 0.42, 0, 2*math.Pi*f), col)
	}

	c.paint(stateMark(v), col)
	return c.flatten()
}

// stateMark is the glyph in the middle of the ring. Shapes, not letters: a
// 16-pixel icon has no room for a legible character.
func stateMark(v View) shape {
	if v.Err != "" {
		return hollowSquare()
	}
	switch v.State {
	case protocol.StateRunning:
		return disc(0.5, 0.5, 0.13)
	case protocol.StatePaused:
		return union(rect(0.36, 0.38, 0.45, 0.62), rect(0.55, 0.38, 0.64, 0.62))
	case protocol.StateBlocked:
		return union(capsule(0.5, 0.34, 0.5, 0.55, 0.09), disc(0.5, 0.66, 0.055))
	case protocol.StateError:
		return union(capsule(0.37, 0.37, 0.63, 0.63, 0.09), capsule(0.63, 0.37, 0.37, 0.63, 0.09))
	case protocol.StateCompleted:
		return union(capsule(0.36, 0.52, 0.46, 0.63, 0.085), capsule(0.46, 0.63, 0.66, 0.38, 0.085))
	case protocol.StateIdle:
		return annulus(0.5, 0.5, 0.09, 0.15)
	default:
		return hollowSquare()
	}
}

// shape reports whether a point in 0..1 coordinates is inside the shape.
type shape func(x, y float64) bool

func union(a, b shape) shape {
	return func(x, y float64) bool { return a(x, y) || b(x, y) }
}

func disc(cx, cy, r float64) shape {
	return func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= r*r
	}
}

func rect(x0, y0, x1, y1 float64) shape {
	return func(x, y float64) bool { return x >= x0 && x <= x1 && y >= y0 && y <= y1 }
}

func roundedRect(x0, y0, x1, y1, r float64) shape {
	return func(x, y float64) bool {
		if x < x0 || x > x1 || y < y0 || y > y1 {
			return false
		}
		cx, cy := x, y
		switch {
		case x < x0+r && y < y0+r:
			cx, cy = x0+r, y0+r
		case x > x1-r && y < y0+r:
			cx, cy = x1-r, y0+r
		case x < x0+r && y > y1-r:
			cx, cy = x0+r, y1-r
		case x > x1-r && y > y1-r:
			cx, cy = x1-r, y1-r
		default:
			return true
		}
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= r*r
	}
}

// capsule is a stroke: a rectangle with round ends.
func capsule(x0, y0, x1, y1, w float64) shape {
	return func(x, y float64) bool {
		dx, dy := x1-x0, y1-y0
		var t float64
		if d := dx*dx + dy*dy; d > 0 {
			t = ((x-x0)*dx + (y-y0)*dy) / d
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		px, py := x-x0-t*dx, y-y0-t*dy
		return px*px+py*py <= (w/2)*(w/2)
	}
}

// annulus is a full ring between two radii.
func annulus(cx, cy, r0, r1 float64) shape {
	return func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		d := dx*dx + dy*dy
		return d >= r0*r0 && d <= r1*r1
	}
}

// arc is part of a ring. Angles are radians measured clockwise from 12 o'clock,
// which is where a progress ring should start and which way it should grow on
// a screen where y increases downwards.
func arc(cx, cy, r0, r1, from, to float64) shape {
	return func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		d := dx*dx + dy*dy
		if d < r0*r0 || d > r1*r1 {
			return false
		}
		a := math.Atan2(dx, -dy)
		if a < 0 {
			a += 2 * math.Pi
		}
		return a >= from && a <= to
	}
}

func hollowSquare() shape {
	const lo, hi, w = 0.36, 0.64, 0.05
	return union(union(capsule(lo, lo, hi, lo, w), capsule(hi, lo, hi, hi, w)),
		union(capsule(hi, hi, lo, hi, w), capsule(lo, hi, lo, lo, w)))
}

// canvas is a supersampled picture: supersample² samples per pixel, each
// holding the colour of the last shape that covered it.
type canvas struct {
	edge int
	n    int
	col  []color.RGBA
	set  []bool
}

func newCanvas(edge int) *canvas {
	n := edge * supersample
	return &canvas{edge: edge, n: n, col: make([]color.RGBA, n*n), set: make([]bool, n*n)}
}

func (c *canvas) paint(s shape, col color.RGBA) {
	inv := 1 / float64(c.n)
	for j := 0; j < c.n; j++ {
		y := (float64(j) + 0.5) * inv
		for i := 0; i < c.n; i++ {
			if !s((float64(i)+0.5)*inv, y) {
				continue
			}
			k := j*c.n + i
			c.col[k], c.set[k] = col, true
		}
	}
}

// flatten averages each pixel's samples. Averaging premultiplied colours is
// what makes a half-covered pixel half transparent rather than muddy.
func (c *canvas) flatten() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, c.edge, c.edge))
	for j := 0; j < c.edge; j++ {
		for i := 0; i < c.edge; i++ {
			var r, g, b, a uint64
			for sj := 0; sj < supersample; sj++ {
				for si := 0; si < supersample; si++ {
					k := (j*supersample+sj)*c.n + i*supersample + si
					if !c.set[k] {
						continue
					}
					col := c.col[k]
					a += uint64(col.A)
					r += uint64(col.R) * uint64(col.A)
					g += uint64(col.G) * uint64(col.A)
					b += uint64(col.B) * uint64(col.A)
				}
			}
			p := j*img.Stride + i*4
			if a == 0 {
				img.Pix[p+3] = 0
				continue
			}
			const samples = uint64(supersample * supersample)
			img.Pix[p] = byte(r / a)
			img.Pix[p+1] = byte(g / a)
			img.Pix[p+2] = byte(b / a)
			img.Pix[p+3] = byte(a / samples)
		}
	}
	return img
}
