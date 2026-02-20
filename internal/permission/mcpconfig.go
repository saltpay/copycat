package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type mcpConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// readUserMCPServers reads the user's ~/.claude.json and returns their mcpServers map.
// Returns nil on any error (file missing, parse error, no key) — never blocks GenerateMCPConfig.
func readUserMCPServers() map[string]json.RawMessage {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return nil
	}

	var parsed struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}

	return parsed.MCPServers
}

// GenerateMCPConfig creates a temporary MCP configuration file that points
// Claude Code's permission-prompt-tool at the copycat permission-handler subcommand.
// It also merges the user's MCP servers from ~/.claude.json so they remain available.
// Returns the file path and a cleanup function that removes it.
func GenerateMCPConfig(port int) (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	copycatAuth := struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}{
		Command: exe,
		Args:    []string{"permission-handler"},
		Env: map[string]string{
			"COPYCAT_PERMISSION_PORT": fmt.Sprintf("%d", port),
		},
	}

	copycatAuthRaw, err := json.Marshal(copycatAuth)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal copycat-auth config: %w", err)
	}

	// Start with the user's MCP servers, then add copycat-auth (takes precedence).
	servers := readUserMCPServers()
	if servers == nil {
		servers = make(map[string]json.RawMessage)
	}
	servers["copycat-auth"] = json.RawMessage(copycatAuthRaw)

	cfg := mcpConfig{MCPServers: servers}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	f, err := os.CreateTemp("", "copycat-mcp-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp MCP config: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("failed to write MCP config: %w", err)
	}
	f.Close()

	if err := os.Chmod(f.Name(), 0o600); err != nil {
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("failed to set MCP config permissions: %w", err)
	}

	path := f.Name()
	cleanup := func() {
		os.Remove(path)
	}

	return path, cleanup, nil
}
