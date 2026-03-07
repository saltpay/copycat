package input

import (
	"fmt"
	"github.com/saltpay/copycat/v2/internal/config"
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
	filterTerms      []string // locked/confirmed filter terms during editing
	appliedTerms     []string // terms applied after exiting filter mode
	filteredProjects []config.Project
	// Track if user has manually modified selection in filter mode
	manualSelection bool
	// Pre-filter selection backup
	preFilterSelection map[int]struct{}
	// Zero selection warning
	noSelectionWarning bool
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
				if m.filterText != "" {
					// Clear current text first
					m.filterText = ""
				} else if len(m.filterTerms) > 0 {
					// Then clear all locked terms
					m.filterTerms = nil
				} else {
					// Finally exit filter mode — restore pre-filter selection
					m.filterMode = false
					m.manualSelection = false
					m.appliedTerms = nil
					m.filteredProjects = m.projects
					m.cursor = 0
					if m.preFilterSelection != nil {
						m.selected = m.preFilterSelection
						m.preFilterSelection = nil
					}
					return m, nil
				}
				m.filteredProjects = m.applyAllFilters()
				if !m.manualSelection {
					m.autoSelectFiltered()
				}
				m.cursor = 0
				return m, nil
			case "backspace":
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
				} else if len(m.filterTerms) > 0 {
					// Remove last locked term when backspacing on empty input
					m.filterTerms = m.filterTerms[:len(m.filterTerms)-1]
				}
				m.filteredProjects = m.applyAllFilters()
				if !m.manualSelection {
					m.autoSelectFiltered()
				}
				m.cursor = 0
				return m, nil
			case "enter":
				if m.filterText != "" {
					// Lock current text as a filter term
					m.filterTerms = append(m.filterTerms, strings.TrimSpace(m.filterText))
					m.filterText = ""
					m.filteredProjects = m.applyAllFilters()
					if !m.manualSelection {
						m.autoSelectFiltered()
					}
					m.cursor = 0
				} else {
					// Empty text: exit filter mode, keep filtered list and selections
					m.filterMode = false
					m.manualSelection = false
					m.appliedTerms = m.filterTerms
					m.filterTerms = nil
					m.filterText = ""
					m.cursor = 0
				}
				return m, nil
			default:
				// Add character to filter text
				if len(msg.String()) == 1 && msg.Type != tea.KeyRunes {
					break // Skip special keys
				}
				if msg.Type == tea.KeyRunes {
					m.filterText += msg.String()
					m.filteredProjects = m.applyAllFilters()
					if !m.manualSelection {
						m.autoSelectFiltered()
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
				// Enter filter mode — save current selection for restore on cancel
				m.filterMode = true
				m.filterText = ""
				m.filterTerms = nil
				m.filteredProjects = m.projects
				m.cursor = 0
				m.manualSelection = false
				// Save current selection so ESC can restore it
				m.preFilterSelection = make(map[int]struct{}, len(m.selected))
				for k, v := range m.selected {
					m.preFilterSelection[k] = v
				}
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
				m.noSelectionWarning = false
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
				m.noSelectionWarning = false
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
				selected := m.extractSelected()
				if len(selected) == 0 {
					m.noSelectionWarning = true
					return m, nil
				}
				return m, func() tea.Msg { return projectsConfirmedMsg{Selected: selected} }
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
		// Check if the project matches any term via repo name or topics
		anyTermMatches := false
		for _, term := range terms {
			// Match against repo name
			if strings.Contains(strings.ToLower(project.Repo), term) {
				anyTermMatches = true
				break
			}
			for _, topic := range project.Topics {
				if strings.Contains(strings.ToLower(topic), term) {
					anyTermMatches = true
					break
				}
			}
			if anyTermMatches {
				break
			}
		}

		if anyTermMatches {
			filtered = append(filtered, project)
		}
	}

	return filtered
}

// applyAllFilters combines locked filter terms and current text to filter projects.
// Each term is matched against repo name OR topics; projects must match ALL terms (AND between terms).
func (m projectSelectorModel) applyAllFilters() []config.Project {
	allTerms := make([]string, len(m.filterTerms))
	copy(allTerms, m.filterTerms)
	if t := strings.TrimSpace(m.filterText); t != "" {
		allTerms = append(allTerms, t)
	}

	if len(allTerms) == 0 {
		return m.projects
	}

	var filtered []config.Project
	for _, project := range m.projects {
		matchesAll := true
		for _, term := range allTerms {
			termLower := strings.ToLower(term)
			termMatches := false
			// Match against repo name
			if strings.Contains(strings.ToLower(project.Repo), termLower) {
				termMatches = true
			}
			// Match against topics
			if !termMatches {
				for _, topic := range project.Topics {
					if strings.Contains(strings.ToLower(topic), termLower) {
						termMatches = true
						break
					}
				}
			}
			if !termMatches {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

// autoSelectFiltered selects all filtered projects and deselects non-matching ones.
func (m *projectSelectorModel) autoSelectFiltered() {
	hasFilters := len(m.filterTerms) > 0 || strings.TrimSpace(m.filterText) != ""
	if !hasFilters {
		m.selected = make(map[int]struct{})
		return
	}

	filteredSet := make(map[int]struct{})
	for _, project := range m.filteredProjects {
		filteredSet[m.findOriginalProjectIndex(project)] = struct{}{}
	}
	for _, project := range m.filteredProjects {
		m.selected[m.findOriginalProjectIndex(project)] = struct{}{}
	}
	for i := range m.projects {
		if _, found := filteredSet[i]; !found {
			delete(m.selected, i)
		}
	}
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
	projectsToUse := m.filteredProjects

	// Find longest project name
	maxLen := 0
	for i, p := range projectsToUse {
		// Format: "[ ] 123. repo-name" or "[ ] 123. repo-name ⚠"
		itemLen := len(fmt.Sprintf("[ ] %d. %s", i+1, p.Repo))
		if strings.TrimSpace(p.SlackRoom) == "" {
			itemLen += 2 // " ⚠"
		}
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
		b.WriteString(titleStyle.Render("Filter Projects"))
		b.WriteString(" ")
		// Render locked filter terms as chips
		chipStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("205")).
			Padding(0, 1)
		for _, term := range m.filterTerms {
			b.WriteString(chipStyle.Render(term))
			b.WriteString(" ")
		}
		// Filter input field
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("206")).
			Padding(0, 1)
		b.WriteString(filterStyle.Render("> " + m.filterText))
		b.WriteString("\n\n")
	} else {
		b.WriteString(titleStyle.Render("Select Projects"))
		if len(m.appliedTerms) > 0 {
			b.WriteString("  ")
			chipStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("205")).
				Padding(0, 1)
			for _, term := range m.appliedTerms {
				b.WriteString(chipStyle.Render(term))
				b.WriteString(" ")
			}
		}
		b.WriteString("\n\n")
	}

	// Determine which projects to display
	projectsToDisplay := m.filteredProjects

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
			if strings.TrimSpace(project.SlackRoom) == "" {
				itemText += " ⚠"
			}

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
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	if m.filterMode {
		b.WriteString(helpStyle.Render("type: filter • enter: lock term • enter (empty): apply • esc: clear/exit • space: toggle • a: toggle all • ctrl+c: quit"))
	} else {
		b.WriteString(helpStyle.Render("f: filter • ↑/↓/←/→: navigate • space: toggle • a: toggle all • r: refresh • enter: confirm • q: quit"))
	}

	// Selected count
	b.WriteString("\n")
	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("40")).
		Bold(true)

	selectedCount := len(m.selected)
	countLine := fmt.Sprintf("Selected: %d project(s)", selectedCount)
	if len(m.filteredProjects) < len(m.projects) {
		countLine += fmt.Sprintf("  •  Showing: %d of %d projects", len(m.filteredProjects), len(m.projects))
	}
	b.WriteString("\n")
	b.WriteString(countStyle.Render(countLine))

	if m.noSelectionWarning {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
		b.WriteString("  ")
		b.WriteString(warnStyle.Render("No projects selected — use space to select"))
	}

	missingSlack := m.countMissingSlackRooms()
	if missingSlack > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString("  ")
		b.WriteString(warnStyle.Render(fmt.Sprintf(
			"⚠ %d no slack_room",
			missingSlack)))
	}

	return b.String()
}
