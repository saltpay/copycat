package ai

import (
	"github.com/saltpay/copycat/internal/config"
	"fmt"
)

func VibeCode(aiTool *config.AITool, prompt string, targetPath string) (string, error) {
	// Run the configured AI tool in non-interactive mode to capture output
	fmt.Printf("Running %s to analyze and apply changes...\n", aiTool.Name)
	cmd := aiTool.BuildCommand(prompt, aiTool.CodeArgs)
	cmd.Dir = targetPath

	output, err := cmd.CombinedOutput()

	return string(output), err
}

func GeneratePRDescription(aiTool *config.AITool, project config.Project, aiOutput string, targetPath string) (string, error) {
	summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", aiOutput)

	// Run the configured AI tool in non-interactive mode to capture output
	fmt.Printf("Generating PR description...\n")
	cmd := aiTool.BuildCommand(summaryPrompt, aiTool.SummaryArgs)
	cmd.Dir = targetPath

	summaryOutput, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("Failed to generate PR description for %s: %v\nOutput: %s", project.Repo, err, string(summaryOutput))
	}

	prDescription := string(summaryOutput)
	if len(prDescription) > 2000 {
		prDescription = prDescription[:1997] + "..."
	}

	return prDescription, nil
}
