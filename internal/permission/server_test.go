package permission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPermissionServer_ApproveRequest(t *testing.T) {
	statusCh := make(chan tea.Msg, 10)
	server, err := NewPermissionServer(statusCh)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	port := server.Port()
	if port == 0 {
		t.Fatal("expected non-zero port")
	}

	// Send a permission request in a goroutine
	done := make(chan permissionHTTPResponse, 1)
	go func() {
		body, _ := json.Marshal(permissionHTTPRequest{
			ToolName: "Bash",
			Command:  "npm install",
		})
		resp, err := http.Post(
			fmt.Sprintf("http://127.0.0.1:%d/permission", port),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Error(err)
			return
		}
		defer resp.Body.Close()
		var httpResp permissionHTTPResponse
		json.NewDecoder(resp.Body).Decode(&httpResp)
		done <- httpResp
	}()

	// Read the request from statusCh
	select {
	case msg := <-statusCh:
		reqMsg, ok := msg.(PermissionRequestMsg)
		if !ok {
			t.Fatalf("expected PermissionRequestMsg, got %T", msg)
		}
		if reqMsg.Request.Command != "npm install" {
			t.Errorf("expected command 'npm install', got %q", reqMsg.Request.Command)
		}
		// Approve
		reqMsg.Request.ResponseCh <- PermissionResponse{Approved: true}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for permission request")
	}

	// Check the HTTP response
	select {
	case resp := <-done:
		if !resp.Approved {
			t.Error("expected approved=true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for HTTP response")
	}
}

func TestPermissionServer_DenyRequest(t *testing.T) {
	statusCh := make(chan tea.Msg, 10)
	server, err := NewPermissionServer(statusCh)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	port := server.Port()

	done := make(chan permissionHTTPResponse, 1)
	go func() {
		body, _ := json.Marshal(permissionHTTPRequest{
			ToolName: "Bash",
			Command:  "rm -rf /",
		})
		resp, err := http.Post(
			fmt.Sprintf("http://127.0.0.1:%d/permission", port),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Error(err)
			return
		}
		defer resp.Body.Close()
		var httpResp permissionHTTPResponse
		json.NewDecoder(resp.Body).Decode(&httpResp)
		done <- httpResp
	}()

	select {
	case msg := <-statusCh:
		reqMsg := msg.(PermissionRequestMsg)
		reqMsg.Request.ResponseCh <- PermissionResponse{Approved: false}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	select {
	case resp := <-done:
		if resp.Approved {
			t.Error("expected approved=false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPermissionServer_ShutdownDeniesPending(t *testing.T) {
	statusCh := make(chan tea.Msg, 10)
	server, err := NewPermissionServer(statusCh)
	if err != nil {
		t.Fatal(err)
	}

	port := server.Port()

	done := make(chan permissionHTTPResponse, 1)
	go func() {
		body, _ := json.Marshal(permissionHTTPRequest{
			ToolName: "Bash",
			Command:  "something",
		})
		resp, err := http.Post(
			fmt.Sprintf("http://127.0.0.1:%d/permission", port),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			// Connection refused after shutdown is acceptable
			done <- permissionHTTPResponse{Approved: false}
			return
		}
		defer resp.Body.Close()
		var httpResp permissionHTTPResponse
		json.NewDecoder(resp.Body).Decode(&httpResp)
		done <- httpResp
	}()

	// Wait for request to arrive
	select {
	case <-statusCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Shutdown should deny pending
	server.Shutdown(context.Background())

	select {
	case resp := <-done:
		if resp.Approved {
			t.Error("expected denied after shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
