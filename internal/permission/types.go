package permission

import tea "github.com/charmbracelet/bubbletea"

// QuestionOption represents a single selectable option in an AskUserQuestion prompt.
type QuestionOption struct {
	Label       string
	Description string
}

// Question represents a single question from Claude's AskUserQuestion tool.
type Question struct {
	Text    string
	Header  string
	Options []QuestionOption
}

// PermissionRequest represents a request from the AI tool for permission to run a command.
type PermissionRequest struct {
	ID         string
	Repo       string
	ToolName   string
	Command    string
	ResponseCh chan PermissionResponse
	IsQuestion bool
	Questions  []Question
}

// PermissionResponse carries the user's decision.
type PermissionResponse struct {
	Approved bool
	Answer   string // Selected option label for AskUserQuestion
}

// PermissionRequestMsg wraps a PermissionRequest for the bubbletea message loop.
type PermissionRequestMsg struct {
	Request PermissionRequest
}

// Ensure PermissionRequestMsg satisfies tea.Msg (it does implicitly, but this is for documentation).
var _ tea.Msg = PermissionRequestMsg{}
