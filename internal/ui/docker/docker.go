// Package docker renders the Docker tab: container list with live stats,
// lifecycle actions and a follow-mode log viewer.
package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/ersinkoc/wrongtop/internal/dockerclient"
	"github.com/ersinkoc/wrongtop/internal/format"
	"github.com/ersinkoc/wrongtop/internal/theme"
)

// dockerLogLines caps the in-memory log buffer.
const dockerLogLines = 1000

// Model is the docker tab.
type Model struct {
	cfg *config.Config
	th  *theme.Theme

	width, height int
	table         table.Model

	client *dockerclient.Client
	cons   []dockerclient.Container
	err    error // last poll/connect error

	logFor    string // container name shown in the log pane ("" = list mode)
	logs      []string
	logStream <-chan string
	cancelLog context.CancelFunc

	status string // transient action feedback
}

// New builds the docker tab.
func New(cfg *config.Config, th *theme.Theme) *Model {
	t := table.New(table.WithFocused(true), table.WithWidth(100), table.WithHeight(18))
	t.SetColumns(columns(100)) // sane defaults until SetSize arrives
	return &Model{cfg: cfg, th: th, table: t}
}

// Title implements ui.Tab.
func (m *Model) Title() string { return "DOCKER" }

// SetSize implements ui.Tab.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetWidth(width)
	if m.logFor != "" {
		m.table.SetHeight(0)
	} else {
		m.table.SetHeight(max(3, height-4))
	}
	m.table.SetColumns(columns(width))
}

// Update implements ui.Tab.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case dockerclient.UpdateMsg:
		m.client = msg.Client
		m.err = msg.Err
		m.cons = msg.Containers
		m.rebuild()
		return nil

	case logLineMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > dockerLogLines {
			m.logs = m.logs[len(m.logs)-dockerLogLines:]
		}
		return m.waitForLog()

	case logDoneMsg:
		// stream ended (container exited, daemon closed it, or ctx
		// cancelled): stop waiting instead of re-arming on a closed
		// channel, which would busy-loop
		return nil

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return nil
}

type logLineMsg string

// logDoneMsg signals that the log stream channel was closed by the
// reader goroutine — the terminal event of waitForLog.
type logDoneMsg struct{}

type actionDoneMsg struct {
	label string
	err   error
}

func (m *Model) key(key tea.KeyPressMsg) tea.Cmd {
	if m.logFor != "" {
		switch key.String() {
		case "esc", "enter":
			m.closeLogs()
		}
		return nil
	}
	if m.client == nil {
		return nil
	}

	switch key.String() {
	case "s":
		return m.action("start", m.client.Start)
	case "t":
		return m.action("stop", m.client.Stop)
	case "r":
		return m.action("restart", m.client.Restart)
	case "enter":
		return m.openLogs()
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(key)
		return cmd
	}
}

func (m *Model) selected() (dockerclient.Container, bool) {
	cur := m.table.Cursor()
	if cur < 0 || cur >= len(m.cons) {
		return dockerclient.Container{}, false
	}
	return m.cons[cur], true
}

func (m *Model) action(label string, fn func(context.Context, string) error) tea.Cmd {
	ct, ok := m.selected()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return actionDoneMsg{label: label, err: fn(ctx, ct.ID)}
	}
}

func (m *Model) openLogs() tea.Cmd {
	ct, ok := m.selected()
	if !ok || ct.State != "running" {
		m.status = "logs: container is not running"
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelLog = cancel
	m.logFor = ct.Name
	m.logs = nil
	lines := make(chan string, 256)
	m.logStream = lines
	go func() {
		defer close(lines)
		r, tty, err := m.client.Logs(ctx, ct.ID, true)
		if err != nil {
			lines <- "logs: " + err.Error()
			return
		}
		defer func() { _ = r.Close() }()
		pr, pw := io.Pipe()
		go func() {
			err := dockerclient.CopyLogStream(pw, r, tty)
			pw.CloseWithError(err)
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	m.table.SetHeight(0)
	return m.waitForLog()
}

func (m *Model) closeLogs() {
	if m.cancelLog != nil {
		m.cancelLog()
		m.cancelLog = nil
	}
	m.logFor = ""
	m.logs = nil
	m.logStream = nil
	m.table.SetHeight(max(3, m.height-4))
}

func (m *Model) waitForLog() tea.Cmd {
	lines := m.logStream
	return func() tea.Msg {
		s, ok := <-lines
		if !ok {
			return logDoneMsg{}
		}
		return logLineMsg(s)
	}
}

func (m *Model) rebuild() {
	rows := make([]table.Row, len(m.cons))
	for i, c := range m.cons {
		state := m.th.Styles.Muted.Render(c.State)
		switch c.State {
		case "running":
			state = m.th.Styles.OK.Render(c.State)
		case "exited", "dead":
			state = m.th.Styles.Crit.Render(c.State)
		case "paused":
			state = m.th.Styles.Warn.Render(c.State)
		}
		rows[i] = table.Row{
			c.Name,
			trunc(c.Image, 32),
			state,
			fmt.Sprintf("%5.1f", c.CPU),
			fmt.Sprintf("%10s", format.Bytes(c.Mem)),
			fmt.Sprintf("%4.0f%%", c.MemPct),
			fmt.Sprintf("%9s / %-9s", format.Bytes(c.NetRx), format.Bytes(c.NetTx)),
			fmt.Sprintf("%9s / %-9s", format.Bytes(c.BlkR), format.Bytes(c.BlkW)),
			trunc(c.Status, 22),
		}
	}
	m.table.SetRows(rows)
}

// View implements ui.Tab.
func (m *Model) View() string {
	if m.width < 1 {
		return ""
	}
	st := m.th.Styles

	if m.logFor != "" {
		head := st.BorderTitle.Render(" LOGS · "+m.logFor+" ") +
			st.Muted.Render(fmt.Sprintf("  %d lines  ", len(m.logs))) +
			st.HelpKey.Render("esc") + st.HelpText.Render(" back")
		body := m.logs
		if maxLines := m.height - 3; len(body) > maxLines {
			body = body[len(body)-maxLines:]
		}
		return lipgloss.JoinVertical(lipgloss.Left, head, strings.Join(body, "\n"))
	}

	if m.client == nil {
		msg := st.Warn.Render("Docker daemon not reachable.")
		hint := st.Muted.Render(m.errMsg())
		return lipgloss.JoinVertical(lipgloss.Center, "", msg, hint)
	}

	head := fmt.Sprintf("%d containers", len(m.cons))
	if m.status != "" {
		head += "  " + m.status
	}
	footer := st.HelpText.Render("  ") +
		st.HelpKey.Render("enter") + st.HelpText.Render(" logs  ") +
		st.HelpKey.Render("s") + st.HelpText.Render(" start  ") +
		st.HelpKey.Render("t") + st.HelpText.Render(" stop  ") +
		st.HelpKey.Render("r") + st.HelpText.Render(" restart")
	return lipgloss.JoinVertical(lipgloss.Left,
		st.Muted.Render(head),
		m.table.View(),
		footer,
	)
}

func (m *Model) errMsg() string {
	if m.err != nil {
		return m.err.Error()
	}
	return "start Docker Desktop or the daemon, then come back — wrongtop retries automatically."
}

func columns(width int) []table.Column {
	_ = width
	return []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "IMAGE", Width: 32},
		{Title: "STATE", Width: 9},
		{Title: "CPU%", Width: 5},
		{Title: "MEM", Width: 10},
		{Title: "MEM%", Width: 5},
		{Title: "NET RX/TX", Width: 21},
		{Title: "BLOCK R/W", Width: 21},
		{Title: "STATUS", Width: 22},
	}
}

// trunc shortens s to at most n-1 runes plus an ellipsis, cutting at
// rune boundaries so multi-byte names stay valid UTF-8.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n { // long in bytes, short in runes: nothing to drop
		return s
	}
	return string(r[:n-1]) + "…"
}
