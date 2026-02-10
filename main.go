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
	"github.com/saltpay/copycat/internal/permission"
	"github.com/saltpay/copycat/internal/slack"
)

const (
	reposDir = "repos"
)

// appConfig holds the loaded configuration (used for saving after sync).
var appConfig *config.Config

// configPath holds the resolved path to the config file.
var configPath string

// projectsPath holds the resolved path to the projects file.
var projectsPath string

// ProcessJob represents a single project processing job
type ProcessJob struct {
	Project         config.Project
	AITool          *config.AITool
	AppConfig       config.Config
	PRTitle         string
	VibeCodePrompt  string
	BranchStrategy  string
	SpecifiedBranch string
	MCPConfigPath   string
	UpdateStatus    func(status string)
}

// ProcessResult represents the result of processing a single project
type ProcessResult struct {
	Project config.Project
	Success bool
	Skipped bool
	Error   error
	PRURL   string
}

func main() {
	// Handle subcommands before flag parsing
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "edit":
			if len(os.Args) < 3 {
				log.Fatal("Usage: copycat edit <config|projects>")
			}
			if err := cmd.RunEdit(os.Args[2]); err != nil {
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
		case "permission-handler":
			if err := permission.RunMCPHandler(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	// Parse command-line flags
	parallelism := flag.Int("parallel", 0, "number of repositories to process in parallel (overrides config.yaml)")
	flag.Parse()

	filesystem.DeleteWorkspace()

	// Get XDG config and projects paths
	var err error
	configPath, err = config.ConfigPath()
	if err != nil {
		log.Fatal("Failed to get config path:", err)
	}
	projectsPath, err = config.ProjectsPath()
	if err != nil {
		log.Fatal("Failed to get projects path:", err)
	}

	// Load configuration
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

	// Load projects from separate file, or fetch if empty/missing
	projects, projectsErr := config.LoadProjects(projectsPath)
	if projectsErr != nil || len(projects) == 0 {
		fmt.Println("No projects found. Fetching from GitHub...")
		projects, err = fetchAndSyncProjects(appConfig.GitHub)
		if err != nil {
			log.Fatal("Failed to fetch projects:", err)
		}
	}

	// CLI flag overrides config value
	if *parallelism > 0 {
		if *parallelism > 10 {
			*parallelism = 10
		}
		appConfig.Parallelism = *parallelism
	}
	par := appConfig.Parallelism

	dashCfg := input.DashboardConfig{
		Projects:      projects,
		AIToolsConfig: &appConfig.AIToolsConfig,
		GitHubConfig:  appConfig.GitHub,
		AppConfig:     *appConfig,
		Parallelism:   par,
		FetchProjects: func() ([]config.Project, error) {
			return fetchAndSyncProjects(appConfig.GitHub)
		},
		ProcessRepos: func(sender *input.StatusSender, selectedProjects []config.Project, setup *input.WizardResult) {
			processReposWithSender(sender, selectedProjects, setup, *appConfig, par, projects)
		},
		CreateIssues: git.CreateGitHubIssuesWithSender,
	}

	result, err := input.RunDashboard(dashCfg)
	if err != nil {
		log.Fatal("Dashboard error:", err)
	}

	if result == nil {
		fmt.Println("Cancelled.")
		return
	}

	// Post-processing: workspace management
	if result.Action == "local" {
		filesystem.DeleteEmptyWorkspace()
	}

	fmt.Println("\nDone!")
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

	// Load existing projects to preserve manual edits (like slack_room)
	existingProjects, _ := config.LoadProjects(projectsPath)

	// Merge with existing projects
	mergedProjects := mergeProjects(existingProjects, fetchedProjects)

	// Save projects to separate file
	if err := config.SaveProjects(projectsPath, mergedProjects); err != nil {
		log.Printf("Failed to save projects: %v", err)
	} else {
		fmt.Printf("✓ Updated projects at %s\n", projectsPath)
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

	cleanup := func() {
		filesystem.DeleteDirectory(targetPath)
	}

	// Clone the repository if it doesn't exist
	job.UpdateStatus("Cloning...")
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		repoURL := fmt.Sprintf("git@github.com:%s/%s.git", job.AppConfig.GitHub.Organization, project.Repo)
		cmd := exec.Command("git", "clone", repoURL, targetPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			cleanup()
			return ProcessResult{Project: project, Success: false, Error: fmt.Errorf("clone failed: %v (%s)", err, string(output))}
		}
	}

	// Select or create branch based on strategy
	job.UpdateStatus("Creating branch...")
	branchName, err := git.SelectOrCreateBranch(targetPath, job.PRTitle, job.BranchStrategy, job.SpecifiedBranch)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	// Run AI tool
	job.UpdateStatus("Running AI agent...")
	aiOutput, err := ai.VibeCode(job.AITool, job.VibeCodePrompt, targetPath, job.MCPConfigPath, project.Repo)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: fmt.Errorf("AI tool failed: %v", err)}
	}

	// Generate PR description
	job.UpdateStatus("Generating PR description...")
	prDescription, err := ai.GeneratePRDescription(job.AITool, project, aiOutput, targetPath)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	// Check if there are changes to commit
	job.UpdateStatus("Checking for changes...")
	output, err := git.CheckLocalChanges(targetPath)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}
	if len(output) == 0 {
		cleanup()
		return ProcessResult{Project: project, Skipped: true, Error: fmt.Errorf("no changes detected")}
	}

	// Push changes
	job.UpdateStatus("Pushing changes...")
	err = git.PushChanges(project, targetPath, branchName, job.PRTitle)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: err}
	}

	// Create pull request
	job.UpdateStatus("Creating PR...")
	prOutput, err := git.CreatePullRequest(project, targetPath, branchName, job.PRTitle, prDescription)
	if err != nil {
		cleanup()
		return ProcessResult{Project: project, Success: false, Error: fmt.Errorf("PR creation failed: %v (%s)", err, string(prOutput))}
	}

	prURL := strings.TrimSpace(string(prOutput))

	// Clean up
	job.UpdateStatus("Cleaning up...")
	cleanup()

	return ProcessResult{Project: project, Success: true, Error: nil, PRURL: prURL}
}

func processReposWithSender(sender *input.StatusSender, selectedProjects []config.Project, setup *input.WizardResult, appCfg config.Config, parallelism int, allProjects []config.Project) {
	filesystem.CreateWorkspace()

	checkpoint := parallelism
	if checkpoint < 5 {
		checkpoint = 5
	}

	var jobs []ProcessJob
	for _, project := range selectedProjects {
		jobs = append(jobs, ProcessJob{
			Project:         project,
			AITool:          setup.AITool,
			AppConfig:       appCfg,
			PRTitle:         setup.PRTitle,
			VibeCodePrompt:  setup.Prompt,
			BranchStrategy:  setup.BranchStrategy,
			SpecifiedBranch: setup.BranchName,
			MCPConfigPath:   sender.MCPConfigPath,
		})
	}

	numWorkers := parallelism
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	var mu sync.Mutex
	resultMap := make(map[string]ProcessResult)

	// Process in batches, pausing between them for user confirmation
	for batchStart := 0; batchStart < len(jobs); batchStart += checkpoint {
		batchEnd := batchStart + checkpoint
		if batchEnd > len(jobs) {
			batchEnd = len(jobs)
		}
		batch := jobs[batchStart:batchEnd]

		batchWorkers := numWorkers
		if batchWorkers > len(batch) {
			batchWorkers = len(batch)
		}

		jobCh := make(chan ProcessJob, len(batch))
		var wg sync.WaitGroup

		for w := 0; w < batchWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobCh {
					repo := job.Project.Repo
					job.UpdateStatus = func(status string) {
						sender.UpdateStatus(repo, status)
					}
					result := processProject(job)

					mu.Lock()
					resultMap[repo] = result
					mu.Unlock()

					var status string
					switch {
					case result.Success:
						status = fmt.Sprintf("Completed ✅ PR: %s", result.PRURL)
					case result.Skipped:
						status = fmt.Sprintf("Skipped ⊘ %v", result.Error)
					default:
						status = fmt.Sprintf("Failed ⚠️ %v", result.Error)
					}
					sender.Done(repo, status, result.Success, result.Skipped, result.PRURL, result.Error)
				}
			}()
		}

		for _, job := range batch {
			jobCh <- job
		}
		close(jobCh)
		wg.Wait()

		// Wait for user confirmation before starting next batch
		if batchEnd < len(jobs) && sender.ResumeCh != nil {
			<-sender.ResumeCh
		}
	}

	// Send Slack notifications as part of processing
	if setup.SendSlack {
		var successfulProjects []config.Project
		prURLs := make(map[string]string)

		// Build project lookup from the full projects list for metadata (slack_room)
		projectMap := make(map[string]config.Project)
		for _, p := range allProjects {
			projectMap[p.Repo] = p
		}

		for repo, result := range resultMap {
			if result.Success {
				if p, ok := projectMap[repo]; ok {
					successfulProjects = append(successfulProjects, p)
					prURLs[repo] = result.PRURL
				}
			}
		}

		if len(successfulProjects) > 0 {
			slack.SendNotifications(successfulProjects, setup.PRTitle, prURLs, setup.SlackToken, sender.PostStatus)
		}
	}
}
