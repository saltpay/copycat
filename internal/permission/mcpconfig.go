package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type mcpConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// ParseBashPrefixes extracts Bash command prefixes from an AllowedTools list.
// For example, "Bash(tree:*)" → "tree", "Bash(./mvnw test:*)" → "./mvnw test".
// Non-Bash entries (Edit, Read(*), List(*)) are skipped since those are
// handled by --permission-mode acceptEdits.
func ParseBashPrefixes(allowedTools []string) []string {
	var prefixes []string
	for _, t := range allowedTools {
		if !strings.HasPrefix(t, "Bash(") || !strings.HasSuffix(t, ")") {
			continue
		}
		inner := t[5 : len(t)-1] // strip "Bash(" and ")"
		// Remove trailing ":*" glob if present
		inner = strings.TrimSuffix(inner, ":*")
		if inner != "" {
			prefixes = append(prefixes, inner)
		}
	}
	return prefixes
}

// GenerateMCPConfig creates a temporary MCP configuration file that points
// Claude Code's permission-prompt-tool at the copycat permission-handler subcommand.
// It also merges the user's MCP servers from ~/.claude.json so they remain available.
// The preapprovedTools list (from config's allowed_tools) is parsed for Bash prefixes
// and passed to the handler via the COPYCAT_PREAPPROVED_TOOLS env var.
// Returns the file path and a cleanup function that removes it.
func GenerateMCPConfig(port int, preapprovedTools []string) (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	env := map[string]string{
		"COPYCAT_PERMISSION_PORT": fmt.Sprintf("%d", port),
	}
	if prefixes := ParseBashPrefixes(preapprovedTools); len(prefixes) > 0 {
		env["COPYCAT_PREAPPROVED_TOOLS"] = strings.Join(prefixes, ",")
	}

	copycatAuth := struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}{
		Command: exe,
		Args:    []string{"permission-handler"},
		Env:     env,
	}

	copycatAuthRaw, err := json.Marshal(copycatAuth)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal copycat-auth config: %w", err)
	}

	// Only include copycat-auth — user MCP servers are loaded via --setting-sources
	// and must not be duplicated here (remote/SSE servers can block Claude startup
	// if they are unreachable).
	servers := map[string]json.RawMessage{
		"copycat-auth": json.RawMessage(copycatAuthRaw),
	}

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
