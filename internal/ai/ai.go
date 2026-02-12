package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/saltpay/copycat/internal/config"
)

func VibeCode(ctx context.Context, aiTool *config.AITool, prompt string, targetPath string, mcpConfigPath string, repoName string) (string, error) {
	var opts []config.CommandOptions
	if mcpConfigPath != "" {
		opts = append(opts, config.CommandOptions{MCPConfigPath: mcpConfigPath})
	}

	cmd := aiTool.BuildCommandContext(ctx, prompt, aiTool.CodeArgs, opts...)
	cmd.Dir = targetPath
	if repoName != "" {
		cmd.Env = append(os.Environ(), "COPYCAT_REPO_NAME="+repoName)
	}

	output, err := cmd.CombinedOutput()

	return string(output), err
}

func pickArgs(aiTool *config.AITool) []string {
	if len(aiTool.SummaryArgs) > 0 {
		return aiTool.SummaryArgs
	}
	return aiTool.CodeArgs
}

func RewritePromptForProject(ctx context.Context, aiTool *config.AITool, userPrompt string) (string, error) {
	rewritePrompt := fmt.Sprintf("Rewrite this question so it applies to a single repository. Output ONLY the rewritten question.\n\nOriginal: %s", userPrompt)

	cmd := aiTool.BuildCommandContext(ctx, rewritePrompt, pickArgs(aiTool))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to rewrite prompt: %v\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

func Assess(ctx context.Context, aiTool *config.AITool, prompt string, targetPath string, repoName string) (string, error) {
	cmd := aiTool.BuildCommandContext(ctx, prompt, aiTool.CodeArgs)
	cmd.Dir = targetPath
	if repoName != "" {
		cmd.Env = append(os.Environ(), "COPYCAT_REPO_NAME="+repoName)
	}

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func SummarizeFindings(ctx context.Context, aiTool *config.AITool, findings map[string]string) (string, error) {
	var b strings.Builder
	for repo, finding := range findings {
		b.WriteString(fmt.Sprintf("## %s\n%s\n\n", repo, finding))
	}
	input := b.String()
	if len(input) > 50000 {
		input = input[:50000] + "\n...(truncated)"
	}

	summaryPrompt := fmt.Sprintf("You are summarizing the results of an assessment across multiple repositories. Provide an executive summary of the findings, highlighting common patterns, outliers, and actionable insights. Output ONLY the summary.\n\n%s", input)

	cmd := aiTool.BuildCommandContext(ctx, summaryPrompt, pickArgs(aiTool))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to summarize findings: %v\nOutput: %s", err, string(output))
	}

	summary := string(output)
	if len(summary) > 5000 {
		summary = summary[:4997] + "..."
	}

	return strings.TrimSpace(summary), nil
}

func GeneratePRDescription(ctx context.Context, aiTool *config.AITool, project config.Project, aiOutput string, targetPath string) (string, error) {
	summaryPrompt := fmt.Sprintf("Write a concise PR description (2-3 sentences) for the following changes. Output ONLY the description text, no preamble:\n\n%s", aiOutput)

	cmd := aiTool.BuildCommandContext(ctx, summaryPrompt, aiTool.SummaryArgs)
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
