package ui

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"multibrowser/internal/session"
)

// DoneMsg signals that all browser sessions have finished.
type DoneMsg struct{}

// Callbacks exposes UI actions back to the CLI/controller layer.
type Callbacks struct {
	AddInstances func(int) error
	CloseSession func(int) error
	QuitAll      func()
}

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Add    key.Binding
	Close  key.Binding
	Enter  key.Binding
	Cancel key.Binding
	Quit   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add instances"),
		),
		Close: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "close selected"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel form"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit all"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Add, k.Close, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Add, k.Close},
		{k.Enter, k.Cancel, k.Quit},
	}
}

// Run starts the Bubble Tea program and feeds it with session events.
func Run(output io.Writer, events <-chan session.Event, done <-chan error, callbacks Callbacks) error {
	program := tea.NewProgram(
		newModel(callbacks),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	)

	go func() {
		for event := range events {
			program.Send(event)
		}
	}()

	go func() {
		if err := <-done; err != nil {
			program.Send(errMsg{err: err})
			return
		}
		program.Send(DoneMsg{})
	}()

	_, err := program.Run()
	return err
}

type model struct {
	table       table.Model
	input       textinput.Model
	help        help.Model
	spinner     spinner.Model
	keys        keyMap
	callbacks   Callbacks
	sessions    map[int]session.Info
	orderedIDs  []int
	done        bool
	showAddForm bool
	width       int
	height      int
	notice      string
	noticeAt    time.Time
	lastError   error
}

func newModel(callbacks Callbacks) *model {
	columns := []table.Column{
		{Title: "ID", Width: 5},
		{Title: "Session", Width: 16},
		{Title: "PID", Width: 8},
		{Title: "State", Width: 11},
		{Title: "Tile", Width: 18},
	}

	tbl := table.New(
		table.WithColumns(columns),
		table.WithRows(nil),
		table.WithFocused(true),
		table.WithHeight(12),
	)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false).
		Foreground(lipgloss.Color("252"))
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Bold(true)
	tbl.SetStyles(styles)

	input := textinput.New()
	input.Placeholder = "How many new instances?"
	input.SetValue("1")
	input.CharLimit = 3
	input.Width = 10

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))

	return &model{
		table:     tbl,
		input:     input,
		help:      help.New(),
		spinner:   spin,
		keys:      newKeyMap(),
		callbacks: callbacks,
		sessions:  make(map[int]session.Info),
	}
}

func (m *model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
	case tea.KeyMsg:
		if m.showAddForm {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				m.hideAddForm()
				return m, nil
			case key.Matches(msg, m.keys.Enter):
				count, err := strconv.Atoi(strings.TrimSpace(m.input.Value()))
				if err != nil || count <= 0 {
					m.setNotice("Enter a positive number of instances.")
					return m, nil
				}
				return m, m.runAddInstances(count)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			m.done = true
			if m.callbacks.QuitAll != nil {
				m.callbacks.QuitAll()
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Add):
			m.showAddForm = true
			m.input.SetValue("1")
			m.input.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Close):
			selected := m.selectedSession()
			if selected == nil {
				return m, nil
			}
			return m, m.runCloseSession(selected.ID)
		}
	case session.Event:
		m.sessions[msg.Session.ID] = msg.Session
		m.rebuildRows()
	case actionResultMsg:
		if msg.err != nil {
			m.lastError = msg.err
			m.setNotice(msg.err.Error())
		} else if msg.notice != "" {
			m.setNotice(msg.notice)
		}
		if msg.action == actionAdd {
			m.hideAddForm()
		}
	case DoneMsg:
		m.done = true
		return m, tea.Quit
	case errMsg:
		m.lastError = msg.err
		m.done = true
		return m, tea.Quit
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)

	var spinnerCmd tea.Cmd
	m.spinner, spinnerCmd = m.spinner.Update(msg)
	cmds = append(cmds, spinnerCmd)

	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if m.width == 0 {
		return "Loading multibrowser..."
	}

	headerStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(true)
	subStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1)
	noticeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("166")).
		Padding(0, 1)

	active, stopped := m.counts()
	title := headerStyle.Render(" multibrowser monitor ")
	summary := subStyle.Render(fmt.Sprintf("%s %d active | %d total", m.spinner.View(), active, len(m.sessions)))
	left := panelStyle.Width(max(48, m.width/2-2)).Render(m.table.View())
	right := panelStyle.Width(max(30, m.width/2-6)).Render(m.detailView())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := m.help.View(m.keys)

	var parts []string
	parts = append(parts, title, summary, body)

	if m.showAddForm {
		form := panelStyle.
			BorderForeground(lipgloss.Color("62")).
			Render("Add Chrome instances\n\n" + m.input.View() + "\n\nPress enter to launch more windows.")
		parts = append(parts, form)
	}

	if m.notice != "" && time.Since(m.noticeAt) < 6*time.Second {
		parts = append(parts, noticeStyle.Render(m.notice))
	}

	if m.lastError != nil && m.done {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("error: "+m.lastError.Error()))
	}

	if !m.done && stopped > 0 {
		parts = append(parts, subStyle.Render(fmt.Sprintf("%d sessions have already exited or been cleaned.", stopped)))
	}

	parts = append(parts, footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *model) rebuildRows() {
	ids := make([]int, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	m.orderedIDs = ids

	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		item := m.sessions[id]
		rows = append(rows, table.Row{
			strconv.Itoa(item.ID),
			item.Name,
			strconv.Itoa(item.PID),
			string(item.State),
			fmt.Sprintf("%d,%d %dx%d", item.X, item.Y, item.Width, item.Height),
		})
	}
	m.table.SetRows(rows)
	if len(rows) > 0 && m.table.Cursor() >= len(rows) {
		m.table.SetCursor(len(rows) - 1)
	}
}

func (m *model) detailView() string {
	selected := m.selectedSession()
	if selected == nil {
		return "No Chrome sessions yet."
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Render(selected.Name),
		fmt.Sprintf("State: %s", selected.State),
		fmt.Sprintf("PID: %d", selected.PID),
		fmt.Sprintf("URL: %s", selected.URL),
		fmt.Sprintf("Profile: %s", shortPath(selected.ProfileDir)),
		fmt.Sprintf("Tile: (%d,%d) %dx%d", selected.X, selected.Y, selected.Width, selected.Height),
		fmt.Sprintf("Started: %s", selected.StartedAt.Format(time.Kitchen)),
	}
	if !selected.EndedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("Ended: %s", selected.EndedAt.Format(time.Kitchen)))
	}
	if selected.Error != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("Error: "+selected.Error))
	}
	lines = append(lines, "", "Keys", "a add instances", "x close selected", "q close all and exit")
	return strings.Join(lines, "\n")
}

func (m *model) selectedSession() *session.Info {
	rows := m.table.Rows()
	if len(rows) == 0 || m.table.Cursor() >= len(m.orderedIDs) {
		return nil
	}
	id := m.orderedIDs[m.table.Cursor()]
	item := m.sessions[id]
	return &item
}

func (m *model) counts() (active int, stopped int) {
	for _, item := range m.sessions {
		switch item.State {
		case session.StateStarting, session.StateRunning, session.StateStopping:
			active++
		default:
			stopped++
		}
	}
	return active, stopped
}

func (m *model) resize() {
	tableHeight := max(8, m.height-14)
	m.table.SetHeight(tableHeight)
}

func (m *model) hideAddForm() {
	m.showAddForm = false
	m.input.Blur()
}

func (m *model) setNotice(value string) {
	m.notice = value
	m.noticeAt = time.Now()
}

func (m *model) runAddInstances(count int) tea.Cmd {
	return func() tea.Msg {
		if m.callbacks.AddInstances == nil {
			return actionResultMsg{action: actionAdd, err: fmt.Errorf("add action is not configured")}
		}
		if err := m.callbacks.AddInstances(count); err != nil {
			return actionResultMsg{action: actionAdd, err: err}
		}
		return actionResultMsg{action: actionAdd, notice: fmt.Sprintf("Launching %d new Chrome instance(s).", count)}
	}
}

func (m *model) runCloseSession(id int) tea.Cmd {
	return func() tea.Msg {
		if m.callbacks.CloseSession == nil {
			return actionResultMsg{action: actionClose, err: fmt.Errorf("close action is not configured")}
		}
		if err := m.callbacks.CloseSession(id); err != nil {
			return actionResultMsg{action: actionClose, err: err}
		}
		return actionResultMsg{action: actionClose, notice: fmt.Sprintf("Closing session %d.", id)}
	}
}

type actionType string

const (
	actionAdd   actionType = "add"
	actionClose actionType = "close"
)

type actionResultMsg struct {
	action actionType
	notice string
	err    error
}

type errMsg struct {
	err error
}

func shortPath(path string) string {
	if len(path) <= 44 {
		return path
	}
	return "..." + path[len(path)-41:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
