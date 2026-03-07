package permission

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsPreapprovedBash(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		command  string
		expected bool
	}{
		{
			name:     "matching prefix",
			envVar:   "tree,cat,grep",
			command:  "tree -L 2",
			expected: true,
		},
		{
			name:     "matching multi-word prefix",
			envVar:   "tree,./mvnw test",
			command:  "./mvnw test -pl module-a",
			expected: true,
		},
		{
			name:     "no match",
			envVar:   "tree,cat,grep",
			command:  "gh pr list",
			expected: false,
		},
		{
			name:     "empty env var",
			envVar:   "",
			command:  "tree",
			expected: false,
		},
		{
			name:     "exact match",
			envVar:   "tree",
			command:  "tree",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COPYCAT_PREAPPROVED_TOOLS", tc.envVar)
			if got := isPreapprovedBash(tc.command); got != tc.expected {
				t.Errorf("isPreapprovedBash(%q) = %v, want %v (env=%q)", tc.command, got, tc.expected, tc.envVar)
			}
		})
	}
}

func TestHandleToolCall_MCPToolsGoToHTTP(t *testing.T) {
	// MCP tools should go through the HTTP permission server for user approval.
	// With an unreachable server, this results in a deny.
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
	}

	args := map[string]any{
		"name": "handle_permission",
		"arguments": map[string]any{
			"tool_name": "mcp__teya-developer__check_schema_versions",
			"input":     map[string]any{"schemas": "some.schema_v1"},
		},
	}
	paramsJSON, _ := json.Marshal(args)
	req.Params = paramsJSON

	resp := handleToolCall(req, "http://127.0.0.1:0") // invalid URL

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	// Should deny because HTTP server is unreachable — proving it attempted the HTTP call
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, `"behavior":"deny"`) {
		t.Errorf("expected deny response (HTTP unreachable), got: %s", resp.Result)
	}
}

func TestHandleToolCall_NonMatchingBashGoesToHTTP(t *testing.T) {
	// Non-matching Bash commands should try to contact the HTTP server (and fail here).
	t.Setenv("COPYCAT_PREAPPROVED_TOOLS", "tree,cat")

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
	}
	args := map[string]any{
		"name": "handle_permission",
		"arguments": map[string]any{
			"tool_name": "Bash",
			"input":     map[string]any{"command": "gh pr list"},
		},
	}
	paramsJSON, _ := json.Marshal(args)
	req.Params = paramsJSON

	resp := handleToolCall(req, "http://127.0.0.1:0") // invalid URL
	// Should deny because HTTP server is unreachable
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, `"behavior":"deny"`) {
		t.Errorf("expected deny response for non-matching Bash, got: %s", resp.Result)
	}
}

func TestHandleToolCall_PreapprovedBashAutoApproved(t *testing.T) {
	t.Setenv("COPYCAT_PREAPPROVED_TOOLS", "tree,cat")

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
	}
	args := map[string]any{
		"name": "handle_permission",
		"arguments": map[string]any{
			"tool_name": "Bash",
			"input":     map[string]any{"command": "tree -L 2"},
		},
	}
	paramsJSON, _ := json.Marshal(args)
	req.Params = paramsJSON

	resp := handleToolCall(req, "http://127.0.0.1:0")
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, `"behavior":"allow"`) {
		t.Errorf("expected allow response for preapproved Bash, got: %s", resp.Result)
	}
}
