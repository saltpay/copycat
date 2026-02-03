package cmd

import (
	"fmt"
	"os"

	"copycat/internal/config"
	"copycat/internal/input"
)

// RunReset deletes the configuration file.
func RunReset() error {
	configPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("No configuration to reset.")
		return nil
	}

	fmt.Printf("This will delete: %s\n", configPath)

	confirm, err := input.SelectOption("Are you sure?", []string{
		"No, cancel",
		"Yes, delete config",
	})
	if err != nil || confirm == "No, cancel" {
		fmt.Println("Reset cancelled.")
		return nil
	}

	if err := os.Remove(configPath); err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}

	fmt.Println("✓ Configuration deleted.")
	fmt.Println("Run 'copycat' to set up a new configuration.")

	return nil
}
