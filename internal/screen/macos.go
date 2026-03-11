package screen

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"multibrowser/internal/layout"
)

const fallbackWidth = 1440
const fallbackHeight = 900

// DetectMainScreen returns the main display size on macOS.
func DetectMainScreen(ctx context.Context) layout.ScreenBounds {
	cmd := exec.CommandContext(ctx, "osascript", "-e", `tell application "Finder" to get bounds of window of desktop`)
	output, err := cmd.Output()
	if err != nil {
		return layout.ScreenBounds{Width: fallbackWidth, Height: fallbackHeight}
	}

	bounds, err := parseFinderBounds(output)
	if err != nil {
		return layout.ScreenBounds{Width: fallbackWidth, Height: fallbackHeight}
	}

	return bounds
}

func parseFinderBounds(output []byte) (layout.ScreenBounds, error) {
	parts := strings.Split(strings.TrimSpace(string(bytes.TrimSpace(output))), ",")
	if len(parts) != 4 {
		return layout.ScreenBounds{}, fmt.Errorf("unexpected bounds output %q", string(output))
	}

	values := make([]int, 0, 4)
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return layout.ScreenBounds{}, fmt.Errorf("parse bounds value %q: %w", part, err)
		}
		values = append(values, value)
	}

	width := values[2] - values[0]
	height := values[3] - values[1]
	if width <= 0 || height <= 0 {
		return layout.ScreenBounds{}, fmt.Errorf("invalid bounds %v", values)
	}

	return layout.ScreenBounds{Width: width, Height: height}, nil
}
