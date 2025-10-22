package config

import "testing"

func TestNoDuplicateRepositories(t *testing.T) {
	projects, err := LoadProjects("../../projects.yaml")
	if err != nil {
		t.Fatalf("failed to load projects.yaml: %v", err)
	}

	seen := make(map[string]struct{})
	for _, project := range projects {
		if _, exists := seen[project.Repo]; exists {
			t.Fatalf("duplicate repo detected in projects.yaml: %s", project.Repo)
		}
		seen[project.Repo] = struct{}{}
	}
}
