package input

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"
)

func ReadClaudePrompt() string {
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

		if err := tmpFile.Close(); err != nil {
			log.Fatal("Failed to close temp file:", err)
		}
		defer func() {
			if err := os.Remove(tmpFilePath); err != nil {
				log.Println("Failed to remove temp file:", err)
			}
		}()

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

	return claudePrompt
}
