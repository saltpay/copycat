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
	"github.com/saltpay/copycat/internal/slack"
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

	// Assessment done screen navigation
	expandedFindingRepo string // which repo's finding is expanded (empty = none)
	findingScrollOffset int    // scroll offset within the expanded finding box
	summaryExpanded     bool   // whether the overall summary box is expanded
	summaryScrollOffset int    // scroll offset within the expanded summary box

	// Assessment Slack confirmation
	assessSlackPending  bool
	assessSlackSending  bool
	assessSlackFocused  bool            // true when cursor is in the Slack toggle section
	assessSlackRepos    []string        // repos eligible for Slack (successful + has slack room)
	assessSlackSelected map[string]bool // toggled repos (true = will send)
	assessSlackCursor   int             // cursor index into assessSlackRepos
	assessSlackResults  []string
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
	m.progress.termWidth = m.termWidth
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

// updateDoneAssessment handles key input for the assessment done screen.
func (m dashboardModel) updateDoneAssessment(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle assessment Slack done message
	if slackDone, ok := msg.(assessSlackDoneMsg); ok {
		m.assessSlackSending = false
		m.assessSlackResults = slackDone.Results
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Expanded summary — scroll within summary content
		if m.summaryExpanded {
			switch keyMsg.String() {
			case "q":
				return m, tea.Quit
			case "enter", "l", "esc":
				m.summaryExpanded = false
				m.summaryScrollOffset = 0
				return m, nil
			case "up", "k":
				if m.summaryScrollOffset > 0 {
					m.summaryScrollOffset--
				}
				return m, nil
			case "down", "j":
				lines := strings.Split(m.assessmentSummary, "\n")
				maxScroll := len(lines) - maxLogLines
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.summaryScrollOffset < maxScroll {
					m.summaryScrollOffset++
				}
				return m, nil
			}
			return m, nil
		}

		// Expanded finding — scroll within finding content
		if m.expandedFindingRepo != "" {
			switch keyMsg.String() {
			case "q":
				return m, tea.Quit
			case "enter", "l", "esc":
				m.expandedFindingRepo = ""
				m.findingScrollOffset = 0
				return m, nil
			case "up", "k":
				if m.findingScrollOffset > 0 {
					m.findingScrollOffset--
				}
				return m, nil
			case "down", "j":
				finding := m.assessmentFindings[m.expandedFindingRepo]
				if finding != "" {
					lines := strings.Split(strings.TrimSpace(finding), "\n")
					maxScroll := len(lines) - maxLogLines
					if maxScroll < 0 {
						maxScroll = 0
					}
					if m.findingScrollOffset < maxScroll {
						m.findingScrollOffset++
					}
				}
				return m, nil
			}
			return m, nil
		}

		// Don't allow input while sending Slack
		if m.assessSlackSending {
			return m, nil
		}

		// Slack-focused: cursor is in the Slack toggle section
		if m.assessSlackFocused && m.assessSlackPending {
			return m.handleAssessSlackConfirmKey(keyMsg)
		}

		// Normal cursor navigation (findings zone)
		switch keyMsg.String() {
		case "q":
			return m, tea.Quit
		case "enter", "l":
			if m.doneCursorRepo == "_summary" {
				// Expand summary box
				summaryLines := strings.Split(m.assessmentSummary, "\n")
				if len(summaryLines) > maxLogLines {
					m.summaryExpanded = true
					m.summaryScrollOffset = 0
				}
			} else if m.doneCursorRepo != "" {
				// Expand finding for cursor repo (only if successful with a finding)
				results := m.doneResults()
				if result, ok := results[m.doneCursorRepo]; ok && result.Success {
					if finding, exists := m.assessmentFindings[m.doneCursorRepo]; exists && finding != "" {
						m.expandedFindingRepo = m.doneCursorRepo
						m.findingScrollOffset = 0
					}
				}
			}
			return m, nil
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
			m = m.moveAssessCursor(-1)
		case "down", "j":
			// If at the last findings row and Slack is pending, move focus to Slack
			if m.assessSlackPending {
				rows := m.assessNavigableRows()
				if len(rows) > 0 && m.doneCursorRepo == rows[len(rows)-1] {
					m.assessSlackFocused = true
					m.assessSlackCursor = 0
					return m, nil
				}
			}
			m = m.moveAssessCursor(1)
		}
	}
	return m, nil
}

// handleAssessSlackConfirmKey handles keys when cursor is focused on the Slack toggle section.
func (m dashboardModel) handleAssessSlackConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.assessSlackCursor > 0 {
			m.assessSlackCursor--
		} else {
			// Move focus back to findings (last row)
			m.assessSlackFocused = false
			rows := m.assessNavigableRows()
			if len(rows) > 0 {
				m.doneCursorRepo = rows[len(rows)-1]
			}
		}
	case "down", "j":
		if m.assessSlackCursor < len(m.assessSlackRepos)-1 {
			m.assessSlackCursor++
		}
	case " ", "x":
		if m.assessSlackCursor >= 0 && m.assessSlackCursor < len(m.assessSlackRepos) {
			repo := m.assessSlackRepos[m.assessSlackCursor]
			m.assessSlackSelected[repo] = !m.assessSlackSelected[repo]
		}
	case "enter":
		// Send only if at least one repo is selected
		hasSelected := false
		for _, sel := range m.assessSlackSelected {
			if sel {
				hasSelected = true
				break
			}
		}
		if hasSelected {
			return m.sendAssessSlack()
		}
	case "n", "esc":
		m.assessSlackPending = false
		m.assessSlackFocused = false
		return m, nil
	}
	return m, nil
}

// sendAssessSlack launches Slack notification sending in a goroutine.
func (m dashboardModel) sendAssessSlack() (tea.Model, tea.Cmd) {
	m.assessSlackPending = false
	m.assessSlackSending = true

	// Collect only the selected projects
	var sendProjects []config.Project
	for _, p := range m.selectedProjects {
		if m.assessSlackSelected[p.Repo] {
			sendProjects = append(sendProjects, p)
		}
	}

	question := m.wizardResult.Prompt
	findings := m.assessmentFindings
	token := m.wizardResult.SlackToken
	ch := m.statusCh

	go func() {
		var results []string
		slack.SendAssessmentFindings(sendProjects, question, findings, token, func(line string) {
			results = append(results, line)
		})
		ch <- assessSlackDoneMsg{Results: results}
	}()

	return m, listenForStatus(m.statusCh)
}

func (m dashboardModel) initDoneScreen() dashboardModel {
	m.doneScrollOffset = 0
	m.expandedLogRepo = ""
	m.logScrollOffset = 0
	m.expandedFindingRepo = ""
	m.findingScrollOffset = 0
	m.summaryExpanded = false
	m.summaryScrollOffset = 0
	repos := m.doneVisibleRepos()
	// For assessment, start cursor on summary row if there's a summary
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" && m.assessmentSummary != "" {
		m.doneCursorRepo = "_summary"
	} else if len(repos) > 0 {
		m.doneCursorRepo = repos[0]
	}

	// Check if assessment Slack confirmation is needed
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" && m.wizardResult.SendSlack {
		results := m.doneResults()
		var slackRepos []string
		for _, p := range m.selectedProjects {
			if result, ok := results[p.Repo]; ok && result.Success {
				room := strings.TrimSpace(p.SlackRoom)
				if room != "" {
					slackRepos = append(slackRepos, p.Repo)
				}
			}
		}
		if len(slackRepos) > 0 {
			m.assessSlackPending = true
			m.assessSlackFocused = false
			m.assessSlackRepos = slackRepos
			m.assessSlackSelected = make(map[string]bool)
			m.assessSlackCursor = 0
		}
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

// assessDoneMaxVisibleRepos returns how many repo rows fit on the assessment done screen.
func (m dashboardModel) assessDoneMaxVisibleRepos() int {
	// Base overhead: banner(3) + border(2) + title(1) + blank(1) + stats(1) + blank(1) + blank before help(1) + help(1) + padding(2) = 13
	overhead := 13

	// Summary row + box height
	if m.assessmentSummary != "" {
		summaryLines := strings.Split(m.assessmentSummary, "\n")
		if m.summaryExpanded {
			// Expanded: show all lines up to maxLogLines with scroll indicators
			boxLines := len(summaryLines)
			if boxLines > maxLogLines {
				boxLines = maxLogLines
			}
			boxLines += 2 // border top + bottom
			if len(summaryLines) > maxLogLines {
				if m.summaryScrollOffset > 0 {
					boxLines++
				}
				if m.summaryScrollOffset+maxLogLines < len(summaryLines) {
					boxLines++
				}
			}
			overhead += boxLines
		} else {
			// Collapsed: show first maxLogLines with possible "↓ N more"
			boxLines := len(summaryLines)
			if boxLines > maxLogLines {
				boxLines = maxLogLines
			}
			boxLines += 2 // border top + bottom
			if len(summaryLines) > maxLogLines {
				boxLines++ // "↓ N more" indicator
			}
			overhead += boxLines
		}
		overhead++ // summary label row
		overhead++ // blank line after summary box
	}

	// Expanded finding box height
	if m.expandedFindingRepo != "" {
		finding := m.assessmentFindings[m.expandedFindingRepo]
		if finding != "" {
			lines := strings.Split(strings.TrimSpace(finding), "\n")
			boxLines := len(lines)
			if boxLines > maxLogLines {
				boxLines = maxLogLines
			}
			boxLines += 2 // border top + bottom
			// Scroll indicators
			if len(lines) > maxLogLines {
				if m.findingScrollOffset > 0 {
					boxLines++
				}
				if m.findingScrollOffset+maxLogLines < len(lines) {
					boxLines++
				}
			}
			overhead += boxLines
		}
	}

	// Slack overlay height
	if m.assessSlackPending && !m.assessSlackSending {
		// blank(1) + prompt(1) + blank(1) + repo rows
		overhead += 3 + len(m.assessSlackRepos)
	} else if m.assessSlackSending {
		overhead += 2 // blank + sending line
	}
	if len(m.assessSlackResults) > 0 {
		overhead += 1 + len(m.assessSlackResults) // blank + result lines
	}

	// Post-processing lines
	overhead += len(m.progress.postLines)

	available := m.termHeight - overhead
	if available < 3 {
		available = 3
	}
	return available
}

// assessNavigableRows returns the cursor items for the assessment done screen.
// Includes "_summary" as the first item (if summary exists), followed by repo names.
func (m dashboardModel) assessNavigableRows() []string {
	var rows []string
	if m.assessmentSummary != "" {
		rows = append(rows, "_summary")
	}
	rows = append(rows, m.doneVisibleRepos()...)
	return rows
}

// moveAssessCursor moves the cursor up or down in the assessment done screen.
func (m dashboardModel) moveAssessCursor(delta int) dashboardModel {
	rows := m.assessNavigableRows()
	if len(rows) == 0 {
		return m
	}
	curIdx := 0
	for i, row := range rows {
		if row == m.doneCursorRepo {
			curIdx = i
			break
		}
	}
	newIdx := curIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(rows) {
		newIdx = len(rows) - 1
	}
	m.doneCursorRepo = rows[newIdx]

	// Collapse expanded items if cursor moved away
	if m.expandedFindingRepo != "" && m.expandedFindingRepo != m.doneCursorRepo {
		m.expandedFindingRepo = ""
		m.findingScrollOffset = 0
	}
	if m.summaryExpanded && m.doneCursorRepo != "_summary" {
		m.summaryExpanded = false
		m.summaryScrollOffset = 0
	}

	// Auto-scroll repo list to keep cursor visible (only for repo rows)
	if m.doneCursorRepo != "_summary" {
		repos := m.doneVisibleRepos()
		repoIdx := 0
		for i, r := range repos {
			if r == m.doneCursorRepo {
				repoIdx = i
				break
			}
		}
		maxVisible := m.assessDoneMaxVisibleRepos()
		if repoIdx < m.doneScrollOffset {
			m.doneScrollOffset = repoIdx
		} else if repoIdx >= m.doneScrollOffset+maxVisible {
			m.doneScrollOffset = repoIdx - maxVisible + 1
		}
	} else {
		// Cursor is on summary — make sure scroll is at top
		m.doneScrollOffset = 0
	}
	return m
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

func (m dashboardModel) renderAssessmentSummary() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	detailBtnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	detailBtnActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	findingLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	b.WriteString(titleStyle.Render("Assessment Complete!"))
	b.WriteString("\n\n")

	results := m.doneResults()

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

	// Summary box with cursor + expand support
	if m.assessmentSummary != "" {
		isSummaryCursor := !m.assessSlackFocused && m.doneCursorRepo == "_summary"
		summaryLines := strings.Split(m.assessmentSummary, "\n")
		canExpand := len(summaryLines) > maxLogLines

		// Summary label row with cursor and expand button
		summaryPrefix := "  "
		if isSummaryCursor {
			summaryPrefix = cursorStyle.Render("▸") + " "
		}
		summaryLabel := repoStyle.Render("Summary")
		expandBtn := ""
		if canExpand {
			if m.summaryExpanded {
				expandBtn = " " + detailBtnActiveStyle.Render("[▼ details]")
			} else {
				expandBtn = " " + detailBtnStyle.Render("[▶ details]")
			}
		}
		b.WriteString(fmt.Sprintf("%s%s%s\n", summaryPrefix, summaryLabel, expandBtn))

		summaryBoxWidth := m.termWidth - 10
		if summaryBoxWidth < 40 {
			summaryBoxWidth = 40
		}
		maxContentWidth := summaryBoxWidth - 4

		if m.summaryExpanded {
			// Expanded: scrollable view
			scrollStart := m.summaryScrollOffset
			scrollEnd := scrollStart + maxLogLines
			if scrollEnd > len(summaryLines) {
				scrollEnd = len(summaryLines)
			}

			var boxContent []string
			if scrollStart > 0 {
				boxContent = append(boxContent, dimStyle.Render(fmt.Sprintf("  ↑ %d more", scrollStart)))
			}
			for _, line := range summaryLines[scrollStart:scrollEnd] {
				if len(line) > maxContentWidth {
					line = line[:maxContentWidth-3] + "..."
				}
				boxContent = append(boxContent, findingLineStyle.Render(line))
			}
			if len(summaryLines)-scrollEnd > 0 {
				boxContent = append(boxContent, dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(summaryLines)-scrollEnd)))
			}

			summaryBoxStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("33")).
				Padding(0, 1).
				Width(summaryBoxWidth)

			rendered := summaryBoxStyle.Render(strings.Join(boxContent, "\n"))
			for _, boxLine := range strings.Split(rendered, "\n") {
				b.WriteString("    " + boxLine + "\n")
			}
		} else {
			// Collapsed: show first maxLogLines
			visibleLines := summaryLines
			if len(visibleLines) > maxLogLines {
				visibleLines = visibleLines[:maxLogLines]
			}

			var boxContent []string
			for _, line := range visibleLines {
				if len(line) > maxContentWidth {
					line = line[:maxContentWidth-3] + "..."
				}
				boxContent = append(boxContent, findingLineStyle.Render(line))
			}
			if canExpand {
				boxContent = append(boxContent, dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(summaryLines)-maxLogLines)))
			}

			summaryBoxStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1).
				Width(summaryBoxWidth)

			rendered := summaryBoxStyle.Render(strings.Join(boxContent, "\n"))
			for _, boxLine := range strings.Split(rendered, "\n") {
				b.WriteString("    " + boxLine + "\n")
			}
		}
		b.WriteString("\n")
	}

	// Per-repo findings with cursor navigation
	visibleRepos := m.doneVisibleRepos()
	maxVisible := m.assessDoneMaxVisibleRepos()
	start := m.doneScrollOffset
	end := start + maxVisible
	if end > len(visibleRepos) {
		end = len(visibleRepos)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	findingBoxWidth := m.termWidth - 14
	if findingBoxWidth < 40 {
		findingBoxWidth = 40
	}

	for _, repo := range visibleRepos[start:end] {
		result := results[repo]
		isCursor := !m.assessSlackFocused && repo == m.doneCursorRepo
		isExpanded := repo == m.expandedFindingRepo

		// Cursor indicator
		prefix := "  "
		if isCursor {
			prefix = cursorStyle.Render("▸") + " "
		}

		if result.Success {
			// Truncated finding preview
			finding := m.assessmentFindings[repo]
			findingPreview := strings.ReplaceAll(finding, "\n", " ")
			if len(findingPreview) > 120 {
				findingPreview = findingPreview[:117] + "..."
			}

			// Details button
			detailsBtn := ""
			if finding != "" {
				if isExpanded {
					detailsBtn = " " + detailBtnActiveStyle.Render("[▼ details]")
				} else {
					detailsBtn = " " + detailBtnStyle.Render("[▶ details]")
				}
			}

			b.WriteString(fmt.Sprintf("%s%s %s%s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), findingPreview, detailsBtn))
		} else {
			b.WriteString(fmt.Sprintf("%s%s Failed ⚠️ %s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status))
		}

		// Expanded finding box
		if isExpanded {
			finding := m.assessmentFindings[repo]
			if finding != "" {
				lines := strings.Split(strings.TrimSpace(finding), "\n")
				if len(lines) > 0 {
					findingStart := m.findingScrollOffset
					findingEnd := findingStart + maxLogLines
					if findingEnd > len(lines) {
						findingEnd = len(lines)
					}

					maxContentWidth := findingBoxWidth - 4
					var contentLines []string
					if findingStart > 0 {
						contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↑ %d more", findingStart)))
					}
					for _, line := range lines[findingStart:findingEnd] {
						if len(line) > maxContentWidth {
							line = line[:maxContentWidth-3] + "..."
						}
						contentLines = append(contentLines, findingLineStyle.Render(line))
					}
					if len(lines)-findingEnd > 0 {
						contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(lines)-findingEnd)))
					}

					findingBoxStyle := lipgloss.NewStyle().
						Border(lipgloss.RoundedBorder()).
						BorderForeground(lipgloss.Color("238")).
						Padding(0, 1).
						Width(findingBoxWidth)

					rendered := findingBoxStyle.Render(strings.Join(contentLines, "\n"))
					for _, boxLine := range strings.Split(rendered, "\n") {
						b.WriteString("    " + boxLine + "\n")
					}
				}
			}
		}
	}

	remaining := len(visibleRepos) - end
	if remaining > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	// Slack results
	if len(m.assessSlackResults) > 0 {
		b.WriteString("\n")
		for _, line := range m.assessSlackResults {
			b.WriteString("  ")
			b.WriteString(dimStyle.Render(line))
			b.WriteString("\n")
		}
	}

	// Slack confirmation prompt — per-repo toggle list
	if m.assessSlackPending && !m.assessSlackSending {
		b.WriteString("\n")
		slackStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		b.WriteString(slackStyle.Render("  Send findings to Slack? Select repos:"))
		b.WriteString("\n\n")

		// Build repo→channel lookup
		repoChannel := make(map[string]string)
		for _, p := range m.selectedProjects {
			room := strings.TrimSpace(p.SlackRoom)
			if room != "" {
				repoChannel[p.Repo] = room
			}
		}

		slackCursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		slackRepoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		slackChannelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		checkStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
		uncheckStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

		for i, repo := range m.assessSlackRepos {
			isCursor := m.assessSlackFocused && i == m.assessSlackCursor
			isSelected := m.assessSlackSelected[repo]

			prefix := "  "
			if isCursor {
				prefix = slackCursorStyle.Render("▸") + " "
			}

			check := uncheckStyle.Render("[ ]")
			if isSelected {
				check = checkStyle.Render("[x]")
			}

			channel := repoChannel[repo]
			b.WriteString(fmt.Sprintf("  %s%s %s %s\n", prefix, check, slackRepoStyle.Render(repo), slackChannelStyle.Render("#"+channel)))
		}
	} else if m.assessSlackSending {
		b.WriteString("\n")
		sendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString(sendingStyle.Render("  Sending assessment findings to Slack..."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	retryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var hints []string
	if m.summaryExpanded || m.expandedFindingRepo != "" {
		hints = append(hints, helpStyle.Render("↑↓: scroll"))
		hints = append(hints, helpStyle.Render("enter/esc: close"))
	} else if m.assessSlackFocused && m.assessSlackPending {
		hints = append(hints, helpStyle.Render("↑↓: navigate"))
		hints = append(hints, helpStyle.Render("space/x: toggle"))
		hints = append(hints, helpStyle.Render("enter: send"))
		hints = append(hints, helpStyle.Render("n/esc: skip"))
	} else if m.assessSlackSending {
		hints = append(hints, helpStyle.Render("sending..."))
	} else {
		hints = append(hints, helpStyle.Render("↑↓: navigate"))
		hints = append(hints, helpStyle.Render("enter/l: expand"))
		if failed > 0 {
			hints = append(hints, retryStyle.Render(fmt.Sprintf("r: retry %d failed", failed)))
		}
	}
	hints = append(hints, helpStyle.Render("q: exit"))
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
