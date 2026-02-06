package input

import (
	"github.com/saltpay/copycat/internal/config"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func ReadAIPrompt(aiTool *config.AITool) string {
	// Ask for the Claude prompt
	fmt.Printf("\nPlease enter the prompt for %s to execute on each repository:\n", aiTool.Name)

	inputMethod, err := SelectOption("Choose input method", []string{"Single line", "Editor"})
	if err != nil {
		log.Fatal("Failed to select input method:", err)
	}

	if inputMethod == "Single line" {
		title := fmt.Sprintf("%s Prompt", aiTool.Name)
		placeholder := "Describe the instructions to run in each repository"
		prompt, err := GetTextInputWithLimit(title, placeholder, 2048)
		if err != nil {
			log.Fatal("Failed to read prompt:", err)
		}
		fmt.Printf("\n%s prompt: %s\n", aiTool.Name, prompt)
		return prompt
	}

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

	prompt := strings.TrimSpace(string(content))
	fmt.Printf("\n%s prompt:\n%s\n", aiTool.Name, prompt)
	return prompt
}
