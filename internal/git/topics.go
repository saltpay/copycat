package git

import (
	"github.com/saltpay/copycat/internal/config"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type repoTopicsResponse struct {
	Names []string `json:"names"`
}

var errRepoNotFound = errors.New("repository not found")

// SyncTopicsWithCache ensures GitHub topics reflect the cached project metadata.
func SyncTopicsWithCache(projects []config.Project, githubCfg config.GitHubConfig) error {
	if len(projects) == 0 {
		return nil
	}

	owner := githubCfg.Organization
	for _, project := range projects {
		if err := syncProjectTopics(project, owner, githubCfg); err != nil {
			return err
		}
	}

	return nil
}

func syncProjectTopics(project config.Project, owner string, githubCfg config.GitHubConfig) error {
	repoSlug := fmt.Sprintf("%s/%s", owner, project.Repo)

	existingTopics, err := fetchRepositoryTopics(owner, project.Repo)
	if err != nil {
		if errors.Is(err, errRepoNotFound) {
			reportTopicFailure(project.Repo)
			return nil
		}
		return fmt.Errorf("failed to fetch topics for %s: %w", repoSlug, err)
	}

	addTopics, removeTopics := computeTopicChanges(existingTopics, project, githubCfg)
	if len(addTopics) == 0 && len(removeTopics) == 0 {
		fmt.Printf("✓ %s topics already up to date\n", project.Repo)
		return nil
	}

	args := []string{"repo", "edit", repoSlug}
	for _, t := range addTopics {
		args = append(args, "--add-topic", t)
	}
	for _, t := range removeTopics {
		args = append(args, "--remove-topic", t)
	}

	cmd := exec.Command("gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isNotFoundResponse(string(output)) {
			reportTopicFailure(project.Repo)
			return nil
		}
		return fmt.Errorf("failed to update topics for %s: %w\nOutput: %s", repoSlug, err, strings.TrimSpace(string(output)))
	}

	fmt.Printf("✓ Synced topics for %s (added: %s removed: %s)\n", project.Repo, summarizeTopics(addTopics), summarizeTopics(removeTopics))
	return nil
}

func fetchRepositoryTopics(owner, repo string) ([]string, error) {
	args := []string{
		"api",
		fmt.Sprintf("repos/%s/%s/topics", owner, repo),
		"-H", "Accept: application/vnd.github+json",
	}

	cmd := exec.Command("gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))
		if isNotFoundResponse(outputStr) {
			return nil, fmt.Errorf("%w: %s", errRepoNotFound, outputStr)
		}
		return nil, fmt.Errorf("gh api fetch topics failed: %w\nOutput: %s", err, strings.TrimSpace(string(output)))
	}

	var resp repoTopicsResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse topics response: %w", err)
	}

	// Deduplicate and sort for consistency.
	unique := make(map[string]struct{}, len(resp.Names))
	for _, name := range resp.Names {
		if name != "" {
			unique[name] = struct{}{}
		}
	}

	topics := make([]string, 0, len(unique))
	for name := range unique {
		topics = append(topics, name)
	}

	sort.Strings(topics)
	return topics, nil
}

func computeTopicChanges(existing []string, project config.Project, githubCfg config.GitHubConfig) (addTopics []string, removeTopics []string) {
	existingSet := make(map[string]struct{}, len(existing))
	for _, topic := range existing {
		existingSet[topic] = struct{}{}
	}

	// System-managed topics that should be tracked separately from project topics
	systemTopics := make(map[string]struct{})

	discoveryTopic := strings.TrimSpace(githubCfg.AutoDiscoveryTopic)
	if discoveryTopic != "" {
		systemTopics[discoveryTopic] = struct{}{}
		if _, hasTopic := existingSet[discoveryTopic]; !hasTopic {
			addTopics = append(addTopics, discoveryTopic)
		}
	}

	requiresTopic := strings.TrimSpace(githubCfg.RequiresTicketTopic)
	if requiresTopic != "" {
		systemTopics[requiresTopic] = struct{}{}
		_, hasTopic := existingSet[requiresTopic]
		if project.RequiresTicket && !hasTopic {
			addTopics = append(addTopics, requiresTopic)
		}
		if !project.RequiresTicket && hasTopic {
			removeTopics = append(removeTopics, requiresTopic)
		}
	}

	// Handle project-specific topics (from the cached project's Topics field)
	projectTopicsSet := make(map[string]struct{}, len(project.Topics))
	for _, topic := range project.Topics {
		if topic != "" {
			projectTopicsSet[topic] = struct{}{}
		}
	}

	// Add topics that are in the project but not in GitHub
	for topic := range projectTopicsSet {
		if _, exists := existingSet[topic]; !exists {
			// Only add if it's not a system-managed topic
			if _, isSystemTopic := systemTopics[topic]; !isSystemTopic {
				addTopics = append(addTopics, topic)
			}
		}
	}

	// If the project explicitly defines its Topics field (not nil),
	// manage those topics by removing any non-system topics that aren't in the project.
	// The project.Topics field is managed if it's not nil or empty.
	if project.Topics != nil {
		for _, topic := range existing {
			_, isProjectTopic := projectTopicsSet[topic]
			_, isSystemTopic := systemTopics[topic]

			// Remove if it's not a project topic and not a system topic
			if !isProjectTopic && !isSystemTopic {
				removeTopics = append(removeTopics, topic)
			}
		}
	}

	addTopics = deduplicate(addTopics)
	removeTopics = deduplicate(removeTopics)

	return addTopics, removeTopics
}

func deduplicate(items []string) []string {
	if len(items) <= 1 {
		return items
	}

	seen := make(map[string]struct{}, len(items))
	var result []string
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}

	return result
}

func summarizeTopics(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func isNotFoundResponse(output string) bool {
	if output == "" {
		return false
	}

	lower := strings.ToLower(output)
	return strings.Contains(lower, "404") && strings.Contains(lower, "not found")
}

func reportTopicFailure(repo string) {
	fmt.Printf("✘ %s (could not update topics in repository)\n", repo)
}
