package input

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type textInputModel struct {
	title     string
	textInput textinput.Model
	quitted   bool
	submitted bool
}

func initialTextInputModel(title, placeholder string) textInputModel {
	return initialTextInputModelWithLimit(title, placeholder, 256)
}

func initialTextInputModelWithLimit(title, placeholder string, charLimit int) textInputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	if charLimit <= 0 {
		charLimit = 256
	}
	ti.CharLimit = charLimit
	ti.Width = 80

	return textInputModel{
		title:     title,
		textInput: ti,
		quitted:   false,
		submitted: false,
	}
}

func (m textInputModel) Init() tea.Cmd {
	return tea.Batch(tea.ClearScreen, textinput.Blink)
}

func (m textInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitted = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.submitted = true
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m textInputModel) View() string {
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

	// Text input
	b.WriteString(m.textInput.View())
	b.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(1, 0)

	help := "enter: submit • esc: cancel"
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// GetTextInput prompts the user to enter text
func GetTextInput(title, placeholder string) (string, error) {
	return GetTextInputWithLimit(title, placeholder, 256)
}

// GetTextInputWithLimit prompts the user to enter text with a custom character limit
func GetTextInputWithLimit(title, placeholder string, charLimit int) (string, error) {
	p := tea.NewProgram(initialTextInputModelWithLimit(title, placeholder, charLimit))
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	m := finalModel.(textInputModel)

	// User quit without submitting
	if m.quitted || !m.submitted {
		return "", fmt.Errorf("input cancelled")
	}

	value := strings.TrimSpace(m.textInput.Value())
	if value == "" {
		return "", fmt.Errorf("no input provided")
	}

	return value, nil
}
