package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"multibrowser/internal/browser"
	"multibrowser/internal/layout"
	"multibrowser/internal/session"
)

func TestValidateBinaryPath(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "chrome")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	file.Close()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "empty ok", path: "", wantErr: false},
		{name: "file ok", path: file.Name(), wantErr: false},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing"), wantErr: true},
		{name: "directory rejected", path: t.TempDir(), wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBinaryPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBinaryPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManagerLifecycle(t *testing.T) {
	t.Parallel()

	launcher := fakeLauncher{
		processes: []browser.Process{
			&fakeProcess{pid: 101},
			&fakeProcess{pid: 102},
		},
	}
	manager := NewManager(&launcher)

	events := make(chan session.Event, 10)
	if err := manager.Start(context.Background(), Options{
		URL:      "https://example.com",
		Count:    2,
		BaseName: "test",
		Screen:   layout.ScreenBounds{Width: 1200, Height: 800},
	}, events); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	result := manager.Wait(context.Background())
	if len(result.Sessions) != 2 {
		t.Fatalf("Wait() sessions = %d, want 2", len(result.Sessions))
	}

	for _, item := range result.Sessions {
		if item.State != session.StateCleaned {
			t.Fatalf("session %d final state = %s, want %s", item.ID, item.State, session.StateCleaned)
		}
		if _, err := os.Stat(item.ProfileDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("profile dir %s should be removed, stat err = %v", item.ProfileDir, err)
		}
	}

	requests := launcher.snapshotRequests()
	if len(requests) != 2 {
		t.Fatalf("launcher requests = %d, want 2", len(requests))
	}

	wantTiles := []browser.WindowBounds{
		{X: 0, Y: 0, Width: 600, Height: 800},
		{X: 600, Y: 0, Width: 600, Height: 800},
	}
	for i, req := range requests {
		if req.Bounds != wantTiles[i] {
			t.Fatalf("request %d bounds = %+v, want %+v", i, req.Bounds, wantTiles[i])
		}
	}

	if len(launcher.snapshotRetiles()) == 0 {
		t.Fatal("expected at least one retile call")
	}
}

func TestManagerCancellationTerminatesProcesses(t *testing.T) {
	t.Parallel()

	proc := &fakeProcess{pid: 201, blockWait: true}
	manager := NewManager(&fakeLauncher{processes: []browser.Process{proc}})
	events := make(chan session.Event, 4)

	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx, Options{
		URL:      "https://example.com",
		Count:    1,
		BaseName: "cancel",
		Screen:   layout.ScreenBounds{Width: 1200, Height: 800},
	}, events); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	cancel()
	result := manager.Wait(context.Background())
	if len(result.Sessions) != 1 {
		t.Fatalf("Wait() sessions = %d, want 1", len(result.Sessions))
	}
	if !proc.terminated {
		t.Fatal("expected process to be terminated")
	}
}

func TestManagerAddAndTerminateSession(t *testing.T) {
	t.Parallel()

	proc1 := &fakeProcess{pid: 301, blockWait: true}
	proc2 := &fakeProcess{pid: 302, blockWait: true}
	launcher := &fakeLauncher{processes: []browser.Process{proc1, proc2}}
	manager := NewManager(launcher)
	events := make(chan session.Event, 16)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Start(ctx, Options{
		URL:      "https://example.com",
		Count:    1,
		BaseName: "live",
		Screen:   layout.ScreenBounds{Width: 1200, Height: 800},
	}, events); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := manager.Add(ctx, 1); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if len(launcher.snapshotRequests()) != 2 {
		t.Fatalf("launcher requests = %d, want 2", len(launcher.snapshotRequests()))
	}

	if err := manager.TerminateSession(ctx, 1); err != nil {
		t.Fatalf("TerminateSession() error = %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if proc1.isTerminated() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !proc1.isTerminated() {
		t.Fatal("expected first process to terminate")
	}

	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(launcher.snapshotRetiles()) >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("retile calls = %d, want at least 2", len(launcher.snapshotRetiles()))
}

type fakeLauncher struct {
	processes   []browser.Process
	index       int
	requests    []browser.LaunchRequest
	retileCalls [][]browser.WindowBounds
	mu          sync.Mutex
}

func (l *fakeLauncher) Launch(_ context.Context, req browser.LaunchRequest) (browser.Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, req)
	proc := l.processes[l.index]
	l.index++
	return proc, nil
}

func (l *fakeLauncher) Retile(_ context.Context, bounds []browser.WindowBounds) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	copied := append([]browser.WindowBounds(nil), bounds...)
	l.retileCalls = append(l.retileCalls, copied)
	return nil
}

func (l *fakeLauncher) snapshotRequests() []browser.LaunchRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]browser.LaunchRequest(nil), l.requests...)
}

func (l *fakeLauncher) snapshotRetiles() [][]browser.WindowBounds {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([][]browser.WindowBounds(nil), l.retileCalls...)
}

type fakeProcess struct {
	pid        int
	waitErr    error
	blockWait  bool
	terminated bool
	mu         sync.RWMutex
}

func (p *fakeProcess) PID() int {
	return p.pid
}

func (p *fakeProcess) Wait() error {
	if p.blockWait {
		for !p.isTerminated() {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return p.waitErr
}

func (p *fakeProcess) Terminate(context.Context) error {
	p.mu.Lock()
	p.terminated = true
	p.mu.Unlock()
	return nil
}

func (p *fakeProcess) isTerminated() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.terminated
}
