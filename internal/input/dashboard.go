package input

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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

type notifPhaseType int

const (
	notifPhaseReady   notifPhaseType = iota // token input + repo checkboxes + send button
	notifPhaseSending                       // sending in progress
	notifPhaseDone                          // results displayed
)

const notifMaxVisibleRepos = 15

// notifFocus tracks which element has focus on the notifications tab
type notifFocus int

const (
	notifFocusToken notifFocus = iota // text input for token
	notifFocusRepos                   // repo checkbox list
	notifFocusSend                    // send button
)

// slackSendDoneMsg carries the results of sending Slack notifications.
type slackSendDoneMsg struct {
	Results []string
}

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

	// Slack notification callbacks (invoked from the done screen)
	SendSlackNotifications      func(projects []config.Project, prTitle string, prURLs map[string]string, token string, onStatus func(string))
	SendSlackAssessmentFindings func(projects []config.Project, question string, findings map[string]string, token string, onStatus func(string))
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
	resumeCh       chan string
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

	// Tabbed done screen
	activeTab         int            // current tab index
	notifPhase        notifPhaseType // notification tab phase
	notifFocus        notifFocus     // which element has focus
	slackTokenInput   textinput.Model
	slackToken        string
	slackRepos        []string        // repos eligible for Slack (successful + has slack room)
	slackSelected     map[string]bool // toggled repos (true = will send)
	slackCursor       int             // cursor index into slackRepos
	notifScrollOffset int             // scroll offset for repo list
	slackResults      []string
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
		m.wizard = newWizardModel(m.cfg.AIToolsConfig, m.cfg.AppConfig.AgentInstructions, m.selectedProjects)
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
		if !m.wizard.skipIgnoreInstructions {
			m.wizard.currentStep = stepIgnoreInstructions
			return m, nil
		}
		result := m.wizard.buildResult()
		m.wizardResult = &result
		return m.startProcessing()
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
		m.resumeCh = make(chan string, 1)
	}

	m.cancelRegistry = &CancelRegistry{}
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
			m.resumeCh <- msg.NewPrompt
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
	case ProjectStatusMsg, ProjectDoneMsg, permission.PermissionRequestMsg, PostStatusMsg, AssessmentResultMsg:
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

// doneTabCount returns the number of tabs for the current workflow.
func (m dashboardModel) doneTabCount() int {
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		return 3 // Summary | Projects | Notifications
	}
	return 2 // Results | Notifications
}

// doneTabLabel returns the label for tab at the given index.
func (m dashboardModel) doneTabLabel(idx int) string {
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		switch idx {
		case 0:
			return "Summary"
		case 1:
			return "Projects"
		case 2:
			return "Notifications"
		}
	}
	switch idx {
	case 0:
		return "Results"
	case 1:
		return "Notifications"
	}
	return ""
}

// isNotifTab returns true if the current active tab is the Notifications tab.
func (m dashboardModel) isNotifTab() bool {
	return m.activeTab == m.doneTabCount()-1
}

func (m dashboardModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle Slack send done message (works for any tab)
	if slackDone, ok := msg.(slackSendDoneMsg); ok {
		m.notifPhase = notifPhaseDone
		m.slackResults = slackDone.Results
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		tokenInputFocused := m.isNotifTab() && m.notifFocus == notifFocusToken && m.slackTokenInput.Focused()

		// Global keys
		switch keyMsg.String() {
		case "q":
			if !tokenInputFocused {
				return m, tea.Quit
			}
		case "ctrl+c":
			return m, tea.Quit
		}

		// Tab switching always works (blur/focus token input as needed)
		tabCount := m.doneTabCount()
		switchTab := func(newTab int) {
			m.slackTokenInput.Blur()
			m.activeTab = newTab
			if m.activeTab == tabCount-1 && m.notifPhase == notifPhaseReady && m.notifFocus == notifFocusToken {
				m.slackTokenInput.Focus()
			}
		}
		switch keyMsg.String() {
		case "tab":
			switchTab((m.activeTab + 1) % tabCount)
			return m, nil
		case "shift+tab":
			switchTab((m.activeTab - 1 + tabCount) % tabCount)
			return m, nil
		}

		// Number key tab switching (not in text input to allow typing digits)
		if !tokenInputFocused {
			switch keyMsg.String() {
			case "1":
				if tabCount >= 1 {
					switchTab(0)
				}
				return m, nil
			case "2":
				if tabCount >= 2 {
					switchTab(1)
				}
				return m, nil
			case "3":
				if tabCount >= 3 {
					switchTab(2)
				}
				return m, nil
			}
		}

		// Delegate to tab-specific handler
		if m.isNotifTab() {
			return m.updateDoneNotifTab(keyMsg)
		}

		if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
			return m.updateDoneAssessmentTab(keyMsg)
		}
		return m.updateDoneResultsTab(keyMsg)
	}

	// Forward text input updates for token entry
	if m.isNotifTab() && m.notifFocus == notifFocusToken {
		var cmd tea.Cmd
		m.slackTokenInput, cmd = m.slackTokenInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateDoneResultsTab handles keys on the Results tab (local workflow).
func (m dashboardModel) updateDoneResultsTab(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When a log is expanded, handle inner navigation
	if m.expandedLogRepo != "" {
		switch keyMsg.String() {
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

	switch keyMsg.String() {
	case "enter", "l":
		if m.doneCursorRepo != "" {
			results := m.doneResults()
			if result, ok := results[m.doneCursorRepo]; ok && result.AIOutput != "" {
				m.expandedLogRepo = m.doneCursorRepo
				m.logScrollOffset = 0
			}
		}
		return m, nil
	case "r":
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
	return m, nil
}

// updateDoneAssessmentTab handles keys on the Summary (tab 0) or Projects (tab 1) tabs for assessment.
func (m dashboardModel) updateDoneAssessmentTab(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Summary tab (tab 0)
	if m.activeTab == 0 {
		if m.summaryExpanded {
			switch keyMsg.String() {
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
		switch keyMsg.String() {
		case "enter", "l":
			summaryLines := strings.Split(m.assessmentSummary, "\n")
			if len(summaryLines) > maxLogLines {
				m.summaryExpanded = true
				m.summaryScrollOffset = 0
			}
			return m, nil
		}
		return m, nil
	}

	// Projects tab (tab 1)
	if m.expandedFindingRepo != "" {
		switch keyMsg.String() {
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

	switch keyMsg.String() {
	case "enter", "l":
		if m.doneCursorRepo != "" {
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
		m = m.moveAssessCursor(1)
	}
	return m, nil
}

// updateDoneNotifTab handles keys on the Notifications tab.
func (m dashboardModel) updateDoneNotifTab(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.notifPhase {
	case notifPhaseReady:
		switch m.notifFocus {
		case notifFocusToken:
			switch keyMsg.Type {
			case tea.KeyEnter:
				// Move focus to repos (or send if no repos)
				m.slackToken = strings.TrimSpace(m.slackTokenInput.Value())
				m.slackTokenInput.Blur()
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = 0
				} else {
					m.notifFocus = notifFocusSend
				}
				return m, nil
			case tea.KeyDown:
				m.slackToken = strings.TrimSpace(m.slackTokenInput.Value())
				m.slackTokenInput.Blur()
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = 0
				} else {
					m.notifFocus = notifFocusSend
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.slackTokenInput, cmd = m.slackTokenInput.Update(keyMsg)
			return m, cmd

		case notifFocusRepos:
			switch keyMsg.String() {
			case "up", "k":
				if m.slackCursor > 0 {
					m.slackCursor--
				} else {
					// Move focus back to token input
					m.notifFocus = notifFocusToken
					m.slackTokenInput.Focus()
				}
			case "down", "j":
				if m.slackCursor < len(m.slackRepos)-1 {
					m.slackCursor++
				} else {
					m.notifFocus = notifFocusSend
				}
			case " ", "x":
				if m.slackCursor >= 0 && m.slackCursor < len(m.slackRepos) {
					repo := m.slackRepos[m.slackCursor]
					m.slackSelected[repo] = !m.slackSelected[repo]
				}
			case "a":
				allSelected := true
				for _, repo := range m.slackRepos {
					if !m.slackSelected[repo] {
						allSelected = false
						break
					}
				}
				for _, repo := range m.slackRepos {
					m.slackSelected[repo] = !allSelected
				}
			}
			m.ensureNotifCursorVisible()
			return m, nil

		case notifFocusSend:
			switch keyMsg.String() {
			case "up", "k":
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = len(m.slackRepos) - 1
				} else {
					m.notifFocus = notifFocusToken
					m.slackTokenInput.Focus()
				}
			case "enter":
				token := strings.TrimSpace(m.slackTokenInput.Value())
				if token == "" {
					// Focus back on token input if empty
					m.notifFocus = notifFocusToken
					m.slackTokenInput.Focus()
					return m, nil
				}
				m.slackToken = token
				hasSelected := false
				for _, sel := range m.slackSelected {
					if sel {
						hasSelected = true
						break
					}
				}
				if hasSelected {
					return m.sendSlackNotifications()
				}
			}
			return m, nil
		}
		return m, nil

	case notifPhaseSending:
		return m, nil

	case notifPhaseDone:
		return m, nil
	}
	return m, nil
}

// sendSlackNotifications launches Slack notification sending in a goroutine.
func (m dashboardModel) sendSlackNotifications() (tea.Model, tea.Cmd) {
	m.notifPhase = notifPhaseSending

	var sendProjects []config.Project
	for _, p := range m.selectedProjects {
		if m.slackSelected[p.Repo] {
			sendProjects = append(sendProjects, p)
		}
	}

	token := m.slackToken
	ch := m.statusCh

	if m.wizardResult.Action == "assessment" {
		question := m.wizardResult.Prompt
		findings := m.assessmentFindings
		sendFn := m.cfg.SendSlackAssessmentFindings

		go func() {
			var results []string
			if sendFn != nil {
				sendFn(sendProjects, question, findings, token, func(line string) {
					results = append(results, line)
				})
			}
			ch <- slackSendDoneMsg{Results: results}
		}()
	} else {
		prTitle := m.wizardResult.PRTitle
		prURLs := make(map[string]string)
		results := m.doneResults()
		for _, p := range sendProjects {
			if result, ok := results[p.Repo]; ok {
				prURLs[p.Repo] = result.PRURL
			}
		}
		sendFn := m.cfg.SendSlackNotifications

		go func() {
			var resultLines []string
			if sendFn != nil {
				sendFn(sendProjects, prTitle, prURLs, token, func(line string) {
					resultLines = append(resultLines, line)
				})
			}
			ch <- slackSendDoneMsg{Results: resultLines}
		}()
	}

	return m, listenForStatus(m.statusCh)
}

func (m dashboardModel) initDoneScreen() dashboardModel {
	m.activeTab = 0
	m.doneScrollOffset = 0
	m.expandedLogRepo = ""
	m.logScrollOffset = 0
	m.expandedFindingRepo = ""
	m.findingScrollOffset = 0
	m.summaryExpanded = false
	m.summaryScrollOffset = 0
	m.slackResults = nil

	repos := m.doneVisibleRepos()
	if m.wizardResult != nil && m.wizardResult.Action == "assessment" && m.assessmentSummary != "" {
		m.doneCursorRepo = "_summary"
	} else if len(repos) > 0 {
		m.doneCursorRepo = repos[0]
	}

	// Build Slack-eligible repos list (successful with slack rooms)
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
	m.slackRepos = slackRepos
	m.slackSelected = make(map[string]bool)
	for _, repo := range slackRepos {
		m.slackSelected[repo] = true // default all selected
	}
	m.slackCursor = 0

	// Initialize Slack token input
	tokenInput := textinput.New()
	tokenInput.Placeholder = "xoxb-..."
	tokenInput.CharLimit = 512
	tokenInput.Width = 60
	m.notifPhase = notifPhaseReady
	if envToken := os.Getenv("SLACK_BOT_TOKEN"); envToken != "" {
		tokenInput.SetValue(envToken)
		m.slackToken = envToken
		// Token pre-filled: start focused on repos
		m.notifFocus = notifFocusRepos
	} else {
		tokenInput.Focus()
		m.notifFocus = notifFocusToken
	}
	m.slackTokenInput = tokenInput

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

// assessDoneMaxVisibleRepos returns how many repo rows fit on the assessment done screen (Projects tab).
func (m dashboardModel) assessDoneMaxVisibleRepos() int {
	// Base overhead: banner(3) + border(2) + tab bar(2) + stats(2) + blank before help(1) + help(1) + padding(2) = 13
	overhead := 13

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

	available := m.termHeight - overhead
	if available < 3 {
		available = 3
	}
	return available
}

// moveAssessCursor moves the cursor up or down in the assessment Projects tab.
func (m dashboardModel) moveAssessCursor(delta int) dashboardModel {
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

	// Collapse expanded items if cursor moved away
	if m.expandedFindingRepo != "" && m.expandedFindingRepo != m.doneCursorRepo {
		m.expandedFindingRepo = ""
		m.findingScrollOffset = 0
	}

	// Auto-scroll
	maxVisible := m.assessDoneMaxVisibleRepos()
	if newIdx < m.doneScrollOffset {
		m.doneScrollOffset = newIdx
	} else if newIdx >= m.doneScrollOffset+maxVisible {
		m.doneScrollOffset = newIdx - maxVisible + 1
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

// renderTabBar renders the tab bar for the done screen.
func (m dashboardModel) renderTabBar() string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("206")).Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Padding(0, 1)
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	tabCount := m.doneTabCount()
	var tabs []string
	for i := 0; i < tabCount; i++ {
		label := fmt.Sprintf("%d: %s", i+1, m.doneTabLabel(i))
		if i == m.activeTab {
			tabs = append(tabs, activeStyle.Render(label))
		} else {
			tabs = append(tabs, inactiveStyle.Render(label))
		}
	}
	return "  " + strings.Join(tabs, separatorStyle.Render(" │ "))
}

func (m dashboardModel) renderDoneSummary() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))

	isAssessment := m.wizardResult != nil && m.wizardResult.Action == "assessment"

	if isAssessment {
		b.WriteString(titleStyle.Render("Assessment Complete!"))
	} else if m.interrupted {
		b.WriteString(titleStyle.Render("Processing interrupted"))
	} else {
		b.WriteString(titleStyle.Render("Processing complete!"))
	}
	b.WriteString("\n")

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n\n")

	// Dispatch to tab content
	if m.isNotifTab() {
		b.WriteString(m.renderNotifTabContent())
	} else if isAssessment {
		if m.activeTab == 0 {
			b.WriteString(m.renderAssessSummaryTabContent())
		} else {
			b.WriteString(m.renderAssessProjectsTabContent())
		}
	} else {
		b.WriteString(m.renderLocalResultsTabContent())
	}

	// Help text
	b.WriteString("\n")
	b.WriteString(m.renderDoneHelp())
	b.WriteString("\n")

	return b.String()
}

func (m dashboardModel) renderLocalResultsTabContent() string {
	var b strings.Builder

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	skipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	logBtnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	logBtnActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	logLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	cancelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	results := m.doneResults()

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

		prefix := "  "
		if isCursor {
			prefix = cursorStyle.Render("▸") + " "
		}

		logsBtn := ""
		if result.AIOutput != "" {
			if isExpanded {
				logsBtn = " " + logBtnActiveStyle.Render("[▼ logs]")
			} else {
				logsBtn = " " + logBtnStyle.Render("[▶ logs]")
			}
		}

		b.WriteString(fmt.Sprintf("%s%s %s%s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status, logsBtn))

		if isExpanded {
			lines := aiOutputLines(result.AIOutput)
			if len(lines) > 0 {
				logStart := m.logScrollOffset
				logEnd := logStart + maxLogLines
				if logEnd > len(lines) {
					logEnd = len(lines)
				}

				maxContentWidth := logBoxWidth - 4
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

	if len(m.progress.postLines) > 0 {
		b.WriteString("\n")
		for _, line := range m.progress.postLines {
			b.WriteString("  ")
			b.WriteString(dimStyle.Render(line))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m dashboardModel) renderAssessSummaryTabContent() string {
	var b strings.Builder

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	findingLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	detailBtnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	detailBtnActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if m.assessmentSummary == "" {
		b.WriteString(dimStyle.Render("  No summary available."))
		b.WriteString("\n")
		return b.String()
	}

	summaryLines := strings.Split(m.assessmentSummary, "\n")
	canExpand := len(summaryLines) > maxLogLines

	summaryLabel := repoStyle.Render("Summary")
	expandBtn := ""
	if canExpand {
		if m.summaryExpanded {
			expandBtn = " " + detailBtnActiveStyle.Render("[▼ details]")
		} else {
			expandBtn = " " + detailBtnStyle.Render("[▶ details]")
		}
	}
	b.WriteString(fmt.Sprintf("  %s%s\n", summaryLabel, expandBtn))

	summaryBoxWidth := m.termWidth - 10
	if summaryBoxWidth < 40 {
		summaryBoxWidth = 40
	}
	maxContentWidth := summaryBoxWidth - 4

	if m.summaryExpanded {
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

	return b.String()
}

func (m dashboardModel) renderAssessProjectsTabContent() string {
	var b strings.Builder

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	detailBtnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	detailBtnActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	findingLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

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
		isCursor := repo == m.doneCursorRepo
		isExpanded := repo == m.expandedFindingRepo

		prefix := "  "
		if isCursor {
			prefix = cursorStyle.Render("▸") + " "
		}

		if result.Success {
			finding := m.assessmentFindings[repo]
			findingPreview := strings.ReplaceAll(finding, "\n", " ")
			if len(findingPreview) > 120 {
				findingPreview = findingPreview[:117] + "..."
			}

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

	return b.String()
}

// ensureNotifCursorVisible adjusts notifScrollOffset so the cursor is within the visible window.
func (m *dashboardModel) ensureNotifCursorVisible() {
	n := len(m.slackRepos)
	if n == 0 {
		m.notifScrollOffset = 0
		return
	}

	if m.slackCursor < m.notifScrollOffset {
		m.notifScrollOffset = m.slackCursor
	}
	if m.slackCursor >= m.notifScrollOffset+notifMaxVisibleRepos {
		m.notifScrollOffset = m.slackCursor - notifMaxVisibleRepos + 1
	}

	maxOffset := n - notifMaxVisibleRepos
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.notifScrollOffset > maxOffset {
		m.notifScrollOffset = maxOffset
	}
	if m.notifScrollOffset < 0 {
		m.notifScrollOffset = 0
	}
}

func (m dashboardModel) renderNotifTabContent() string {
	var b strings.Builder

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	repoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	channelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	checkStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
	uncheckStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	sendBtnStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("206")).Padding(0, 2)
	sendBtnDimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	if len(m.slackRepos) == 0 {
		b.WriteString(dimStyle.Render("  No repos with Slack rooms configured."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Set slack_room in your projects config to enable notifications."))
		b.WriteString("\n")
		return b.String()
	}

	switch m.notifPhase {
	case notifPhaseReady:
		// Token input
		tokenPrefix := "  "
		if m.notifFocus == notifFocusToken {
			tokenPrefix = cursorStyle.Render("▸") + " "
		}
		b.WriteString(fmt.Sprintf("  %s%s\n", tokenPrefix, labelStyle.Render("Slack Bot Token")))
		b.WriteString(hintStyle.Render("      Pre-filled from $SLACK_BOT_TOKEN if set"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("      %s", m.slackTokenInput.View()))
		b.WriteString("\n\n")

		// Repo checkboxes
		repoChannel := make(map[string]string)
		for _, p := range m.selectedProjects {
			room := strings.TrimSpace(p.SlackRoom)
			if room != "" {
				repoChannel[p.Repo] = room
			}
		}

		numRepos := len(m.slackRepos)
		visibleRows := numRepos
		if visibleRows > notifMaxVisibleRepos {
			visibleRows = notifMaxVisibleRepos
		}
		scrollEnd := m.notifScrollOffset + visibleRows

		// Scroll-up indicator
		if m.notifScrollOffset > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    ↑ %d more above", m.notifScrollOffset)))
			b.WriteString("\n")
		}

		for i := m.notifScrollOffset; i < scrollEnd && i < numRepos; i++ {
			repo := m.slackRepos[i]
			isCursor := m.notifFocus == notifFocusRepos && i == m.slackCursor
			isSelected := m.slackSelected[repo]

			prefix := "  "
			if isCursor {
				prefix = cursorStyle.Render("▸") + " "
			}

			check := uncheckStyle.Render("[ ]")
			if isSelected {
				check = checkStyle.Render("[x]")
			}

			channel := repoChannel[repo]
			b.WriteString(fmt.Sprintf("  %s%s %s %s\n", prefix, check, repoStyle.Render(repo), channelStyle.Render("#"+channel)))
		}

		// Scroll-down indicator
		reposBelow := numRepos - scrollEnd
		if reposBelow > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    ↓ %d more below", reposBelow)))
			b.WriteString("\n")
		}

		// Send button
		b.WriteString("\n")
		hasSelected := false
		for _, sel := range m.slackSelected {
			if sel {
				hasSelected = true
				break
			}
		}
		if m.notifFocus == notifFocusSend {
			btnPrefix := cursorStyle.Render("▸") + " "
			if hasSelected {
				b.WriteString(fmt.Sprintf("  %s%s\n", btnPrefix, sendBtnStyle.Render("Send")))
			} else {
				b.WriteString(fmt.Sprintf("  %s%s\n", btnPrefix, sendBtnDimStyle.Render("Send")))
			}
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", sendBtnDimStyle.Render("Send")))
		}

	case notifPhaseSending:
		sendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString(sendingStyle.Render("  Sending notifications to Slack..."))
		b.WriteString("\n")

	case notifPhaseDone:
		if len(m.slackResults) > 0 {
			for _, line := range m.slackResults {
				b.WriteString("  ")
				b.WriteString(dimStyle.Render(line))
				b.WriteString("\n")
			}
		} else {
			b.WriteString(dimStyle.Render("  No notifications sent."))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m dashboardModel) renderDoneHelp() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	retryStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	var hints []string
	hints = append(hints, helpStyle.Render("tab: switch tabs"))

	if m.isNotifTab() {
		switch m.notifPhase {
		case notifPhaseReady:
			hints = append(hints, helpStyle.Render("↑↓: navigate"))
			hints = append(hints, helpStyle.Render("space/x: toggle"))
			hints = append(hints, helpStyle.Render("a: select all"))
		case notifPhaseSending:
			hints = append(hints, helpStyle.Render("sending..."))
		}
	} else if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		if m.activeTab == 0 {
			// Summary tab
			if m.summaryExpanded {
				hints = append(hints, helpStyle.Render("↑↓: scroll"))
				hints = append(hints, helpStyle.Render("enter/esc: close"))
			} else {
				hints = append(hints, helpStyle.Render("enter/l: expand"))
			}
		} else {
			// Projects tab
			if m.expandedFindingRepo != "" {
				hints = append(hints, helpStyle.Render("↑↓: scroll"))
				hints = append(hints, helpStyle.Render("enter/esc: close"))
			} else {
				results := m.doneResults()
				failed := 0
				for _, result := range results {
					if !result.Success {
						failed++
					}
				}
				hints = append(hints, helpStyle.Render("↑↓: navigate"))
				hints = append(hints, helpStyle.Render("enter/l: expand"))
				if failed > 0 {
					hints = append(hints, retryStyle.Render(fmt.Sprintf("r: retry %d failed", failed)))
				}
			}
		}
	} else {
		// Local results tab
		results := m.doneResults()
		failed := 0
		skipped := 0
		for _, result := range results {
			switch {
			case result.Success:
			case result.Skipped:
				skipped++
			default:
				failed++
			}
		}

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
	}
	hints = append(hints, helpStyle.Render("q: exit"))
	return "  " + strings.Join(hints, helpStyle.Render("  •  "))
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
