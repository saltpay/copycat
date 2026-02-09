package ai

import (
	"fmt"
	"os"

	"github.com/saltpay/copycat/internal/config"
)

func VibeCode(aiTool *config.AITool, prompt string, targetPath string, mcpConfigPath string, repoName string) (string, error) {
	var opts []config.CommandOptions
	if mcpConfigPath != "" {
		opts = append(opts, config.CommandOptions{MCPConfigPath: mcpConfigPath})
	}

	cmd := aiTool.BuildCommand(prompt, aiTool.CodeArgs, opts...)
	cmd.Dir = targetPath
	if repoName != "" {
		cmd.Env = append(os.Environ(), "COPYCAT_REPO_NAME="+repoName)
	}

	output, err := cmd.CombinedOutput()

	return string(output), err
}

func GeneratePRDescription(aiTool *config.AITool, project config.Project, aiOutput string, targetPath string) (string, error) {
	summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", aiOutput)

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
