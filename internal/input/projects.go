package input

import (
	"copycat/internal/config"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type projectSelectorModel struct {
	projects   []config.Project
	cursor     int
	selected   map[int]struct{}
	confirmed  bool
	termWidth  int
	termHeight int
	quitted    bool
}

func initialModel(projects []config.Project) projectSelectorModel {
	// Sort projects alphabetically by repo name
	sortedProjects := make([]config.Project, len(projects))
	copy(sortedProjects, projects)
	sort.Slice(sortedProjects, func(i, j int) bool {
		return sortedProjects[i].Repo < sortedProjects[j].Repo
	})

	return projectSelectorModel{
		projects:  sortedProjects,
		cursor:    0,
		selected:  make(map[int]struct{}),
		confirmed: false,
	}
}

func (m projectSelectorModel) Init() tea.Cmd {
	return nil
}

func (m projectSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitted = true
			return m, tea.Quit

		case "up", "k":
			// Move up in column (layout is column-by-column)
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			// Move down in column
			if m.cursor < len(m.projects)-1 {
				m.cursor++
			}

		case "left", "h":
			// Move left to previous column
			numCols := m.calculateColumns()
			numRows := (len(m.projects) + numCols - 1) / numCols
			if m.cursor >= numRows {
				m.cursor -= numRows
			}

		case "right", "l":
			// Move right to next column
			numCols := m.calculateColumns()
			numRows := (len(m.projects) + numCols - 1) / numCols
			if m.cursor+numRows < len(m.projects) {
				m.cursor += numRows
			}

		case " ":
			// Toggle selection
			if _, ok := m.selected[m.cursor]; ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}

		case "a":
			// Select all
			if len(m.selected) == len(m.projects) {
				// Deselect all if all are selected
				m.selected = make(map[int]struct{})
			} else {
				// Select all
				for i := range m.projects {
					m.selected[i] = struct{}{}
				}
			}

		case "enter":
			m.confirmed = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
	}

	return m, nil
}

func (m projectSelectorModel) calculateColumns() int {
	if m.termWidth == 0 {
		return 3 // Default to 3 columns
	}

	// Find longest project name
	maxLen := 0
	for i, p := range m.projects {
		// Format: "[ ] 123. repo-name"
		itemLen := len(fmt.Sprintf("[ ] %d. %s", i+1, p.Repo))
		if itemLen > maxLen {
			maxLen = itemLen
		}
	}

	// Add padding between columns
	colWidth := maxLen + 4

	// Calculate how many columns fit
	numCols := m.termWidth / colWidth
	if numCols < 1 {
		numCols = 1
	}
	if numCols > 4 {
		numCols = 4 // Max 4 columns for readability
	}

	return numCols
}

func (m projectSelectorModel) View() string {
	if m.quitted {
		return ""
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("206")).
		Padding(1, 0)

	b.WriteString(titleStyle.Render("Select Projects"))
	b.WriteString("\n\n")

	// Calculate columns and layout
	numCols := m.calculateColumns()
	numRows := (len(m.projects) + numCols - 1) / numCols // Ceiling division

	// Find max width for alignment
	maxLen := 0
	for i, p := range m.projects {
		itemLen := len(fmt.Sprintf("[ ] %d. %s", i+1, p.Repo))
		if itemLen > maxLen {
			maxLen = itemLen
		}
	}
	colWidth := maxLen + 2

	// Render in grid layout (column by column)
	for row := 0; row < numRows; row++ {
		var rowItems []string

		for col := 0; col < numCols; col++ {
			// Column-by-column layout: idx = col * numRows + row
			idx := col*numRows + row
			if idx >= len(m.projects) {
				break
			}

			project := m.projects[idx]

			// Checkbox
			checkbox := "[ ]"
			if _, ok := m.selected[idx]; ok {
				checkbox = "[✓]"
			}

			// Item text
			itemText := fmt.Sprintf("%s %d. %s", checkbox, idx+1, project.Repo)

			// Style based on cursor position
			itemStyle := lipgloss.NewStyle().Width(colWidth)
			if idx == m.cursor {
				itemStyle = itemStyle.
					Foreground(lipgloss.Color("205")).
					Bold(true).
					Underline(true)
			}

			rowItems = append(rowItems, itemStyle.Render(itemText))
		}

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, rowItems...))
		b.WriteString("\n")
	}

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(1, 0)

	help := "↑/↓/←/→: navigate • space: toggle • a: toggle all • enter: confirm • q: quit"
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(help))

	// Selected count
	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	selectedCount := len(m.selected)
	countText := fmt.Sprintf("\nSelected: %d project(s)", selectedCount)
	b.WriteString(countStyle.Render(countText))

	return b.String()
}

func SelectProjects(projects []config.Project) ([]config.Project, error) {
	if len(projects) == 0 {
		return nil, nil
	}

	p := tea.NewProgram(initialModel(projects))
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	m := finalModel.(projectSelectorModel)

	// User quit without confirming
	if m.quitted || !m.confirmed {
		return nil, nil
	}

	// Extract selected projects
	var selected []config.Project
	for i := range m.selected {
		selected = append(selected, m.projects[i])
	}

	return selected, nil
}
