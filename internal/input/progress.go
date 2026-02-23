package input

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/saltpay/copycat/internal/permission"
)

const maxVisibleProjects = 10
const maxPermissionCmdLines = 8

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerColors = []string{"205", "213", "141", "111", "75", "33", "40", "48", "214", "208"}

// CancelRegistry is a thread-safe map of repo -> context.CancelFunc.
type CancelRegistry struct {
	funcs sync.Map
}

// Register stores a cancel function for a repo.
func (r *CancelRegistry) Register(repo string, cancel context.CancelFunc) {
	r.funcs.Store(repo, cancel)
}

// Cancel calls and removes the cancel function for a repo.
func (r *CancelRegistry) Cancel(repo string) {
	if val, ok := r.funcs.LoadAndDelete(repo); ok {
		val.(context.CancelFunc)()
	}
}

// processingDoneMsg signals that all projects have finished processing.
type processingDoneMsg struct{}

// resumeProcessingMsg signals that the user has confirmed to continue processing.
type resumeProcessingMsg struct{}

// cancelProjectMsg requests cancellation of a single project.
type cancelProjectMsg struct {
	Repo string
}

// ProjectStatusMsg updates the status line for a single project.
type ProjectStatusMsg struct {
	Repo   string
	Status string
}

// ProjectDoneMsg signals that a project has finished processing.
type ProjectDoneMsg struct {
	Repo     string
	Status   string
	Success  bool
	Skipped  bool
	PRURL    string
	Error    error
	AIOutput string
}

// PostStatusMsg carries a post-processing status line (e.g. Slack notifications).
type PostStatusMsg struct {
	Line string
}

// AssessmentResultMsg carries the final assessment summary and per-project findings.
type AssessmentResultMsg struct {
	Summary  string
	Findings map[string]string
}

// StatusSender sends status updates to the progress dashboard.
type StatusSender struct {
	send           func(tea.Msg)
	ResumeCh       chan struct{}
	MCPConfigPath  string
	CancelRegistry *CancelRegistry
}

// UpdateStatus updates the status line for a project.
func (s *StatusSender) UpdateStatus(repo, status string) {
	s.send(ProjectStatusMsg{Repo: repo, Status: status})
}

// Done signals that a project has finished processing.
func (s *StatusSender) Done(repo, status string, success, skipped bool, prURL string, err error, aiOutput string) {
	s.send(ProjectDoneMsg{
		Repo:     repo,
		Status:   status,
		Success:  success,
		Skipped:  skipped,
		PRURL:    prURL,
		Error:    err,
		AIOutput: aiOutput,
	})
}

// PostStatus sends a post-processing status line to the progress view.
func (s *StatusSender) PostStatus(line string) {
	s.send(PostStatusMsg{Line: line})
}

// AssessmentResult sends the final assessment summary and per-project findings.
func (s *StatusSender) AssessmentResult(summary string, findings map[string]string) {
	s.send(AssessmentResultMsg{Summary: summary, Findings: findings})
}

// Finish signals that all processing (including post-processing) is done.
func (s *StatusSender) Finish() {
	s.send(processingDoneMsg{})
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

	postLines []string

	paused             bool
	checkpointInterval int
	nextCheckpoint     int

	// Spinner animation
	tickCount int

	// Cursor-based navigation (tracks by repo name for stability)
	cursorRepo   string
	manualScroll bool
	scrollOffset int

	// Cancel support
	cancelRegistry *CancelRegistry
	cancelled      map[string]bool

	// Permission prompting
	permissionQueue     []permission.PermissionRequest
	currentPermission   *permission.PermissionRequest
	permissionChoice    int // 0=approve, 1=deny, 2=approve-all
	approvedPatterns    map[string]bool
	permissionCmdScroll int // scroll offset for the command box

	// Question prompting (AskUserQuestion)
	questionOptionIdx int // currently highlighted option index

	// Context from wizard (displayed as header)
	branchName     string
	prTitle        string
	prompt         string
	promptExpanded bool
	cursorOnPrompt bool
}

// NewProgressModel creates a new progress model for tracking repository processing.
// checkpointInterval controls how often the user is asked to confirm (0 = no checkpoints).
func NewProgressModel(repos []string, checkpointInterval int, branchName, prTitle, prompt string) progressModel {
	statuses := make(map[string]string)
	for _, repo := range repos {
		statuses[repo] = "Waiting..."
	}
	var cursorRepo string
	if len(repos) > 0 {
		cursorRepo = repos[0]
	}
	return progressModel{
		repos:              repos,
		statuses:           statuses,
		results:            make(map[string]ProjectDoneMsg),
		total:              len(repos),
		startTime:          time.Now(),
		checkpointInterval: checkpointInterval,
		nextCheckpoint:     checkpointInterval,
		cursorRepo:         cursorRepo,
		cancelled:          make(map[string]bool),
		approvedPatterns:   make(map[string]bool),
		branchName:         branchName,
		prTitle:            prTitle,
		prompt:             prompt,
	}
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(tea.ClearScreen, m.tickCmd())
}

type tickMsg time.Time

func (m progressModel) tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
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
		if m.checkpointInterval > 0 && m.completed < m.total && m.completed >= m.nextCheckpoint {
			m.paused = true
		}
	case PostStatusMsg:
		m.postLines = append(m.postLines, msg.Line)
	case permission.PermissionRequestMsg:
		return m.handlePermissionRequest(msg.Request)
	case tickMsg:
		m.tickCount++
		return m, m.tickCmd()
	case tea.KeyMsg:
		// Permission input takes priority
		if m.currentPermission != nil {
			return m.handlePermissionKey(msg)
		}
		if m.paused && msg.String() == "enter" {
			m.paused = false
			m.nextCheckpoint += m.checkpointInterval
			return m, func() tea.Msg { return resumeProcessingMsg{} }
		}
		switch msg.String() {
		case "ctrl+c":
			m.quitted = true
			return m, tea.Quit
		case "enter":
			if m.cursorOnPrompt && m.prompt != "" {
				m.promptExpanded = !m.promptExpanded
			}
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "x":
			if m.cursorOnPrompt {
				break
			}
			if m.cursorRepo != "" && !m.isCancellable(m.cursorRepo) {
				break
			}
			if m.cursorRepo != "" {
				return m, func() tea.Msg { return cancelProjectMsg{Repo: m.cursorRepo} }
			}
		}
	}
	return m, nil
}

// isCancellable returns true if the repo can be cancelled (not completed, not already cancelled).
func (m progressModel) isCancellable(repo string) bool {
	if _, done := m.results[repo]; done {
		return false
	}
	if m.cancelled[repo] {
		return false
	}
	return true
}

// moveCursor moves the cursor up or down by delta positions in the sorted list.
// The prompt line sits above the repo list; moving up from the first repo lands there.
func (m *progressModel) moveCursor(delta int) {
	hasPrompt := m.prompt != ""
	sorted := m.sortedRepos()

	// Currently on the prompt line
	if m.cursorOnPrompt {
		if delta > 0 && len(sorted) > 0 {
			// Move down into the repo list
			m.cursorOnPrompt = false
			m.cursorRepo = sorted[0]
			m.manualScroll = true
			m.scrollOffset = 0
		}
		return
	}

	if len(sorted) == 0 {
		return
	}

	// Find current cursor position in sorted list
	curIdx := 0
	for i, repo := range sorted {
		if repo == m.cursorRepo {
			curIdx = i
			break
		}
	}

	newIdx := curIdx + delta
	if newIdx < 0 {
		if hasPrompt {
			// Move up past the first repo → land on prompt line
			m.cursorOnPrompt = true
			m.cursorRepo = ""
			return
		}
		newIdx = 0
	}
	if newIdx >= len(sorted) {
		newIdx = len(sorted) - 1
	}

	m.cursorRepo = sorted[newIdx]
	m.manualScroll = true

	// Adjust scroll offset to keep cursor visible
	if newIdx < m.scrollOffset {
		m.scrollOffset = newIdx
	} else if newIdx >= m.scrollOffset+maxVisibleProjects {
		m.scrollOffset = newIdx - maxVisibleProjects + 1
	}
}

func (m progressModel) handlePermissionRequest(req permission.PermissionRequest) (tea.Model, tea.Cmd) {
	// Questions skip auto-approve patterns
	if !req.IsQuestion {
		pattern := extractPattern(req.Command)
		if m.approvedPatterns[pattern] {
			req.ResponseCh <- permission.PermissionResponse{Approved: true}
			return m, nil
		}
	}

	// Enqueue or show immediately
	if m.currentPermission == nil {
		m.currentPermission = &req
		m.permissionCmdScroll = 0
		if req.IsQuestion {
			m.questionOptionIdx = 0
		} else {
			m.permissionChoice = 0
		}
	} else {
		m.permissionQueue = append(m.permissionQueue, req)
	}
	return m, nil
}

func (m progressModel) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.currentPermission.IsQuestion {
		return m.handleQuestionKey(msg)
	}

	switch msg.String() {
	case "y":
		m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: true}
		return m.advancePermissionQueue(), nil
	case "n":
		m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: false}
		return m.advancePermissionQueue(), nil
	case "a":
		pattern := extractPattern(m.currentPermission.Command)
		m.approvedPatterns[pattern] = true
		m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: true}
		// Auto-approve any queued requests matching this pattern
		m = m.drainAutoApproved()
		return m.advancePermissionQueue(), nil
	case "up", "k":
		if m.permissionCmdScroll > 0 {
			m.permissionCmdScroll--
		}
	case "down", "j":
		maxScroll := m.countWrappedLines() - maxPermissionCmdLines
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.permissionCmdScroll < maxScroll {
			m.permissionCmdScroll++
		}
	case "left", "h":
		if m.permissionChoice > 0 {
			m.permissionChoice--
		}
	case "right", "l":
		if m.permissionChoice < 2 {
			m.permissionChoice++
		}
	case "enter":
		switch m.permissionChoice {
		case 0: // Approve
			m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: true}
			return m.advancePermissionQueue(), nil
		case 1: // Deny
			m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: false}
			return m.advancePermissionQueue(), nil
		case 2: // Approve all matching
			pattern := extractPattern(m.currentPermission.Command)
			m.approvedPatterns[pattern] = true
			m.currentPermission.ResponseCh <- permission.PermissionResponse{Approved: true}
			m = m.drainAutoApproved()
			return m.advancePermissionQueue(), nil
		}
	}
	return m, nil
}

func (m progressModel) handleQuestionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Collect all options across all questions
	options := m.collectQuestionOptions()
	if len(options) == 0 {
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.questionOptionIdx > 0 {
			m.questionOptionIdx--
		}
	case "down", "j":
		if m.questionOptionIdx < len(options)-1 {
			m.questionOptionIdx++
		}
	case "enter":
		selected := options[m.questionOptionIdx]
		m.currentPermission.ResponseCh <- permission.PermissionResponse{
			Approved: false,
			Answer:   selected.Label,
		}
		return m.advancePermissionQueue(), nil
	default:
		// Number keys for quick selection (1-9)
		key := msg.String()
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(options) {
				selected := options[idx]
				m.currentPermission.ResponseCh <- permission.PermissionResponse{
					Approved: false,
					Answer:   selected.Label,
				}
				return m.advancePermissionQueue(), nil
			}
		}
	}
	return m, nil
}

// collectQuestionOptions returns a flat list of all options across all questions.
func (m progressModel) collectQuestionOptions() []permission.QuestionOption {
	if m.currentPermission == nil {
		return nil
	}
	var options []permission.QuestionOption
	for _, q := range m.currentPermission.Questions {
		options = append(options, q.Options...)
	}
	return options
}

func (m progressModel) advancePermissionQueue() progressModel {
	if len(m.permissionQueue) > 0 {
		next := m.permissionQueue[0]
		m.permissionQueue = m.permissionQueue[1:]

		// Questions skip auto-approve; regular permissions check patterns
		if !next.IsQuestion {
			pattern := extractPattern(next.Command)
			if m.approvedPatterns[pattern] {
				next.ResponseCh <- permission.PermissionResponse{Approved: true}
				m.currentPermission = nil
				return m.advancePermissionQueue()
			}
		}

		m.currentPermission = &next
		m.permissionCmdScroll = 0
		if next.IsQuestion {
			m.questionOptionIdx = 0
		} else {
			m.permissionChoice = 0
		}
	} else {
		m.currentPermission = nil
		m.permissionCmdScroll = 0
	}
	return m
}

func (m progressModel) drainAutoApproved() progressModel {
	var remaining []permission.PermissionRequest
	for _, req := range m.permissionQueue {
		pattern := extractPattern(req.Command)
		if m.approvedPatterns[pattern] {
			req.ResponseCh <- permission.PermissionResponse{Approved: true}
		} else {
			remaining = append(remaining, req)
		}
	}
	m.permissionQueue = remaining
	return m
}

// extractPattern returns a glob-like pattern from a command (first token + *).
func extractPattern(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "*"
	}
	return parts[0] + " *"
}

func (m progressModel) countWrappedLines() int {
	if m.currentPermission == nil {
		return 0
	}
	maxContentWidth := m.termWidth - 10 - 4
	if maxContentWidth < 36 {
		maxContentWidth = 36
	}
	total := 0
	for _, line := range strings.Split(m.currentPermission.Command, "\n") {
		if len(line) == 0 {
			total++
		} else {
			total += (len(line)-1)/maxContentWidth + 1
		}
	}
	return total
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

	// Wizard context (branch, PR title, prompt)
	dimLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	dimValue := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	if m.branchName != "" || m.prTitle != "" {
		var parts []string
		if m.branchName != "" {
			parts = append(parts, dimLabel.Render("Branch: ")+dimValue.Render(m.branchName))
		}
		if m.prTitle != "" {
			parts = append(parts, dimLabel.Render("PR: ")+dimValue.Render(m.prTitle))
		}
		b.WriteString("  " + strings.Join(parts, "    "))
		b.WriteString("\n")
	}
	if m.prompt != "" {
		promptCursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		promptPrefix := "  "
		if m.cursorOnPrompt {
			promptPrefix = promptCursorStyle.Render("▸") + " "
		}

		if m.promptExpanded {
			// Expanded: show full prompt in a bordered box
			btnStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
			b.WriteString(promptPrefix + dimLabel.Render("Prompt ") + btnStyle.Render("[▼ collapse]"))
			b.WriteString("\n")

			boxWidth := m.termWidth - 10
			if boxWidth < 40 {
				boxWidth = 40
			}
			boxStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1).
				Width(boxWidth)
			promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
			rendered := boxStyle.Render(promptStyle.Render(m.prompt))
			for _, line := range strings.Split(rendered, "\n") {
				b.WriteString("    " + line + "\n")
			}
		} else {
			// Collapsed: single line with expand hint
			maxLen := m.termWidth - 22
			if maxLen < 30 {
				maxLen = 30
			}
			p := m.prompt
			p = strings.ReplaceAll(p, "\n", " ")
			if len(p) > maxLen {
				p = p[:maxLen-3] + "..."
			}
			btnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
			btn := btnStyle.Render(" [▶ expand]")
			b.WriteString(promptPrefix + dimLabel.Render("Prompt: ") + dimValue.Render(p) + btn)
			b.WriteString("\n")
		}
	}
	if m.branchName != "" || m.prTitle != "" || m.prompt != "" {
		b.WriteString("\n")
	}

	// Permission prompt or question prompt (shown between progress bar and project list)
	if m.currentPermission != nil {
		if m.currentPermission.IsQuestion {
			b.WriteString(m.renderQuestionPrompt())
		} else {
			b.WriteString(m.renderPermissionPrompt())
		}
		b.WriteString("\n")
	}

	// Pause confirmation
	if m.paused {
		pauseStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		b.WriteString(pauseStyle.Render(fmt.Sprintf(
			"⏸  Batch complete — %d of %d repos processed.", m.completed, m.total)))
		b.WriteString("\n")
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		b.WriteString(hintStyle.Render("  💰 Please verify you have sufficient AI credits before continuing with the next batch."))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  Press Enter to continue or Ctrl+C to stop."))
		b.WriteString("\n\n")
	}

	// Per-project status lines (sorted by status, with scrolling)
	sorted := m.sortedRepos()
	start, end := m.visibleWindow(sorted)

	if start > 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	repoStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	spinnerColor := spinnerColors[m.tickCount%len(spinnerColors)]
	spinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(spinnerColor))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	frame := spinnerFrames[m.tickCount%len(spinnerFrames)]
	for _, repo := range sorted[start:end] {
		status := m.statuses[repo]
		isCursor := !m.cursorOnPrompt && repo == m.cursorRepo

		prefix := "  "
		if isCursor {
			prefix = cursorStyle.Render("▸") + " "
		} else if m.statusPriority(repo) == 1 {
			prefix = spinnerStyle.Render(frame) + " "
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", prefix, repoStyle.Render(fmt.Sprintf("[%s]", repo)), status))
	}

	remaining := len(sorted) - end
	if remaining > 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	// Post-processing status lines
	if len(m.postLines) > 0 {
		b.WriteString("\n")
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		for i, line := range m.postLines {
			isLast := i == len(m.postLines)-1
			// Show spinner on the last line if it looks like an in-progress step
			if isLast && !strings.HasPrefix(line, "✓") && !strings.HasPrefix(line, "⚠") {
				b.WriteString(spinnerStyle.Render(frame) + " ")
			}
			b.WriteString(dimStyle.Render(line))
			b.WriteString("\n")
		}
	}

	// Help hints
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var hints []string
	if m.currentPermission != nil && !m.currentPermission.IsQuestion {
		totalWrapped := m.countWrappedLines()
		if totalWrapped > maxPermissionCmdLines {
			hints = append(hints, helpStyle.Render("↑↓: scroll command"))
		}
	} else {
		hints = append(hints, helpStyle.Render("↑↓: navigate"))
	}
	if m.cursorOnPrompt {
		if m.promptExpanded {
			hints = append(hints, helpStyle.Render("enter: collapse"))
		} else {
			hints = append(hints, helpStyle.Render("enter: expand"))
		}
	} else if m.cursorRepo != "" && m.isCancellable(m.cursorRepo) {
		cancelHintStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		hints = append(hints, cancelHintStyle.Render("x: cancel"))
	}
	hints = append(hints, helpStyle.Render("ctrl+c: abort all"))
	b.WriteString("  " + strings.Join(hints, helpStyle.Render("  •  ")))
	b.WriteString("\n")

	return b.String()
}

func (m progressModel) renderPermissionPrompt() string {
	var b strings.Builder

	lockStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	cmdStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	repoName := m.currentPermission.Repo
	if repoName == "" {
		repoName = "repo"
	}

	toolLabel := m.currentPermission.ToolName
	if toolLabel == "" {
		toolLabel = "command"
	}
	b.WriteString(lockStyle.Render(fmt.Sprintf("🔐 [%s] wants to run %s:", repoName, toolLabel)))
	b.WriteString("\n")

	cmdLines := strings.Split(m.currentPermission.Command, "\n")
	boxWidth := m.termWidth - 10
	if boxWidth < 40 {
		boxWidth = 40
	}
	maxContentWidth := boxWidth - 4

	// Wrap long lines so the full command is visible
	var wrappedLines []string
	for _, line := range cmdLines {
		for len(line) > maxContentWidth {
			wrappedLines = append(wrappedLines, line[:maxContentWidth])
			line = line[maxContentWidth:]
		}
		wrappedLines = append(wrappedLines, line)
	}

	// Determine visible window from wrapped lines
	visibleLines := len(wrappedLines)
	if visibleLines > maxPermissionCmdLines {
		visibleLines = maxPermissionCmdLines
	}
	start := m.permissionCmdScroll
	end := start + visibleLines
	if end > len(wrappedLines) {
		end = len(wrappedLines)
	}

	var rendered []string
	if start > 0 {
		rendered = append(rendered, dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}
	for _, line := range wrappedLines[start:end] {
		rendered = append(rendered, cmdStyle.Render(line))
	}
	if remaining := len(wrappedLines) - end; remaining > 0 {
		rendered = append(rendered, dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
	}

	cmdBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1).
		Width(boxWidth)
	renderedBox := cmdBox.Render(strings.Join(rendered, "\n"))
	for _, line := range strings.Split(renderedBox, "\n") {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("\n")

	pattern := extractPattern(m.currentPermission.Command)
	options := []string{
		"Approve (y)",
		"Deny (n)",
		fmt.Sprintf("Approve all \"%s\" (a)", pattern),
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	b.WriteString("  ")
	for i, opt := range options {
		if i == m.permissionChoice {
			b.WriteString(selectedStyle.Render("> " + opt))
		} else {
			b.WriteString(normalStyle.Render("  " + opt))
		}
		if i < len(options)-1 {
			b.WriteString("    ")
		}
	}
	b.WriteString("\n")

	if len(m.permissionQueue) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  [%d more pending]", len(m.permissionQueue))))
		b.WriteString("\n")
	}

	return b.String()
}

func (m progressModel) renderQuestionPrompt() string {
	var b strings.Builder

	questionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	repoName := m.currentPermission.Repo
	if repoName == "" {
		repoName = "repo"
	}

	optionIdx := 0
	for _, q := range m.currentPermission.Questions {
		b.WriteString(questionStyle.Render(fmt.Sprintf("❓ [%s]", repoName)))
		if q.Header != "" {
			b.WriteString("  ")
			b.WriteString(headerStyle.Render(q.Header))
		}
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(q.Text)
		b.WriteString("\n\n")

		for _, opt := range q.Options {
			num := fmt.Sprintf("%d", optionIdx+1)
			label := fmt.Sprintf("  %s. %s", num, opt.Label)
			if opt.Description != "" {
				label += " — " + opt.Description
			}

			if optionIdx == m.questionOptionIdx {
				b.WriteString(selectedStyle.Render("▸ " + label))
			} else {
				b.WriteString(normalStyle.Render("  " + label))
			}
			b.WriteString("\n")
			optionIdx++
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑↓: navigate  enter: select  1-9: quick select"))
	b.WriteString("\n")

	if len(m.permissionQueue) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  [%d more pending]", len(m.permissionQueue))))
		b.WriteString("\n")
	}

	return b.String()
}

// statusPriority returns the sort priority for a repo: 0=completed, 1=in-progress, 2=waiting.
func (m progressModel) statusPriority(repo string) int {
	if _, done := m.results[repo]; done {
		return 0
	}
	if m.statuses[repo] == "Waiting..." {
		return 2
	}
	return 1
}

// sortedRepos returns repos sorted by status: completed first, in-progress second, waiting last.
func (m progressModel) sortedRepos() []string {
	sorted := make([]string, len(m.repos))
	copy(sorted, m.repos)
	sort.SliceStable(sorted, func(i, j int) bool {
		return m.statusPriority(sorted[i]) < m.statusPriority(sorted[j])
	})
	return sorted
}

// visibleWindow returns the start and end indices for the visible window of projects.
func (m progressModel) visibleWindow(sorted []string) (int, int) {
	if len(sorted) <= maxVisibleProjects {
		return 0, len(sorted)
	}

	// Use manual scroll offset if the user has scrolled
	if m.manualScroll {
		start := m.scrollOffset
		if start+maxVisibleProjects > len(sorted) {
			start = len(sorted) - maxVisibleProjects
		}
		if start < 0 {
			start = 0
		}
		return start, start + maxVisibleProjects
	}

	// Auto-anchor: find the first in-progress item
	firstActive := -1
	for i, repo := range sorted {
		if m.statusPriority(repo) == 1 {
			firstActive = i
			break
		}
	}

	if firstActive == -1 {
		if m.completed > 0 {
			// All done or waiting; anchor to last completed item
			firstActive = m.completed - 1
		} else {
			// All waiting; start from top
			firstActive = 0
		}
	}

	// Show a couple of items above the anchor for context
	start := firstActive - 2
	if start < 0 {
		start = 0
	}
	if start+maxVisibleProjects > len(sorted) {
		start = len(sorted) - maxVisibleProjects
	}

	return start, start + maxVisibleProjects
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
