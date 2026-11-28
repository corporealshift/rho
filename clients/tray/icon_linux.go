//go:build linux

package tray

// platformIcon is the bytes this desktop's tray wants. StatusNotifierItem takes
// a pixmap, so a PNG is enough and the smaller one is better.
func platformIcon(v View) []byte {
	return IconPNG(v)
}
