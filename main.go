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

	"github.com/saltpay/copycat/internal/ai"
	"github.com/saltpay/copycat/internal/cmd"
	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/filesystem"
	"github.com/saltpay/copycat/internal/git"
	"github.com/saltpay/copycat/internal/input"
	"github.com/saltpay/copycat/internal/slack"
)

const (
	reposDir = "repos"
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

// appConfig holds the loaded configuration (used for saving after sync).
var appConfig *config.Config

// configPath holds the resolved path to the config file.
var configPath string

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
	PRURL   string
}

func main() {
	// Handle subcommands before flag parsing
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "edit":
			if err := cmd.RunEdit(); err != nil {
				log.Fatal(err)
			}
			return
		case "migrate":
			if err := cmd.RunMigrate(); err != nil {
				log.Fatal(err)
			}
			return
		case "reset":
			if err := cmd.RunReset(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

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

	// Display banner
	printBanner()

	// Get XDG config path
	var err error
	configPath, err = config.ConfigPath()
	if err != nil {
		log.Fatal("Failed to get config path:", err)
	}

	// Load combined configuration
	appConfig, err = config.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// First run - set up config interactively
			appConfig, err = handleFirstRun(configPath)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal("Failed to load configuration:", err)
		}
	}

	// Load projects from config, or fetch if empty
	projects := appConfig.Projects
	if len(projects) == 0 {
		fmt.Println("No projects in config. Fetching from GitHub...")
		projects, err = fetchAndSyncProjects(appConfig.GitHub)
		if err != nil {
			log.Fatal("Failed to fetch projects:", err)
		}
	} else {
		fmt.Printf("\n✓ Loaded %d projects from config (%s)\n", len(projects), configPath)
		fmt.Println("Press 'r' in the selector to sync from GitHub.")
	}

	// Warn about projects without slack rooms configured
	warnProjectsWithoutSlackRoom(projects)

	var selectedProjects []config.Project

	for {
		fmt.Println("Project Selector")
		fmt.Println("================")

		projectSelections, refreshRequested, err := input.SelectProjects(projects)
		if err != nil {
			log.Fatal("Project selection failed:", err)
		}

		if refreshRequested {
			fmt.Println("\nSyncing project list from GitHub...")
			refreshedProjects, refreshErr := fetchAndSyncProjects(appConfig.GitHub)
			if refreshErr != nil {
				log.Printf("Failed to sync project list: %v", refreshErr)
				continue
			}
			projects = refreshedProjects
			warnProjectsWithoutSlackRoom(projects)
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
		git.CreateGitHubIssues(appConfig.GitHub, selectedProjects)
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

func warnProjectsWithoutSlackRoom(projects []config.Project) {
	var missing []string
	for _, p := range projects {
		if strings.TrimSpace(p.SlackRoom) == "" {
			missing = append(missing, p.Repo)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("\n⚠️  Warning: %d project(s) have no slack_room configured:\n", len(missing))
		for _, repo := range missing {
			fmt.Printf("   - %s\n", repo)
		}
		fmt.Printf("Run 'copycat edit' to add slack_room values.\n\n")
	}
}

func printBanner() {
	banner := `
 /\_/\
( o.o ) COPYCAT
 > ^ <
`
	fmt.Println(banner)
}

func handleFirstRun(configPath string) (*config.Config, error) {
	fmt.Println("Welcome to Copycat!")
	fmt.Printf("Configuration now follows XDG structure: %s\n", configPath)
	fmt.Println()

	// Check for old local config files (only if XDG config doesn't exist)
	oldConfigExists := fileExists("config.yaml")
	oldProjectsExists := fileExists(".projects.yaml")

	if (oldConfigExists || oldProjectsExists) && !fileExists(configPath) {
		fmt.Println("Found existing local config files:")
		if oldConfigExists {
			fmt.Println("  - config.yaml")
		}
		if oldProjectsExists {
			fmt.Println("  - .projects.yaml")
		}
		fmt.Println()

		choice, err := input.SelectOption("How would you like to proceed?", []string{
			"Migrate existing config",
			"Start fresh",
		})
		if err != nil {
			return nil, fmt.Errorf("setup cancelled")
		}

		if choice == "Migrate existing config" {
			if err := cmd.RunMigrate(); err != nil {
				return nil, err
			}
			return config.Load(configPath)
		}
	}

	// Start fresh - prompt for org
	fmt.Println("Let's set up your configuration.")
	fmt.Println()

	org, err := input.GetTextInput("GitHub Organization", "e.g., my-org")
	if err != nil {
		return nil, fmt.Errorf("setup cancelled")
	}

	// Ensure config directory exists
	if err := config.EnsureConfigDir(); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create default config
	cfg := config.DefaultConfig(org)
	if err := cfg.Save(configPath); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✓ Configuration created at: %s\n", configPath)
	fmt.Println()

	return cfg, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fetchAndSyncProjects(githubCfg config.GitHubConfig) ([]config.Project, error) {
	if githubCfg.AutoDiscoveryTopic != "" {
		fmt.Printf("\nFetching repositories from %s with topic '%s'...\n", githubCfg.Organization, githubCfg.AutoDiscoveryTopic)
	} else {
		fmt.Printf("\nFetching all repositories from %s...\n", githubCfg.Organization)
	}

	fetchedProjects, err := git.FetchRepositories(githubCfg)
	if err != nil {
		return nil, err
	}

	if githubCfg.AutoDiscoveryTopic != "" {
		fmt.Printf("✓ Found %d unarchived repositories with topic '%s'\n", len(fetchedProjects), githubCfg.AutoDiscoveryTopic)
	} else {
		fmt.Printf("✓ Found %d unarchived repositories\n", len(fetchedProjects))
	}

	// Merge with existing projects to preserve manual edits (like slack_room)
	mergedProjects := mergeProjects(appConfig.Projects, fetchedProjects)

	// Update config and save
	appConfig.Projects = mergedProjects
	if err := appConfig.Save(configPath); err != nil {
		log.Printf("Failed to save config: %v", err)
	} else {
		fmt.Printf("✓ Updated config at %s\n", configPath)
	}

	return mergedProjects, nil
}

// mergeProjects merges fetched projects with existing ones, preserving manual edits.
func mergeProjects(existing, fetched []config.Project) []config.Project {
	// Build a map of existing projects by repo name
	existingMap := make(map[string]config.Project)
	for _, p := range existing {
		existingMap[p.Repo] = p
	}

	// Merge: use fetched data but preserve slack_room from existing
	merged := make([]config.Project, 0, len(fetched))
	for _, fp := range fetched {
		if ep, ok := existingMap[fp.Repo]; ok {
			// Preserve slack_room if it was set manually
			if fp.SlackRoom == "" && ep.SlackRoom != "" {
				fp.SlackRoom = ep.SlackRoom
			}
		}
		merged = append(merged, fp)
	}

	return merged
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

	prURL := strings.TrimSpace(string(output))
	safeLogger.Printf("%s✓ Successfully created PR for %s\n", logPrefix, project.Repo)
	safeLogger.Printf("%sPR URL: %s\n", logPrefix, prURL)

	// Clean up the cloned repository
	cleanup()

	return ProcessResult{Project: project, Success: true, Error: nil, PRURL: prURL}
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
	var prURLs map[string]string
	if parallelism == 1 {
		successfulProjects, prURLs = serialProcessing(jobs)
	} else {
		successfulProjects, prURLs = parallelProcessing(jobs, parallelism)
	}

	fmt.Println("\nAll repositories have been processed.")

	// Send notifications for successful projects
	slack.SendNotifications(successfulProjects, prTitle, prURLs)

	// Final cleanup - remove the repos directory if it's empty
	filesystem.DeleteEmptyWorkspace()
}

func serialProcessing(processJobs []ProcessJob) ([]config.Project, map[string]string) {
	// Sequential processing (maintain existing behavior)
	var successfulProjects []config.Project
	prURLs := make(map[string]string)

	for _, job := range processJobs {
		result := processProject(job)
		if result.Success {
			successfulProjects = append(successfulProjects, result.Project)
			prURLs[result.Project.Repo] = result.PRURL
		}
	}

	return successfulProjects, prURLs
}

func parallelProcessing(processJobs []ProcessJob, parallelism int) ([]config.Project, map[string]string) {
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
	prURLs := make(map[string]string)
	var mu sync.Mutex

	for result := range results {
		mu.Lock()
		if result.Success {
			successfulProjects = append(successfulProjects, result.Project)
			prURLs[result.Project.Repo] = result.PRURL
		}
		mu.Unlock()
	}

	return successfulProjects, prURLs
}
