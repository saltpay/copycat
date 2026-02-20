package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/saltpay/copycat/internal/config"
)

const slackAPIURL = "https://slack.com/api/chat.postMessage"

type slackMessage struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

type slackResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// repoWithURL holds a repository name and its PR URL
type repoWithURL struct {
	Repo  string
	PRURL string
}

// SendNotifications sends notifications for successful projects, grouped by Slack room.
// The onStatus callback receives progress lines instead of printing to stdout.
func SendNotifications(successfulProjects []config.Project, prTitle string, prURLs map[string]string, token string, onStatus func(string)) {
	if len(successfulProjects) == 0 {
		return
	}

	// Group projects by Slack room
	projectsByRoom := make(map[string][]repoWithURL)
	for _, project := range successfulProjects {
		slackRoom := strings.TrimSpace(project.SlackRoom)
		if slackRoom == "" {
			continue // Skip projects without a Slack room
		}
		projectsByRoom[slackRoom] = append(projectsByRoom[slackRoom], repoWithURL{
			Repo:  project.Repo,
			PRURL: prURLs[project.Repo],
		})
	}

	if len(projectsByRoom) == 0 {
		onStatus("⚠️  No Slack rooms configured for successful projects, skipping notifications")
		return
	}

	onStatus("Sending Slack notifications...")

	for channel, repos := range projectsByRoom {
		message := formatMessage(prTitle, repos)
		err := sendMessage(token, channel, message)
		repoNames := make([]string, len(repos))
		for i, r := range repos {
			repoNames[i] = r.Repo
		}
		if err != nil {
			onStatus(fmt.Sprintf("⚠️  Failed to send notification to %s for: %s: %v", channel, strings.Join(repoNames, ", "), err))
		} else {
			onStatus(fmt.Sprintf("✓ Notification sent to %s for: %s", channel, strings.Join(repoNames, ", ")))
		}
	}
}

// SendAssessmentFindings sends per-project assessment findings to Slack, grouped by channel.
func SendAssessmentFindings(projects []config.Project, question string, findings map[string]string, token string, onStatus func(string)) {
	if len(projects) == 0 {
		return
	}

	// Group projects by Slack room
	projectsByRoom := make(map[string][]string)
	for _, project := range projects {
		slackRoom := strings.TrimSpace(project.SlackRoom)
		if slackRoom == "" {
			continue
		}
		projectsByRoom[slackRoom] = append(projectsByRoom[slackRoom], project.Repo)
	}

	if len(projectsByRoom) == 0 {
		onStatus("⚠️  No Slack rooms configured for assessed projects, skipping notifications")
		return
	}

	onStatus("Sending assessment findings to Slack...")

	for channel, repos := range projectsByRoom {
		// Build findings for repos in this channel
		repoFindings := make(map[string]string)
		for _, repo := range repos {
			if finding, ok := findings[repo]; ok {
				repoFindings[repo] = finding
			}
		}
		if len(repoFindings) == 0 {
			continue
		}

		message := formatAssessmentMessage(question, repoFindings)
		err := sendMessage(token, channel, message)
		repoNames := strings.Join(repos, ", ")
		if err != nil {
			onStatus(fmt.Sprintf("⚠️  Failed to send findings to %s for: %s: %v", channel, repoNames, err))
		} else {
			onStatus(fmt.Sprintf("✓ Findings sent to %s for: %s", channel, repoNames))
		}
	}
}

func formatAssessmentMessage(question string, repoFindings map[string]string) string {
	var sb strings.Builder
	sb.WriteString("🐱 *Assessment Results*\n\n")
	sb.WriteString(fmt.Sprintf("> Question: %s\n\n", question))
	for repo, finding := range repoFindings {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", repo, finding))
	}
	return sb.String()
}

func formatMessage(prTitle string, repos []repoWithURL) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🐱 *%s*\n\n", prTitle))
	sb.WriteString("Copycat dropped some PRs for you - don't leave them hanging! 👀\n\n")
	for _, r := range repos {
		if r.PRURL != "" {
			sb.WriteString(fmt.Sprintf("• <%s|%s>\n", r.PRURL, r.Repo))
		} else {
			sb.WriteString(fmt.Sprintf("• %s\n", r.Repo))
		}
	}
	sb.WriteString("\nReview, approve, merge - you know the drill 🚀")
	return sb.String()
}

func sendMessage(token, channel, text string) error {
	msg := slackMessage{
		Channel: channel,
		Text:    text,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequest("POST", slackAPIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var slackResp slackResponse
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	return nil
}
