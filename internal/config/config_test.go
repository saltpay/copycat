package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := Load("../../config.yaml")
	if err != nil {
		t.Fatalf("failed to load config.yaml: %v", err)
	}

	if cfg.GitHub.Organization == "" {
		t.Fatal("organization should be set")
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

func TestLoadProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")

	content := `projects:
  - repo: my-repo
    slack_room: "#general"
    topics:
      - go
  - repo: other-repo
    slack_room: ""
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	projects, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("LoadProjects failed: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	if projects[0].Repo != "my-repo" {
		t.Errorf("expected repo 'my-repo', got %q", projects[0].Repo)
	}
	if projects[0].SlackRoom != "#general" {
		t.Errorf("expected slack_room '#general', got %q", projects[0].SlackRoom)
	}
	if len(projects[0].Topics) != 1 || projects[0].Topics[0] != "go" {
		t.Errorf("expected topics [go], got %v", projects[0].Topics)
	}
}

func TestLoadProjectsFileNotFound(t *testing.T) {
	_, err := LoadProjects("/nonexistent/projects.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSaveProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")

	projects := []Project{
		{Repo: "repo-a", SlackRoom: "#team-a", Topics: []string{"go", "api"}},
		{Repo: "repo-b", SlackRoom: ""},
	}

	if err := SaveProjects(path, projects); err != nil {
		t.Fatalf("SaveProjects failed: %v", err)
	}

	// Load them back
	loaded, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("LoadProjects after save failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(loaded))
	}
	if loaded[0].Repo != "repo-a" {
		t.Errorf("expected repo 'repo-a', got %q", loaded[0].Repo)
	}
	if loaded[0].SlackRoom != "#team-a" {
		t.Errorf("expected slack_room '#team-a', got %q", loaded[0].SlackRoom)
	}
	if len(loaded[0].Topics) != 2 {
		t.Errorf("expected 2 topics, got %v", loaded[0].Topics)
	}
}

func TestSaveProjectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")

	if err := SaveProjects(path, []Project{}); err != nil {
		t.Fatalf("SaveProjects with empty list failed: %v", err)
	}

	loaded, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("LoadProjects after empty save failed: %v", err)
	}

	if len(loaded) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(loaded))
	}
}
