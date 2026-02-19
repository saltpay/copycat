package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateMCPConfig(t *testing.T) {
	path, cleanup, err := GenerateMCPConfig(12345)
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

func TestGenerateMCPConfig_MergesUserServers(t *testing.T) {
	// Create a fake ~/.claude.json with user MCP servers.
	home := t.TempDir()
	t.Setenv("HOME", home)

	userConfig := map[string]any{
		"mcpServers": map[string]any{
			"teya-developer": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "@anthropic-ai/mcp-server"},
				"env":     map[string]string{"API_KEY": "secret"},
			},
			"jetbrains": map[string]any{
				"url": "http://localhost:6637/sse",
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

	path, cleanup, err := GenerateMCPConfig(9999)
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

	// All three servers should be present.
	for _, name := range []string{"copycat-auth", "teya-developer", "jetbrains"} {
		if _, ok := cfg.MCPServers[name]; !ok {
			t.Errorf("expected server %q in merged config", name)
		}
	}

	// Verify the user's jetbrains server kept its url field.
	var jb struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(cfg.MCPServers["jetbrains"], &jb); err != nil {
		t.Fatal(err)
	}
	if jb.URL != "http://localhost:6637/sse" {
		t.Errorf("expected jetbrains url http://localhost:6637/sse, got %s", jb.URL)
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

	path, cleanup, err := GenerateMCPConfig(7777)
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
