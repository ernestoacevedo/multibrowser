package session

import "time"

// State represents the lifecycle state of a managed browser session.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"
	StateFailed   State = "failed"
	StateCleaned  State = "cleaned"
)

// Info stores the runtime state of a browser session.
type Info struct {
	ID         int
	Name       string
	URL        string
	ProfileDir string
	PID        int
	State      State
	Error      string
	StartedAt  time.Time
	EndedAt    time.Time
}

// Event is emitted whenever a session changes state.
type Event struct {
	Session Info
}
