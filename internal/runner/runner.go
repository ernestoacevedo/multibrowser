package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"multibrowser/internal/browser"
	"multibrowser/internal/layout"
	"multibrowser/internal/session"
)

// Options controls how sessions are launched and named.
type Options struct {
	URL        string
	Count      int
	BaseName   string
	BinaryPath string
	Screen     layout.ScreenBounds
}

// Result contains the final session state and any cleanup warnings.
type Result struct {
	Sessions []session.Info
	Warnings []error
}

// Manager launches and tracks multiple browser sessions.
type Manager struct {
	launcher browser.Launcher
	retiler  browser.Retiler

	mu       sync.Mutex
	opsMu    sync.Mutex
	watchWG  sync.WaitGroup
	sessions map[int]*managedSession
	nextID   int
	opts     Options
	ctx      context.Context
	events   chan<- session.Event
}

// NewManager returns a manager for the provided launcher.
func NewManager(launcher browser.Launcher) *Manager {
	manager := &Manager{
		launcher: launcher,
		sessions: make(map[int]*managedSession),
		nextID:   1,
	}
	if retiler, ok := launcher.(browser.Retiler); ok {
		manager.retiler = retiler
	}
	return manager
}

type managedSession struct {
	info      session.Info
	process   browser.Process
	cleanOnce sync.Once
}

// Start launches the initial batch of browser sessions and streams updates to events.
func (m *Manager) Start(ctx context.Context, opts Options, events chan<- session.Event) error {
	if err := validateOptions(opts); err != nil {
		return err
	}

	m.mu.Lock()
	m.ctx = ctx
	m.opts = opts
	m.events = events
	m.mu.Unlock()

	return m.Add(ctx, opts.Count)
}

// Add launches additional browser sessions using the stored options.
func (m *Manager) Add(ctx context.Context, count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be greater than zero")
	}

	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	opts := m.opts
	startID := m.nextID
	activeIDs := m.activeSessionIDsLocked()
	total := len(activeIDs) + count
	tiles := layout.TileWindows(total, opts.Screen)
	if len(tiles) != total {
		m.mu.Unlock()
		return fmt.Errorf("unable to compute window tiles for %d sessions", total)
	}
	byID := make(map[int]layout.WindowBounds, total)
	for index, id := range activeIDs {
		byID[id] = tiles[index]
	}
	m.nextID += count
	m.mu.Unlock()

	newSessions := make([]*managedSession, 0, count)
	for offset := 0; offset < count; offset++ {
		id := startID + offset
		info, proc, err := m.launchOne(ctx, opts, id, tiles[len(activeIDs)+offset])
		if err != nil {
			return err
		}
		newSessions = append(newSessions, &managedSession{info: info, process: proc})
	}

	m.mu.Lock()
	for _, ms := range newSessions {
		m.sessions[ms.info.ID] = ms
	}
	m.mu.Unlock()

	for _, ms := range newSessions {
		m.emit(ms.info)
		m.watchWG.Add(1)
		go m.watch(ms)
	}

	m.retileActive(ctx, byID)
	return nil
}

// TerminateSession stops a single browser session.
func (m *Manager) TerminateSession(ctx context.Context, id int) error {
	m.mu.Lock()
	ms, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %d not found", id)
	}
	switch ms.info.State {
	case session.StateStarting, session.StateRunning:
		ms.info.State = session.StateStopping
		m.emit(ms.info)
	default:
		m.mu.Unlock()
		return fmt.Errorf("session %d is not active", id)
	}
	process := ms.process
	m.mu.Unlock()

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := process.Terminate(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("terminate session %d: %w", id, err)
	}
	return nil
}

func (m *Manager) launchOne(ctx context.Context, opts Options, id int, tile layout.WindowBounds) (session.Info, browser.Process, error) {
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
		X:          tile.X,
		Y:          tile.Y,
		Width:      tile.Width,
		Height:     tile.Height,
		State:      session.StateStarting,
		StartedAt:  time.Now(),
	}

	proc, err := m.launcher.Launch(ctx, browser.LaunchRequest{
		Name:       name,
		URL:        opts.URL,
		ProfileDir: profileDir,
		BinaryPath: opts.BinaryPath,
		Bounds: browser.WindowBounds{
			X:      tile.X,
			Y:      tile.Y,
			Width:  tile.Width,
			Height: tile.Height,
		},
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

func (m *Manager) watch(ms *managedSession) {
	defer m.watchWG.Done()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ms.process.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-errCh:
	case <-m.ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		terminateErr := ms.process.Terminate(stopCtx)
		waitErr = <-errCh
		if terminateErr != nil && !errors.Is(terminateErr, context.Canceled) {
			waitErr = errors.Join(waitErr, terminateErr)
		}
	}

	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	ms.info.EndedAt = time.Now()
	if waitErr != nil {
		ms.info.State = session.StateFailed
		ms.info.Error = waitErr.Error()
	} else {
		ms.info.State = session.StateExited
		ms.info.Error = ""
	}
	m.emit(ms.info)

	cleanupErr := m.cleanupSession(ms)
	if cleanupErr != nil {
		ms.info.Error = appendError(ms.info.Error, cleanupErr)
		ms.info.State = session.StateFailed
	} else {
		ms.info.State = session.StateCleaned
	}

	activeIDs := m.activeSessionIDsLocked()
	nextTiles := make(map[int]layout.WindowBounds, len(activeIDs))
	if len(activeIDs) > 0 {
		tiles := layout.TileWindows(len(activeIDs), m.opts.Screen)
		for idx, id := range activeIDs {
			nextTiles[id] = tiles[idx]
		}
	}
	m.mu.Unlock()

	m.emit(ms.info)
	m.retileActive(m.ctx, nextTiles)
}

func (m *Manager) cleanupSession(ms *managedSession) error {
	var cleanupErr error
	ms.cleanOnce.Do(func() {
		cleanupErr = os.RemoveAll(ms.info.ProfileDir)
	})
	return cleanupErr
}

func (m *Manager) emit(info session.Info) {
	if m.events == nil {
		return
	}
	m.events <- session.Event{Session: info}
}

func (m *Manager) retileActive(ctx context.Context, byID map[int]layout.WindowBounds) {
	if m.retiler == nil || len(byID) == 0 {
		return
	}

	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	bounds := make([]browser.WindowBounds, 0, len(ids))
	m.mu.Lock()
	for _, id := range ids {
		tile := byID[id]
		if ms, ok := m.sessions[id]; ok {
			ms.info.X = tile.X
			ms.info.Y = tile.Y
			ms.info.Width = tile.Width
			ms.info.Height = tile.Height
			m.emit(ms.info)
		}
		bounds = append(bounds, browser.WindowBounds{
			X:      tile.X,
			Y:      tile.Y,
			Width:  tile.Width,
			Height: tile.Height,
		})
	}
	m.mu.Unlock()

	go func() {
		retileCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = m.retiler.Retile(retileCtx, bounds)

		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delayedCtx, delayedCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer delayedCancel()
			_ = m.retiler.Retile(delayedCtx, bounds)
		}
	}()
}

func (m *Manager) activeSessionIDsLocked() []int {
	ids := make([]int, 0, len(m.sessions))
	for id, ms := range m.sessions {
		switch ms.info.State {
		case session.StateStarting, session.StateRunning, session.StateStopping:
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

// Snapshot returns the current view of all sessions.
func (m *Manager) Snapshot() []session.Info {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]int, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	items := make([]session.Info, 0, len(ids))
	for _, id := range ids {
		items = append(items, m.sessions[id].info)
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
				m.watchWG.Wait()
				return Result{Sessions: m.Snapshot()}
			}
		}
	}
}

func allTerminal(items []session.Info) bool {
	for _, item := range items {
		switch item.State {
		case session.StateStarting, session.StateRunning, session.StateStopping:
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

func validateOptions(opts Options) error {
	if opts.Count <= 0 {
		return fmt.Errorf("count must be greater than zero")
	}
	if opts.URL == "" {
		return fmt.Errorf("url is required")
	}
	if opts.BaseName == "" {
		return fmt.Errorf("base name is required")
	}
	if opts.Screen.Width <= 0 || opts.Screen.Height <= 0 {
		return fmt.Errorf("screen bounds must be greater than zero")
	}
	return nil
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
