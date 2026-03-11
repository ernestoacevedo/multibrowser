package layout

import "math"

// ScreenBounds represents the available screen size for tiling.
type ScreenBounds struct {
	Width  int
	Height int
}

// WindowBounds describes a single tile within the screen grid.
type WindowBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

// TileWindows computes a grid layout that covers the full screen.
func TileWindows(count int, screen ScreenBounds) []WindowBounds {
	if count <= 0 || screen.Width <= 0 || screen.Height <= 0 {
		return nil
	}

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	rows := int(math.Ceil(float64(count) / float64(cols)))

	bounds := make([]WindowBounds, 0, count)
	for i := 0; i < count; i++ {
		row := i / cols
		col := i % cols

		x := screen.Width * col / cols
		y := screen.Height * row / rows
		nextX := screen.Width * (col + 1) / cols
		nextY := screen.Height * (row + 1) / rows

		bounds = append(bounds, WindowBounds{
			X:      x,
			Y:      y,
			Width:  nextX - x,
			Height: nextY - y,
		})
	}

	return bounds
}
