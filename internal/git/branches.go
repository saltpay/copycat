package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/saltpay/copycat/internal/config"
	"github.com/saltpay/copycat/internal/util"
)

// ErrBranchExists is returned when a branch already exists and the skip strategy is used.
var ErrBranchExists = errors.New("branch already exists")

func CheckLocalChanges(ctx context.Context, targetPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = targetPath
	return cmd.CombinedOutput()
}

func PushChanges(ctx context.Context, project config.Project, targetPath string, branchName string, prTitle string) error {
	// Check if there are changes to commit
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = targetPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check git status in %s: %v", project.Repo, err)
	}

	if len(output) == 0 {
		return fmt.Errorf("no changes detected in %s, skipping PR creation", project.Repo)
	}

	// Add all changes
	cmd = exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = targetPath
	_, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add changes in %s: %v", project.Repo, err)
	}

	// Commit changes
	commitMessage := prTitle
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", commitMessage)
	cmd.Dir = targetPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to commit changes in %s: %v\nOutput: %s", project.Repo, err, string(output))
	}

	// Push branch
	cmd = exec.CommandContext(ctx, "git", "push", "-u", "origin", branchName)
	cmd.Dir = targetPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to push branch in %s: %v\nOutput: %s", project.Repo, err, string(output))
	}

	return nil
}

func SelectOrCreateBranch(ctx context.Context, repoPath, prTitle, branchStrategy, specifiedBranch string) (string, error) {
	// Fetch latest branches from remote
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = repoPath
	fetchCmd.CombinedOutput()

	// Handle "Specify branch name (reuse if exists)" strategy
	if strings.Contains(branchStrategy, "reuse if exists") {
		return checkoutOrCreateBranch(ctx, repoPath, specifiedBranch)
	}

	// Handle "Specify branch name (skip if exists)" strategy
	if strings.Contains(branchStrategy, "skip if exists") {
		return createBranchOrSkip(ctx, repoPath, specifiedBranch)
	}

	// Handle "Always create new" strategy (default)
	return createNewBranch(ctx, repoPath, prTitle)
}

// checkoutOrCreateBranch checks out a branch if it exists, or creates it if it doesn't
func checkoutOrCreateBranch(ctx context.Context, repoPath, branchName string) (string, error) {
	// Try to checkout the branch
	checkoutCmd := exec.CommandContext(ctx, "git", "checkout", branchName)
	checkoutCmd.Dir = repoPath
	output, err := checkoutCmd.CombinedOutput()
	if err != nil {
		// If local checkout fails, try checking out from remote
		checkoutCmd = exec.CommandContext(ctx, "git", "checkout", "-b", branchName, fmt.Sprintf("origin/%s", branchName))
		checkoutCmd.Dir = repoPath
		output, err = checkoutCmd.CombinedOutput()
		if err != nil {
			// Branch doesn't exist locally or remotely, create it
			createCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName)
			createCmd.Dir = repoPath
			output, err = createCmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
			}
			return branchName, nil
		}
	}

	// Pull latest changes if branch already existed
	pullCmd := exec.CommandContext(ctx, "git", "pull", "origin", branchName)
	pullCmd.Dir = repoPath
	pullCmd.CombinedOutput()

	return branchName, nil
}

// createBranchOrSkip creates a new branch, or returns ErrBranchExists if it already exists locally or remotely.
func createBranchOrSkip(ctx context.Context, repoPath, branchName string) (string, error) {
	if branchExistsLocally(ctx, repoPath, branchName) || branchExistsRemotely(ctx, repoPath, branchName) {
		return "", fmt.Errorf("%w: %s", ErrBranchExists, branchName)
	}

	createCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName)
	createCmd.Dir = repoPath
	output, err := createCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
	}
	return branchName, nil
}

func branchExistsLocally(ctx context.Context, repoPath, branchName string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branchName)
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func branchExistsRemotely(ctx context.Context, repoPath, branchName string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", fmt.Sprintf("origin/%s", branchName))
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

// createNewBranch creates a new branch with timestamp and slug
func createNewBranch(ctx context.Context, repoPath, prTitle string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	slug := util.CreateSlugFromTitle(prTitle)

	var newBranch string
	if slug != "" {
		newBranch = fmt.Sprintf("copycat-%s-%s", timestamp, slug)
	} else {
		// Fallback to just timestamp if slug is empty
		newBranch = fmt.Sprintf("copycat-%s", timestamp)
	}

	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", newBranch)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
	}

	return newBranch, nil
}
