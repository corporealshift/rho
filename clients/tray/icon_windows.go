//go:build windows

package tray

// platformIcon is the bytes this desktop's tray wants. Windows hands the path
// to LoadImage, which wants an icon container rather than a bare PNG.
func platformIcon(v View) []byte {
	return IconICO(v)
}
