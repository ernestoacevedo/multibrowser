package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"multibrowser/internal/browser"
	"multibrowser/internal/session"
)

// Options controls how sessions are launched and named.
type Options struct {
	URL        string
	Count      int
	BaseName   string
	BinaryPath string
}

// Result contains the final session state and any cleanup warnings.
type Result struct {
	Sessions []session.Info
	Warnings []error
}

// Manager launches and tracks multiple browser sessions.
type Manager struct {
	launcher browser.Launcher
	mu       sync.Mutex
	sessions map[int]*managedSession
}

// NewManager returns a manager for the provided launcher.
func NewManager(launcher browser.Launcher) *Manager {
	return &Manager{
		launcher: launcher,
		sessions: make(map[int]*managedSession),
	}
}

type managedSession struct {
	info      session.Info
	process   browser.Process
	cleanOnce sync.Once
}

// Start launches all requested browser sessions and streams updates to events.
func (m *Manager) Start(ctx context.Context, opts Options, events chan<- session.Event) error {
	if opts.Count <= 0 {
		return fmt.Errorf("count must be greater than zero")
	}
	if opts.URL == "" {
		return fmt.Errorf("url is required")
	}
	if opts.BaseName == "" {
		opts.BaseName = "session"
	}

	for i := 1; i <= opts.Count; i++ {
		info, proc, err := m.launchOne(ctx, opts, i)
		if err != nil {
			return err
		}

		ms := &managedSession{info: info, process: proc}
		m.mu.Lock()
		m.sessions[i] = ms
		m.mu.Unlock()

		m.emit(events, ms.info)
		go m.watch(ctx, ms, events)
	}

	return nil
}

func (m *Manager) emit(events chan<- session.Event, info session.Info) {
	if events == nil {
		return
	}
	events <- session.Event{Session: info}
}

func (m *Manager) launchOne(ctx context.Context, opts Options, id int) (session.Info, browser.Process, error) {
	name := fmt.Sprintf("%s-%d", opts.BaseName, id)
	profileDir, err := os.MkdirTemp("", "multibrowser-"+name+"-")
	if err != nil {
		return session.Info{}, nil, fmt.Errorf("create temp profile for %s: %w", name, err)
	}

	info := session.Info{
		ID:         id,
		Name:       name,
		URL:        opts.URL,
		ProfileDir: profileDir,
		State:      session.StateStarting,
		StartedAt:  time.Now(),
	}

	proc, err := m.launcher.Launch(ctx, browser.LaunchRequest{
		Name:       name,
		URL:        opts.URL,
		ProfileDir: profileDir,
		BinaryPath: opts.BinaryPath,
	})
	if err != nil {
		if removeErr := os.RemoveAll(profileDir); removeErr != nil {
			return session.Info{}, nil, errors.Join(err, fmt.Errorf("cleanup failed for %s: %w", profileDir, removeErr))
		}
		return session.Info{}, nil, err
	}

	info.PID = proc.PID()
	info.State = session.StateRunning
	return info, proc, nil
}

func (m *Manager) watch(ctx context.Context, ms *managedSession, events chan<- session.Event) {
	errCh := make(chan error, 1)
	go func() {
		errCh <- ms.process.Wait()
	}()

	select {
	case err := <-errCh:
		m.finish(ms, err, events)
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		terminateErr := ms.process.Terminate(stopCtx)
		waitErr := <-errCh
		if terminateErr != nil && !errors.Is(terminateErr, context.Canceled) {
			m.finish(ms, errors.Join(waitErr, terminateErr), events)
			return
		}
		m.finish(ms, waitErr, events)
	}
}

func (m *Manager) finish(ms *managedSession, waitErr error, events chan<- session.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms.info.EndedAt = time.Now()
	if waitErr != nil {
		ms.info.State = session.StateFailed
		ms.info.Error = waitErr.Error()
	} else {
		ms.info.State = session.StateExited
		ms.info.Error = ""
	}
	m.emit(events, ms.info)

	cleanupErr := m.cleanupSession(ms)
	if cleanupErr != nil {
		ms.info.Error = appendError(ms.info.Error, cleanupErr)
		ms.info.State = session.StateFailed
	} else {
		ms.info.State = session.StateCleaned
	}
	m.emit(events, ms.info)
}

func (m *Manager) cleanupSession(ms *managedSession) error {
	var cleanupErr error
	ms.cleanOnce.Do(func() {
		cleanupErr = os.RemoveAll(ms.info.ProfileDir)
	})
	return cleanupErr
}

// Snapshot returns the current view of all sessions.
func (m *Manager) Snapshot() []session.Info {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]session.Info, 0, len(m.sessions))
	for i := 1; i <= len(m.sessions); i++ {
		if ms, ok := m.sessions[i]; ok {
			items = append(items, ms.info)
		}
	}
	return items
}

// Wait blocks until all sessions have reached a terminal state.
func (m *Manager) Wait(ctx context.Context) Result {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return Result{Sessions: m.Snapshot()}
		case <-ticker.C:
			sessions := m.Snapshot()
			if len(sessions) == 0 {
				continue
			}
			if allTerminal(sessions) {
				return Result{Sessions: sessions}
			}
		}
	}
}

func allTerminal(items []session.Info) bool {
	for _, item := range items {
		switch item.State {
		case session.StateStarting, session.StateRunning:
			return false
		}
	}
	return true
}

func appendError(base string, err error) string {
	if err == nil {
		return base
	}
	if base == "" {
		return err.Error()
	}
	return base + "; " + err.Error()
}

// ValidateBinaryPath ensures the supplied binary path points to a file.
func ValidateBinaryPath(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat chrome path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("chrome path must be a file")
	}
	return nil
}

// BuildDefaultBaseName returns a stable fallback name for temporary sessions.
func BuildDefaultBaseName(base string) string {
	if base != "" {
		return base
	}
	return filepath.Base("session")
}
