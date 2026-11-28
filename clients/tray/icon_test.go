package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/corporealshift/rho/protocol"
)

func decodeIcon(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("icon bytes do not decode: %v", err)
	}
	return img
}

func TestIconPNGIsTheTraySize(t *testing.T) {
	v := View{State: protocol.StateRunning, Tasks: []protocol.Task{{Status: protocol.TaskDone}}}
	img := decodeIcon(t, IconPNG(v))
	if got := img.Bounds().Dx(); got != iconEdge {
		t.Errorf("width = %d, want %d", got, iconEdge)
	}
	if got := img.Bounds().Dy(); got != iconEdge {
		t.Errorf("height = %d, want %d", got, iconEdge)
	}
}

func TestIconCarriesStateAndProgress(t *testing.T) {
	// The top of the ring is where progress starts, and the middle is where
	// the state mark is. Both are sampled at the 32-pixel size.
	running := decodeIcon(t, IconPNG(View{State: protocol.StateRunning,
		Tasks: []protocol.Task{{Status: protocol.TaskDone}, {Status: protocol.TaskDone}}}))
	empty := decodeIcon(t, IconPNG(View{State: protocol.StateRunning,
		Tasks: []protocol.Task{{Status: protocol.TaskPending}, {Status: protocol.TaskPending}}}))

	if same(running.At(16, 4), empty.At(16, 4)) {
		t.Error("a session with every task done looks the same at the top of the ring as one with none")
	}
	if !same(running.At(16, 16), stateColor(View{State: protocol.StateRunning})) {
		t.Errorf("centre of a running icon = %v, want the running colour", running.At(16, 16))
	}
	if !same(empty.At(16, 16), stateColor(View{State: protocol.StateRunning})) {
		t.Error("the centre mark changed between two running sessions")
	}
}

func TestIconOfflineIsNotAProgressRing(t *testing.T) {
	off := decodeIcon(t, IconPNG(offline("daemon unreachable")))
	// The ring's track is the same colour everywhere: no arc, no state colour.
	if !same(off.At(16, 4), off.At(16, 27)) {
		t.Errorf("offline ring differs across the ring: %v vs %v", off.At(16, 4), off.At(16, 27))
	}
	if !same(off.At(16, 4), trackColor) {
		t.Errorf("offline ring = %v, want the track colour", off.At(16, 4))
	}
}

func TestIconEveryStateDraws(t *testing.T) {
	states := []protocol.SessionState{
		protocol.StateRunning, protocol.StateBlocked, protocol.StatePaused,
		protocol.StateError, protocol.StateIdle, protocol.StateCompleted,
		protocol.SessionState("unknown"),
	}
	for _, s := range states {
		v := View{State: s, Tasks: []protocol.Task{{Status: protocol.TaskDone}, {Status: protocol.TaskInProgress}}}
		img := decodeIcon(t, IconPNG(v))
		if img.Bounds().Dx() != iconEdge {
			t.Errorf("%s: width = %d", s, img.Bounds().Dx())
		}
		if !opaque(img.At(16, 16)) {
			t.Errorf("%s: the centre is transparent", s)
		}
		if opaque(img.At(0, 0)) {
			t.Errorf("%s: the corner should be outside the plate", s)
		}
	}
}

func TestIconICOHoldsBothSizes(t *testing.T) {
	ico := IconICO(View{State: protocol.StateCompleted, Tasks: []protocol.Task{{Status: protocol.TaskDone}}})
	if len(ico) < 6+2*16 {
		t.Fatalf("ico is %d bytes, too short to hold a header and two entries", len(ico))
	}
	var dir struct{ Reserved, Type, Count uint16 }
	if err := binary.Read(bytes.NewReader(ico), binary.LittleEndian, &dir); err != nil {
		t.Fatal(err)
	}
	if dir.Reserved != 0 || dir.Type != 1 {
		t.Errorf("ICONDIR = %+v, want reserved 0 and type 1", dir)
	}
	if dir.Count != 2 {
		t.Fatalf("ICONDIR count = %d, want 2", dir.Count)
	}

	// The headers run from byte 6; the image data starts after the last one.
	at := 6
	want := 6 + 16*int(dir.Count)
	for i := 0; i < int(dir.Count); i++ {
		var e struct {
			Width, Height, Count, Reserved uint8
			Planes, BitCount               uint16
			Size, Offset                   uint32
		}
		if err := binary.Read(bytes.NewReader(ico[at:]), binary.LittleEndian, &e); err != nil {
			t.Fatal(err)
		}
		at += 16

		edge := 16
		if i == 1 {
			edge = iconEdge
		}
		if int(e.Width) != edge || int(e.Height) != edge {
			t.Errorf("entry %d is %dx%d, want %dx%d", i, e.Width, e.Height, edge, edge)
		}
		if e.BitCount != 32 || e.Planes != 1 {
			t.Errorf("entry %d is %d-plane %d-bit, want 1-plane 32-bit", i, e.Planes, e.BitCount)
		}
		if int(e.Offset) != want {
			t.Errorf("entry %d offset = %d, want %d", i, e.Offset, want)
		}
		if int(e.Size) > len(ico)-want {
			t.Fatalf("entry %d claims %d bytes, only %d remain", i, e.Size, len(ico)-want)
		}
		img, _, err := image.Decode(bytes.NewReader(ico[want : want+int(e.Size)]))
		if err != nil {
			t.Fatalf("entry %d image does not decode: %v", i, err)
		}
		if img.Bounds().Dx() != edge {
			t.Errorf("entry %d image is %d wide, want %d", i, img.Bounds().Dx(), edge)
		}
		want += int(e.Size)
	}
	if want != len(ico) {
		t.Errorf("entries cover %d of %d bytes", want, len(ico))
	}
}

func TestIconPNGIsDeterministic(t *testing.T) {
	v := View{State: protocol.StateBlocked, Tasks: []protocol.Task{{Status: protocol.TaskBlocked}}}
	if a, b := IconPNG(v), IconPNG(v); !bytes.Equal(a, b) {
		t.Error("the same view drew two different icons")
	}
}

func same(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func opaque(c color.Color) bool {
	_, _, _, a := c.RGBA()
	return a > 0xf000
}
