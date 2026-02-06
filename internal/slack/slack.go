package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/input"
	"net/http"
	"os"
	"strings"
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

// SendNotifications sends notifications for successful projects, grouped by Slack room
func SendNotifications(successfulProjects []config.Project, prTitle string, prURLs map[string]string) {
	if len(successfulProjects) == 0 {
		return
	}

	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return // Silently skip if no token configured
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
		fmt.Println("\n⚠️  No Slack rooms configured for successful projects, skipping notifications")
		return
	}

	// Show which channels will receive notifications
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Slack Notifications")
	fmt.Println(strings.Repeat("=", 60))
	for channel, repos := range projectsByRoom {
		repoNames := make([]string, len(repos))
		for i, r := range repos {
			repoNames[i] = r.Repo
		}
		fmt.Printf("  %s: %s\n", channel, strings.Join(repoNames, ", "))
	}

	// Ask for confirmation
	confirm, err := input.SelectOption("Send Slack notifications?", []string{"Yes", "No"})
	if err != nil || confirm == "No" {
		fmt.Println("Skipping Slack notifications")
		return
	}

	fmt.Println("\nSending Slack notifications...")

	for channel, repos := range projectsByRoom {
		message := formatMessage(prTitle, repos)
		err := sendMessage(token, channel, message)
		repoNames := make([]string, len(repos))
		for i, r := range repos {
			repoNames[i] = r.Repo
		}
		if err != nil {
			fmt.Printf("⚠️  Failed to send notification to %s: %v\n", channel, err)
		} else {
			fmt.Printf("✓ Notification sent to %s for: %s\n", channel, strings.Join(repoNames, ", "))
		}
	}
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
