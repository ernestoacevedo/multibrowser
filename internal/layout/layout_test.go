package layout

import "testing"

func TestTileWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		count  int
		screen ScreenBounds
		want   []WindowBounds
	}{
		{
			name:   "single window fills screen",
			count:  1,
			screen: ScreenBounds{Width: 1200, Height: 800},
			want: []WindowBounds{
				{X: 0, Y: 0, Width: 1200, Height: 800},
			},
		},
		{
			name:   "two windows split screen vertically",
			count:  2,
			screen: ScreenBounds{Width: 1200, Height: 800},
			want: []WindowBounds{
				{X: 0, Y: 0, Width: 600, Height: 800},
				{X: 600, Y: 0, Width: 600, Height: 800},
			},
		},
		{
			name:   "three windows use two by two grid",
			count:  3,
			screen: ScreenBounds{Width: 1200, Height: 800},
			want: []WindowBounds{
				{X: 0, Y: 0, Width: 600, Height: 400},
				{X: 600, Y: 0, Width: 600, Height: 400},
				{X: 0, Y: 400, Width: 600, Height: 400},
			},
		},
		{
			name:   "zero count returns empty layout",
			count:  0,
			screen: ScreenBounds{Width: 1200, Height: 800},
			want:   nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := TileWindows(tt.count, tt.screen)
			if len(got) != len(tt.want) {
				t.Fatalf("TileWindows() len = %d, want %d", len(got), len(tt.want))
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("TileWindows()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
