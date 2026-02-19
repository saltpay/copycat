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

const maxLogLines = 10

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
	AssessRepos   func(sender *StatusSender, projects []config.Project, setup *WizardResult)
}

// DashboardResult holds everything the caller needs after the dashboard exits.
type DashboardResult struct {
	Action             string
	SelectedProjects   []config.Project
	WizardResult       *WizardResult
	ProcessResults     map[string]ProjectDoneMsg
	Interrupted        bool
	AssessmentSummary  string
	AssessmentFindings map[string]string
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
	resumeCh       chan struct{}
	slackConfirmCh chan bool
	cancelRegistry *CancelRegistry

	// Permission server
	permServer *permission.PermissionServer
	mcpCleanup func()

	// Shared state
	selectedProjects []config.Project
	wizardResult     *WizardResult
	processResults   map[string]ProjectDoneMsg
	interrupted      bool

	// Assessment results
	assessmentSummary  string
	assessmentFindings map[string]string

	// Done screen navigation
	doneScrollOffset int
	doneCursorRepo   string
	expandedLogRepo  string
	logScrollOffset  int
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
				m = m.initDoneScreen()
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
		m.wizard.termWidth = m.termWidth
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
		// Show warning if any projects are missing slack rooms
		missing := m.projects.countMissingSlackRooms()
		if missing > 0 {
			m.projects.showSlackWarning = true
			m.projects.missingSlackCount = missing
		}
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
		if m.wizard.action == "assessment" {
			return m, func() tea.Msg { return wizardCompletedMsg{Result: m.wizard.buildResult()} }
		}
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

	if checkpointInterval > 0 {
		m.resumeCh = make(chan struct{}, 1)
	}

	m.cancelRegistry = &CancelRegistry{}
	m.slackConfirmCh = make(chan bool, 1)
	m.progress = NewProgressModel(repos, checkpointInterval, m.wizardResult.BranchName, m.wizardResult.PRTitle, m.wizardResult.Prompt)
	m.progress.cancelRegistry = m.cancelRegistry
	m.phase = phaseProcessing

	// Start background processing
	sender := &StatusSender{
		send: func(msg tea.Msg) {
			m.statusCh <- msg
		},
		ResumeCh:       m.resumeCh,
		SlackConfirmCh: m.slackConfirmCh,
		CancelRegistry: m.cancelRegistry,
	}

	// Set up permission server if the AI tool supports it (skip for assessment — read-only)
	if m.wizardResult.Action != "assessment" && m.wizardResult.AITool != nil && m.wizardResult.AITool.SupportsPermissionPrompt {
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
	switch m.wizardResult.Action {
	case "assessment":
		processFn = func() {
			m.cfg.AssessRepos(sender, m.selectedProjects, m.wizardResult)
		}
	default:
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
	switch msg := msg.(type) {
	case processingDoneMsg:
		m.processResults = m.progress.results
		m = m.cleanupPermissionServer()
		m.phase = phaseDone
		m = m.initDoneScreen()
		return m, nil
	case resumeProcessingMsg:
		if m.resumeCh != nil {
			m.resumeCh <- struct{}{}
		}
		return m, nil
	case slackConfirmResponseMsg:
		if m.slackConfirmCh != nil {
			m.slackConfirmCh <- msg.Approved
		}
		return m, nil
	case cancelProjectMsg:
		if m.cancelRegistry != nil {
			m.cancelRegistry.Cancel(msg.Repo)
			m.progress.cancelled[msg.Repo] = true
			m.progress.statuses[msg.Repo] = "Cancelling..."
		}
		return m, nil
	}

	// Handle assessment results
	if ar, ok := msg.(AssessmentResultMsg); ok {
		m.assessmentSummary = ar.Summary
		m.assessmentFindings = ar.Findings
	}

	// Pump status channel messages
	var cmds []tea.Cmd
	switch msg.(type) {
	case ProjectStatusMsg, ProjectDoneMsg, permission.PermissionRequestMsg, PostStatusMsg, AssessmentResultMsg, slackConfirmMsg:
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
	// Assessment done screen uses simple scroll (no cursor/logs)
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		return m.updateDoneAssessment(msg)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// When a log is expanded, handle inner navigation
		if m.expandedLogRepo != "" {
			switch keyMsg.String() {
			case "q":
				return m, tea.Quit
			case "enter", "l", "esc":
				m.expandedLogRepo = ""
				m.logScrollOffset = 0
				return m, nil
			case "up", "k":
				if m.logScrollOffset > 0 {
					m.logScrollOffset--
				}
				return m, nil
			case "down", "j":
				results := m.doneResults()
				if result, ok := results[m.expandedLogRepo]; ok {
					lines := aiOutputLines(result.AIOutput)
					maxScroll := len(lines) - maxLogLines
					if maxScroll < 0 {
						maxScroll = 0
					}
					if m.logScrollOffset < maxScroll {
						m.logScrollOffset++
					}
				}
				return m, nil
			}
			return m, nil
		}

		// Normal done screen navigation
		switch keyMsg.String() {
		case "q":
			return m, tea.Quit
		case "enter", "l":
			// Toggle log expansion for cursor repo
			if m.doneCursorRepo != "" {
				results := m.doneResults()
				if result, ok := results[m.doneCursorRepo]; ok && result.AIOutput != "" {
					m.expandedLogRepo = m.doneCursorRepo
					m.logScrollOffset = 0
				}
			}
			return m, nil
		case "r":
			// Retry only actual failures (not skipped)
			var retryProjects []config.Project
			for _, p := range m.selectedProjects {
				if result, ok := m.processResults[p.Repo]; ok && !result.Success && !result.Skipped {
					retryProjects = append(retryProjects, p)
				}
			}
			if len(retryProjects) == 0 {
				return m, nil
			}
			m.selectedProjects = retryProjects
			return m.startProcessing()
		case "a":
			// Retry all non-success (failures + skipped)
			var retryProjects []config.Project
			for _, p := range m.selectedProjects {
				if result, ok := m.processResults[p.Repo]; ok && !result.Success {
					retryProjects = append(retryProjects, p)
				}
			}
			if len(retryProjects) == 0 {
				return m, nil
			}
			m.selectedProjects = retryProjects
			return m.startProcessing()
		case "up", "k":
			m = m.moveDoneCursor(-1)
		case "down", "j":
			m = m.moveDoneCursor(1)
		}
	}
	return m, nil
}

// updateDoneAssessment handles key input for the assessment done screen (simple scroll).
func (m dashboardModel) updateDoneAssessment(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "q", "enter":
			return m, tea.Quit
		case "r":
			var retryProjects []config.Project
			for _, p := range m.selectedProjects {
				if result, ok := m.processResults[p.Repo]; ok && !result.Success {
					retryProjects = append(retryProjects, p)
				}
			}
			if len(retryProjects) == 0 {
				return m, nil
			}
			m.selectedProjects = retryProjects
			return m.startProcessing()
		case "up", "k":
			if m.doneScrollOffset > 0 {
				m.doneScrollOffset--
			}
		case "down", "j":
			contentLines := m.assessmentContentLines()
			maxVisible := m.doneMaxVisible()
			maxScroll := len(contentLines) - maxVisible
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.doneScrollOffset < maxScroll {
				m.doneScrollOffset++
			}
		}
	}
	return m, nil
}

func (m dashboardModel) initDoneScreen() dashboardModel {
	m.doneScrollOffset = 0
	m.expandedLogRepo = ""
	m.logScrollOffset = 0
	repos := m.doneVisibleRepos()
	if len(repos) > 0 {
		m.doneCursorRepo = repos[0]
	}
	return m
}

// doneResults returns the process results, falling back to progress results.
func (m dashboardModel) doneResults() map[string]ProjectDoneMsg {
	results := m.processResults
	if results == nil {
		results = m.progress.results
	}
	return results
}

// doneVisibleRepos returns the list of repos that have results.
func (m dashboardModel) doneVisibleRepos() []string {
	results := m.doneResults()
	var repos []string
	for _, repo := range m.progress.repos {
		if _, ok := results[repo]; ok {
			repos = append(repos, repo)
		}
	}
	return repos
}

// moveDoneCursor moves the cursor up or down in the done screen.
func (m dashboardModel) moveDoneCursor(delta int) dashboardModel {
	repos := m.doneVisibleRepos()
	if len(repos) == 0 {
		return m
	}
	curIdx := 0
	for i, repo := range repos {
		if repo == m.doneCursorRepo {
			curIdx = i
			break
		}
	}
	newIdx := curIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(repos) {
		newIdx = len(repos) - 1
	}
	m.doneCursorRepo = repos[newIdx]

	// Auto-scroll to keep cursor visible
	maxVisible := m.doneMaxVisibleRepos()
	if newIdx < m.doneScrollOffset {
		m.doneScrollOffset = newIdx
	} else if newIdx >= m.doneScrollOffset+maxVisible {
		m.doneScrollOffset = newIdx - maxVisible + 1
	}
	return m
}

// doneMaxVisibleRepos returns how many repo rows fit on screen.
// Reserves space for: banner(3) + border(2) + header(3) + summary(2) + postLines + help(2) + padding(2).
func (m dashboardModel) doneMaxVisibleRepos() int {
	overhead := 14 + len(m.progress.postLines)
	// Account for expanded log box (content lines + 2 for box border)
	if m.expandedLogRepo != "" {
		results := m.doneResults()
		if result, ok := results[m.expandedLogRepo]; ok {
			lines := aiOutputLines(result.AIOutput)
			logHeight := len(lines)
			if logHeight > maxLogLines {
				logHeight = maxLogLines
			}
			// Add scroll indicator lines
			if len(lines) > maxLogLines {
				if m.logScrollOffset > 0 {
					logHeight++
				}
				if m.logScrollOffset+maxLogLines < len(lines) {
					logHeight++
				}
			}
			logHeight += 2 // box border top + bottom
			overhead += logHeight
		}
	}
	available := m.termHeight - overhead
	if available < 3 {
		available = 3
	}
	return available
}

// doneMaxVisible returns the max visible lines for the assessment done screen (line-based scroll).
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
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		return m.renderAssessmentSummary()
	}

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	skipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	logBtnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	logBtnActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	logLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	if m.interrupted {
		b.WriteString(titleStyle.Render("Processing interrupted"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(titleStyle.Render("Processing complete!"))
		b.WriteString("\n\n")
	}

	results := m.doneResults()
	cancelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	succeeded := 0
	skipped := 0
	failed := 0
	cancelled := 0
	for _, result := range results {
		switch {
		case result.Success:
			succeeded++
		case result.Skipped:
			skipped++
		case result.Error != nil && result.Error.Error() == "cancelled":
			cancelled++
		default:
			failed++
		}
	}

	b.WriteString(fmt.Sprintf("  Total: %d  ", len(results)))
	b.WriteString(successStyle.Render(fmt.Sprintf("Succeeded: %d  ", succeeded)))
	if skipped > 0 {
		b.WriteString(skipStyle.Render(fmt.Sprintf("Skipped: %d  ", skipped)))
	}
	if cancelled > 0 {
		b.WriteString(cancelStyle.Render(fmt.Sprintf("Cancelled: %d  ", cancelled)))
	}
	if failed > 0 {
		b.WriteString(failStyle.Render(fmt.Sprintf("Failed: %d", failed)))
	}
	b.WriteString("\n\n")

	visibleRepos := m.doneVisibleRepos()

	// Scrolling
	maxVisible := m.doneMaxVisibleRepos()
	start := m.doneScrollOffset
	end := start + maxVisible
	if end > len(visibleRepos) {
		end = len(visibleRepos)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	logBoxWidth := m.termWidth - 14
	if logBoxWidth < 40 {
		logBoxWidth = 40
	}

	for _, repo := range visibleRepos[start:end] {
		result := results[repo]
		isCursor := repo == m.doneCursorRepo
		isExpanded := repo == m.expandedLogRepo

		// Cursor indicator
		prefix := "  "
		if isCursor {
			prefix = cursorStyle.Render("▸") + " "
		}

		// Logs button
		logsBtn := ""
		if result.AIOutput != "" {
			if isExpanded {
				logsBtn = " " + logBtnActiveStyle.Render("[▼ logs]")
			} else {
				logsBtn = " " + logBtnStyle.Render("[▶ logs]")
			}
		}

		b.WriteString(fmt.Sprintf("%s%s %s%s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status, logsBtn))

		// Expanded log panel
		if isExpanded {
			lines := aiOutputLines(result.AIOutput)
			if len(lines) > 0 {
				logStart := m.logScrollOffset
				logEnd := logStart + maxLogLines
				if logEnd > len(lines) {
					logEnd = len(lines)
				}

				// Build log content lines
				maxContentWidth := logBoxWidth - 4 // account for box padding
				var contentLines []string
				if logStart > 0 {
					contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↑ %d more", logStart)))
				}
				for _, line := range lines[logStart:logEnd] {
					if len(line) > maxContentWidth {
						line = line[:maxContentWidth-3] + "..."
					}
					contentLines = append(contentLines, logLineStyle.Render(line))
				}
				if len(lines)-logEnd > 0 {
					contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(lines)-logEnd)))
				}

				// Render as a bordered box
				logBoxStyle := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("238")).
					Padding(0, 1).
					Width(logBoxWidth)

				rendered := logBoxStyle.Render(strings.Join(contentLines, "\n"))
				for _, boxLine := range strings.Split(rendered, "\n") {
					b.WriteString("    " + boxLine + "\n")
				}
			}
		}
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
	retryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var hints []string
	if m.expandedLogRepo != "" {
		hints = append(hints, helpStyle.Render("↑↓: scroll logs"))
		hints = append(hints, helpStyle.Render("enter/esc: close"))
	} else {
		hints = append(hints, helpStyle.Render("↑↓: navigate"))
		hints = append(hints, helpStyle.Render("enter/l: view logs"))
		if failed > 0 {
			hints = append(hints, retryStyle.Render(fmt.Sprintf("r: retry %d failed", failed)))
		}
		if failed > 0 && skipped > 0 {
			hints = append(hints, retryStyle.Render(fmt.Sprintf("a: retry all %d", failed+skipped)))
		} else if skipped > 0 {
			hints = append(hints, retryStyle.Render(fmt.Sprintf("a: retry %d skipped", skipped)))
		}
	}
	hints = append(hints, helpStyle.Render("q: exit"))
	b.WriteString("  " + strings.Join(hints, helpStyle.Render("  •  ")))
	b.WriteString("\n")

	return b.String()
}

// assessmentContentLines builds the combined scrollable lines for the assessment report:
// summary text lines, a blank separator, then per-repo findings.
func (m dashboardModel) assessmentContentLines() []string {
	summaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	results := m.processResults
	if results == nil {
		results = m.progress.results
	}

	var lines []string

	// Summary paragraph lines
	if m.assessmentSummary != "" {
		for _, line := range strings.Split(m.assessmentSummary, "\n") {
			lines = append(lines, summaryStyle.Render(line))
		}
		lines = append(lines, "") // blank separator
	}

	// Per-repo findings
	for _, repo := range m.progress.repos {
		result, ok := results[repo]
		if !ok {
			continue
		}
		if result.Success {
			finding := m.assessmentFindings[repo]
			if len(finding) > 120 {
				finding = finding[:117] + "..."
			}
			finding = strings.ReplaceAll(finding, "\n", " ")
			lines = append(lines, fmt.Sprintf("  %s %s", repoStyle.Render(fmt.Sprintf("[%s]", repo)), finding))
		} else {
			lines = append(lines, fmt.Sprintf("  %s %s", repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status))
		}
	}

	return lines
}

func (m dashboardModel) renderAssessmentSummary() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	b.WriteString(titleStyle.Render("Assessment Complete!"))
	b.WriteString("\n\n")

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

	// Scrollable content: summary + repo findings
	contentLines := m.assessmentContentLines()
	maxVisible := m.doneMaxVisible()
	start := m.doneScrollOffset
	end := start + maxVisible
	if end > len(contentLines) {
		end = len(contentLines)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	for _, line := range contentLines[start:end] {
		b.WriteString(line)
		b.WriteString("\n")
	}

	remaining := len(contentLines) - end
	if remaining > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	retryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var hints []string
	if failed > 0 {
		hints = append(hints, retryStyle.Render(fmt.Sprintf("r: retry %d failed", failed)))
	}
	hints = append(hints, helpStyle.Render("q/enter: exit"))
	if len(contentLines) > maxVisible {
		hints = append(hints, helpStyle.Render("↑↓/jk: scroll"))
	}
	b.WriteString("  " + strings.Join(hints, helpStyle.Render("  •  ")))
	b.WriteString("\n")

	return b.String()
}

// aiOutputLines splits AI output into non-empty lines for display.
func aiOutputLines(output string) []string {
	if output == "" {
		return nil
	}
	raw := strings.Split(strings.TrimSpace(output), "\n")
	var lines []string
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
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
		Action:             m.wizardResult.Action,
		SelectedProjects:   m.selectedProjects,
		WizardResult:       m.wizardResult,
		ProcessResults:     results,
		Interrupted:        m.interrupted,
		AssessmentSummary:  m.assessmentSummary,
		AssessmentFindings: m.assessmentFindings,
	}, nil
}
