package git

import (
	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/input"
	"fmt"
	"log"
	"os/exec"
)

func CreateGitHubIssues(githubCfg config.GitHubConfig, selectedProjects []config.Project) {
	issueTitle, err := input.GetTextInput("Issue Title", "Enter the title for the GitHub issue")
	if err != nil {
		fmt.Println("No title provided. Exiting.")
		return
	}

	issueDescription, err := input.GetTextInput("Issue Description", "Enter the description for the GitHub issue")
	if err != nil {
		fmt.Println("No description provided. Exiting.")
		return
	}

	fmt.Println("\nCreating GitHub issues using GitHub CLI...")
	fmt.Println("Please make sure you are authenticated with 'gh auth login' if needed.")

	for _, project := range selectedProjects {
		fmt.Printf("\nCreating issue for %s...\n", project.Repo)
		err := createGitHubIssueWithCLI(githubCfg, project, issueTitle, issueDescription)
		if err != nil {
			log.Printf("Failed to create issue for %s: %v", project.Repo, err)
		} else {
			fmt.Printf("✓ Successfully created issue for %s\n", project.Repo)
		}
	}
}

func createGitHubIssueWithCLI(githubCfg config.GitHubConfig, project config.Project, title string, description string) error {
	// Use GitHub CLI to create the issue
	cmd := exec.Command("gh", "issue", "create",
		"--repo", fmt.Sprintf("%s/%s", githubCfg.Organization, project.Repo),
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
