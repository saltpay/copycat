package input

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/saltpay/copycat/internal/config"
)

// wizardCompletedMsg is emitted when the wizard finishes collecting all inputs.
type wizardCompletedMsg struct {
	Result WizardResult
}

// editorRequestedMsg is emitted when the user presses ctrl+e to open an editor.
type editorRequestedMsg struct{}

type wizardStep int

const (
	stepAction wizardStep = iota
	// Local changes path
	stepAITool
	stepIgnoreInstructions
	stepBranchStrategy
	stepBranchName
	stepPRTitle
	stepPrompt
)

// WizardResult holds all values collected by the setup wizard.
type WizardResult struct {
	Action                  string // "local" or "assessment"
	AITool                  *config.AITool
	IgnoreAgentInstructions bool
	BranchStrategy          string
	BranchName              string
	PRTitle                 string
	Prompt                  string
}

type wizardModel struct {
	currentStep      wizardStep
	selectedProjects []config.Project

	// Action
	actionOptions []string
	actionCursor  int
	action        string // "local" or "assessment"

	// AI Tool
	aiTools      []config.AITool
	aiToolCursor int
	aiTool       *config.AITool
	skipAITool   bool

	// Ignore agent instructions
	ignoreInstructionsCursor int
	ignoreInstructions       bool
	ignoreInstructionsSet    bool
	skipIgnoreInstructions   bool

	// Branch strategy
	branchOptions  []string
	branchCursor   int
	branchStrategy string

	// Branch name
	branchNameInput textinput.Model
	branchName      string
	needsBranchName bool

	// PR Title
	prTitleInput textinput.Model
	prTitle      string

	// Prompt
	promptInput textinput.Model
	prompt      string
	useEditor   bool

	// State
	termWidth int
}

func newWizardModel(aiToolsConfig *config.AIToolsConfig, agentInstructions []string, selectedProjects []config.Project) wizardModel {
	branchInput := textinput.New()
	branchInput.Placeholder = "my-branch-name"
	branchInput.CharLimit = 256
	branchInput.Width = 60

	prTitleInput := textinput.New()
	prTitleInput.Placeholder = "e.g., PROJ-123 - Update dependencies"
	prTitleInput.CharLimit = 256
	prTitleInput.Width = 60

	promptInput := textinput.New()
	promptInput.Placeholder = "Describe the changes to apply to each repository"
	promptInput.CharLimit = 2048
	promptInput.Width = 60

	m := wizardModel{
		selectedProjects: selectedProjects,
		actionOptions: []string{
			"Perform Changes Locally",
			"Run Assessment",
		},
		currentStep: stepAction,
		aiTools:     aiToolsConfig.Tools,
		branchOptions: []string{
			"Always create new branches",
			"Specify branch name (reuse if exists)",
			"Specify branch name (skip if exists)",
		},
		branchNameInput: branchInput,
		prTitleInput:    prTitleInput,
		promptInput:     promptInput,
	}

	if len(aiToolsConfig.Tools) <= 1 {
		m.skipAITool = true
		if len(aiToolsConfig.Tools) == 1 {
			m.aiTool = &aiToolsConfig.Tools[0]
		}
	} else {
		for i, tool := range aiToolsConfig.Tools {
			if tool.Name == aiToolsConfig.Default {
				m.aiToolCursor = i
				break
			}
		}
	}

	if len(agentInstructions) == 0 {
		m.skipIgnoreInstructions = true
	}

	return m
}

func (m wizardModel) Init() tea.Cmd {
	return tea.ClearScreen
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.currentStep {
	case stepAction:
		return m.updateActionStep(msg)
	case stepAITool:
		return m.updateAIToolStep(msg)
	case stepIgnoreInstructions:
		return m.updateIgnoreInstructionsStep(msg)
	case stepBranchStrategy:
		return m.updateBranchStrategyStep(msg)
	case stepBranchName:
		return m.updateBranchNameStep(msg)
	case stepPRTitle:
		return m.updatePRTitleStep(msg)
	case stepPrompt:
		return m.updatePromptStep(msg)
	}

	return m, nil
}

func (m wizardModel) updateActionStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.actionCursor > 0 {
			m.actionCursor--
		}
	case "down", "j":
		if m.actionCursor < len(m.actionOptions)-1 {
			m.actionCursor++
		}
	case "enter", " ":
		switch m.actionCursor {
		case 0:
			m.action = "local"
			if m.skipAITool {
				return m.advanceFromAITool()
			}
			m.currentStep = stepAITool
		case 1:
			m.action = "assessment"
			if m.skipAITool {
				return m.advanceFromAITool()
			}
			m.currentStep = stepAITool
		}
	}
	return m, nil
}

func (m wizardModel) updateAIToolStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.aiToolCursor > 0 {
			m.aiToolCursor--
		}
	case "down", "j":
		if m.aiToolCursor < len(m.aiTools)-1 {
			m.aiToolCursor++
		}
	case "enter", " ":
		m.aiTool = &m.aiTools[m.aiToolCursor]
		return m.advanceFromAITool()
	}
	return m, nil
}

// advanceFromAITool moves to the next step after the AI tool is selected (or skipped).
func (m wizardModel) advanceFromAITool() (tea.Model, tea.Cmd) {
	if !m.skipIgnoreInstructions {
		m.currentStep = stepIgnoreInstructions
		return m, nil
	}
	return m.advanceFromIgnoreInstructions()
}

// advanceFromIgnoreInstructions moves to the next step after ignore-instructions is handled.
func (m wizardModel) advanceFromIgnoreInstructions() (tea.Model, tea.Cmd) {
	if m.action == "assessment" {
		m.promptInput.Placeholder = "Enter your assessment question (e.g., Are these projects using circuit breakers?)"
		m.promptInput.Focus()
		m.currentStep = stepPrompt
		return m, textinput.Blink
	}
	m.currentStep = stepBranchStrategy
	return m, nil
}

func (m wizardModel) updateIgnoreInstructionsStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.ignoreInstructionsCursor > 0 {
			m.ignoreInstructionsCursor--
		}
	case "down", "j":
		if m.ignoreInstructionsCursor < 1 {
			m.ignoreInstructionsCursor++
		}
	case "enter", " ":
		m.ignoreInstructions = m.ignoreInstructionsCursor == 0
		m.ignoreInstructionsSet = true
		return m.advanceFromIgnoreInstructions()
	}
	return m, nil
}

func (m wizardModel) updateBranchStrategyStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.branchCursor > 0 {
			m.branchCursor--
		}
	case "down", "j":
		if m.branchCursor < len(m.branchOptions)-1 {
			m.branchCursor++
		}
	case "enter", " ":
		m.branchStrategy = m.branchOptions[m.branchCursor]
		m.needsBranchName = strings.Contains(m.branchStrategy, "branch name")
		if m.needsBranchName {
			m.branchNameInput.Focus()
			m.currentStep = stepBranchName
			return m, textinput.Blink
		}
		m.prTitleInput.Focus()
		m.currentStep = stepPRTitle
		return m, textinput.Blink
	}
	return m, nil
}

func (m wizardModel) updateBranchNameStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(m.branchNameInput.Value())
			if value == "" {
				return m, nil
			}
			m.branchName = value
			m.branchNameInput.Blur()
			m.prTitleInput.Focus()
			m.currentStep = stepPRTitle
			return m, textinput.Blink
		case tea.KeyEsc:
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.branchNameInput, cmd = m.branchNameInput.Update(msg)
	return m, cmd
}

func (m wizardModel) updatePRTitleStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(m.prTitleInput.Value())
			if value == "" {
				return m, nil
			}
			m.prTitle = value
			m.prTitleInput.Blur()
			m.promptInput.Focus()
			m.currentStep = stepPrompt
			return m, textinput.Blink
		case tea.KeyEsc:
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.prTitleInput, cmd = m.prTitleInput.Update(msg)
	return m, cmd
}

func (m wizardModel) updatePromptStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(m.promptInput.Value())
			if value == "" {
				return m, nil
			}
			m.prompt = value
			m.promptInput.Blur()
			return m, func() tea.Msg { return wizardCompletedMsg{Result: m.buildResult()} }
		case tea.KeyEsc:
			return m, tea.Quit
		}
		if keyMsg.String() == "ctrl+e" {
			return m, func() tea.Msg { return editorRequestedMsg{} }
		}
	}
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, cmd
}

// View renders the wizard.
func (m wizardModel) View() string {
	var b strings.Builder

	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Projects header
	projectsHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("206"))
	b.WriteString(projectsHeader.Render(formatProjectsSummary(m.selectedProjects)))
	b.WriteString("\n\n")

	// Action
	if m.action != "" {
		var label string
		switch m.action {
		case "local":
			label = "Perform Changes Locally"
		case "assessment":
			label = "Run Assessment"
		}
		b.WriteString(completedStyle.Render(fmt.Sprintf("  ✓ Action: %s", label)))
		b.WriteString("\n")
	} else {
		b.WriteString(labelStyle.Render("  Action"))
		b.WriteString("\n")
		for i, option := range m.actionOptions {
			if i == m.actionCursor {
				b.WriteString(cursorStyle.Render(fmt.Sprintf("    > %s", option)))
			} else {
				b.WriteString(fmt.Sprintf("      %s", option))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  ↑/↓: navigate • enter: select • q/ctrl+c: quit"))
		b.WriteString("\n")
		return b.String()
	}

	// Render path-specific fields
	switch m.action {
	case "local":
		m.viewLocalFields(&b, completedStyle, labelStyle, pendingStyle, cursorStyle, hintStyle)
	case "assessment":
		m.viewAssessmentFields(&b, completedStyle, labelStyle, pendingStyle, cursorStyle, hintStyle)
	}

	// Help text
	b.WriteString("\n")
	switch m.currentStep {
	case stepAITool, stepIgnoreInstructions, stepBranchStrategy:
		b.WriteString(helpStyle.Render("  ↑/↓: navigate • enter: select • q/ctrl+c: quit"))
	case stepBranchName, stepPRTitle:
		b.WriteString(helpStyle.Render("  enter: submit • esc/ctrl+c: quit"))
	case stepPrompt:
		b.WriteString(helpStyle.Render("  enter: submit • ctrl+e: open editor • esc/ctrl+c: quit"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m wizardModel) viewLocalFields(b *strings.Builder, completed, label, pending, cursor, hint lipgloss.Style) {
	// AI Tool
	if !m.skipAITool {
		if m.aiTool != nil {
			b.WriteString(completed.Render(fmt.Sprintf("  ✓ AI Tool: %s (%s)", m.aiTool.Name, m.aiTool.Command)))
			b.WriteString("\n")
		} else if m.currentStep == stepAITool {
			b.WriteString(label.Render("  AI Tool"))
			b.WriteString("\n")
			for i, tool := range m.aiTools {
				text := fmt.Sprintf("%s (%s)", tool.Name, tool.Command)
				if i == m.aiToolCursor {
					b.WriteString(cursor.Render(fmt.Sprintf("    > %s", text)))
				} else {
					b.WriteString(fmt.Sprintf("      %s", text))
				}
				b.WriteString("\n")
			}
		} else {
			b.WriteString(pending.Render("  ○ AI Tool"))
			b.WriteString("\n")
		}
	}

	// Ignore Agent Instructions
	if !m.skipIgnoreInstructions {
		m.viewIgnoreInstructions(b, completed, label, pending, cursor)
	}

	// Branch Strategy
	if m.branchStrategy != "" {
		b.WriteString(completed.Render(fmt.Sprintf("  ✓ Branch: %s", m.branchStrategy)))
		b.WriteString("\n")
	} else if m.currentStep == stepBranchStrategy {
		b.WriteString(label.Render("  Branch Strategy"))
		b.WriteString("\n")
		for i, option := range m.branchOptions {
			if i == m.branchCursor {
				b.WriteString(cursor.Render(fmt.Sprintf("    > %s", option)))
			} else {
				b.WriteString(fmt.Sprintf("      %s", option))
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString(pending.Render("  ○ Branch Strategy"))
		b.WriteString("\n")
	}

	// Branch Name (conditional)
	if m.needsBranchName {
		if m.branchName != "" {
			b.WriteString(completed.Render(fmt.Sprintf("  ✓ Branch Name: %s", m.branchName)))
			b.WriteString("\n")
		} else if m.currentStep == stepBranchName {
			b.WriteString(label.Render("  Branch Name"))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf("    %s", m.branchNameInput.View()))
			b.WriteString("\n")
		} else {
			b.WriteString(pending.Render("  ○ Branch Name"))
			b.WriteString("\n")
		}
	}

	// PR Title
	if m.prTitle != "" {
		b.WriteString(completed.Render(fmt.Sprintf("  ✓ PR Title: %s", m.prTitle)))
		b.WriteString("\n")
	} else if m.currentStep == stepPRTitle {
		b.WriteString(label.Render("  PR Title"))
		b.WriteString("\n")
		b.WriteString(hint.Render("    You may include a ticket reference (e.g., PROJ-123 - Description)"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("    %s", m.prTitleInput.View()))
		b.WriteString("\n")
	} else {
		b.WriteString(pending.Render("  ○ PR Title"))
		b.WriteString("\n")
	}

	// Prompt
	if m.prompt != "" && m.currentStep != stepPrompt {
		display := m.prompt
		if len(display) > 60 {
			display = display[:57] + "..."
		}
		b.WriteString(completed.Render(fmt.Sprintf("  ✓ Prompt: %s", display)))
		b.WriteString("\n")
	} else if m.currentStep == stepPrompt {
		b.WriteString(label.Render("  Prompt"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("    %s", m.promptInput.View()))
		b.WriteString("\n")
	} else {
		b.WriteString(pending.Render("  ○ Prompt"))
		b.WriteString("\n")
	}
}

func (m wizardModel) viewAssessmentFields(b *strings.Builder, completed, label, pending, cursor, hint lipgloss.Style) {
	// AI Tool
	if !m.skipAITool {
		if m.aiTool != nil {
			b.WriteString(completed.Render(fmt.Sprintf("  ✓ AI Tool: %s (%s)", m.aiTool.Name, m.aiTool.Command)))
			b.WriteString("\n")
		} else if m.currentStep == stepAITool {
			b.WriteString(label.Render("  AI Tool"))
			b.WriteString("\n")
			for i, tool := range m.aiTools {
				text := fmt.Sprintf("%s (%s)", tool.Name, tool.Command)
				if i == m.aiToolCursor {
					b.WriteString(cursor.Render(fmt.Sprintf("    > %s", text)))
				} else {
					b.WriteString(fmt.Sprintf("      %s", text))
				}
				b.WriteString("\n")
			}
		} else {
			b.WriteString(pending.Render("  ○ AI Tool"))
			b.WriteString("\n")
		}
	}

	// Ignore Agent Instructions
	if !m.skipIgnoreInstructions {
		m.viewIgnoreInstructions(b, completed, label, pending, cursor)
	}

	// Prompt
	if m.prompt != "" && m.currentStep != stepPrompt {
		display := m.prompt
		if len(display) > 60 {
			display = display[:57] + "..."
		}
		b.WriteString(completed.Render(fmt.Sprintf("  ✓ Question: %s", display)))
		b.WriteString("\n")
	} else if m.currentStep == stepPrompt {
		b.WriteString(label.Render("  Assessment Question"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("    %s", m.promptInput.View()))
		b.WriteString("\n")
	} else {
		b.WriteString(pending.Render("  ○ Assessment Question"))
		b.WriteString("\n")
	}
}

func (m wizardModel) viewIgnoreInstructions(b *strings.Builder, completed, label, pending, cursor lipgloss.Style) {
	if m.ignoreInstructionsSet {
		val := "No"
		if m.ignoreInstructions {
			val = "Yes"
		}
		b.WriteString(completed.Render(fmt.Sprintf("  ✓ Ignore Agent Instructions: %s", val)))
		b.WriteString("\n")
	} else if m.currentStep == stepIgnoreInstructions {
		b.WriteString(label.Render("  Ignore Agent Instructions"))
		b.WriteString("\n")
		options := []string{"Yes - ignore repo instructions", "No - keep repo instructions"}
		for i, option := range options {
			if i == m.ignoreInstructionsCursor {
				b.WriteString(cursor.Render(fmt.Sprintf("    > %s", option)))
			} else {
				b.WriteString(fmt.Sprintf("      %s", option))
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString(pending.Render("  ○ Ignore Agent Instructions"))
		b.WriteString("\n")
	}
}

func formatProjectsSummary(projects []config.Project) string {
	if len(projects) == 0 {
		return "No projects selected"
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Repo)
	}
	if len(names) <= 3 {
		return fmt.Sprintf("%d project(s): %s", len(names), strings.Join(names, ", "))
	}
	return fmt.Sprintf("%d projects: %s, +%d more", len(names), strings.Join(names[:3], ", "), len(names)-3)
}

func (m wizardModel) buildResult() WizardResult {
	return WizardResult{
		Action:                  m.action,
		AITool:                  m.aiTool,
		IgnoreAgentInstructions: m.ignoreInstructions,
		BranchStrategy:          m.branchStrategy,
		BranchName:              m.branchName,
		PRTitle:                 m.prTitle,
		Prompt:                  m.prompt,
	}
}
