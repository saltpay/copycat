package config

import "testing"

func TestLoadConfig(t *testing.T) {
	cfg, err := Load("../../config.yaml")
	if err != nil {
		t.Fatalf("failed to load config.yaml: %v", err)
	}

	if cfg.GitHub.Organization == "" {
		t.Fatal("organization should be set")
	}

	if cfg.GitHub.RequiresTicketTopic == "" {
		t.Fatal("requires ticket topic should be set")
	}

	if cfg.GitHub.SlackRoomTopicPrefix == "" {
		t.Fatal("slack room topic prefix should be set")
	}

	if len(cfg.AIToolsConfig.Tools) == 0 {
		t.Fatal("expected at least one AI tool")
	}

	if cfg.AIToolsConfig.Default == "" {
		t.Fatal("default AI tool should be derived during load")
	}

	if _, ok := cfg.AIToolsConfig.ToolByName(cfg.AIToolsConfig.Default); !ok {
		t.Fatalf("default AI tool %q not found in tools list", cfg.AIToolsConfig.Default)
	}
}
