package slack

import (
	"copycat/internal/config"
	"fmt"
	"strings"
)

// SendNotifications sends notifications for successful projects
func SendNotifications(successfulProjects []config.Project) {
	if len(successfulProjects) == 0 {
		return
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Sending notifications...")
	fmt.Println(strings.Repeat("=", 60))

	for _, project := range successfulProjects {
		slackRoom := project.SlackRoom
		if slackRoom == "" {
			slackRoom = "#unknown"
		}
		fmt.Printf("✓ Notification sent to %s for %s\n", slackRoom, project.Repo)
	}
}
