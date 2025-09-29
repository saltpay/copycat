package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
)

type Project struct {
	Repo string
}

func main() {
	// Clean up repos directory at startup
	reposDir := "repos"
	if _, err := os.Stat(reposDir); err == nil {
		fmt.Println("Cleaning up existing repos directory...")
		if err := os.RemoveAll(reposDir); err != nil {
			log.Printf("Warning: Failed to clean repos directory: %v", err)
		} else {
			fmt.Println("✓ Repos directory cleaned")
		}
	}

	projects := []Project{
		{Repo: "acceptance-aggregates-api"},
		{Repo: "acceptance-fee-service"},
		{Repo: "acceptance-fx-api"},
		{Repo: "acceptance-fraud-engine"},
		{Repo: "acceptance-otlp-collector"},
		{Repo: "acceptance-tap-onboarding"},
		{Repo: "acquiring-payments-api"},
		{Repo: "basket-data-service"},
		{Repo: "card-transaction-insights"},
		{Repo: "commshub-sender-service"},
		{Repo: "consent-orchestrator-gateway"},
		{Repo: "ecom-transaction-payments"},
		{Repo: "ecom-callback-gateway"},
		{Repo: "ecom-checkout-generator"},
		{Repo: "ecom-checkout-backend"},
		{Repo: "fake4-acquiring-host"},
		{Repo: "gmd-crm-sync"},
		{Repo: "iso-8583-proxy"},
		{Repo: "kafka-secure-proxy"},
		{Repo: "payments-refunds-wrapper"},
		{Repo: "payments-gateway-service"},
		{Repo: "pricing-engine-service"},
		{Repo: "pricing-app-backend"},
		{Repo: "salt-tokenization-service"},
		{Repo: "sample-backend-service"},
		{Repo: "teya-laime-helper"},
		{Repo: "transaction-block-manager"},
	}

	fmt.Println("Project Selector")
	fmt.Println("================")

	selectedProjects, err := selectProjects(projects)
	if err != nil {
		log.Fatal("Project selection failed:", err)
	}

	if len(selectedProjects) == 0 {
		fmt.Println("No projects selected. Exiting.")
		return
	}

	fmt.Println("\nSelected projects:")
	for _, project := range selectedProjects {
		fmt.Printf("- %s\n", project.Repo)
	}

	// Ask user to choose the workflow
	prompt := promptui.Select{
		Label: "Choose an action",
		Items: []string{"Create GitHub Issues", "Perform Changes Locally"},
	}

	_, result, err := prompt.Run()
	if err != nil {
		log.Fatal("Action selection failed:", err)
	}

	switch result {
	case "Create GitHub Issues":
		fmt.Println("\n⚠️  WARNING: The Copilot agent does not sign commits.")
		fmt.Println("You will need to fix unsigned commits before merging any pull request.")
		fmt.Println("")
		createGitHubIssues(selectedProjects)
	case "Perform Changes Locally":
		performChangesLocally(selectedProjects)
	}

	fmt.Println("\nDone!")
}

func selectProjects(projects []Project) ([]Project, error) {
	var selected []Project

	fmt.Println("\nAvailable projects:")
	for i, project := range projects {
		fmt.Printf("%d. %s\n", i+1, project.Repo)
	}

	prompt := promptui.Prompt{
		Label: "Enter project numbers separated by commas (e.g., 1,2) or 'all' for all projects",
	}

	input, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	input = strings.TrimSpace(input)

	if input == "" {
		return selected, nil
	}

	// Check if user wants to select all projects
	if strings.ToLower(input) == "all" {
		return projects, nil
	}

	indices := strings.Split(input, ",")
	for _, indexStr := range indices {
		indexStr = strings.TrimSpace(indexStr)
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 1 || index > len(projects) {
			fmt.Printf("Invalid selection: %s\n", indexStr)
			continue
		}

		project := projects[index-1]
		alreadySelected := false
		for _, sel := range selected {
			if sel.Repo == project.Repo {
				alreadySelected = true
				break
			}
		}

		if !alreadySelected {
			selected = append(selected, project)
		}
	}

	return selected, nil
}

func createGitHubIssues(selectedProjects []Project) {
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

func performChangesLocally(selectedProjects []Project) {
	fmt.Println("\nCloning selected repositories...")

	// Create repos directory if it doesn't exist
	reposDir := "repos"
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		log.Fatal("Failed to create repos directory:", err)
	}

	for _, project := range selectedProjects {
		fmt.Printf("\nCloning %s...\n", project.Repo)

		// Use SSH URL for cloning
		repoURL := fmt.Sprintf("git@github.com:saltpay/%s.git", project.Repo)
		targetPath := fmt.Sprintf("%s/%s", reposDir, project.Repo)

		// Check if repository already exists
		if _, err := os.Stat(targetPath); err == nil {
			fmt.Printf("✓ Repository %s already exists in %s\n", project.Repo, targetPath)
			continue
		}

		cmd := exec.Command("git", "clone", repoURL, targetPath)

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to clone %s: %v\nOutput: %s", project.Repo, err, string(output))
		} else {
			fmt.Printf("✓ Successfully cloned %s to %s\n", project.Repo, targetPath)
		}
	}

	fmt.Println("\nAll repositories cloned successfully.")

	// Generate branch name with timestamp
	timestamp := time.Now().Format("20060102-150405")
	branchName := fmt.Sprintf("copycat-%s", timestamp)
	fmt.Printf("\nUsing branch name: %s\n", branchName)

	// Ask for PR title only
	fmt.Println("\nPlease enter the PR title:")
	titlePrompt := promptui.Prompt{
		Label: "PR Title",
	}

	prTitle, err := titlePrompt.Run()
	if err != nil {
		log.Fatal("Failed to get PR title:", err)
	}

	if strings.TrimSpace(prTitle) == "" {
		fmt.Println("No PR title provided. Exiting.")
		return
	}

	// Ask for the Claude prompt
	fmt.Println("\nPlease enter the prompt for Claude CLI to execute on each repository:")
	promptInput := promptui.Prompt{
		Label: "Prompt",
	}

	claudePrompt, err := promptInput.Run()
	if err != nil {
		log.Fatal("Failed to get prompt:", err)
	}

	if strings.TrimSpace(claudePrompt) == "" {
		fmt.Println("No prompt provided. Exiting.")
		return
	}

	// Execute Claude CLI and create PRs on each repository
	fmt.Println("\nProcessing repositories...")
	for _, project := range selectedProjects {
		targetPath := fmt.Sprintf("%s/%s", reposDir, project.Repo)

		// Check if directory exists before running claude
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			fmt.Printf("⚠️  Skipping %s - directory not found\n", project.Repo)
			continue
		}

		fmt.Printf("\n════════════════════════════════════════\n")
		fmt.Printf("Processing %s\n", project.Repo)
		fmt.Printf("════════════════════════════════════════\n")

		// Create and checkout new branch
		fmt.Printf("Creating branch '%s'...\n", branchName)
		cmd := exec.Command("git", "checkout", "-b", branchName)
		cmd.Dir = targetPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Try to checkout existing branch if creation failed
			cmd = exec.Command("git", "checkout", branchName)
			cmd.Dir = targetPath
			output, err = cmd.CombinedOutput()
			if err != nil {
				log.Printf("Failed to create/checkout branch in %s: %v\nOutput: %s", project.Repo, err, string(output))
				continue
			}
		}

		// Run claude CLI in non-interactive mode to capture output
		fmt.Printf("Running Claude CLI to analyze and apply changes...\n")
		cmd = exec.Command("claude", "--print", claudePrompt)
		cmd.Dir = targetPath

		claudeOutput, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to run Claude CLI on %s: %v\nOutput: %s", project.Repo, err, string(claudeOutput))
			continue
		}

		// Use Claude's output as PR description
		prDescription := string(claudeOutput)
		if len(prDescription) > 2000 {
			// GitHub has a limit on PR description length, truncate if needed
			prDescription = prDescription[:1997] + "..."
		}

		fmt.Printf("Claude completed the changes.\n")

		// Show the proposed PR description and ask for confirmation
		fmt.Println("\n════════════════════════════════════════")
		fmt.Println("Proposed PR Description (from Claude):")
		fmt.Println("════════════════════════════════════════")
		fmt.Println(prDescription)
		fmt.Println("════════════════════════════════════════")

		// Ask user if they want to edit the description
		editPrompt := promptui.Select{
			Label: "Do you want to edit the PR description?",
			Items: []string{"Use as is", "Edit description"},
		}

		choice, _, err := editPrompt.Run()
		if err != nil {
			log.Printf("Failed to get user choice: %v", err)
			continue
		}

		switch choice {
		case 0: // Use as is
			// Continue with the current description
		case 1: // Edit description
			descPrompt := promptui.Prompt{
				Label:   "PR Description",
				Default: prDescription,
			}

			prDescription, err = descPrompt.Run()
			if err != nil {
				log.Printf("Failed to get PR description: %v", err)
				continue
			}
		}

		// Check if there are changes to commit
		cmd = exec.Command("git", "status", "--porcelain")
		cmd.Dir = targetPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to check git status in %s: %v", project.Repo, err)
			continue
		}

		if len(output) == 0 {
			fmt.Printf("No changes detected in %s, skipping PR creation\n", project.Repo)
			continue
		}

		// Add all changes
		fmt.Printf("Committing changes...\n")
		cmd = exec.Command("git", "add", "-A")
		cmd.Dir = targetPath
		_, err = cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to add changes in %s: %v", project.Repo, err)
			continue
		}

		// Commit changes
		commitMessage := fmt.Sprintf("%s\n\nGenerated by Copycat using Claude CLI", prTitle)
		cmd = exec.Command("git", "commit", "-m", commitMessage)
		cmd.Dir = targetPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to commit changes in %s: %v\nOutput: %s", project.Repo, err, string(output))
			continue
		}

		// Push branch
		fmt.Printf("Pushing branch to remote...\n")
		cmd = exec.Command("git", "push", "-u", "origin", branchName)
		cmd.Dir = targetPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to push branch in %s: %v\nOutput: %s", project.Repo, err, string(output))
			continue
		}

		// Create PR using GitHub CLI
		fmt.Printf("Creating pull request...\n")
		cmd = exec.Command("gh", "pr", "create",
			"--title", prTitle,
			"--body", prDescription,
			"--base", "main",
			"--head", branchName)
		cmd.Dir = targetPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to create PR for %s: %v\nOutput: %s", project.Repo, err, string(output))
			continue
		}

		fmt.Printf("✓ Successfully created PR for %s\n", project.Repo)
		fmt.Printf("PR URL: %s", string(output))

		// Clean up the cloned repository
		fmt.Printf("Cleaning up %s...\n", targetPath)
		if err := os.RemoveAll(targetPath); err != nil {
			log.Printf("Warning: Failed to remove %s: %v", targetPath, err)
		} else {
			fmt.Printf("✓ Cleaned up %s\n", targetPath)
		}
	}

	fmt.Println("\nAll repositories have been processed.")

	// Final cleanup - remove the repos directory if it's empty
	if err := os.Remove(reposDir); err == nil {
		fmt.Println("✓ Removed empty repos directory")
	}
}

func createGitHubIssueWithCLI(project Project, title string, description string) error {
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
