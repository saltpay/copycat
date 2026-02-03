package cmd

import (
	"fmt"
	"os"
	"strings"

	"copycat/internal/config"
	"copycat/internal/input"

	"gopkg.in/yaml.v3"
)

// oldProjectCache represents the old .projects.yaml structure.
type oldProjectCache struct {
	Projects []oldCachedProject `yaml:"projects"`
}

type oldCachedProject struct {
	Repo           string   `yaml:"repo"`
	SlackRoom      string   `yaml:"slack_room"`
	RequiresTicket bool     `yaml:"requires_ticket"`
	Topics         []string `yaml:"topics,omitempty"`
}

// RunMigrate migrates from old config structure to new unified config.
func RunMigrate() error {
	// Check if new config already exists at XDG path
	xdgPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine XDG config path: %w", err)
	}

	if _, err := os.Stat(xdgPath); err == nil {
		fmt.Printf("Config already exists at %s\n", xdgPath)
		override, err := input.SelectOption("Override existing config?", []string{
			"No, keep existing",
			"Yes, override",
		})
		if err != nil || override == "No, keep existing" {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	// Check for old config files
	oldConfigPath := "config.yaml"
	oldProjectsPath := ".projects.yaml"

	oldConfigExists := fileExists(oldConfigPath)
	oldProjectsExists := fileExists(oldProjectsPath)

	if !oldConfigExists && !oldProjectsExists {
		return fmt.Errorf("no old config files found (config.yaml or .projects.yaml)\nRun 'copycat init' to create a new config")
	}

	fmt.Println("Migration: Old structure -> Unified XDG config")
	fmt.Println()

	// Load old config
	var cfg *config.Config
	if oldConfigExists {
		fmt.Printf("Found: %s\n", oldConfigPath)
		cfg, err = config.Load(oldConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load old config: %w", err)
		}
	} else {
		fmt.Println("No old config.yaml found, will prompt for organization")
		org, err := input.GetTextInput("GitHub Organization", "e.g., my-org")
		if err != nil {
			return fmt.Errorf("organization input cancelled")
		}
		cfg = config.DefaultConfig(org)
	}

	// Load old projects
	if oldProjectsExists {
		fmt.Printf("Found: %s\n", oldProjectsPath)
		projects, err := loadOldProjects(oldProjectsPath)
		if err != nil {
			return fmt.Errorf("failed to load old projects: %w", err)
		}
		cfg.Projects = projects
		fmt.Printf("Loaded %d projects\n", len(projects))
	} else {
		fmt.Println("No old .projects.yaml found, starting with empty projects list")
		cfg.Projects = []config.Project{}
	}

	// Ensure config directory exists
	if err := config.EnsureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Save new unified config
	if err := cfg.Save(xdgPath); err != nil {
		return fmt.Errorf("failed to save new config: %w", err)
	}

	fmt.Printf("\n✓ Created unified config at: %s\n", xdgPath)
	fmt.Println("\nMigration complete!")
	fmt.Println("Run 'copycat edit' to review your configuration.")
	if oldConfigExists || oldProjectsExists {
		fmt.Println("\nYou can now manually delete the old files if desired:")
		if oldConfigExists {
			fmt.Printf("  rm %s\n", oldConfigPath)
		}
		if oldProjectsExists {
			fmt.Printf("  rm %s\n", oldProjectsPath)
		}
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadOldProjects(filename string) ([]config.Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cache oldProjectCache
	if err := yaml.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", filename, err)
	}

	projects := make([]config.Project, len(cache.Projects))
	for i, p := range cache.Projects {
		slackRoom := strings.TrimSpace(p.SlackRoom)
		if slackRoom == "#none" {
			slackRoom = ""
		}

		projects[i] = config.Project{
			Repo:           p.Repo,
			SlackRoom:      slackRoom,
			RequiresTicket: p.RequiresTicket,
			Topics:         p.Topics,
		}
	}

	return projects, nil
}
