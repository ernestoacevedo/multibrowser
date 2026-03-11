package screen

import (
	"testing"

	"multibrowser/internal/layout"
)

func TestParseFinderBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    layout.ScreenBounds
		wantErr bool
	}{
		{
			name:  "valid bounds",
			input: []byte("0, 0, 1512, 982\n"),
			want:  layout.ScreenBounds{Width: 1512, Height: 982},
		},
		{
			name:    "invalid shape",
			input:   []byte("0, 0, 1512\n"),
			wantErr: true,
		},
		{
			name:    "invalid numbers",
			input:   []byte("0, nope, 1512, 982\n"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFinderBounds(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseFinderBounds() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseFinderBounds() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
