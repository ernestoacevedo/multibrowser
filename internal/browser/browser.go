package browser

import "context"

// WindowBounds describes where a browser window should be placed.
type WindowBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

// LaunchRequest describes a browser process to start.
type LaunchRequest struct {
	Name       string
	URL        string
	ProfileDir string
	BinaryPath string
	Bounds     WindowBounds
}

// Process represents a running browser process.
type Process interface {
	PID() int
	Wait() error
	Terminate(context.Context) error
}

// Launcher starts browser processes.
type Launcher interface {
	Launch(context.Context, LaunchRequest) (Process, error)
}

// Retiler repositions existing browser windows.
type Retiler interface {
	Retile(context.Context, []WindowBounds) error
}
