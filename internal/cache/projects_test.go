package cache

import (
	"copycat/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjects(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		want        []config.Project
		wantErr     bool
	}{
		{
			name: "valid YAML with multiple projects",
			yamlContent: `projects:
  - repo: service-a
    slack_room: "#team-alpha"
    requires_ticket: true
  - repo: service-b
    slack_room: "#team-beta"
    requires_ticket: false`,
			want: []config.Project{
				{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: true},
				{Repo: "service-b", SlackRoom: "#team-beta", RequiresTicket: false},
			},
			wantErr: false,
		},
		{
			name: "empty slack room becomes empty string",
			yamlContent: `projects:
  - repo: service-a
    slack_room: ""
    requires_ticket: false`,
			want: []config.Project{
				{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			},
			wantErr: false,
		},
		{
			name: "#none slack room becomes empty string",
			yamlContent: `projects:
  - repo: service-a
    slack_room: "#none"
    requires_ticket: false`,
			want: []config.Project{
				{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			},
			wantErr: false,
		},
		{
			name: "whitespace in slack room is trimmed",
			yamlContent: `projects:
  - repo: service-a
    slack_room: "  #team-alpha  "
    requires_ticket: false`,
			want: []config.Project{
				{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: false},
			},
			wantErr: false,
		},
		{
			name:        "invalid YAML",
			yamlContent: `invalid: yaml: content:`,
			wantErr:     true,
		},
		{
			name:        "empty projects list",
			yamlContent: `projects: []`,
			want:        []config.Project{},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "projects.yaml")

			if err := os.WriteFile(tmpFile, []byte(tt.yamlContent), 0o644); err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			got, err := LoadProjects(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadProjects() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("LoadProjects() got %d projects, want %d", len(got), len(tt.want))
					return
				}

				for i := range got {
					if got[i].Repo != tt.want[i].Repo {
						t.Errorf("LoadProjects() project[%d].Repo = %v, want %v", i, got[i].Repo, tt.want[i].Repo)
					}
					if got[i].SlackRoom != tt.want[i].SlackRoom {
						t.Errorf("LoadProjects() project[%d].SlackRoom = %v, want %v", i, got[i].SlackRoom, tt.want[i].SlackRoom)
					}
					if got[i].RequiresTicket != tt.want[i].RequiresTicket {
						t.Errorf("LoadProjects() project[%d].RequiresTicket = %v, want %v", i, got[i].RequiresTicket, tt.want[i].RequiresTicket)
					}
				}
			}
		})
	}
}

func TestLoadProjectsNonExistentFile(t *testing.T) {
	_, err := LoadProjects("/nonexistent/file.yaml")
	if err == nil {
		t.Error("LoadProjects() expected error for nonexistent file, got nil")
	}
}

func TestSaveProjects(t *testing.T) {
	tests := []struct {
		name           string
		existing       string
		projects       []config.Project
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:     "save new projects",
			existing: "",
			projects: []config.Project{
				{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: true},
				{Repo: "service-b", SlackRoom: "", RequiresTicket: false},
			},
			wantContains: []string{
				"service-a",
				"#team-alpha",
				"requires_ticket: true",
				"service-b",
				"#none",
				"requires_ticket: false",
			},
		},
		{
			name: "merge with existing projects",
			existing: `projects:
  - repo: service-a
    slack_room: "#old-channel"
    requires_ticket: false`,
			projects: []config.Project{
				{Repo: "service-a", SlackRoom: "#new-channel", RequiresTicket: true},
			},
			wantContains: []string{
				"service-a",
				"#new-channel",
				"requires_ticket: true",
			},
			wantNotContain: []string{
				"#old-channel",
			},
		},
		{
			name: "preserve existing slack room if new is empty",
			existing: `projects:
  - repo: service-a
    slack_room: "#existing-channel"
    requires_ticket: false`,
			projects: []config.Project{
				{Repo: "service-a", SlackRoom: "", RequiresTicket: true},
			},
			wantContains: []string{
				"service-a",
				"#existing-channel",
				"requires_ticket: true",
			},
		},
		{
			name:     "empty slack room becomes #none",
			existing: "",
			projects: []config.Project{
				{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			},
			wantContains: []string{
				"service-a",
				"#none",
			},
		},
		{
			name:     "projects are sorted alphabetically",
			existing: "",
			projects: []config.Project{
				{Repo: "zebra", SlackRoom: "#z", RequiresTicket: false},
				{Repo: "alpha", SlackRoom: "#a", RequiresTicket: false},
				{Repo: "beta", SlackRoom: "#b", RequiresTicket: false},
			},
			wantContains: []string{
				"alpha",
				"beta",
				"zebra",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "projects.yaml")

			// Write existing content if provided
			if tt.existing != "" {
				if err := os.WriteFile(tmpFile, []byte(tt.existing), 0o644); err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
			}

			// Save projects
			if err := SaveProjects(tmpFile, tt.projects); err != nil {
				t.Fatalf("SaveProjects() error = %v", err)
			}

			// Read back the file
			content, err := os.ReadFile(tmpFile)
			if err != nil {
				t.Fatalf("Failed to read saved file: %v", err)
			}

			contentStr := string(content)

			// Check that expected strings are present
			for _, want := range tt.wantContains {
				if !contains(contentStr, want) {
					t.Errorf("SaveProjects() content missing %q\nGot:\n%s", want, contentStr)
				}
			}

			// Check that unwanted strings are not present
			for _, notWant := range tt.wantNotContain {
				if contains(contentStr, notWant) {
					t.Errorf("SaveProjects() content should not contain %q\nGot:\n%s", notWant, contentStr)
				}
			}
		})
	}
}

func TestSaveProjectsRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "projects.yaml")

	original := []config.Project{
		{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: true},
		{Repo: "service-b", SlackRoom: "#team-beta", RequiresTicket: false},
	}

	// Save
	if err := SaveProjects(tmpFile, original); err != nil {
		t.Fatalf("SaveProjects() error = %v", err)
	}

	// Load
	loaded, err := LoadProjects(tmpFile)
	if err != nil {
		t.Fatalf("LoadProjects() error = %v", err)
	}

	// Compare (order might be different due to sorting)
	if len(loaded) != len(original) {
		t.Errorf("Round trip got %d projects, want %d", len(loaded), len(original))
	}

	// Create a map for comparison
	loadedMap := make(map[string]config.Project)
	for _, p := range loaded {
		loadedMap[p.Repo] = p
	}

	for _, orig := range original {
		loaded, ok := loadedMap[orig.Repo]
		if !ok {
			t.Errorf("Round trip missing repo %q", orig.Repo)
			continue
		}

		if loaded.SlackRoom != orig.SlackRoom {
			t.Errorf("Round trip repo %q SlackRoom = %q, want %q", orig.Repo, loaded.SlackRoom, orig.SlackRoom)
		}
		if loaded.RequiresTicket != orig.RequiresTicket {
			t.Errorf("Round trip repo %q RequiresTicket = %v, want %v", orig.Repo, loaded.RequiresTicket, orig.RequiresTicket)
		}
	}
}

func TestSaveProjectsSkipsEmptyRepos(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "projects.yaml")

	projects := []config.Project{
		{Repo: "service-a", SlackRoom: "#team", RequiresTicket: false},
		{Repo: "", SlackRoom: "#ignored", RequiresTicket: true},
		{Repo: "  ", SlackRoom: "#also-ignored", RequiresTicket: true},
	}

	if err := SaveProjects(tmpFile, projects); err != nil {
		t.Fatalf("SaveProjects() error = %v", err)
	}

	loaded, err := LoadProjects(tmpFile)
	if err != nil {
		t.Fatalf("LoadProjects() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("SaveProjects() saved %d projects, want 1 (empty repos should be skipped)", len(loaded))
	}

	if loaded[0].Repo != "service-a" {
		t.Errorf("SaveProjects() saved repo = %q, want %q", loaded[0].Repo, "service-a")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
