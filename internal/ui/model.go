package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"multibrowser/internal/session"
)

// DoneMsg signals that all browser sessions have finished.
type DoneMsg struct{}

// Model renders the runtime status of managed browser sessions.
type Model struct {
	sessions map[int]session.Info
	done     bool
	err      error
	quitCh   chan<- struct{}
	quitOnce *sync.Once
}

// NewModel creates a status panel model.
func NewModel(quitCh chan<- struct{}) *Model {
	return &Model{
		sessions: make(map[int]session.Info),
		quitCh:   quitCh,
		quitOnce: &sync.Once{},
	}
}

// Run starts the Bubble Tea program and feeds it with session events.
func Run(output io.Writer, events <-chan session.Event, done <-chan error, quitCh chan<- struct{}) error {
	program := tea.NewProgram(
		NewModel(quitCh),
		tea.WithOutput(output),
	)

	go func() {
		for event := range events {
			program.Send(event)
		}
	}()

	go func() {
		err := <-done
		if err != nil {
			program.Send(errMsg{err: err})
			return
		}
		program.Send(DoneMsg{})
	}()

	_, err := program.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitOnce.Do(func() {
				if m.quitCh != nil {
					close(m.quitCh)
				}
			})
			m.done = true
			return m, tea.Quit
		}
	case session.Event:
		m.sessions[msg.Session.ID] = msg.Session
	case DoneMsg:
		m.done = true
		return m, tea.Quit
	case errMsg:
		m.err = msg.err
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString("multibrowser\n\n")

	if len(m.sessions) == 0 {
		b.WriteString("Launching Chrome sessions...\n")
		return b.String()
	}

	ids := make([]int, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		item := m.sessions[id]
		line := fmt.Sprintf(
			"[%d] %s | pid=%d | %s | %s",
			item.ID,
			item.Name,
			item.PID,
			item.State,
			shortPath(item.ProfileDir),
		)
		if !item.EndedAt.IsZero() {
			line += " | ended " + item.EndedAt.Format(time.Kitchen)
		}
		if item.Error != "" {
			line += " | " + item.Error
		}
		b.WriteString(line + "\n")
	}

	if m.err != nil {
		b.WriteString("\nerror: " + m.err.Error() + "\n")
	}

	if !m.done {
		b.WriteString("\nPress q to stop all sessions.\n")
	}

	return b.String()
}

type errMsg struct {
	err error
}

func shortPath(path string) string {
	if len(path) <= 36 {
		return path
	}
	return "..." + path[len(path)-33:]
}
