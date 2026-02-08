package input

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// processingDoneMsg signals that all projects have finished processing.
type processingDoneMsg struct{}

// ProjectStatusMsg updates the status line for a single project.
type ProjectStatusMsg struct {
	Repo   string
	Status string
}

// ProjectDoneMsg signals that a project has finished processing.
type ProjectDoneMsg struct {
	Repo    string
	Status  string
	Success bool
	PRURL   string
	Error   error
}

// StatusSender sends status updates to the progress dashboard.
type StatusSender struct {
	send func(tea.Msg)
}

// UpdateStatus updates the status line for a project.
func (s *StatusSender) UpdateStatus(repo, status string) {
	s.send(ProjectStatusMsg{Repo: repo, Status: status})
}

// Done signals that a project has finished processing.
func (s *StatusSender) Done(repo, status string, success bool, prURL string, err error) {
	s.send(ProjectDoneMsg{
		Repo:    repo,
		Status:  status,
		Success: success,
		PRURL:   prURL,
		Error:   err,
	})
}

type progressModel struct {
	repos     []string
	statuses  map[string]string
	results   map[string]ProjectDoneMsg
	completed int
	total     int
	startTime time.Time
	termWidth int
	quitted   bool
}

// NewProgressModel creates a new progress model for tracking repository processing.
func NewProgressModel(repos []string) progressModel {
	statuses := make(map[string]string)
	for _, repo := range repos {
		statuses[repo] = "Waiting..."
	}
	return progressModel{
		repos:     repos,
		statuses:  statuses,
		results:   make(map[string]ProjectDoneMsg),
		total:     len(repos),
		startTime: time.Now(),
	}
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(tea.ClearScreen, m.tickCmd())
}

type tickMsg time.Time

func (m progressModel) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
	case ProjectStatusMsg:
		m.statuses[msg.Repo] = msg.Status
	case ProjectDoneMsg:
		m.statuses[msg.Repo] = msg.Status
		m.results[msg.Repo] = msg
		m.completed++
		if m.completed >= m.total {
			return m, func() tea.Msg { return processingDoneMsg{} }
		}
	case tickMsg:
		return m, m.tickCmd()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m progressModel) View() string {
	if m.quitted {
		return ""
	}

	var b strings.Builder

	// Progress bar
	elapsed := time.Since(m.startTime)
	pct := 0
	if m.total > 0 {
		pct = m.completed * 100 / m.total
	}

	barWidth := 40
	if m.termWidth > 80 {
		barWidth = m.termWidth - 50
	}
	if barWidth < 10 {
		barWidth = 10
	}

	filled := barWidth * pct / 100
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	// Time display
	var timeInfo string
	if m.completed > 0 {
		avgPerItem := elapsed / time.Duration(m.completed)
		remaining := avgPerItem * time.Duration(m.total-m.completed)
		timeInfo = fmt.Sprintf("[%s:%s]", formatDuration(elapsed), formatDuration(remaining))
	} else {
		timeInfo = fmt.Sprintf("[%s:--]", formatDuration(elapsed))
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	b.WriteString(titleStyle.Render(fmt.Sprintf(
		"Processing repos %3d%% |%s| (%d/%d) %s",
		pct, bar, m.completed, m.total, timeInfo)))
	b.WriteString("\n\n")

	// Per-project status lines
	for _, repo := range m.repos {
		status := m.statuses[repo]
		repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
		b.WriteString(fmt.Sprintf("%s %s\n", repoStyle.Render(fmt.Sprintf("[%s]", repo)), status))
	}

	return b.String()
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
