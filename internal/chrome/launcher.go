package chrome

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"multibrowser/internal/browser"
)

const defaultBinaryPath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// DefaultBinaryPath returns the default macOS Chrome executable path.
func DefaultBinaryPath() string {
	return defaultBinaryPath
}

// Launcher starts Google Chrome processes with isolated profile directories.
type Launcher struct{}

// Launch starts Chrome with the requested profile directory and URL.
func (Launcher) Launch(ctx context.Context, req browser.LaunchRequest) (browser.Process, error) {
	if req.BinaryPath == "" {
		req.BinaryPath = defaultBinaryPath
	}

	if _, err := os.Stat(req.BinaryPath); err != nil {
		return nil, fmt.Errorf("chrome binary not found at %q: %w", req.BinaryPath, err)
	}

	args := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		fmt.Sprintf("--user-data-dir=%s", req.ProfileDir),
		fmt.Sprintf("--window-position=%d,%d", req.Bounds.X, req.Bounds.Y),
		fmt.Sprintf("--window-size=%d,%d", req.Bounds.Width, req.Bounds.Height),
		req.URL,
	}

	cmd := exec.CommandContext(ctx, req.BinaryPath, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	return newProcess(cmd), nil
}

type process struct {
	cmd      *exec.Cmd
	waitOnce sync.Once
	done     chan struct{}
	waitErr  error
}

func newProcess(cmd *exec.Cmd) *process {
	return &process{
		cmd:  cmd,
		done: make(chan struct{}),
	}
}

func (p *process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}

	return p.cmd.Process.Pid
}

func (p *process) Wait() error {
	p.startWait()
	<-p.done
	return p.waitErr
}

func (p *process) Terminate(ctx context.Context) error {
	if p.cmd.Process == nil {
		return nil
	}
	p.startWait()

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	select {
	case <-p.done:
		err := p.waitErr
		if err != nil && !isExitError(err) {
			return err
		}
		return nil
	case <-ctx.Done():
		if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("kill process after timeout: %w", killErr)
		}
		<-p.done
		return fmt.Errorf("terminate timed out: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("kill process after grace period: %w", killErr)
		}
		<-p.done
		return nil
	}
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func (p *process) startWait() {
	p.waitOnce.Do(func() {
		go func() {
			p.waitErr = p.cmd.Wait()
			close(p.done)
		}()
	})
}

// Retile best-effort repositions all visible Chrome windows.
func (Launcher) Retile(ctx context.Context, bounds []browser.WindowBounds) error {
	if len(bounds) == 0 {
		return nil
	}

	lines := []string{
		`tell application "Google Chrome"`,
		`if not running then return`,
	}
	for idx, bound := range bounds {
		lines = append(lines,
			fmt.Sprintf("if (count of windows) >= %d then", idx+1),
			fmt.Sprintf("set bounds of window %d to {%d, %d, %d, %d}", idx+1, bound.X, bound.Y, bound.X+bound.Width, bound.Y+bound.Height),
			"end if",
		)
	}
	lines = append(lines, "end tell")

	cmd := exec.CommandContext(ctx, "osascript", "-e", strings.Join(lines, "\n"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("retile chrome windows: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}
