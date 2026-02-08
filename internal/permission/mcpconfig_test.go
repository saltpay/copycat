package permission

import (
	"encoding/json"
	"os"
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

	server, ok := cfg.MCPServers["copycat-auth"]
	if !ok {
		t.Fatal("expected copycat-auth server in config")
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
