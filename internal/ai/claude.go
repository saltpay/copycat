package ai

import (
	"copycat/internal/config"
	"fmt"
	"os/exec"
)

func VibeCode(claudePrompt string, targetPath string) (string, error) {
	// Run claude CLI in non-interactive mode to capture output
	fmt.Printf("Running Claude CLI to analyze and apply changes...\n")
	cmd := exec.Command("claude", "--permission-mode", "acceptEdits", claudePrompt)
	cmd.Dir = targetPath

	claudeOutput, err := cmd.CombinedOutput()

	return string(claudeOutput), err
}

func GeneratePRDescription(project config.Project, claudeOutput string, targetPath string) (string, error) {
	fmt.Printf("Generating PR description...\n")
	summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", claudeOutput)

	cmd := exec.Command("claude", "--permission-mode", "acceptEdits", summaryPrompt)
	cmd.Dir = targetPath

	summaryOutput, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to generate PR description for %s: %v\nOutput: %s", project.Repo, err, string(summaryOutput))
	}

	prDescription := string(summaryOutput)
	if len(prDescription) > 2000 {
		prDescription = prDescription[:1997] + "..."
	}

	return prDescription, nil
}
