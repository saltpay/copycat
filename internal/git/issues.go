package git

import (
	"fmt"

	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/input"
)

// CreateGitHubIssuesWithSender creates GitHub issues across projects, reporting progress via StatusSender.
func CreateGitHubIssuesWithSender(sender *input.StatusSender, githubCfg config.GitHubConfig, selectedProjects []config.Project, issueTitle, issueDescription string) {
	for _, project := range selectedProjects {
		sender.UpdateStatus(project.Repo, "Creating issue...")
		err := createGitHubIssueWithCLI(githubCfg, project, issueTitle, issueDescription)
		if err != nil {
			sender.Done(project.Repo, fmt.Sprintf("Failed ⚠️ %v", err), false, "", err)
		} else {
			sender.Done(project.Repo, "Issue created ✅", true, "", nil)
		}
	}
}

func createGitHubIssueWithCLI(githubCfg config.GitHubConfig, project config.Project, title string, description string) error {
	output, err := runGh("", "issue", "create",
		"--repo", fmt.Sprintf("%s/%s", githubCfg.Organization, project.Repo),
		"--title", title,
		"--body", description,
		"--assignee", "@copilot")
	if err != nil {
		return fmt.Errorf("failed to create issue: %v\nOutput: %s", err, string(output))
	}

	return nil
}
