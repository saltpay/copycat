package git

import (
	"copycat/internal/config"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const copycatLabel = "copycat"

func ensureLabelExists(targetPath string) {
	// Try to create the label - if it already exists, gh will return an error which we ignore
	cmd := exec.Command("gh", "label", "create", copycatLabel,
		"--description", "Created by Copycat automation tool",
		"--color", "7C3AED")
	cmd.Dir = targetPath
	_ = cmd.Run() // Ignore error - label may already exist
}

func CreatePullRequest(project config.Project, targetPath string, branchName string, prTitle string, jiraTicket string, prDescription string) ([]byte, error) {
	// Ensure the copycat label exists before creating the PR
	ensureLabelExists(targetPath)

	// Get the default branch for this repository
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = targetPath
	defaultBranchOutput, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to get default branch for %s: %v, defaulting to 'main'", project.Repo, err)
		defaultBranchOutput = []byte("origin/main")
	}
	defaultBranch := strings.TrimPrefix(strings.TrimSpace(string(defaultBranchOutput)), "origin/")
	fmt.Printf("Using base branch: %s\n", defaultBranch)

	// Create PR using GitHub CLI
	fmt.Printf("Creating pull request...\n")

	// Determine final PR title when Jira ticket was provided
	// Ignore when the PR title already starts with the Jira ticket
	finalPRTitle := prTitle
	if jiraTicket != "" && !strings.HasPrefix(strings.ToUpper(prTitle), jiraTicket) {
		finalPRTitle = fmt.Sprintf("%s - %s", jiraTicket, prTitle)
	}

	cmd = exec.Command("gh", "pr", "create",
		"--title", finalPRTitle,
		"--body", prDescription,
		"--base", defaultBranch,
		"--head", branchName,
		"--label", copycatLabel)
	cmd.Dir = targetPath

	return cmd.CombinedOutput()
}
