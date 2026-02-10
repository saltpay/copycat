package permission

import (
	"encoding/json"
	"fmt"
	"os"
)

type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

// GenerateMCPConfig creates a temporary MCP configuration file that points
// Claude Code's permission-prompt-tool at the copycat permission-handler subcommand.
// Returns the file path and a cleanup function that removes it.
func GenerateMCPConfig(port int) (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	cfg := mcpConfig{
		MCPServers: map[string]mcpServerConfig{
			"copycat-auth": {
				Command: exe,
				Args:    []string{"permission-handler"},
				Env: map[string]string{
					"COPYCAT_PERMISSION_PORT": fmt.Sprintf("%d", port),
				},
			},
		},
	}

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
