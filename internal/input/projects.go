package input

import (
	"fmt"
	"github.com/saltpay/copycat/internal/config"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxVisibleRows = 10
	maxColumns     = 3
)

// projectsConfirmedMsg is emitted when the user confirms their project selection.
type projectsConfirmedMsg struct {
	Selected []config.Project
}

// projectsRefreshMsg is emitted when the user requests a project list refresh.
type projectsRefreshMsg struct{}

type projectSelectorModel struct {
	projects     []config.Project
	cursor       int
	selected     map[int]struct{}
	scrollOffset int
	termWidth    int
	termHeight   int
	quitted      bool
	refreshing   bool
	// Filter fields
	filterMode       bool
	filterText       string
	filteredProjects []config.Project
	// Track if user has manually modified selection in filter mode
	manualSelection bool
	// Slack room warning after refresh
	showSlackWarning  bool
	missingSlackCount int
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
		filterMode:       false,
		filterText:       "",
		filteredProjects: sortedProjects, // Initially show all projects
		manualSelection:  false,
	}
}

func (m projectSelectorModel) Init() tea.Cmd {
	return tea.ClearScreen
}

func (m projectSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle slack warning dismissal
		if m.showSlackWarning {
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitted = true
				return m, tea.Quit
			case "enter", " ":
				m.showSlackWarning = false
				return m, nil
			}
			return m, nil
		}

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
				return m, func() tea.Msg { return projectsRefreshMsg{} }

			case "enter":
				return m, func() tea.Msg { return projectsConfirmedMsg{Selected: m.extractSelected()} }
			}
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
	}

	m.ensureCursorVisible()
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

func (m projectSelectorModel) countMissingSlackRooms() int {
	count := 0
	for _, p := range m.projects {
		if strings.TrimSpace(p.SlackRoom) == "" {
			count++
		}
	}
	return count
}

func (m projectSelectorModel) calculateColumns() int {
	if m.termWidth == 0 {
		return maxColumns
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
	if numCols > maxColumns {
		numCols = maxColumns
	}

	return numCols
}

func (m projectSelectorModel) extractSelected() []config.Project {
	var selected []config.Project
	for i, project := range m.projects {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, project)
		}
	}
	return selected
}

// ensureCursorVisible adjusts scrollOffset so the cursor's row is within the visible window.
func (m *projectSelectorModel) ensureCursorVisible() {
	numCols := m.calculateColumns()
	projectsToUse := m.filteredProjects
	numRows := (len(projectsToUse) + numCols - 1) / numCols
	if numRows == 0 {
		m.scrollOffset = 0
		return
	}

	cursorRow := m.cursor % numRows

	if cursorRow < m.scrollOffset {
		m.scrollOffset = cursorRow
	}
	if cursorRow >= m.scrollOffset+maxVisibleRows {
		m.scrollOffset = cursorRow - maxVisibleRows + 1
	}

	// Clamp
	maxOffset := numRows - maxVisibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m projectSelectorModel) View() string {
	if m.quitted {
		return ""
	}

	if m.refreshing {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
		return style.Render("  Refreshing project list...")
	}

	if m.showSlackWarning {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		return fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			warnStyle.Render(fmt.Sprintf("⚠ %d project(s) have no slack_room configured", m.missingSlackCount)),
			dimStyle.Render("Slack notifications will be skipped for these projects.\nRun 'copycat edit projects' to configure slack_room."),
			dimStyle.Render("Press enter to continue"),
		)
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("206"))

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

	// Scrolling viewport: only show maxVisibleRows rows
	visibleRows := numRows
	if visibleRows > maxVisibleRows {
		visibleRows = maxVisibleRows
	}
	scrollEnd := m.scrollOffset + visibleRows

	// Scroll-up indicator
	if m.scrollOffset > 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more row(s) above", m.scrollOffset)))
		b.WriteString("\n")
	}

	// Render visible rows of the grid (column by column layout)
	for row := m.scrollOffset; row < scrollEnd && row < numRows; row++ {
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

	// Scroll-down indicator
	rowsBelow := numRows - scrollEnd
	if rowsBelow > 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more row(s) below", rowsBelow)))
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
		Foreground(lipgloss.Color("40")).
		Bold(true)

	selectedCount := len(m.selected)
	countText := fmt.Sprintf("\nSelected: %d project(s)", selectedCount)
	b.WriteString(countStyle.Render(countText))

	if m.filterMode {
		filterCountText := fmt.Sprintf("\nShowing: %d of %d projects", len(m.filteredProjects), len(m.projects))
		b.WriteString(countStyle.Render(filterCountText))
	}

	// Warn about projects without slack rooms
	missingSlack := m.countMissingSlackRooms()
	if missingSlack > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString("\n")
		b.WriteString(warnStyle.Render(fmt.Sprintf(
			"⚠ %d project(s) have no slack_room — run 'copycat edit projects' to configure",
			missingSlack)))
	}

	return b.String()
}
