package input

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type selectorModel struct {
	title     string
	items     []string
	cursor    int
	selected  int
	confirmed bool
	quitted   bool
}

func initialSelectorModel(title string, items []string) selectorModel {
	return selectorModel{
		title:     title,
		items:     items,
		cursor:    0,
		selected:  0,
		confirmed: false,
		quitted:   false,
	}
}

func (m selectorModel) Init() tea.Cmd {
	return nil
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.items)-1 {
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

func (m selectorModel) View() string {
	if m.quitted {
		return ""
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("206")).
		Padding(1, 0)

	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	// List of items
	for i, item := range m.items {
		itemText := item

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

// SelectOption prompts the user to select one option from a list
func SelectOption(title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	// If there's only one option, automatically select it
	if len(options) == 1 {
		return options[0], nil
	}

	p := tea.NewProgram(initialSelectorModel(title, options))
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	m := finalModel.(selectorModel)

	// User quit without confirming
	if m.quitted || !m.confirmed {
		return "", fmt.Errorf("selection cancelled")
	}

	return m.items[m.selected], nil
}
