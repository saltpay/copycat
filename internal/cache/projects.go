package cache

import (
	"copycat/internal/config"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type cachedProject struct {
	Repo           string `yaml:"repo"`
	SlackRoom      string `yaml:"slack_room"`
	RequiresTicket bool   `yaml:"requires_ticket"`
}

type projectCache struct {
	Projects []cachedProject `yaml:"projects"`
}

// LoadProjects reads cached projects from disk.
func LoadProjects(filename string) ([]config.Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var wrapped projectCache
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("failed to parse project cache %s: %w", filename, err)
	}

	cached := wrapped.Projects
	projects := make([]config.Project, len(cached))
	for i, entry := range cached {
		slackRoom := strings.TrimSpace(entry.SlackRoom)
		if slackRoom == "#none" {
			slackRoom = ""
		}

		projects[i] = config.Project{
			Repo:           entry.Repo,
			SlackRoom:      slackRoom,
			RequiresTicket: entry.RequiresTicket,
		}
	}

	return projects, nil
}

// SaveProjects writes the provided projects to disk as YAML.
func SaveProjects(filename string, projects []config.Project) error {
	byRepo := make(map[string]cachedProject)

	if existingData, err := os.ReadFile(filename); err == nil {
		var existing projectCache
		if err := yaml.Unmarshal(existingData, &existing); err == nil {
			for _, entry := range existing.Projects {
				repo := strings.TrimSpace(entry.Repo)
				if repo == "" {
					continue
				}
				byRepo[repo] = entry
			}
		}
	}

	for _, project := range projects {
		repo := strings.TrimSpace(project.Repo)
		if repo == "" {
			continue
		}

		slackRoom := strings.TrimSpace(project.SlackRoom)

		existing, found := byRepo[repo]
		if !found {
			existing = cachedProject{Repo: repo}
		}

		if slackRoom != "" {
			existing.SlackRoom = slackRoom
		}

		if strings.TrimSpace(existing.SlackRoom) == "" {
			existing.SlackRoom = "#none"
		}

		existing.RequiresTicket = project.RequiresTicket

		byRepo[repo] = existing
	}

	merged := make([]cachedProject, 0, len(byRepo))
	for _, entry := range byRepo {
		merged = append(merged, entry)
	}

	// Keep cache stable to reduce noise in diffs.
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Repo < merged[j].Repo
	})

	data, err := yaml.Marshal(&projectCache{Projects: merged})
	if err != nil {
		return fmt.Errorf("failed to encode project cache %s: %w", filename, err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("failed to write project cache %s: %w", filename, err)
	}

	return nil
}
