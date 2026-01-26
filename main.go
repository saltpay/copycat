package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

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

// SafeLogger provides thread-safe logging for parallel operations
type SafeLogger struct {
	mu sync.Mutex
}

// Printf prints formatted output in a thread-safe manner
func (sl *SafeLogger) Printf(format string, args ...interface{}) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	fmt.Printf(format, args...)
}

// Println prints a line in a thread-safe manner
func (sl *SafeLogger) Println(args ...interface{}) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	fmt.Println(args...)
}

// LogError logs an error in a thread-safe manner
func (sl *SafeLogger) LogError(format string, args ...interface{}) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	log.Printf(format, args...)
}

var safeLogger = &SafeLogger{}

// ProcessJob represents a single project processing job
type ProcessJob struct {
	Project         config.Project
	AITool          *config.AITool
	AppConfig       config.Config
	JiraTicket      string
	PRTitle         string
	VibeCodePrompt  string
	BranchStrategy  string
	SpecifiedBranch string
}

// ProcessResult represents the result of processing a single project
type ProcessResult struct {
	Project config.Project
	Success bool
	Error   error
}

func main() {
	// Parse command-line flags
	parallelism := flag.Int("parallel", 3, "number of repositories to process in parallel (default: 3)")
	flag.Parse()

	// Validate parallelism value
	if *parallelism < 1 {
		log.Fatal("Parallelism must be at least 1")
	}
	if *parallelism > 10 {
		fmt.Printf("⚠️  Warning: High parallelism (%d) may cause API rate limiting issues\n", *parallelism)
	}

	filesystem.DeleteWorkspace()

	// Load combined configuration
	appConfig, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

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
	action, err := input.SelectOption("Choose an action", []string{
		"Perform Changes Locally",
		"Create GitHub Issues (⚠️ Copilot does not sign commits)",
	})
	if err != nil {
		fmt.Println("Action selection cancelled. Exiting.")
		return
	}

	switch {
	case strings.HasPrefix(action, "Create GitHub Issues"):
		git.CreateGitHubIssues(selectedProjects)
	case strings.HasPrefix(action, "Perform Changes Locally"):
		// Interactive AI tool selection (only needed for local changes)
		selectedTool, err := input.SelectAITool(&appConfig.AIToolsConfig)
		if err != nil {
			fmt.Println("AI tool selection cancelled. Exiting.")
			return
		}

		fmt.Printf("\n✓ Using AI tool: %s (%s)\n\n", selectedTool.Name, selectedTool.Command)

		// Branch strategy selection
		branchStrategy, err := input.SelectOption("Branch strategy?", []string{
			"Always create new branches",
			"Specify branch name (reuse if exists)",
		})
		if err != nil {
			fmt.Println("Branch strategy selection cancelled. Exiting.")
			return
		}

		var specifiedBranch string
		if strings.HasPrefix(branchStrategy, "Specify branch name") {
			specifiedBranch, err = input.GetTextInput("Branch name", "Enter the branch name to use/create across all repos")
			if err != nil || specifiedBranch == "" {
				fmt.Println("No branch name provided. Exiting.")
				return
			}
			fmt.Printf("\n✓ Branch name: %s\n", specifiedBranch)
		} else {
			fmt.Printf("\n✓ Branch strategy: %s\n", branchStrategy)
		}

		performChangesLocally(selectedProjects, selectedTool, *appConfig, *parallelism, branchStrategy, specifiedBranch)
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

// processProject handles the processing of a single project
func processProject(job ProcessJob) ProcessResult {
	project := job.Project
	targetPath := fmt.Sprintf("%s/%s", reposDir, project.Repo)

	// Add project name prefix for parallel logging
	logPrefix := fmt.Sprintf("[%s] ", project.Repo)

	safeLogger.Printf("\n%s════════════════════════════════════════\n", logPrefix)
	safeLogger.Printf("%sProcessing %s\n", logPrefix, project.Repo)
	safeLogger.Printf("%s════════════════════════════════════════\n", logPrefix)

	// Helper function to clean-up before continuing
	cleanup := func() {
		filesystem.DeleteDirectory(targetPath)
	}

	// Clone the repository if it doesn't exist
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		safeLogger.Printf("\n%sCloning %s...\n", logPrefix, project.Repo)

		// Use SSH URL for cloning with the configured organization
		repoURL := fmt.Sprintf("git@github.com:%s/%s.git", job.AppConfig.GitHub.Organization, project.Repo)

		cmd := exec.Command("git", "clone", repoURL, targetPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			safeLogger.LogError("%sFailed to clone %s: %v\nOutput: %s", logPrefix, project.Repo, err, string(output))
			return ProcessResult{Project: project, Success: false, Error: err}
		}
		safeLogger.Printf("%s✓ Successfully cloned %s\n", logPrefix, project.Repo)
	} else {
		safeLogger.Printf("%s✓ Repository %s already exists, using existing clone\n", logPrefix, project.Repo)
	}

	// Select or create branch based on strategy
	branchName, err := git.SelectOrCreateBranch(targetPath, job.PRTitle, job.BranchStrategy, job.SpecifiedBranch)
	if err != nil {
		safeLogger.LogError("%sFailed to select/create branch in %s: %v", logPrefix, project.Repo, err)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	safeLogger.Printf("%sUsing branch: %s\n", logPrefix, branchName)

	aiOutput, err := ai.VibeCode(job.AITool, job.VibeCodePrompt, targetPath)
	if err != nil {
		safeLogger.LogError("%sFailed to run AI tool on %s: %v\nOutput: %s", logPrefix, project.Repo, err, aiOutput)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	safeLogger.Printf("%s%s completed the changes.\n", logPrefix, job.AITool.Name)

	prDescription, err := ai.GeneratePRDescription(job.AITool, project, aiOutput, targetPath)
	if err != nil {
		safeLogger.LogError("%sFailed to generate PR description for %s: %v\nOutput: %s", logPrefix, project.Repo, err, prDescription)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	// Check if there are changes to commit
	output, err := git.CheckLocalChanges(targetPath)
	if err != nil {
		safeLogger.LogError("%sFailed to check git status in %s: %v", logPrefix, project.Repo, err)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	if len(output) == 0 {
		safeLogger.Printf("%sNo changes detected in %s, skipping PR creation\n", logPrefix, project.Repo)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: fmt.Errorf("no changes detected")}
	}

	err = git.PushChanges(project, targetPath, branchName, job.PRTitle)
	if err != nil {
		safeLogger.LogError("%sFailed to push changes in %s: %v", logPrefix, project.Repo, err)
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	output, err = git.CreatePullRequest(project, targetPath, branchName, job.PRTitle, job.JiraTicket, prDescription)

	if err != nil {
		safeLogger.LogError("%sFailed to create PR for %s: %v\nOutput: %s", logPrefix, project.Repo, err, string(output))
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	safeLogger.Printf("%s✓ Successfully created PR for %s\n", logPrefix, project.Repo)
	safeLogger.Printf("%sPR URL: %s", logPrefix, string(output))

	// Clean up the cloned repository
	cleanup()

	return ProcessResult{Project: project, Success: true, Error: nil}
}

func performChangesLocally(selectedProjects []config.Project, aiTool *config.AITool, appConfig config.Config, parallelism int, branchStrategy string, specifiedBranch string) {
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
	fmt.Printf("All inputs collected! Starting repository processing with parallelism=%d...\n", parallelism)
	fmt.Println(strings.Repeat("=", 60))

	filesystem.CreateWorkspace()

	var jobs []ProcessJob
	for _, project := range selectedProjects {
		jobs = append(jobs, ProcessJob{
			Project:         project,
			AITool:          aiTool,
			AppConfig:       appConfig,
			JiraTicket:      jiraTicket,
			PRTitle:         prTitle,
			VibeCodePrompt:  vibeCodePrompt,
			BranchStrategy:  branchStrategy,
			SpecifiedBranch: specifiedBranch,
		})
	}

	// Check if we should run sequentially (parallelism = 1)
	var successfulProjects []config.Project
	if parallelism == 1 {
		successfulProjects = serialProcessing(jobs)
	} else {
		successfulProjects = parallelProcessing(jobs, parallelism)
	}

	fmt.Println("\nAll repositories have been processed.")

	// Send notifications for successful projects
	slack.SendNotifications(successfulProjects, prTitle)

	// Final cleanup - remove the repos directory if it's empty
	filesystem.DeleteEmptyWorkspace()
}

func serialProcessing(processJobs []ProcessJob) []config.Project {
	// Sequential processing (maintain existing behavior)
	var successfulProjects []config.Project

	for _, job := range processJobs {
		result := processProject(job)
		if result.Success {
			successfulProjects = append(successfulProjects, result.Project)
		}
	}

	return successfulProjects
}

func parallelProcessing(processJobs []ProcessJob, parallelism int) []config.Project {
	// Parallel processing with worker pool
	numWorkers := parallelism
	if numWorkers > len(processJobs) {
		numWorkers = len(processJobs)
	}

	// Create channels for job distribution and result collection
	jobs := make(chan ProcessJob, len(processJobs))
	results := make(chan ProcessResult, len(processJobs))

	// Start worker goroutines
	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				safeLogger.Printf("[Worker %d] Starting %s\n", workerID, job.Project.Repo)
				result := processProject(job)
				results <- result
				if result.Success {
					safeLogger.Printf("[Worker %d] ✓ Completed %s successfully\n", workerID, job.Project.Repo)
				} else {
					safeLogger.Printf("[Worker %d] ⚠ Failed to process %s: %v\n", workerID, job.Project.Repo, result.Error)
				}
			}
		}(w)
	}

	// Queue all jobs
	for _, job := range processJobs {
		jobs <- job
	}
	close(jobs)

	// Start a goroutine to collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var successfulProjects []config.Project
	var mu sync.Mutex

	for result := range results {
		mu.Lock()
		if result.Success {
			successfulProjects = append(successfulProjects, result.Project)
		}
		mu.Unlock()
	}

	return successfulProjects
}
