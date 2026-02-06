package cmd

import (
	"fmt"
	"os"

	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/input"
)

// RunReset deletes the configuration and projects files.
func RunReset() error {
	configPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	projectsPath, err := config.ProjectsPath()
	if err != nil {
		return fmt.Errorf("failed to get projects path: %w", err)
	}

	configExists := fileExists(configPath)
	projectsExists := fileExists(projectsPath)

	if !configExists && !projectsExists {
		fmt.Println("No configuration to reset.")
		return nil
	}

	fmt.Println("This will delete:")
	if configExists {
		fmt.Printf("  - %s\n", configPath)
	}
	if projectsExists {
		fmt.Printf("  - %s\n", projectsPath)
	}

	confirm, err := input.SelectOption("Are you sure?", []string{
		"No, cancel",
		"Yes, delete config",
	})
	if err != nil || confirm == "No, cancel" {
		fmt.Println("Reset cancelled.")
		return nil
	}

	if configExists {
		if err := os.Remove(configPath); err != nil {
			return fmt.Errorf("failed to delete config: %w", err)
		}
	}
	if projectsExists {
		if err := os.Remove(projectsPath); err != nil {
			return fmt.Errorf("failed to delete projects: %w", err)
		}
	}

	fmt.Println("✓ Configuration deleted.")
	fmt.Println("Run 'copycat' to set up a new configuration.")

	return nil
}
