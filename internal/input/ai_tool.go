package input

import (
	"copycat/internal/config"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type aiToolSelectorModel struct {
	tools     []config.AITool
	cursor    int
	selected  int
	confirmed bool
	quitted   bool
}

func initialAIToolModel(tools []config.AITool, defaultToolName string) aiToolSelectorModel {
	// Find the default tool index
	defaultIndex := 0
	for i, tool := range tools {
		if tool.Name == defaultToolName {
			defaultIndex = i
			break
		}
	}

	return aiToolSelectorModel{
		tools:     tools,
		cursor:    defaultIndex,
		selected:  defaultIndex,
		confirmed: false,
		quitted:   false,
	}
}

func (m aiToolSelectorModel) Init() tea.Cmd {
	return nil
}

func (m aiToolSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitted = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.tools)-1 {
				m.cursor++
			}

		case "enter", " ":
			m.selected = m.cursor
			m.confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m aiToolSelectorModel) View() string {
	if m.quitted {
		return ""
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("206")).
		Padding(1, 0)

	b.WriteString(titleStyle.Render("Select AI Tool"))
	b.WriteString("\n\n")

	// List of tools
	for i, tool := range m.tools {
		// Build the item text
		itemText := fmt.Sprintf("%s (%s)", tool.Name, tool.Command)

		// Style based on cursor position
		itemStyle := lipgloss.NewStyle()
		if i == m.cursor {
			itemStyle = itemStyle.
				Foreground(lipgloss.Color("205")).
				Bold(true)
			itemText = "> " + itemText
		} else {
			itemText = "  " + itemText
		}

		b.WriteString(itemStyle.Render(itemText))
		b.WriteString("\n")
	}

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(1, 0)

	help := "↑/↓: navigate • enter/space: select • q: quit"
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// SelectAITool prompts the user to select an AI tool from the available tools.
// The default tool (from config) will be pre-selected as the cursor default.
func SelectAITool(aiToolsConfig *config.AIToolsConfig) (*config.AITool, error) {
	if len(aiToolsConfig.Tools) == 0 {
		return nil, fmt.Errorf("no AI tools available")
	}

	// If there's only one tool, automatically select it
	if len(aiToolsConfig.Tools) == 1 {
		return &aiToolsConfig.Tools[0], nil
	}

	p := tea.NewProgram(initialAIToolModel(aiToolsConfig.Tools, aiToolsConfig.Default))
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	m := finalModel.(aiToolSelectorModel)

	// User quit without confirming
	if m.quitted || !m.confirmed {
		return nil, fmt.Errorf("AI tool selection cancelled")
	}

	return &m.tools[m.selected], nil
}
