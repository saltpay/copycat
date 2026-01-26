package slack

import (
	"bytes"
	"copycat/internal/config"
	"copycat/internal/input"
	"encoding/json"
	"fmt"
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

// SendNotifications sends notifications for successful projects, grouped by Slack room
func SendNotifications(successfulProjects []config.Project, prTitle string) {
	if len(successfulProjects) == 0 {
		return
	}

	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return // Silently skip if no token configured
	}

	// Group projects by Slack room
	projectsByRoom := make(map[string][]string)
	for _, project := range successfulProjects {
		slackRoom := strings.TrimSpace(project.SlackRoom)
		if slackRoom == "" {
			continue // Skip projects without a Slack room
		}
		projectsByRoom[slackRoom] = append(projectsByRoom[slackRoom], project.Repo)
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
		fmt.Printf("  %s: %s\n", channel, strings.Join(repos, ", "))
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
		if err != nil {
			fmt.Printf("⚠️  Failed to send notification to %s: %v\n", channel, err)
		} else {
			fmt.Printf("✓ Notification sent to %s for: %s\n", channel, strings.Join(repos, ", "))
		}
	}
}

func formatMessage(prTitle string, repos []string) string {
	repoList := strings.Join(repos, ", ")
	return fmt.Sprintf("🐱 *Copycat* created PRs for: %s\n>%s", repoList, prTitle)
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
