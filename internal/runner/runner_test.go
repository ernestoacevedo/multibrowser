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

type fakeLauncher struct {
	processes []browser.Process
	index     int
}

func (l *fakeLauncher) Launch(context.Context, browser.LaunchRequest) (browser.Process, error) {
	proc := l.processes[l.index]
	l.index++
	return proc, nil
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
