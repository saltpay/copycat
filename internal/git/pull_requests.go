package git

import (
	"context"
	"os/exec"
	"strings"

	"github.com/saltpay/copycat/internal/config"
)

// ensureLabelExists creates the 'copycat' label in the repository if it doesn't exist
func ensureLabelExists(ctx context.Context, targetPath string) {
	_, _ = runGhContext(ctx, targetPath, "label", "create", "copycat",
		"--description", "Created by Copycat",
		"--color", "6f42c1",
		"--force")
}

func CreatePullRequest(ctx context.Context, project config.Project, targetPath string, branchName string, prTitle string, prDescription string) ([]byte, error) {
	ensureLabelExists(ctx, targetPath)

	// Get the default branch for this repository
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = targetPath
	defaultBranchOutput, err := cmd.CombinedOutput()
	if err != nil {
		defaultBranchOutput = []byte("origin/main")
	}
	defaultBranch := strings.TrimPrefix(strings.TrimSpace(string(defaultBranchOutput)), "origin/")

	return runGhContext(ctx, targetPath, "pr", "create",
		"--title", prTitle,
		"--body", prDescription,
		"--base", defaultBranch,
		"--head", branchName,
		"--label", "copycat")
}
