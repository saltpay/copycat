package git

import (
	"copycat/internal/config"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitHubRepo represents the JSON response from gh repo list
type GitHubRepo struct {
	Name             string  `json:"name"`
	IsArchived       bool    `json:"isArchived"`
	RepositoryTopics []Topic `json:"repositoryTopics"`
}

type Topic struct {
	Topic string `json:"name"`
}

// FetchRepositories fetches unarchived repositories with the specified topic from GitHub
func FetchRepositories(githubCfg config.GitHubConfig) ([]config.Project, error) {
	// Use gh CLI to fetch repositories
	args := []string{
		"repo", "list", githubCfg.Organization,
		"--json", "name,isArchived,repositoryTopics",
	}
	if githubCfg.AutoDiscoveryTopic != "" {
		args = append(args, "--topic", githubCfg.AutoDiscoveryTopic)
	}
	args = append(args, "--no-archived", "--limit", "1000")

	cmd := exec.Command("gh", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repositories from GitHub: %w\nOutput: %s", err, string(output))
	}

	var repos []GitHubRepo
	if err := json.Unmarshal(output, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub response: %w", err)
	}

	// Convert to projects and check for requires-ticket topic
	var projects []config.Project
	for _, repo := range repos {
		requiresTicket := false
		slackRoom := ""
		for _, t := range repo.RepositoryTopics {
			name := t.Topic
			if githubCfg.RequiresTicketTopic != "" && name == githubCfg.RequiresTicketTopic {
				requiresTicket = true
			}
			if slackRoom == "" && githubCfg.SlackRoomTopicPrefix != "" && strings.HasPrefix(name, githubCfg.SlackRoomTopicPrefix) {
				channel := strings.TrimPrefix(name, githubCfg.SlackRoomTopicPrefix)
				if channel != "" {
					if !strings.HasPrefix(channel, "#") {
						channel = "#" + channel
					}
					slackRoom = channel
				}
			}
		}

		project := config.Project{
			Repo:           repo.Name,
			SlackRoom:      slackRoom,
			RequiresTicket: requiresTicket,
		}
		projects = append(projects, project)
	}

	if len(projects) == 0 {
		if githubCfg.AutoDiscoveryTopic == "" {
			return nil, fmt.Errorf("no unarchived repositories found in organization '%s'", githubCfg.Organization)
		}
		return nil, fmt.Errorf("no unarchived repositories found with topic '%s' in organization '%s'", githubCfg.AutoDiscoveryTopic, githubCfg.Organization)
	}

	return projects, nil
}
