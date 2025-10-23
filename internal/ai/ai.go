package ai

import (
	"copycat/internal/config"
	"fmt"
	"os/exec"
)

func VibeCode(aiTool *config.AITool, prompt string, targetPath string) (string, error) {
	// Run the configured AI tool in non-interactive mode to capture output
	fmt.Printf("Running %s to analyze and apply changes...\n", aiTool.Name)
	cmd := buildAIToolCommand(aiTool, prompt, false)
	cmd.Dir = targetPath

	output, err := cmd.CombinedOutput()

	return string(output), err
}

func GeneratePRDescription(aiTool *config.AITool, project config.Project, aiOutput string, targetPath string) (string, error) {
	summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", aiOutput)

	// Run the configured AI tool in non-interactive mode to capture output
	fmt.Printf("Generating PR description...\n")
	cmd := buildAIToolCommand(aiTool, summaryPrompt, true)
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

func buildAIToolCommand(tool *config.AITool, prompt string, useSummaryArgs bool) *exec.Cmd {
	var args []string
	switch {
	case useSummaryArgs && len(tool.SummaryArgs) > 0:
		args = append([]string{}, tool.SummaryArgs...)
	default:
		args = append([]string{}, tool.CodeArgs...)
	}
	args = append(args, prompt)
	return exec.Command(tool.Command, args...)
}
