package permission

import tea "github.com/charmbracelet/bubbletea"

// PermissionRequest represents a request from the AI tool for permission to run a command.
type PermissionRequest struct {
	ID         string
	Repo       string
	ToolName   string
	Command    string
	ResponseCh chan PermissionResponse
}

// PermissionResponse carries the user's decision.
type PermissionResponse struct {
	Approved bool
}

// PermissionRequestMsg wraps a PermissionRequest for the bubbletea message loop.
type PermissionRequestMsg struct {
	Request PermissionRequest
}

// Ensure PermissionRequestMsg satisfies tea.Msg (it does implicitly, but this is for documentation).
var _ tea.Msg = PermissionRequestMsg{}
