package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/saltpay/copycat/internal/config"
)

// RunEdit opens a configuration file in the user's editor.
func RunEdit(target string) error {
	var filePath string
	switch target {
	case "config":
		p, err := config.ConfigPath()
		if err != nil {
			return fmt.Errorf("failed to resolve config path: %w", err)
		}
		filePath = p
	case "projects":
		p, err := config.ProjectsPath()
		if err != nil {
			return fmt.Errorf("failed to resolve projects path: %w", err)
		}
		filePath = p
	default:
		return fmt.Errorf("unknown edit target %q\n\nUsage: copycat edit <config|projects>", target)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist at %s\n\nRun 'copycat' to set up your configuration", filePath)
	}

	editor := getEditor()
	fmt.Printf("Opening %s in %s\n", filePath, editor)

	cmd := exec.Command(editor, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	return nil
}

// getEditor returns the user's preferred editor.
func getEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	// Check for common editors
	editors := []string{"vim", "nano", "vi"}
	for _, editor := range editors {
		if _, err := exec.LookPath(editor); err == nil {
			return editor
		}
	}

	// Fallback
	return "vi"
}
