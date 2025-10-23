package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"copycat/internal/ai"
	"copycat/internal/config"
	"copycat/internal/filesystem"
	"copycat/internal/git"
	"copycat/internal/input"

	"github.com/manifoldco/promptui"
)

const reposDir = "repos"

func main() {
	aiToolFlag := flag.String("ai-tool", "", "Name of the AI tool to use (configured in ai-tools.yaml)")
	flag.Parse()

	aiToolsConfig, err := config.LoadAITools("ai-tools.yaml")
	if err != nil {
		log.Fatal("Failed to load AI tools:", err)
	}

	selectedToolName := *aiToolFlag
	if selectedToolName == "" {
		selectedToolName = aiToolsConfig.Default
	}

	selectedTool, ok := aiToolsConfig.ToolByName(selectedToolName)
	if !ok {
		log.Fatalf("AI tool %q not found in ai-tools.yaml", selectedToolName)
	}

	fmt.Printf("Using AI tool: %s (%s)\n", selectedTool.Name, selectedTool.Command)

	filesystem.DeleteWorkspace()

	projects, err := config.LoadProjects("projects.yaml")
	if err != nil {
		log.Fatal("Failed to load projects:", err)
	}

	fmt.Println("Project Selector")
	fmt.Println("================")

	selectedProjects, err := input.SelectProjects(projects)
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
		git.CreateGitHubIssues(selectedProjects)
	case "Perform Changes Locally":
		performChangesLocally(selectedProjects, selectedTool)
	}

	fmt.Println("\nDone!")
}

func performChangesLocally(selectedProjects []config.Project, aiTool *config.AITool) {
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
		jiraTicket = strings.ToUpper(jiraTicket)
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

	vibeCodePrompt := input.ReadAIPrompt(aiTool)
	if vibeCodePrompt == "" {
		fmt.Println("No prompt provided. Exiting.")
		return
	}

	// ============================================================
	// STEP 2: Now proceed with processing each repository
	// ============================================================

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("All inputs collected! Starting repository processing...")
	fmt.Println(strings.Repeat("=", 60))

	filesystem.CreateWorkspace()

	// Process each repository: clone → apply changes → commit → PR
	for _, project := range selectedProjects {
		targetPath := fmt.Sprintf("%s/%s", reposDir, project.Repo)

		fmt.Printf("\n════════════════════════════════════════\n")
		fmt.Printf("Processing %s\n", project.Repo)
		fmt.Printf("════════════════════════════════════════\n")

		// Helper function to clean-up before continuing
		cleanup := func() {
			filesystem.DeleteDirectory(targetPath)
		}

		// Clone the repository if it doesn't exist
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			fmt.Printf("\nCloning %s...\n", project.Repo)

			// Use SSH URL for cloning
			repoURL := fmt.Sprintf("git@github.com:saltpay/%s.git", project.Repo)

			cmd := exec.Command("git", "clone", repoURL, targetPath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Failed to clone %s: %v\nOutput: %s", project.Repo, err, string(output))
				continue
			}
			fmt.Printf("✓ Successfully cloned %s\n", project.Repo)
		} else {
			fmt.Printf("✓ Repository %s already exists, using existing clone\n", project.Repo)
		}

		// Check for existing copycat branches
		branchName, err := git.SelectOrCreateBranch(targetPath, prTitle)
		if err != nil {
			log.Printf("Failed to select/create branch in %s: %v", project.Repo, err)
			cleanup()
			continue
		}

		fmt.Printf("Using branch: %s\n", branchName)

		aiOutput, err := ai.VibeCode(aiTool, vibeCodePrompt, targetPath)
		if err != nil {
			log.Printf("Failed to run AI tool on %s: %v\nOutput: %s", project.Repo, err, aiOutput)
			cleanup()
			continue
		}

		fmt.Printf("%s completed the changes.\n", aiTool.Name)

		prDescription, err := ai.GeneratePRDescription(aiTool, project, aiOutput, targetPath)
		if err != nil {
			log.Printf("Failed to generate PR description for %s: %v\nOutput: %s", project.Repo, err, prDescription)
			cleanup()
			continue
		}

		// Check if there are changes to commit
		output, err := git.CheckLocalChanges(targetPath)
		if err != nil {
			log.Printf("Failed to check git status in %s: %v", project.Repo, err)
			cleanup()
			continue
		}

		if len(output) == 0 {
			fmt.Printf("No changes detected in %s, skipping PR creation\n", project.Repo)
			cleanup()
			continue
		}

		err = git.PushChanges(project, targetPath, branchName, prTitle)
		if err != nil {
			log.Printf("Failed to push changes in %s: %v", project.Repo, err)
			cleanup()
			continue
		}

		output, err = git.CreatePullRequest(project, targetPath, branchName, prTitle, jiraTicket, prDescription)

		if err != nil {
			log.Printf("Failed to create PR for %s: %v\nOutput: %s", project.Repo, err, string(output))
			cleanup()
			continue
		}

		fmt.Printf("✓ Successfully created PR for %s\n", project.Repo)
		fmt.Printf("PR URL: %s", string(output))

		// Clean up the cloned repository
		cleanup()
	}

	fmt.Println("\nAll repositories have been processed.")

	// Final cleanup - remove the repos directory if it's empty
	filesystem.DeleteEmptyWorkspace()
}
