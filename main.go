package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"copycat/internal/ai"
	"copycat/internal/cache"
	"copycat/internal/config"
	"copycat/internal/filesystem"
	"copycat/internal/git"
	"copycat/internal/input"
	"copycat/internal/slack"
)

const (
	reposDir         = "repos"
	projectCacheFile = ".projects.yaml"
)

func main() {
	filesystem.DeleteWorkspace()

	// Load combined configuration
	appConfig, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	// Interactive AI tool selection
	selectedTool, err := input.SelectAITool(&appConfig.AIToolsConfig)
	if err != nil {
		fmt.Println("AI tool selection cancelled. Exiting.")
		return
	}

	fmt.Printf("\n✓ Using AI tool: %s (%s)\n\n", selectedTool.Name, selectedTool.Command)

	projects, fromCache, err := loadProjectList(appConfig.GitHub)
	if err != nil {
		log.Fatal("Failed to load project list:", err)
	}

	if fromCache {
		fmt.Printf("\n✓ Loaded %d projects from cache (%s)\n", len(projects), projectCacheFile)
		fmt.Println("Press 'r' in the selector to refresh from GitHub.")
	}

	var selectedProjects []config.Project

	for {
		fmt.Println("Project Selector")
		fmt.Println("================")

		projectSelections, refreshRequested, syncRequested, err := input.SelectProjects(projects)
		if err != nil {
			log.Fatal("Project selection failed:", err)
		}

		if refreshRequested {
			fmt.Println("\nRefreshing project list from GitHub...")
			refreshedProjects, refreshErr := fetchAndCacheProjectList(appConfig.GitHub)
			if refreshErr != nil {
				log.Printf("Failed to refresh project list: %v", refreshErr)
				continue
			}
			projects = refreshedProjects
			continue
		}

		if syncRequested {
			fmt.Println("\nSyncing GitHub topics based on cached project metadata...")

			// Reload cache to pick up any external edits before syncing.
			cachedProjects, cacheErr := cache.LoadProjects(projectCacheFile)
			if cacheErr == nil && len(cachedProjects) > 0 {
				projects = cachedProjects
			} else if cacheErr != nil && !errors.Is(cacheErr, os.ErrNotExist) {
				log.Printf("Warning: failed to reload project cache: %v", cacheErr)
			}

			if err := git.SyncTopicsWithCache(projects, appConfig.GitHub); err != nil {
				log.Printf("Failed to sync topics: %v", err)
				continue
			}

			fmt.Println("✓ Topics synced. Fetching latest data from GitHub...")
			refreshedProjects, refreshErr := fetchAndCacheProjectList(appConfig.GitHub)
			if refreshErr != nil {
				log.Printf("Failed to refresh project list after sync: %v", refreshErr)
				continue
			}
			projects = refreshedProjects
			continue
		}

		if len(projectSelections) == 0 {
			fmt.Println("No projects selected. Exiting.")
			return
		}

		selectedProjects = projectSelections
		break
	}

	fmt.Println("\nSelected projects:")
	for _, project := range selectedProjects {
		fmt.Printf("- %s\n", project.Repo)
	}

	// Ask user to choose the workflow
	action, err := input.SelectOption("Choose an action", []string{"Create GitHub Issues", "Perform Changes Locally"})
	if err != nil {
		fmt.Println("Action selection cancelled. Exiting.")
		return
	}

	switch action {
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

func loadProjectList(githubCfg config.GitHubConfig) ([]config.Project, bool, error) {
	projects, err := cache.LoadProjects(projectCacheFile)
	if err == nil {
		return projects, true, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("No project cache found. Fetching project list from GitHub...")
	} else {
		log.Printf("Failed to load project cache %s: %v. Fetching from GitHub...", projectCacheFile, err)
	}

	projects, err = fetchAndCacheProjectList(githubCfg)
	if err != nil {
		return nil, false, err
	}

	return projects, false, nil
}

func fetchAndCacheProjectList(githubCfg config.GitHubConfig) ([]config.Project, error) {
	if githubCfg.AutoDiscoveryTopic != "" {
		fmt.Printf("\nFetching repositories from %s with topic '%s'...\n", githubCfg.Organization, githubCfg.AutoDiscoveryTopic)
	} else {
		fmt.Printf("\nFetching all repositories from %s...\n", githubCfg.Organization)
	}

	projects, err := git.FetchRepositories(githubCfg)
	if err != nil {
		return nil, err
	}

	if githubCfg.AutoDiscoveryTopic != "" {
		fmt.Printf("✓ Found %d unarchived repositories with topic '%s'\n", len(projects), githubCfg.AutoDiscoveryTopic)
	} else {
		fmt.Printf("✓ Found %d unarchived repositories\n", len(projects))
	}

	if err := cache.SaveProjects(projectCacheFile, projects); err != nil {
		log.Printf("Failed to update project cache: %v", err)
	} else {
		fmt.Printf("✓ Updated project cache at %s\n", projectCacheFile)
	}

	mergedProjects, err := cache.LoadProjects(projectCacheFile)
	if err != nil {
		return projects, nil
	}

	return mergedProjects, nil
}

func performChangesLocally(selectedProjects []config.Project, aiTool *config.AITool) {
	// ============================================================
	// STEP 1: Collect all user inputs BEFORE any cloning/changes
	// ============================================================

	// Check if any selected projects require a ticket
	hasProjectsRequiringTicket := false
	for _, project := range selectedProjects {
		if project.RequiresTicket {
			hasProjectsRequiringTicket = true
			break
		}
	}

	// Ask for Jira ticket when required
	var jiraTicket string
	if hasProjectsRequiringTicket {
		fmt.Println("\n⚠️  Note: Some selected projects require a Jira ticket in the PR title.")

		var err error
		jiraTicket, err = input.GetTextInput("Jira Ticket", "e.g., PROJ-123")
		if err != nil {
			fmt.Println("No Jira ticket provided. Exiting.")
			return
		}

		jiraTicket = strings.ToUpper(jiraTicket)
	}

	// Ask for PR title
	prTitle, err := input.GetTextInput("PR Title", "Enter a descriptive title for the pull request")
	if err != nil {
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

	// Track successful projects for notifications
	var successfulProjects []config.Project

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

		// Track successful project for notifications
		successfulProjects = append(successfulProjects, project)

		// Clean up the cloned repository
		cleanup()
	}

	fmt.Println("\nAll repositories have been processed.")

	// Send notifications for successful projects
	slack.SendNotifications(successfulProjects)

	// Final cleanup - remove the repos directory if it's empty
	filesystem.DeleteEmptyWorkspace()
}
