package chrome

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		fmt.Sprintf("--user-data-dir=%s", req.ProfileDir),
		req.URL,
	}

	cmd := exec.CommandContext(ctx, req.BinaryPath, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	return &process{cmd: cmd}, nil
}

type process struct {
	cmd *exec.Cmd
}

func (p *process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}

	return p.cmd.Process.Pid
}

func (p *process) Wait() error {
	return p.cmd.Wait()
}

func (p *process) Terminate(ctx context.Context) error {
	if p.cmd.Process == nil {
		return nil
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- p.cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		if err != nil && !isExitError(err) {
			return err
		}
		return nil
	case <-ctx.Done():
		if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("kill process after timeout: %w", killErr)
		}
		<-waitCh
		return fmt.Errorf("terminate timed out: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("kill process after grace period: %w", killErr)
		}
		<-waitCh
		return nil
	}
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
