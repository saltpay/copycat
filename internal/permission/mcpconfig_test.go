package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGenerateMCPConfig(t *testing.T) {
	path, cleanup, err := GenerateMCPConfig(12345, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// File should exist
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal("MCP config file should exist:", err)
	}

	// File permissions should be 0600
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}

	// Parse and validate content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal("failed to parse MCP config:", err)
	}

	raw, ok := cfg.MCPServers["copycat-auth"]
	if !ok {
		t.Fatal("expected copycat-auth server in config")
	}

	var server struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &server); err != nil {
		t.Fatal("failed to parse copycat-auth server:", err)
	}

	if len(server.Args) != 1 || server.Args[0] != "permission-handler" {
		t.Errorf("expected args [permission-handler], got %v", server.Args)
	}

	if server.Env["COPYCAT_PERMISSION_PORT"] != "12345" {
		t.Errorf("expected port 12345, got %s", server.Env["COPYCAT_PERMISSION_PORT"])
	}

	// Cleanup should remove the file
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup should have removed the file")
	}
}

func TestGenerateMCPConfig_DoesNotMergeUserServers(t *testing.T) {
	// User MCP servers from ~/.claude.json should NOT be merged —
	// they are loaded via --setting-sources user by Claude Code directly.
	home := t.TempDir()
	t.Setenv("HOME", home)

	userConfig := map[string]any{
		"mcpServers": map[string]any{
			"teya-developer": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "@anthropic-ai/mcp-server"},
			},
		},
	}
	data, err := json.Marshal(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	path, cleanup, err := GenerateMCPConfig(9999, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal("failed to parse MCP config:", err)
	}

	// Only copycat-auth should be present.
	if _, ok := cfg.MCPServers["copycat-auth"]; !ok {
		t.Error("expected copycat-auth server in config")
	}
	if _, ok := cfg.MCPServers["teya-developer"]; ok {
		t.Error("user MCP servers should not be merged into the config")
	}
}

func TestGenerateMCPConfig_CopycatAuthTakesPrecedence(t *testing.T) {
	// User's ~/.claude.json has a "copycat-auth" entry that should be overwritten.
	home := t.TempDir()
	t.Setenv("HOME", home)

	userConfig := map[string]any{
		"mcpServers": map[string]any{
			"copycat-auth": map[string]any{
				"command": "fake-command",
				"args":    []string{"fake-arg"},
			},
		},
	}
	data, err := json.Marshal(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	path, cleanup, err := GenerateMCPConfig(7777, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal("failed to parse MCP config:", err)
	}

	var server struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(cfg.MCPServers["copycat-auth"], &server); err != nil {
		t.Fatal(err)
	}

	// Should be the real copycat-auth, not the user's fake one.
	if len(server.Args) != 1 || server.Args[0] != "permission-handler" {
		t.Errorf("expected args [permission-handler], got %v (user's fake was not overwritten)", server.Args)
	}

	if server.Env["COPYCAT_PERMISSION_PORT"] != "7777" {
		t.Errorf("expected port 7777, got %s", server.Env["COPYCAT_PERMISSION_PORT"])
	}
}

func TestParseBashPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "bash patterns extracted",
			input:    []string{"Bash(tree:*)", "Bash(cat:*)", "Bash(./mvnw test:*)"},
			expected: []string{"tree", "cat", "./mvnw test"},
		},
		{
			name:     "non-bash entries skipped",
			input:    []string{"Edit", "Read(*)", "List(*)", "Bash(grep:*)"},
			expected: []string{"grep"},
		},
		{
			name:     "empty input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "no bash entries",
			input:    []string{"Edit", "Read(*)", "List(*)"},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseBashPrefixes(tc.input)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("ParseBashPrefixes(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestGenerateMCPConfig_PreapprovedToolsEnvVar(t *testing.T) {
	allowedTools := []string{
		"Edit",
		"Read(*)",
		"Bash(tree:*)",
		"Bash(./mvnw test:*)",
	}

	path, cleanup, err := GenerateMCPConfig(5555, allowedTools)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal("failed to parse MCP config:", err)
	}

	var server struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(cfg.MCPServers["copycat-auth"], &server); err != nil {
		t.Fatal(err)
	}

	expected := "tree,./mvnw test"
	if got := server.Env["COPYCAT_PREAPPROVED_TOOLS"]; got != expected {
		t.Errorf("expected COPYCAT_PREAPPROVED_TOOLS=%q, got %q", expected, got)
	}
}

func TestGenerateMCPConfig_NoPreapprovedToolsEnvVar(t *testing.T) {
	// When no Bash patterns exist, env var should not be set
	path, cleanup, err := GenerateMCPConfig(5556, []string{"Edit", "Read(*)"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal("failed to parse MCP config:", err)
	}

	var server struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(cfg.MCPServers["copycat-auth"], &server); err != nil {
		t.Fatal(err)
	}

	if _, ok := server.Env["COPYCAT_PREAPPROVED_TOOLS"]; ok {
		t.Error("COPYCAT_PREAPPROVED_TOOLS should not be set when no Bash patterns exist")
	}
}
