package browser

import "context"

// LaunchRequest describes a browser process to start.
type LaunchRequest struct {
	Name       string
	URL        string
	ProfileDir string
	BinaryPath string
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
