package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"gopkg.in/yaml.v3"
)

type Project struct {
	Repo      string `yaml:"repo"`
	SlackRoom string `yaml:"slack_room"`
	InCDE     bool   `yaml:"in_cde"`
}

type ProjectConfig struct {
	Projects []Project `yaml:"projects"`
}

func loadProjects(filename string) ([]Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	var config ProjectConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return config.Projects, nil
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

	projects, err := loadProjects("projects.yaml")
	if err != nil {
		log.Fatal("Failed to load projects:", err)
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
	// ============================================================
	// STEP 1: Collect all user inputs BEFORE any cloning/changes
	// ============================================================

	// Check if any selected projects are in CDE
	hasCDEProjects := false
	for _, project := range selectedProjects {
		if project.InCDE {
			hasCDEProjects = true
			break
		}
	}

	// Ask for Jira ticket if there are CDE projects
	var jiraTicket string
	if hasCDEProjects {
		fmt.Println("\n⚠️  Note: Some selected projects are in CDE and require a Jira ticket in the PR title.")
		fmt.Println("Please enter the Jira ticket (e.g., PROJ-123):")
		jiraPrompt := promptui.Prompt{
			Label: "Jira Ticket",
		}

		var err error
		jiraTicket, err = jiraPrompt.Run()
		if err != nil {
			log.Fatal("Failed to get Jira ticket:", err)
		}

		jiraTicket = strings.TrimSpace(jiraTicket)
		if jiraTicket == "" {
			fmt.Println("No Jira ticket provided. Exiting.")
			return
		}
	}

	// Ask for PR title
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
	fmt.Println("Choose input method:")
	fmt.Println("1. Type/paste single line (press Enter when done)")
	fmt.Println("2. Open editor for multi-line input")

	methodPrompt := promptui.Select{
		Label: "Input method",
		Items: []string{"Single line", "Editor"},
	}

	_, inputMethod, err := methodPrompt.Run()
	if err != nil {
		log.Fatal("Failed to select input method:", err)
	}

	var claudePrompt string

	if inputMethod == "Single line" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Prompt: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Failed to read prompt:", err)
		}
		claudePrompt = strings.TrimSpace(line)
	} else {
		// Create a temporary file for the prompt
		tmpFile, err := os.CreateTemp("", "copycat-prompt-*.txt")
		if err != nil {
			log.Fatal("Failed to create temp file:", err)
		}
		tmpFilePath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpFilePath)

		// Get the editor from environment or use vim as default
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim"
		}

		// Open the editor
		cmd := exec.Command(editor, tmpFilePath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			log.Fatal("Failed to run editor:", err)
		}

		// Read the content from the temp file
		content, err := os.ReadFile(tmpFilePath)
		if err != nil {
			log.Fatal("Failed to read prompt from temp file:", err)
		}

		claudePrompt = strings.TrimSpace(string(content))
	}

	if claudePrompt == "" {
		fmt.Println("No prompt provided. Exiting.")
		return
	}

	// ============================================================
	// STEP 2: Now proceed with cloning and making changes
	// ============================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("All inputs collected! Starting repository processing...")
	fmt.Println(strings.Repeat("=", 60))

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
		cmd = exec.Command("claude", "--permission-mode", "acceptEdits", claudePrompt)
		cmd.Dir = targetPath

		claudeOutput, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to run Claude CLI on %s: %v\nOutput: %s", project.Repo, err, string(claudeOutput))
			continue
		}

		fmt.Printf("Claude completed the changes.\n")

		// Generate PR description using Claude
		fmt.Printf("Generating PR description...\n")
		summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", string(claudeOutput))

		cmd = exec.Command("claude", "--permission-mode", "acceptEdits", summaryPrompt)
		cmd.Dir = targetPath

		summaryOutput, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to generate PR description for %s: %v\nOutput: %s", project.Repo, err, string(summaryOutput))
			continue
		}

		prDescription := string(summaryOutput)
		if len(prDescription) > 2000 {
			prDescription = prDescription[:1997] + "..."
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
		commitMessage := prTitle
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

		// Get the default branch for this repository
		cmd = exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
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

		// Determine final PR title based on whether project is in CDE
		finalPRTitle := prTitle
		if project.InCDE && jiraTicket != "" {
			finalPRTitle = fmt.Sprintf("%s - %s", jiraTicket, prTitle)
		}

		cmd = exec.Command("gh", "pr", "create",
			"--title", finalPRTitle,
			"--body", prDescription,
			"--base", defaultBranch,
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
