package git

import (
	"fmt"
	"github.com/saltpay/copycat/internal/config"
	"log"
	"os/exec"
	"strings"
)

// ensureLabelExists creates the 'copycat' label in the repository if it doesn't exist
func ensureLabelExists(targetPath string) {
	cmd := exec.Command("gh", "label", "create", "copycat",
		"--description", "Created by Copycat",
		"--color", "6f42c1",
		"--force")
	cmd.Dir = targetPath
	_ = cmd.Run()
}

func CreatePullRequest(project config.Project, targetPath string, branchName string, prTitle string, prDescription string) ([]byte, error) {
	ensureLabelExists(targetPath)

	// Get the default branch for this repository
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = targetPath
	defaultBranchOutput, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to get default branch for %s: %v, defaulting to 'main'", project.Repo, err)
		defaultBranchOutput = []byte("origin/main")
	}
	defaultBranch := strings.TrimPrefix(strings.TrimSpace(string(defaultBranchOutput)), "origin/")
	fmt.Printf("Using base branch: %s\n", defaultBranch)

	// Create PR using GitHub CLI
	fmt.Printf("Creating pull request...\n")

	cmd = exec.Command("gh", "pr", "create",
		"--title", prTitle,
		"--body", prDescription,
		"--base", defaultBranch,
		"--head", branchName,
		"--label", "copycat")
	cmd.Dir = targetPath

	return cmd.CombinedOutput()
}
