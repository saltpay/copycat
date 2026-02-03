package input

import (
	"github.com/saltpay/copycat/internal/config"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type projectSelectorModel struct {
	projects         []config.Project
	cursor           int
	selected         map[int]struct{}
	confirmed        bool
	termWidth        int
	termHeight       int
	quitted          bool
	refreshRequested bool
	// Filter fields
	filterMode       bool
	filterText       string
	filteredProjects []config.Project
	// Track if user has manually modified selection in filter mode
	manualSelection bool
}

func initialModel(projects []config.Project) projectSelectorModel {
	// Sort projects alphabetically by repo name
	sortedProjects := make([]config.Project, len(projects))
	copy(sortedProjects, projects)
	sort.Slice(sortedProjects, func(i, j int) bool {
		return sortedProjects[i].Repo < sortedProjects[j].Repo
	})

	return projectSelectorModel{
		projects:         sortedProjects,
		cursor:           0,
		selected:         make(map[int]struct{}), // Initially no projects selected
		confirmed:        false,
		filterMode:       false,
		filterText:       "",
		filteredProjects: sortedProjects, // Initially show all projects
		manualSelection:  false,
	}
}

func (m projectSelectorModel) Init() tea.Cmd {
	return nil
}

func (m projectSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle filter mode
		if m.filterMode {
			switch msg.String() {
			case "ctrl+c":
				// Exit the entire program
				m.quitted = true
				return m, tea.Quit
			case "esc":
				if m.filterText == "" {
					// If filter text is empty, exit filter mode entirely
					m.filterMode = false
					m.manualSelection = false // Reset manual selection flag
					m.filteredProjects = m.projects
					m.cursor = 0
					return m, nil
				} else {
					// If filter text exists, just clear the filter text but stay in filter mode
					m.filterText = ""
					m.filteredProjects = m.projects
					// If user hasn't manually modified selection, clear all selections (projects start unselected)
					if !m.manualSelection {
						m.selected = make(map[int]struct{}) // Clear all selections when filter is cleared
					}
					m.cursor = 0
					return m, nil
				}
			case "backspace":
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
					m.filteredProjects = m.filterProjectsByTopic(m.filterText)
					// If user hasn't manually modified selection, handle auto-selection based on filter text
					if !m.manualSelection {
						if m.filterText == "" {
							// If no filter text, deselect all projects
							m.selected = make(map[int]struct{})
						} else {
							// If filter text exists, auto-select matching projects and deselect non-matching
							// Create a set of filtered project indices for quick lookup
							filteredProjectIndices := make(map[int]struct{})
							for _, project := range m.filteredProjects {
								currentProjectIdx := m.findOriginalProjectIndex(project)
								filteredProjectIndices[currentProjectIdx] = struct{}{}
							}

							// Select all filtered projects
							for _, project := range m.filteredProjects {
								currentProjectIdx := m.findOriginalProjectIndex(project)
								m.selected[currentProjectIdx] = struct{}{}
							}

							// Deselect any projects not in the filtered results
							for i := range m.projects {
								if _, found := filteredProjectIndices[i]; !found {
									delete(m.selected, i)
								}
							}
						}
					}
					m.cursor = 0
				}
				return m, nil
			case "enter":
				// Exit filter mode
				if m.filterText == "" {
					// If no filter text, clear all selections before exiting
					m.selected = make(map[int]struct{})
				}
				m.filterMode = false
				m.manualSelection = false // Reset manual selection flag when exiting
				m.filterText = ""
				m.filteredProjects = m.projects
				m.cursor = 0
				return m, nil
			default:
				// Add character to filter text
				if len(msg.String()) == 1 && msg.Type != tea.KeyRunes {
					break // Skip special keys
				}
				if msg.Type == tea.KeyRunes {
					m.filterText += msg.String()
					m.filteredProjects = m.filterProjectsByTopic(m.filterText)
					// If user hasn't manually modified selection, handle auto-selection based on filter text
					if !m.manualSelection {
						if m.filterText == "" {
							// If no filter text, deselect all projects
							m.selected = make(map[int]struct{})
						} else {
							// If filter text exists, auto-select matching projects and deselect non-matching
							// Create a set of filtered project indices for quick lookup
							filteredProjectIndices := make(map[int]struct{})
							for _, project := range m.filteredProjects {
								currentProjectIdx := m.findOriginalProjectIndex(project)
								filteredProjectIndices[currentProjectIdx] = struct{}{}
							}

							// Select all filtered projects
							for _, project := range m.filteredProjects {
								currentProjectIdx := m.findOriginalProjectIndex(project)
								m.selected[currentProjectIdx] = struct{}{}
							}

							// Deselect any projects not in the filtered results
							for i := range m.projects {
								if _, found := filteredProjectIndices[i]; !found {
									delete(m.selected, i)
								}
							}
						}
					}
					m.cursor = 0
				}
			}
		} else {
			// Normal mode handling
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitted = true
				return m, tea.Quit

			case "f":
				// Enter filter mode
				m.filterMode = true
				m.filterText = ""
				m.filteredProjects = m.projects
				m.cursor = 0
				// Reset manual selection state when entering filter mode
				m.manualSelection = false
				// Don't modify the current selection until they start typing
				return m, nil

			case "up", "k":
				// Move up in column (layout is column-by-column)
				if m.cursor > 0 {
					m.cursor--
				}

			case "down", "j":
				// Move down in column
				if m.cursor < len(m.filteredProjects)-1 {
					m.cursor++
				}

			case "left", "h":
				// Move left to previous column
				numCols := m.calculateColumns()
				numRows := (len(m.filteredProjects) + numCols - 1) / numCols
				if m.cursor >= numRows {
					m.cursor -= numRows
				}

			case "right", "l":
				// Move right to next column
				numCols := m.calculateColumns()
				numRows := (len(m.filteredProjects) + numCols - 1) / numCols
				if m.cursor+numRows < len(m.filteredProjects) {
					m.cursor += numRows
				}

			case " ":
				// Toggle selection (only on filtered projects)
				if m.filterMode {
					// In filter mode, toggle the current project selection
					if m.cursor < len(m.filteredProjects) {
						currentProjectIdx := m.findOriginalProjectIndex(m.filteredProjects[m.cursor])
						if _, ok := m.selected[currentProjectIdx]; ok {
							delete(m.selected, currentProjectIdx)
						} else {
							m.selected[currentProjectIdx] = struct{}{}
						}
						// Mark that user has manually modified selection
						m.manualSelection = true
					}
				} else {
					// In normal mode, toggle individual selection
					if m.cursor < len(m.filteredProjects) {
						currentProjectIdx := m.findOriginalProjectIndex(m.filteredProjects[m.cursor])
						if _, ok := m.selected[currentProjectIdx]; ok {
							delete(m.selected, currentProjectIdx)
						} else {
							m.selected[currentProjectIdx] = struct{}{}
						}
					}
				}

			case "a":
				// Select all visible (filtered) projects when in filter mode
				if m.filterMode {
					if m.allFilteredProjectsSelected() {
						// Deselect all filtered projects (but keep other selections)
						for _, project := range m.filteredProjects {
							currentProjectIdx := m.findOriginalProjectIndex(project)
							delete(m.selected, currentProjectIdx)
						}
					} else {
						// Select all filtered projects
						for _, project := range m.filteredProjects {
							currentProjectIdx := m.findOriginalProjectIndex(project)
							m.selected[currentProjectIdx] = struct{}{}
						}
					}
					// Mark that user has manually modified selection
					m.manualSelection = true
				} else {
					// Normal mode: select/deselect all projects
					if len(m.selected) == len(m.projects) {
						// Deselect all projects
						m.selected = make(map[int]struct{})
					} else {
						// Select all projects
						for i := range m.projects {
							m.selected[i] = struct{}{}
						}
					}
				}

			case "r":
				m.refreshRequested = true
				return m, tea.Quit

			case "enter":
				m.confirmed = true
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
	}

	return m, nil
}

// Helper methods for filtering
func (m projectSelectorModel) filterProjectsByTopic(filterText string) []config.Project {
	if filterText == "" {
		return m.projects
	}

	var filtered []config.Project
	filterTextLower := strings.ToLower(strings.TrimSpace(filterText))

	// Split filter text by spaces to allow multiple search terms (OR match - any term can match)
	terms := strings.Fields(filterTextLower)

	for _, project := range m.projects {
		// Check if the project has any of the terms in its topics
		anyTermMatches := false
		for _, term := range terms {
			for _, topic := range project.Topics {
				// Use strings.Contains to allow partial matches
				if strings.Contains(strings.ToLower(topic), term) {
					anyTermMatches = true
					break
				}
			}
			if anyTermMatches {
				break // Found a match, no need to check other terms
			}
		}

		if anyTermMatches {
			filtered = append(filtered, project)
		}
	}

	return filtered
}

func (m projectSelectorModel) findOriginalProjectIndex(project config.Project) int {
	for i, p := range m.projects {
		if p.Repo == project.Repo {
			return i
		}
	}
	return 0 // Default to 0 if not found
}

func (m projectSelectorModel) allFilteredProjectsSelected() bool {
	for _, project := range m.filteredProjects {
		currentProjectIdx := m.findOriginalProjectIndex(project)
		if _, ok := m.selected[currentProjectIdx]; !ok {
			return false
		}
	}
	return true
}

func (m projectSelectorModel) calculateColumns() int {
	if m.termWidth == 0 {
		return 3 // Default to 3 columns
	}

	// Determine which projects to use for column calculation
	projectsToUse := m.projects
	if m.filterMode {
		projectsToUse = m.filteredProjects
	}

	// Find longest project name
	maxLen := 0
	for i, p := range projectsToUse {
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

	if m.filterMode {
		b.WriteString(titleStyle.Render("Filter Projects by Topic"))
		b.WriteString("\n")
		// Filter input field
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)
		b.WriteString(filterStyle.Render("> " + m.filterText))
		b.WriteString("\n\n")
	} else {
		b.WriteString(titleStyle.Render("Select Projects"))
		b.WriteString("\n\n")
	}

	// Determine which projects to display
	projectsToDisplay := m.projects
	if m.filterMode {
		projectsToDisplay = m.filteredProjects
	}

	// Calculate columns and layout
	numCols := m.calculateColumns()
	numRows := (len(projectsToDisplay) + numCols - 1) / numCols // Ceiling division

	// Find max width for alignment
	maxLen := 0
	for i, p := range projectsToDisplay {
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
			if idx >= len(projectsToDisplay) {
				break
			}

			project := projectsToDisplay[idx]

			// Checkbox - need to find original index to check selection
			originalIdx := m.findOriginalProjectIndex(project)
			checkbox := "[ ]"
			if _, ok := m.selected[originalIdx]; ok {
				checkbox = "[✓]"
			}

			// Item text
			itemText := fmt.Sprintf("%s %d. %s", checkbox, idx+1, project.Repo)

			// Style based on cursor position
			itemStyle := lipgloss.NewStyle().Width(colWidth)
			if idx == m.cursor {
				itemStyle = itemStyle.
					Foreground(lipgloss.Color("205")).
					Bold(true)
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

	var help string
	if m.filterMode {
		help = "Type to filter by topic • matching projects auto-selected • esc: clear filter • enter: exit filter • ↑/↓/←/→: navigate • space: toggle selection • a: select/deselect all • ctrl+c: quit"
	} else {
		help = "f: filter by topic • ↑/↓/←/→: navigate • space: toggle • a: toggle all • r: refresh • enter: confirm • q: quit"
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(help))

	// Selected count
	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	selectedCount := len(m.selected)
	countText := fmt.Sprintf("\nSelected: %d project(s)", selectedCount)
	b.WriteString(countStyle.Render(countText))

	if m.filterMode {
		countText := fmt.Sprintf("\nShowing: %d of %d projects", len(m.filteredProjects), len(m.projects))
		b.WriteString(countStyle.Render(countText))
	}

	return b.String()
}

func SelectProjects(projects []config.Project) ([]config.Project, bool, error) {
	if len(projects) == 0 {
		return nil, false, nil
	}

	p := tea.NewProgram(initialModel(projects))
	finalModel, err := p.Run()
	if err != nil {
		return nil, false, err
	}

	m := finalModel.(projectSelectorModel)

	if m.refreshRequested {
		return nil, true, nil
	}

	// User quit without confirming
	if m.quitted || !m.confirmed {
		return nil, false, nil
	}

	// Extract selected projects (maintain original order)
	var selected []config.Project
	for i, project := range m.projects {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, project)
		}
	}

	return selected, false, nil
}
