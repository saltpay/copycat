package input

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/permission"
)

type dashboardPhase int

const (
	phaseProjects dashboardPhase = iota
	phaseWizard
	phaseProcessing
	phaseDone
)

// projectsFetchedMsg carries the result of an async project refresh.
type projectsFetchedMsg struct {
	Projects []config.Project
	Err      error
}

// editorFinishedMsg carries the result of the external editor.
type editorFinishedMsg struct {
	Content string
	Err     error
}

// DashboardConfig holds all dependencies injected by main.go.
type DashboardConfig struct {
	Projects      []config.Project
	AIToolsConfig *config.AIToolsConfig
	GitHubConfig  config.GitHubConfig
	AppConfig     config.Config
	Parallelism   int
	FetchProjects func() ([]config.Project, error)
	ProcessRepos  func(sender *StatusSender, projects []config.Project, setup *WizardResult)
	CreateIssues  func(sender *StatusSender, githubCfg config.GitHubConfig, projects []config.Project, title, desc string)
}

// DashboardResult holds everything the caller needs after the dashboard exits.
type DashboardResult struct {
	Action           string
	SelectedProjects []config.Project
	WizardResult     *WizardResult
	ProcessResults   map[string]ProjectDoneMsg
	Interrupted      bool
}

type dashboardModel struct {
	phase      dashboardPhase
	cfg        DashboardConfig
	statusCh   chan tea.Msg
	termWidth  int
	termHeight int

	// Sub-models
	projects projectSelectorModel
	wizard   wizardModel
	progress progressModel

	// Processing control
	resumeCh chan struct{}

	// Permission server
	permServer *permission.PermissionServer
	mcpCleanup func()

	// Shared state
	selectedProjects []config.Project
	wizardResult     *WizardResult
	processResults   map[string]ProjectDoneMsg
	interrupted      bool

	// Done screen scrolling
	doneScrollOffset int
}

func newDashboardModel(cfg DashboardConfig) dashboardModel {
	return dashboardModel{
		phase:    phaseProjects,
		cfg:      cfg,
		statusCh: make(chan tea.Msg, 100),
		projects: initialModel(cfg.Projects),
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return m.projects.Init()
}

// listenForStatus pumps one message from the status channel into bubbletea.
func listenForStatus(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.phase == phaseProcessing {
				m.interrupted = true
				m = m.cleanupPermissionServer()
				m.phase = phaseDone
				return m, nil
			}
			return m, tea.Quit
		}
	}

	switch m.phase {
	case phaseProjects:
		return m.updateProjects(msg)
	case phaseWizard:
		return m.updateWizard(msg)
	case phaseProcessing:
		return m.updateProcessing(msg)
	case phaseDone:
		return m.updateDone(msg)
	}

	return m, nil
}

func (m dashboardModel) updateProjects(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case projectsConfirmedMsg:
		if len(msg.Selected) == 0 {
			return m, tea.Quit
		}
		m.selectedProjects = msg.Selected
		m.wizard = newWizardModel(m.cfg.AIToolsConfig, m.selectedProjects)
		m.phase = phaseWizard
		return m, m.wizard.Init()

	case projectsRefreshMsg:
		m.projects.refreshing = true
		return m, func() tea.Msg {
			projects, err := m.cfg.FetchProjects()
			return projectsFetchedMsg{Projects: projects, Err: err}
		}

	case projectsFetchedMsg:
		if msg.Err != nil {
			// Stay on projects phase, just stop refreshing
			m.projects.refreshing = false
			return m, nil
		}
		// Re-create projects model with fresh data, preserving nothing
		m.cfg.Projects = msg.Projects
		m.projects = initialModel(msg.Projects)
		return m, m.projects.Init()
	}

	// Delegate to projects sub-model
	updated, cmd := m.projects.Update(msg)
	m.projects = updated.(projectSelectorModel)
	return m, cmd
}

func (m dashboardModel) updateWizard(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case wizardCompletedMsg:
		m.wizardResult = &msg.Result
		return m.startProcessing()

	case editorRequestedMsg:
		return m, m.openEditor()
	case editorFinishedMsg:
		if msg.Err != nil || strings.TrimSpace(msg.Content) == "" {
			// Editor failed or empty — go back to prompt step
			return m, nil
		}
		m.wizard.prompt = msg.Content
		m.wizard.promptInput.Blur()
		m.wizard.currentStep = stepSlackNotify
		return m, nil
	}

	updated, cmd := m.wizard.Update(msg)
	m.wizard = updated.(wizardModel)
	return m, cmd
}

func (m dashboardModel) openEditor() tea.Cmd {
	tmpFile, err := os.CreateTemp("", "copycat-prompt-*.txt")
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{Err: err}
		}
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	c := exec.Command(editor, tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			return editorFinishedMsg{Err: err}
		}
		content, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return editorFinishedMsg{Err: readErr}
		}
		return editorFinishedMsg{Content: strings.TrimSpace(string(content))}
	})
}

func (m dashboardModel) startProcessing() (tea.Model, tea.Cmd) {
	var repos []string
	for _, p := range m.selectedProjects {
		repos = append(repos, p.Repo)
	}

	checkpointInterval := 0
	if m.cfg.Parallelism > 0 && len(repos) > 0 {
		// Only checkpoint for non-issues workflows (checked below)
		checkpointInterval = m.cfg.Parallelism
		if checkpointInterval < 5 {
			checkpointInterval = 5
		}
	}

	if m.wizardResult.Action == "issues" {
		checkpointInterval = 0
	}

	if checkpointInterval > 0 {
		m.resumeCh = make(chan struct{}, 1)
	}

	m.progress = NewProgressModel(repos, checkpointInterval)
	m.phase = phaseProcessing

	// Start background processing
	sender := &StatusSender{
		send: func(msg tea.Msg) {
			m.statusCh <- msg
		},
		ResumeCh: m.resumeCh,
	}

	// Set up permission server if the AI tool supports it
	if m.wizardResult.AITool != nil && m.wizardResult.AITool.SupportsPermissionPrompt {
		permServer, err := permission.NewPermissionServer(m.statusCh)
		if err != nil {
			log.Printf("⚠️ Failed to start permission server: %v", err)
		} else {
			m.permServer = permServer
			mcpPath, cleanup, err := permission.GenerateMCPConfig(permServer.Port())
			if err != nil {
				log.Printf("⚠️ Failed to generate MCP config: %v", err)
				permServer.Shutdown(context.Background())
				m.permServer = nil
			} else {
				m.mcpCleanup = cleanup
				sender.MCPConfigPath = mcpPath
			}
		}
	}

	var processFn func()
	if m.wizardResult.Action == "issues" {
		processFn = func() {
			m.cfg.CreateIssues(sender, m.cfg.GitHubConfig, m.selectedProjects,
				m.wizardResult.IssueTitle, m.wizardResult.IssueDescription)
		}
	} else {
		processFn = func() {
			m.cfg.ProcessRepos(sender, m.selectedProjects, m.wizardResult)
		}
	}

	go func() {
		processFn()
		sender.Finish()
	}()

	return m, tea.Batch(
		m.progress.Init(),
		listenForStatus(m.statusCh),
	)
}

func (m dashboardModel) updateProcessing(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case processingDoneMsg:
		m.processResults = m.progress.results
		m = m.cleanupPermissionServer()
		m.phase = phaseDone
		return m, nil
	case resumeProcessingMsg:
		if m.resumeCh != nil {
			m.resumeCh <- struct{}{}
		}
		return m, nil
	}

	// Pump status channel messages
	var cmds []tea.Cmd
	switch msg.(type) {
	case ProjectStatusMsg, ProjectDoneMsg, permission.PermissionRequestMsg, PostStatusMsg:
		cmds = append(cmds, listenForStatus(m.statusCh))
	}

	updated, cmd := m.progress.Update(msg)
	m.progress = updated.(progressModel)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m dashboardModel) cleanupPermissionServer() dashboardModel {
	if m.permServer != nil {
		m.permServer.Shutdown(context.Background())
		m.permServer = nil
	}
	if m.mcpCleanup != nil {
		m.mcpCleanup()
		m.mcpCleanup = nil
	}
	return m
}

func (m dashboardModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "q", "enter":
			return m, tea.Quit
		case "r":
			var failedProjects []config.Project
			for _, p := range m.selectedProjects {
				if result, ok := m.processResults[p.Repo]; ok && !result.Success {
					failedProjects = append(failedProjects, p)
				}
			}
			if len(failedProjects) == 0 {
				return m, nil
			}
			m.selectedProjects = failedProjects
			m.doneScrollOffset = 0
			return m.startProcessing()
		case "up", "k":
			if m.doneScrollOffset > 0 {
				m.doneScrollOffset--
			}
		case "down", "j":
			maxScroll := m.doneMaxScroll()
			if m.doneScrollOffset < maxScroll {
				m.doneScrollOffset++
			}
		}
	}
	return m, nil
}

// doneMaxScroll returns the maximum scroll offset for the done screen.
func (m dashboardModel) doneMaxScroll() int {
	total := len(m.progress.repos)
	maxVisible := m.doneMaxVisible()
	if total <= maxVisible {
		return 0
	}
	return total - maxVisible
}

// doneMaxVisible returns how many result rows fit on screen.
// Reserves space for: banner(3) + border(2) + header(3) + summary(2) + postLines + help(2) + padding(2).
func (m dashboardModel) doneMaxVisible() int {
	overhead := 14 + len(m.progress.postLines)
	available := m.termHeight - overhead
	if available < 5 {
		available = 5
	}
	return available
}

func (m dashboardModel) View() string {
	// Banner always visible above the border
	bannerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	banner := bannerStyle.Render(" /\\_/\\\n( o.o ) COPYCAT\n > ^ <")

	// Render phase content
	var content string
	switch m.phase {
	case phaseProjects:
		content = m.projects.View()
	case phaseWizard:
		content = m.wizard.View()
	case phaseProcessing:
		content = m.progress.View()
	case phaseDone:
		content = m.renderDoneSummary()
	}

	// Border wrapping the content
	borderWidth := m.termWidth - 2 // account for border chars
	if borderWidth < 40 {
		borderWidth = 40
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(0, 1).
		Width(borderWidth)

	return banner + "\n" + borderStyle.Render(content)
}

func (m dashboardModel) renderDoneSummary() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if m.interrupted {
		b.WriteString(titleStyle.Render("Processing interrupted"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(titleStyle.Render("Processing complete!"))
		b.WriteString("\n\n")
	}

	results := m.processResults
	if results == nil {
		results = m.progress.results
	}

	succeeded := 0
	failed := 0
	for _, result := range results {
		if result.Success {
			succeeded++
		} else {
			failed++
		}
	}

	b.WriteString(fmt.Sprintf("  Total: %d  ", len(results)))
	b.WriteString(successStyle.Render(fmt.Sprintf("Succeeded: %d  ", succeeded)))
	if failed > 0 {
		b.WriteString(failStyle.Render(fmt.Sprintf("Failed: %d", failed)))
	}
	b.WriteString("\n\n")

	// Build the list of repos that have results
	var visibleRepos []string
	for _, repo := range m.progress.repos {
		if _, ok := results[repo]; ok {
			visibleRepos = append(visibleRepos, repo)
		}
	}

	// Scrolling
	maxVisible := m.doneMaxVisible()
	start := m.doneScrollOffset
	end := start + maxVisible
	if end > len(visibleRepos) {
		end = len(visibleRepos)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	for _, repo := range visibleRepos[start:end] {
		result := results[repo]
		b.WriteString(fmt.Sprintf("  %s %s\n", repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status))
	}

	remaining := len(visibleRepos) - end
	if remaining > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	// Post-processing status lines
	if len(m.progress.postLines) > 0 {
		b.WriteString("\n")
		for _, line := range m.progress.postLines {
			b.WriteString("  ")
			b.WriteString(dimStyle.Render(line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	if failed > 0 {
		retryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		b.WriteString(retryStyle.Render(fmt.Sprintf("  Press r to retry %d failed", failed)))
		b.WriteString(helpStyle.Render("  •  q/enter to exit"))
	} else {
		b.WriteString(helpStyle.Render("  Press q or enter to exit"))
	}
	if len(visibleRepos) > maxVisible {
		b.WriteString(helpStyle.Render("  •  ↑↓/jk to scroll"))
	}
	b.WriteString("\n")

	return b.String()
}

// RunDashboard is the single entry point that replaces all standalone tea.Program calls.
func RunDashboard(cfg DashboardConfig) (*DashboardResult, error) {
	model := newDashboardModel(cfg)

	// Redirect os.Stdout to suppress subprocess output
	origStdout := os.Stdout
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/null: %w", err)
	}
	os.Stdout = devNull
	defer func() {
		os.Stdout = origStdout
		devNull.Close()
	}()

	p := tea.NewProgram(model, tea.WithOutput(origStdout), tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	m := finalModel.(dashboardModel)

	// No wizard result means user quit early
	if m.wizardResult == nil {
		return nil, nil
	}

	results := m.processResults
	if results == nil {
		results = m.progress.results
	}

	return &DashboardResult{
		Action:           m.wizardResult.Action,
		SelectedProjects: m.selectedProjects,
		WizardResult:     m.wizardResult,
		ProcessResults:   results,
		Interrupted:      m.interrupted,
	}, nil
}
