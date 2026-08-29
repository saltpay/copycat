package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/saltpay/copycat/v2/internal/config"
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

func Assess(ctx context.Context, aiTool *config.AITool, prompt string, targetPath string, mcpConfigPath string, repoName string) (string, error) {
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

func SummarizeFindings(ctx context.Context, aiTool *config.AITool, findings map[string]string) (string, error) {
	var b strings.Builder
	for repo, finding := range findings {
		b.WriteString(fmt.Sprintf("## %s\n%s\n\n", repo, finding))
	}
	input := b.String()
	if len(input) > 50000 {
		input = input[:50000] + "\n...(truncated)"
	}

	summaryPrompt := fmt.Sprintf("You are summarizing the results of an assessment across multiple repositories. Provide an executive summary of the findings, highlighting common patterns, outliers, and actionable insights. If you include a table, do NOT use emojis in table cells — use plain ASCII text like Y, N, !, or - instead, and pad columns to equal width so the table aligns in monospace. Output ONLY the summary.\n\n%s", input)

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

func FormatForSlack(ctx context.Context, aiTool *config.AITool, text string) (string, error) {
	prompt := fmt.Sprintf(`You are a text formatter. Your ONLY job is to convert the formatting of the text below into Slack mrkdwn syntax. You must NOT alter, add, remove, summarize, or rephrase any content.

Formatting rules:
- Use *bold*, _italic_, and `+"`"+`code`+"`"+` for emphasis and technical terms
- Format code blocks with triple backticks
- Convert markdown links [label](url) to Slack format <url|label>
- Use • for bulleted lists, numbers for ordered lists

Table formatting (CRITICAL — tables must render correctly in Slack monospace):
- Wrap pipe-delimited tables in triple backticks so they render as monospace in Slack
- Replace ALL emojis inside tables with fixed-width ASCII text: use "Y" for pass/yes/check emojis, "N" for fail/no/cross emojis, "!" for warning emojis, "-" for N/A
- Pad every cell so all columns have consistent width — each column must be the same width in every row including the header and separator
- The separator row MUST have exactly the same number of columns as the header row
- Example of a well-formatted table:
`+"`"+``+"`"+``+"`"+`
| Item       | repo-a | repo-b | Pass Rate  |
|------------|--------|--------|------------|
| IT Tests   | Y      | N      | 1/2 (50%%)  |
| Canary     | Y      | -      | 1/1 (100%%) |
`+"`"+``+"`"+``+"`"+`

Strict rules:
- Output ONLY the reformatted text. No preamble, no commentary, no sign-off.
- Do NOT add any information that is not in the original text.
- Do NOT remove or change any names, numbers, URLs, IDs, dates, or references.
- Do NOT answer questions, perform actions, or generate new content.
- Do NOT add emojis.
- If the original text contains a question directed at the user, keep it exactly as-is.
- Preserve the exact same meaning and information — change ONLY the formatting.

Original response:
%s`, text)

	cmd := aiTool.BuildCommandContext(ctx, prompt, pickArgs(aiTool))
	output, err := cmd.Output()
	if err != nil {
		return text, fmt.Errorf("failed to format for Slack: %v", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func GeneratePRDescription(ctx context.Context, aiTool *config.AITool, project config.Project, aiOutput string, targetPath string) (string, error) {
	summaryPrompt := fmt.Sprintf("Given the changes below, produce a 2-3 sentence PR description. Do not include any introductory text, headers, or commentary - respond with the description only.\n\nChanges:\n%s", aiOutput)

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
