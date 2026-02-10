package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

const permissionTimeout = 5 * time.Minute

// PermissionServer listens on localhost for permission requests from the MCP handler
// and forwards them to the TUI via the statusCh channel.
type PermissionServer struct {
	listener net.Listener
	server   *http.Server
	statusCh chan<- tea.Msg

	mu      sync.Mutex
	pending map[string]chan PermissionResponse
}

type permissionHTTPRequest struct {
	ToolName string `json:"tool_name"`
	Command  string `json:"command"`
	Repo     string `json:"repo"`
}

type permissionHTTPResponse struct {
	Approved bool `json:"approved"`
}

// NewPermissionServer creates a new permission server that sends requests to statusCh.
func NewPermissionServer(statusCh chan<- tea.Msg) (*PermissionServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to bind permission server: %w", err)
	}

	ps := &PermissionServer{
		listener: listener,
		statusCh: statusCh,
		pending:  make(map[string]chan PermissionResponse),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/permission", ps.handlePermission)
	ps.server = &http.Server{Handler: mux}

	go ps.server.Serve(listener)

	return ps, nil
}

// Port returns the port the server is listening on.
func (ps *PermissionServer) Port() int {
	return ps.listener.Addr().(*net.TCPAddr).Port
}

func (ps *PermissionServer) handlePermission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req permissionHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	responseCh := make(chan PermissionResponse, 1)

	ps.mu.Lock()
	ps.pending[id] = responseCh
	ps.mu.Unlock()

	defer func() {
		ps.mu.Lock()
		delete(ps.pending, id)
		ps.mu.Unlock()
	}()

	// Send to TUI
	ps.statusCh <- PermissionRequestMsg{
		Request: PermissionRequest{
			ID:         id,
			Repo:       req.Repo,
			ToolName:   req.ToolName,
			Command:    req.Command,
			ResponseCh: responseCh,
		},
	}

	// Wait for user response or timeout
	select {
	case resp := <-responseCh:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(permissionHTTPResponse{Approved: resp.Approved})
	case <-time.After(permissionTimeout):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(permissionHTTPResponse{Approved: false})
	}
}

// Shutdown gracefully shuts down the server and denies any pending requests.
func (ps *PermissionServer) Shutdown(ctx context.Context) error {
	ps.mu.Lock()
	for id, ch := range ps.pending {
		ch <- PermissionResponse{Approved: false}
		delete(ps.pending, id)
	}
	ps.mu.Unlock()

	return ps.server.Shutdown(ctx)
}
