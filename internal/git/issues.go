package git

import (
	"copycat/internal/config"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"
)

func CreateGitHubIssues(selectedProjects []config.Project) {
	fmt.Println("\nPlease enter the issue title:")
	titlePrompt := promptui.Prompt{
		Label: "Title",
	}

	issueTitle, err := titlePrompt.Run()
	if err != nil {
		log.Fatal("Failed to get title:", err)
	}

	if strings.TrimSpace(issueTitle) == "" {
		fmt.Println("No title provided. Exiting.")
		return
	}

	fmt.Println("\nPlease enter the issue description:")
	descriptionPrompt := promptui.Prompt{
		Label: "Description",
	}

	issueDescription, err := descriptionPrompt.Run()
	if err != nil {
		log.Fatal("Failed to get description:", err)
	}

	if strings.TrimSpace(issueDescription) == "" {
		fmt.Println("No description provided. Exiting.")
		return
	}

	fmt.Println("\nCreating GitHub issues using GitHub CLI...")
	fmt.Println("Please make sure you are authenticated with 'gh auth login' if needed.")

	for _, project := range selectedProjects {
		fmt.Printf("\nCreating issue for %s...\n", project.Repo)
		err := createGitHubIssueWithCLI(project, issueTitle, issueDescription)
		if err != nil {
			log.Printf("Failed to create issue for %s: %v", project.Repo, err)
		} else {
			fmt.Printf("✓ Successfully created issue for %s\n", project.Repo)
		}
	}
}

func createGitHubIssueWithCLI(project config.Project, title string, description string) error {
	// Use GitHub CLI to create the issue
	cmd := exec.Command("gh", "issue", "create",
		"--repo", fmt.Sprintf("saltpay/%s", project.Repo),
		"--title", title,
		"--body", description,
		"--assignee", "@copilot")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create issue: %v\nOutput: %s", err, string(output))
	}

	// The gh command outputs the URL of the created issue
	fmt.Printf("Created issue: %s", string(output))
	return nil
}
