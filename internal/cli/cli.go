package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"multibrowser/internal/chrome"
	"multibrowser/internal/layout"
	"multibrowser/internal/runner"
	"multibrowser/internal/screen"
	"multibrowser/internal/session"
	"multibrowser/internal/ui"
)

// Execute runs the multibrowser CLI.
func Execute(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "open":
		return runOpen(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stdout, usage())
		return err
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func runOpen(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)

	url := fs.String("url", "", "URL to open in all Chrome sessions")
	count := fs.Int("count", 3, "Number of Chrome sessions to start")
	baseName := fs.String("base-name", "session", "Base name for temporary profile directories")
	chromePath := fs.String("chrome-path", chrome.DefaultBinaryPath(), "Path to the Google Chrome binary")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*url) == "" {
		return errors.New("--url is required")
	}
	if *count <= 0 {
		return errors.New("--count must be greater than zero")
	}
	if err := runner.ValidateBinaryPath(*chromePath); err != nil {
		return err
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithCancel(sigCtx)
	defer cancel()

	events := make(chan session.Event, *count*4)
	done := make(chan error, 1)
	managerDone := make(chan struct{})

	manager := runner.NewManager(chrome.Launcher{})
	screenBounds := screen.DetectMainScreen(runCtx)
	if screenBounds.Width == 0 || screenBounds.Height == 0 {
		screenBounds = layout.ScreenBounds{Width: 1440, Height: 900}
	}
	if err := manager.Start(runCtx, runner.Options{
		URL:        *url,
		Count:      *count,
		BaseName:   *baseName,
		BinaryPath: *chromePath,
		Screen:     screenBounds,
	}, events); err != nil {
		close(events)
		done <- err
		return err
	}

	go func() {
		result := manager.Wait(context.Background())
		close(events)

		var errs []error
		for _, item := range result.Sessions {
			if item.State == session.StateFailed && item.Error != "" {
				errs = append(errs, fmt.Errorf("%s: %s", item.Name, item.Error))
			}
		}

		done <- errors.Join(errs...)
		close(managerDone)
	}()

	if err := ui.Run(stdout, events, done, ui.Callbacks{
		AddInstances: func(extra int) error {
			return manager.Add(runCtx, extra)
		},
		CloseSession: func(id int) error {
			return manager.TerminateSession(runCtx, id)
		},
		QuitAll: cancel,
	}); err != nil {
		return err
	}

	if runCtx.Err() != nil {
		<-managerDone
	}

	return nil
}

func usageError() error {
	return errors.New(usage())
}

func usage() string {
	return `Usage:
  multibrowser open --url <url> [--count 3] [--base-name session] [--chrome-path /path/to/chrome]

Commands:
  open    Launch managed Chrome sessions with temporary profiles
  help    Show this help`
}
