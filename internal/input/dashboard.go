package input

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/saltpay/copycat/v2/internal/config"
	"github.com/saltpay/copycat/v2/internal/permission"
)

type dashboardPhase int

const (
	phaseProjects dashboardPhase = iota
	phaseWizard
	phaseConfirm
	phaseProcessing
	phaseDone
)

const maxLogLines = 20
const maxSummaryBoxLines = 10
const maxFindingBoxLines = 10
const maxConfirmBoxLines = 10

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
	notifFocusToken       notifFocus = iota // text input for token
	notifFocusSummaryRoom                   // summary room checkbox + text input (assessment only)
	notifFocusRepos                         // repo checkbox list
	notifFocusSend                          // send button
)

// clipboardFeedbackMsg clears the clipboard feedback after a delay.
type clipboardFeedbackMsg struct{}

// slackSendDoneMsg carries the results of sending Slack notifications.
type slackSendDoneMsg struct {
	Results []string
}

// slackStatusMsg carries a progress line during Slack notification sending.
type slackStatusMsg struct {
	Line string
}

// notifTickMsg drives the spinner animation on the notifications tab.
type notifTickMsg time.Time

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
	SendSlackAssessmentSummary  func(summary string, channel string, token string, onStatus func(string))
	FormatForSlack              func(aiTool *config.AITool, text string) (string, error)
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
	summaryScrollOffset int    // scroll offset within the summary box

	// Clipboard feedback
	clipboardFeedback string // transient message shown after copy

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
	slackStatusLines  []string // live progress lines during sending
	notifTickCount    int      // spinner animation counter

	// Assessment summary Slack room (assessment only)
	summarySlackEnabled   bool            // whether to send summary to a separate room
	summarySlackRoomInput textinput.Model // text input for the room name

	// Slack send confirmation
	notifConfirming bool // true when showing "are you sure?" before sending

	// Done screen status filter
	doneStatusFilter string // "" = all, "succeeded", "failed", "skipped"

	// Confirm screen navigation
	confirmCursor       int    // which row is highlighted
	confirmExpanded     string // which item key is expanded ("projects", "prompt", or "")
	confirmScrollOffset int    // scroll offset within expanded box
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
	case phaseConfirm:
		return m.updateConfirm(msg)
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
		m.phase = phaseConfirm
		m.confirmCursor = 0
		m.confirmExpanded = ""
		m.confirmScrollOffset = 0
		return m, nil

	case wizardBackMsg:
		m.phase = phaseProjects
		return m, m.projects.Init()

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
		m.phase = phaseConfirm
		m.confirmCursor = 0
		m.confirmExpanded = ""
		m.confirmScrollOffset = 0
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

// confirmItem describes one row in the confirm screen.
type confirmItem struct {
	key    string // unique key: "projects", "action", "ai_tool", "branch", "branch_name", "pr_title", "prompt", "ignore"
	label  string
	value  string // short display value
	detail string // full content for scrollable box (empty = not expandable)
}

func (m dashboardModel) buildConfirmItems() []confirmItem {
	var items []confirmItem

	// Projects (expandable)
	names := make([]string, 0, len(m.selectedProjects))
	for _, p := range m.selectedProjects {
		names = append(names, p.Repo)
	}
	items = append(items, confirmItem{
		key:    "projects",
		label:  "Projects",
		value:  fmt.Sprintf("%d selected", len(m.selectedProjects)),
		detail: strings.Join(names, "\n"),
	})

	// AI Tool
	if m.wizardResult.AITool != nil {
		items = append(items, confirmItem{key: "ai_tool", label: "AI Tool", value: m.wizardResult.AITool.Name})
	}

	// Local-only fields
	if m.wizardResult.Action == "local" {
		if m.wizardResult.BranchStrategy != "" {
			items = append(items, confirmItem{key: "branch", label: "Branch", value: m.wizardResult.BranchStrategy})
		}
		if m.wizardResult.BranchName != "" {
			items = append(items, confirmItem{key: "branch_name", label: "Branch Name", value: m.wizardResult.BranchName})
		}
		if m.wizardResult.PRTitle != "" {
			items = append(items, confirmItem{key: "pr_title", label: "PR Title", value: m.wizardResult.PRTitle})
		}
	}

	// Prompt (expandable)
	promptLabel := "Prompt"
	if m.wizardResult.Action == "assessment" {
		promptLabel = "Question"
	}
	prompt := m.wizardResult.Prompt
	shortPrompt := strings.ReplaceAll(prompt, "\n", " ")
	if len(shortPrompt) > 80 {
		shortPrompt = shortPrompt[:77] + "..."
	}
	detail := ""
	if len(prompt) > 80 || strings.Contains(prompt, "\n") {
		detail = prompt
	}
	items = append(items, confirmItem{key: "prompt", label: promptLabel, value: shortPrompt, detail: detail})

	// Ignore instructions
	if m.wizardResult.IgnoreAgentInstructions {
		items = append(items, confirmItem{key: "ignore", label: "Ignore Agent Instructions", value: "Yes"})
	}

	// Start button (always last)
	startLabel := "Start"
	if m.wizardResult.Action == "assessment" {
		startLabel = "Start Assessment"
	}
	items = append(items, confirmItem{key: "start", label: startLabel})

	return items
}

func (m dashboardModel) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	items := m.buildConfirmItems()

	// If a box is expanded, handle scroll/close
	if m.confirmExpanded != "" {
		switch keyMsg.String() {
		case "esc", "enter", "q":
			m.confirmExpanded = ""
			m.confirmScrollOffset = 0
		case "up", "k":
			if m.confirmScrollOffset > 0 {
				m.confirmScrollOffset--
			}
		case "down", "j":
			// Find expanded item detail
			for _, item := range items {
				if item.key == m.confirmExpanded {
					lines := strings.Split(item.detail, "\n")
					maxScroll := len(lines) - maxConfirmBoxLines
					if maxScroll < 0 {
						maxScroll = 0
					}
					if m.confirmScrollOffset < maxScroll {
						m.confirmScrollOffset++
					}
					break
				}
			}
		}
		return m, nil
	}

	switch keyMsg.String() {
	case "enter":
		if m.confirmCursor >= 0 && m.confirmCursor < len(items) {
			item := items[m.confirmCursor]
			if item.key == "start" {
				return m.startProcessing()
			}
			if item.detail != "" {
				m.confirmExpanded = item.key
				m.confirmScrollOffset = 0
				return m, nil
			}
		}
	case "up", "k":
		if m.confirmCursor > 0 {
			m.confirmCursor--
		}
	case "down", "j":
		if m.confirmCursor < len(items)-1 {
			m.confirmCursor++
		}
	case "esc", "b":
		m.confirmCursor = 0
		m.phase = phaseWizard
		return m, nil
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m dashboardModel) renderConfirmView() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	btnStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("206")).Padding(0, 2)
	btnDimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("240")).Padding(0, 2)

	b.WriteString(titleStyle.Render("Confirm & Run"))
	b.WriteString("\n\n")

	items := m.buildConfirmItems()

	for i, item := range items {
		selected := i == m.confirmCursor && m.confirmExpanded == ""

		// Start button gets special rendering
		if item.key == "start" {
			b.WriteString("\n")
			if selected {
				b.WriteString(fmt.Sprintf("  %s %s\n", cursorStyle.Render("▸"), btnStyle.Render(item.label)))
			} else {
				b.WriteString(fmt.Sprintf("    %s\n", btnDimStyle.Render(item.label)))
			}
			continue
		}

		expandable := item.detail != ""

		// Cursor prefix
		expanded := m.confirmExpanded == item.key
		prefix := "  "
		if expanded {
			prefix = cursorStyle.Render("v ")
		} else if selected {
			prefix = cursorStyle.Render("> ")
		}

		// Expandable hint
		hint := ""
		if expandable && selected {
			hint = " " + dimStyle.Render("[enter: expand]")
		}

		b.WriteString(fmt.Sprintf("  %s%s %s%s\n",
			prefix,
			labelStyle.Render(item.label+":"),
			valueStyle.Render(item.value),
			hint,
		))

		// Render expanded box if this item is expanded
		if m.confirmExpanded == item.key {
			m.renderConfirmBox(&b, item.detail)
		}
	}

	b.WriteString("\n")
	if m.confirmExpanded != "" {
		b.WriteString(helpStyle.Render("  up/down: scroll • esc/enter: close"))
	} else {
		b.WriteString(helpStyle.Render("  up/down: navigate • enter: select • esc/b: back • q: quit"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m dashboardModel) renderConfirmBox(b *strings.Builder, content string) {
	lines := strings.Split(content, "\n")
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	boxWidth := m.termWidth - 12
	if boxWidth < 40 {
		boxWidth = 40
	}
	maxContentWidth := boxWidth - 4

	scrollStart := m.confirmScrollOffset
	scrollEnd := scrollStart + maxConfirmBoxLines
	if scrollEnd > len(lines) {
		scrollEnd = len(lines)
	}

	var contentLines []string
	if scrollStart > 0 {
		contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↑ %d more", scrollStart)))
	}
	for _, line := range lines[scrollStart:scrollEnd] {
		if len(line) > maxContentWidth {
			line = line[:maxContentWidth-3] + "..."
		}
		contentLines = append(contentLines, lineStyle.Render(line))
	}
	remaining := len(lines) - scrollEnd
	if remaining > 0 {
		contentLines = append(contentLines, dimStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)))
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1).
		Width(boxWidth)

	rendered := boxStyle.Render(strings.Join(contentLines, "\n"))
	for _, boxLine := range strings.Split(rendered, "\n") {
		b.WriteString("      " + boxLine + "\n")
	}
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
			log.Printf("⚠ Failed to start permission server: %v", err)
		} else {
			m.permServer = permServer
			mcpPath, cleanup, err := permission.GenerateMCPConfig(permServer.Port())
			if err != nil {
				log.Printf("⚠ Failed to generate MCP config: %v", err)
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
	case ProjectStatusMsg, ProjectDoneMsg, permission.PermissionRequestMsg, PostStatusMsg, PostStatusReplaceMsg, AssessmentResultMsg, PromptUpdateMsg:
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

func (m dashboardModel) notifTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return notifTickMsg(t)
	})
}

func (m dashboardModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle clipboard feedback clear
	if _, ok := msg.(clipboardFeedbackMsg); ok {
		m.clipboardFeedback = ""
		return m, nil
	}

	// Handle notification spinner tick
	if _, ok := msg.(notifTickMsg); ok {
		if m.notifPhase == notifPhaseSending {
			m.notifTickCount++
			return m, m.notifTickCmd()
		}
		return m, nil
	}

	// Handle Slack progress line during sending
	if statusMsg, ok := msg.(slackStatusMsg); ok {
		m.slackStatusLines = append(m.slackStatusLines, statusMsg.Line)
		return m, listenForStatus(m.statusCh)
	}

	// Handle Slack send done message (works for any tab)
	if slackDone, ok := msg.(slackSendDoneMsg); ok {
		m.notifPhase = notifPhaseDone
		m.slackResults = slackDone.Results
		m.slackStatusLines = nil
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		tokenInputFocused := m.isNotifTab() && ((m.notifFocus == notifFocusToken && m.slackTokenInput.Focused()) ||
			(m.notifFocus == notifFocusSummaryRoom && m.summarySlackEnabled && m.summarySlackRoomInput.Focused()))

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
			m.summarySlackRoomInput.Blur()
			m.activeTab = newTab
			if m.activeTab == tabCount-1 && m.notifPhase == notifPhaseReady {
				if m.notifFocus == notifFocusToken {
					m.slackTokenInput.Focus()
				} else if m.notifFocus == notifFocusSummaryRoom && m.summarySlackEnabled {
					m.summarySlackRoomInput.Focus()
				}
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
	// Forward text input updates for summary room entry
	if m.isNotifTab() && m.notifFocus == notifFocusSummaryRoom && m.summarySlackEnabled {
		var cmd tea.Cmd
		m.summarySlackRoomInput, cmd = m.summarySlackRoomInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateDoneResultsTab handles keys on the Results tab (local workflow).
func (m dashboardModel) updateDoneResultsTab(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When a log is expanded, handle inner navigation
	if m.expandedLogRepo != "" {
		switch keyMsg.String() {
		case "enter", "l", "esc", "h":
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
		case "ctrl+u", "pgup":
			m.logScrollOffset -= maxLogLines / 2
			if m.logScrollOffset < 0 {
				m.logScrollOffset = 0
			}
			return m, nil
		case "ctrl+d", "pgdown":
			results := m.doneResults()
			if result, ok := results[m.expandedLogRepo]; ok {
				lines := aiOutputLines(result.AIOutput)
				maxScroll := len(lines) - maxLogLines
				if maxScroll < 0 {
					maxScroll = 0
				}
				m.logScrollOffset += maxLogLines / 2
				if m.logScrollOffset > maxScroll {
					m.logScrollOffset = maxScroll
				}
			}
			return m, nil
		}
		return m, nil
	}

	switch keyMsg.String() {
	case "c":
		text := m.buildLocalReport()
		if err := clipboard.WriteAll(text); err != nil {
			m.clipboardFeedback = "Failed to copy"
		} else {
			m.clipboardFeedback = "Copied to clipboard"
		}
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clipboardFeedbackMsg{} })
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
	case "s":
		switch m.doneStatusFilter {
		case "":
			m.doneStatusFilter = "succeeded"
		case "succeeded":
			m.doneStatusFilter = "failed"
		case "failed":
			m.doneStatusFilter = "skipped"
		case "skipped":
			m.doneStatusFilter = ""
		}
		m.doneScrollOffset = 0
		repos := m.doneVisibleRepos()
		if len(repos) > 0 {
			m.doneCursorRepo = repos[0]
		} else {
			m.doneCursorRepo = ""
		}
		return m, nil
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
		switch keyMsg.String() {
		case "c":
			text := m.buildAssessmentReport()
			if err := clipboard.WriteAll(text); err != nil {
				m.clipboardFeedback = "⚠ Failed to copy"
			} else {
				m.clipboardFeedback = "✓ Copied to clipboard"
			}
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clipboardFeedbackMsg{} })
		case "up", "k":
			if m.summaryScrollOffset > 0 {
				m.summaryScrollOffset--
			}
			return m, nil
		case "down", "j":
			lines := strings.Split(m.assessmentSummary, "\n")
			maxScroll := len(lines) - maxSummaryBoxLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.summaryScrollOffset < maxScroll {
				m.summaryScrollOffset++
			}
			return m, nil
		case "ctrl+u", "pgup":
			m.summaryScrollOffset -= maxSummaryBoxLines / 2
			if m.summaryScrollOffset < 0 {
				m.summaryScrollOffset = 0
			}
			return m, nil
		case "ctrl+d", "pgdown":
			lines := strings.Split(m.assessmentSummary, "\n")
			maxScroll := len(lines) - maxSummaryBoxLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			m.summaryScrollOffset += maxSummaryBoxLines / 2
			if m.summaryScrollOffset > maxScroll {
				m.summaryScrollOffset = maxScroll
			}
			return m, nil
		}
		return m, nil
	}

	// Projects tab (tab 1)
	if m.expandedFindingRepo != "" {
		switch keyMsg.String() {
		case "enter", "l", "esc", "h":
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
				maxScroll := len(lines) - maxFindingBoxLines
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.findingScrollOffset < maxScroll {
					m.findingScrollOffset++
				}
			}
			return m, nil
		case "ctrl+u", "pgup":
			m.findingScrollOffset -= maxFindingBoxLines / 2
			if m.findingScrollOffset < 0 {
				m.findingScrollOffset = 0
			}
			return m, nil
		case "ctrl+d", "pgdown":
			finding := m.assessmentFindings[m.expandedFindingRepo]
			if finding != "" {
				lines := strings.Split(strings.TrimSpace(finding), "\n")
				maxScroll := len(lines) - maxFindingBoxLines
				if maxScroll < 0 {
					maxScroll = 0
				}
				m.findingScrollOffset += maxFindingBoxLines / 2
				if m.findingScrollOffset > maxScroll {
					m.findingScrollOffset = maxScroll
				}
			}
			return m, nil
		}
		return m, nil
	}

	switch keyMsg.String() {
	case "c":
		text := m.buildAssessmentReport()
		if err := clipboard.WriteAll(text); err != nil {
			m.clipboardFeedback = "⚠ Failed to copy"
		} else {
			m.clipboardFeedback = "✓ Copied to clipboard"
		}
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clipboardFeedbackMsg{} })
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

// isAssessmentMode returns true when the current workflow is an assessment.
func (m dashboardModel) isAssessmentMode() bool {
	return m.wizardResult != nil && m.wizardResult.Action == "assessment"
}

// notifFocusAfterToken returns the next focus element after the token input.
func (m dashboardModel) notifFocusAfterToken() notifFocus {
	if m.isAssessmentMode() && m.assessmentSummary != "" {
		return notifFocusSummaryRoom
	}
	if len(m.slackRepos) > 0 {
		return notifFocusRepos
	}
	return notifFocusSend
}

// notifFocusBeforeRepos returns the focus element before the repo list.
func (m dashboardModel) notifFocusBeforeRepos() notifFocus {
	if m.isAssessmentMode() && m.assessmentSummary != "" {
		return notifFocusSummaryRoom
	}
	return notifFocusToken
}

// updateDoneNotifTab handles keys on the Notifications tab.
func (m dashboardModel) updateDoneNotifTab(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.notifPhase {
	case notifPhaseReady:
		switch m.notifFocus {
		case notifFocusToken:
			switch keyMsg.Type {
			case tea.KeyEnter, tea.KeyDown:
				m.slackToken = strings.TrimSpace(m.slackTokenInput.Value())
				m.slackTokenInput.Blur()
				next := m.notifFocusAfterToken()
				m.notifFocus = next
				if next == notifFocusSummaryRoom {
					m.summarySlackRoomInput.Focus()
				} else if next == notifFocusRepos {
					m.slackCursor = 0
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.slackTokenInput, cmd = m.slackTokenInput.Update(keyMsg)
			return m, cmd

		case notifFocusSummaryRoom:
			// Navigation keys always work regardless of input state
			switch keyMsg.Type {
			case tea.KeyEnter:
				m.summarySlackRoomInput.Blur()
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = 0
				} else {
					m.notifFocus = notifFocusSend
				}
				return m, nil
			case tea.KeyDown:
				m.summarySlackRoomInput.Blur()
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = 0
				} else {
					m.notifFocus = notifFocusSend
				}
				return m, nil
			case tea.KeyUp:
				m.summarySlackRoomInput.Blur()
				m.notifFocus = notifFocusToken
				m.slackTokenInput.Focus()
				return m, nil
			}

			// When enabled and input is focused, forward keys to text input
			if m.summarySlackEnabled && m.summarySlackRoomInput.Focused() {
				// Space on empty input unchecks the checkbox
				if keyMsg.String() == " " && strings.TrimSpace(m.summarySlackRoomInput.Value()) == "" {
					m.summarySlackEnabled = false
					m.summarySlackRoomInput.Blur()
					return m, nil
				}
				var cmd tea.Cmd
				m.summarySlackRoomInput, cmd = m.summarySlackRoomInput.Update(keyMsg)
				return m, cmd
			}

			// Checkbox is not checked — space toggles it on
			if keyMsg.String() == " " {
				m.summarySlackEnabled = !m.summarySlackEnabled
				if m.summarySlackEnabled {
					m.summarySlackRoomInput.Focus()
				}
				return m, nil
			}
			// Starting to type a letter enables the checkbox and begins input
			ch := keyMsg.String()
			if len(ch) == 1 && ((ch[0] >= 'a' && ch[0] <= 'z') || (ch[0] >= 'A' && ch[0] <= 'Z')) {
				m.summarySlackEnabled = true
				m.summarySlackRoomInput.Focus()
				var cmd tea.Cmd
				m.summarySlackRoomInput, cmd = m.summarySlackRoomInput.Update(keyMsg)
				return m, cmd
			}
			return m, nil

		case notifFocusRepos:
			switch keyMsg.String() {
			case "up", "k":
				if m.slackCursor > 0 {
					m.slackCursor--
				} else {
					prev := m.notifFocusBeforeRepos()
					m.notifFocus = prev
					if prev == notifFocusSummaryRoom {
						if m.summarySlackEnabled {
							m.summarySlackRoomInput.Focus()
						}
					} else {
						m.slackTokenInput.Focus()
					}
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
				if m.notifConfirming {
					return m, nil
				}
				if len(m.slackRepos) > 0 {
					m.notifFocus = notifFocusRepos
					m.slackCursor = len(m.slackRepos) - 1
				} else if m.isAssessmentMode() && m.assessmentSummary != "" {
					m.notifFocus = notifFocusSummaryRoom
					m.summarySlackRoomInput.Focus()
				} else {
					m.notifFocus = notifFocusToken
					m.slackTokenInput.Focus()
				}
			case "esc":
				if m.notifConfirming {
					m.notifConfirming = false
					return m, nil
				}
			case "enter":
				token := strings.TrimSpace(m.slackTokenInput.Value())
				if token == "" {
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
				if hasSelected || m.summarySlackEnabled {
					if !m.notifConfirming {
						m.notifConfirming = true
						return m, nil
					}
					m.notifConfirming = false
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
		sendSummaryFn := m.cfg.SendSlackAssessmentSummary
		formatFn := m.cfg.FormatForSlack
		aiTool := m.wizardResult.AITool
		summaryEnabled := m.summarySlackEnabled
		summaryRoom := strings.TrimSpace(m.summarySlackRoomInput.Value())
		summary := m.assessmentSummary

		go func() {
			var results []string
			status := func(line string) {
				ch <- slackStatusMsg{Line: line}
				results = append(results, line)
			}

			// Format findings for Slack (only for selected repos)
			formattedFindings := make(map[string]string, len(sendProjects))
			for _, p := range sendProjects {
				if finding, ok := findings[p.Repo]; ok {
					formattedFindings[p.Repo] = finding
				}
			}
			if formatFn != nil && aiTool != nil && len(formattedFindings) > 0 {
				status("Formatting findings for Slack...")
				for _, p := range sendProjects {
					finding, ok := formattedFindings[p.Repo]
					if !ok {
						continue
					}
					if formatted, err := formatFn(aiTool, finding); err == nil {
						formattedFindings[p.Repo] = formatted
						status(fmt.Sprintf("✓ Formatted %s", p.Repo))
					} else {
						status(fmt.Sprintf("⚠  Failed to format %s, using original: %v", p.Repo, err))
					}
				}
			}

			if sendFn != nil && len(sendProjects) > 0 {
				sendFn(sendProjects, question, formattedFindings, token, status)
			}
			if summaryEnabled && summaryRoom != "" && sendSummaryFn != nil {
				formattedSummary := summary
				if formatFn != nil && aiTool != nil {
					status("Formatting summary for Slack...")
					if formatted, err := formatFn(aiTool, summary); err == nil {
						formattedSummary = formatted
						status("✓ Summary formatted")
					} else {
						status(fmt.Sprintf("⚠  Failed to format summary, using original: %v", err))
					}
				}
				sendSummaryFn(formattedSummary, summaryRoom, token, status)
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

	return m, tea.Batch(listenForStatus(m.statusCh), m.notifTickCmd())
}

func (m dashboardModel) initDoneScreen() dashboardModel {
	m.activeTab = 0
	m.doneScrollOffset = 0
	m.expandedLogRepo = ""
	m.logScrollOffset = 0
	m.expandedFindingRepo = ""
	m.findingScrollOffset = 0
	m.summaryScrollOffset = 0
	m.slackResults = nil
	m.slackStatusLines = nil

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
	tokenInput.EchoMode = textinput.EchoPassword
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

	// Initialize summary room input (assessment only)
	summaryRoomInput := textinput.New()
	summaryRoomInput.Placeholder = "#channel-name"
	summaryRoomInput.CharLimit = 100
	summaryRoomInput.Width = 40
	m.summarySlackEnabled = false
	m.summarySlackRoomInput = summaryRoomInput

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

// doneVisibleRepos returns the list of repos that have results, filtered by doneStatusFilter.
func (m dashboardModel) doneVisibleRepos() []string {
	results := m.doneResults()
	var repos []string
	for _, repo := range m.progress.repos {
		result, ok := results[repo]
		if !ok {
			continue
		}
		switch m.doneStatusFilter {
		case "succeeded":
			if !result.Success {
				continue
			}
		case "failed":
			if result.Success || result.Skipped {
				continue
			}
		case "skipped":
			if !result.Skipped {
				continue
			}
		}
		repos = append(repos, repo)
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
			if boxLines > maxFindingBoxLines {
				boxLines = maxFindingBoxLines
			}
			boxLines += 2 // border top + bottom
			if len(lines) > maxFindingBoxLines {
				if m.findingScrollOffset > 0 {
					boxLines++
				}
				if m.findingScrollOffset+maxFindingBoxLines < len(lines) {
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
	case phaseConfirm:
		content = m.renderConfirmView()
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

	// Phase indicator
	phaseIndicator := m.renderPhaseIndicator()

	return banner + "\n" + phaseIndicator + borderStyle.Render(content)
}

// renderPhaseIndicator renders a breadcrumb showing the current workflow phase.
func (m dashboardModel) renderPhaseIndicator() string {
	type phaseLabel struct {
		name  string
		phase dashboardPhase
	}
	phases := []phaseLabel{
		{"Projects", phaseProjects},
		{"Wizard", phaseWizard},
		{"Confirm", phaseConfirm},
		{"Processing", phaseProcessing},
		{"Done", phaseDone},
	}

	sep := stDim.Render(" > ")
	var parts []string
	for _, p := range phases {
		if p.phase == m.phase {
			parts = append(parts, stAccent.Render(p.name))
		} else if p.phase < m.phase {
			parts = append(parts, stDone.Render(p.name))
		} else {
			parts = append(parts, stDim.Render(p.name))
		}
	}
	return " " + strings.Join(parts, sep) + "\n"
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
	if m.clipboardFeedback != "" {
		feedbackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
		b.WriteString("  " + feedbackStyle.Render(m.clipboardFeedback))
	}
	b.WriteString("\n\n")

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
	if m.doneStatusFilter != "" {
		filterStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		b.WriteString("  ")
		b.WriteString(filterStyle.Render(fmt.Sprintf("[filter: %s]", m.doneStatusFilter)))
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

	if m.assessmentSummary == "" {
		b.WriteString(dimStyle.Render("  No summary available."))
		b.WriteString("\n")
		return b.String()
	}

	summaryLines := strings.Split(m.assessmentSummary, "\n")

	summaryBoxWidth := m.termWidth - 10
	if summaryBoxWidth < 40 {
		summaryBoxWidth = 40
	}
	maxContentWidth := summaryBoxWidth - 4

	scrollStart := m.summaryScrollOffset
	scrollEnd := scrollStart + maxSummaryBoxLines
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

	borderColor := lipgloss.Color("238")
	if len(summaryLines) > maxSummaryBoxLines {
		borderColor = lipgloss.Color("33")
	}
	summaryBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(summaryBoxWidth)

	rendered := summaryBoxStyle.Render(strings.Join(boxContent, "\n"))
	for _, boxLine := range strings.Split(rendered, "\n") {
		b.WriteString("    " + boxLine + "\n")
	}

	return b.String()
}

func (m dashboardModel) renderAssessProjectsTabContent() string {
	var b strings.Builder

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
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
			prefix = cursorStyle.Render(">") + " "
		}

		if result.Success {
			finding := m.assessmentFindings[repo]

			detailsBtn := ""
			if finding != "" {
				if isExpanded {
					detailsBtn = " " + detailBtnActiveStyle.Render("[▼ details]")
				} else {
					detailsBtn = " " + detailBtnStyle.Render("[▶ details]")
				}
			}

			b.WriteString(fmt.Sprintf("%s%s%s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), detailsBtn))
		} else {
			b.WriteString(fmt.Sprintf("%s%s Failed ⚠ %s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), result.Status))
		}

		if isExpanded {
			finding := m.assessmentFindings[repo]
			if finding != "" {
				lines := strings.Split(strings.TrimSpace(finding), "\n")
				if len(lines) > 0 {
					findingStart := m.findingScrollOffset
					findingEnd := findingStart + maxFindingBoxLines
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
	sendBtnDimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("240")).Padding(0, 2)

	hasSummaryOption := m.isAssessmentMode() && m.assessmentSummary != ""

	if len(m.slackRepos) == 0 && !hasSummaryOption {
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

		// Summary room checkbox (assessment only)
		if hasSummaryOption {
			summaryPrefix := "  "
			if m.notifFocus == notifFocusSummaryRoom {
				summaryPrefix = cursorStyle.Render("▸") + " "
			}

			check := uncheckStyle.Render("[ ]")
			if m.summarySlackEnabled {
				check = checkStyle.Render("[x]")
			}

			b.WriteString(fmt.Sprintf("  %s%s %s\n", summaryPrefix, check, labelStyle.Render("Send summary to a channel")))
			if m.summarySlackEnabled {
				b.WriteString(fmt.Sprintf("      %s\n", m.summarySlackRoomInput.View()))
			}
			b.WriteString("\n")
		}

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
		hasSomethingToSend := hasSelected || m.summarySlackEnabled
		if m.notifFocus == notifFocusSend {
			btnPrefix := cursorStyle.Render("▸") + " "
			if m.notifConfirming {
				channelCount := 0
				for _, sel := range m.slackSelected {
					if sel {
						channelCount++
					}
				}
				if m.summarySlackEnabled {
					channelCount++
				}
				confirmStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
				confirmMsg := fmt.Sprintf("Send to %d channel(s)? enter: yes • esc: cancel", channelCount)
				b.WriteString(fmt.Sprintf("  %s%s\n", btnPrefix, confirmStyle.Render(confirmMsg)))
			} else if hasSomethingToSend {
				b.WriteString(fmt.Sprintf("  %s%s\n", btnPrefix, sendBtnStyle.Render("Send")))
			} else {
				b.WriteString(fmt.Sprintf("  %s%s\n", btnPrefix, sendBtnDimStyle.Render("Send")))
			}
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", sendBtnDimStyle.Render("Send")))
		}

	case notifPhaseSending:
		frame := spinnerFrames[m.notifTickCount%len(spinnerFrames)]
		sColor := spinnerColors[m.notifTickCount%len(spinnerColors)]
		sStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sColor))

		if len(m.slackStatusLines) > 0 {
			for i, line := range m.slackStatusLines {
				isLast := i == len(m.slackStatusLines)-1
				if isLast && !strings.HasPrefix(line, "✓") && !strings.HasPrefix(line, "⚠") {
					b.WriteString("  " + sStyle.Render(frame) + " ")
				} else {
					b.WriteString("  ")
				}
				b.WriteString(dimStyle.Render(line))
				b.WriteString("\n")
			}
		} else {
			b.WriteString("  " + sStyle.Render(frame) + " ")
			b.WriteString(dimStyle.Render("Sending notifications to Slack..."))
			b.WriteString("\n")
		}

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
			hints = append(hints, helpStyle.Render("space: toggle"))
			hints = append(hints, helpStyle.Render("a: select all"))
		case notifPhaseSending:
			hints = append(hints, helpStyle.Render("sending..."))
		}
	} else if m.wizardResult != nil && m.wizardResult.Action == "assessment" {
		if m.activeTab == 0 {
			// Summary tab
			hints = append(hints, helpStyle.Render("↑↓: scroll"))
			hints = append(hints, helpStyle.Render("pgup/pgdn: page"))
		} else {
			// Projects tab
			if m.expandedFindingRepo != "" {
				hints = append(hints, helpStyle.Render("↑↓: scroll"))
				hints = append(hints, helpStyle.Render("pgup/pgdn: page"))
				hints = append(hints, helpStyle.Render("h/esc: close"))
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
		hints = append(hints, helpStyle.Render("c: copy report"))
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
			hints = append(hints, helpStyle.Render("↑↓: scroll"))
			hints = append(hints, helpStyle.Render("pgup/pgdn: page"))
			hints = append(hints, helpStyle.Render("h/esc: close"))
		} else {
			hints = append(hints, helpStyle.Render("↑↓: navigate"))
			hints = append(hints, helpStyle.Render("enter/l: view logs"))
			hints = append(hints, helpStyle.Render("s: filter status"))
			hints = append(hints, helpStyle.Render("c: copy report"))
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

// buildAssessmentReport builds a plain-text report of the assessment for clipboard.
func (m dashboardModel) buildAssessmentReport() string {
	var b strings.Builder

	b.WriteString("# Assessment Report\n\n")

	if m.assessmentSummary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(m.assessmentSummary)
		b.WriteString("\n\n")
	}

	if len(m.assessmentFindings) > 0 {
		b.WriteString("## Per-Project Findings\n\n")
		for _, repo := range m.progress.repos {
			finding, exists := m.assessmentFindings[repo]
			if !exists || finding == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s\n\n", repo))
			b.WriteString(finding)
			b.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}

// buildLocalReport builds a plain-text report of local results for clipboard.
func (m dashboardModel) buildLocalReport() string {
	var b strings.Builder

	b.WriteString("# Results Report\n\n")

	results := m.doneResults()
	for _, repo := range m.progress.repos {
		result, ok := results[repo]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n", repo))
		b.WriteString(fmt.Sprintf("Status: %s\n", result.Status))
		if result.PRURL != "" {
			b.WriteString(fmt.Sprintf("PR: %s\n", result.PRURL))
		}
		if result.AIOutput != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", strings.TrimSpace(result.AIOutput)))
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
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
