package git

import (
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

func CheckLocalChanges(targetPath string) ([]byte, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = targetPath
	return cmd.CombinedOutput()
}

func PushChanges(project config.Project, targetPath string, branchName string, prTitle string) error {
	// Check if there are changes to commit
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = targetPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check git status in %s: %v", project.Repo, err)
	}

	if len(output) == 0 {
		return fmt.Errorf("no changes detected in %s, skipping PR creation", project.Repo)
	}

	// Add all changes
	cmd = exec.Command("git", "add", "-A")
	cmd.Dir = targetPath
	_, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add changes in %s: %v", project.Repo, err)
	}

	// Commit changes
	commitMessage := prTitle
	cmd = exec.Command("git", "commit", "-m", commitMessage)
	cmd.Dir = targetPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to commit changes in %s: %v\nOutput: %s", project.Repo, err, string(output))
	}

	// Push branch
	cmd = exec.Command("git", "push", "-u", "origin", branchName)
	cmd.Dir = targetPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to push branch in %s: %v\nOutput: %s", project.Repo, err, string(output))
	}

	return nil
}

func SelectOrCreateBranch(repoPath, prTitle, branchStrategy, specifiedBranch string) (string, error) {
	// Fetch latest branches from remote
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoPath
	fetchCmd.CombinedOutput()

	// Handle "Specify branch name (reuse if exists)" strategy
	if strings.Contains(branchStrategy, "reuse if exists") {
		return checkoutOrCreateBranch(repoPath, specifiedBranch)
	}

	// Handle "Specify branch name (skip if exists)" strategy
	if strings.Contains(branchStrategy, "skip if exists") {
		return createBranchOrSkip(repoPath, specifiedBranch)
	}

	// Handle "Always create new" strategy (default)
	return createNewBranch(repoPath, prTitle)
}

// checkoutOrCreateBranch checks out a branch if it exists, or creates it if it doesn't
func checkoutOrCreateBranch(repoPath, branchName string) (string, error) {
	// Try to checkout the branch
	checkoutCmd := exec.Command("git", "checkout", branchName)
	checkoutCmd.Dir = repoPath
	output, err := checkoutCmd.CombinedOutput()
	if err != nil {
		// If local checkout fails, try checking out from remote
		checkoutCmd = exec.Command("git", "checkout", "-b", branchName, fmt.Sprintf("origin/%s", branchName))
		checkoutCmd.Dir = repoPath
		output, err = checkoutCmd.CombinedOutput()
		if err != nil {
			// Branch doesn't exist locally or remotely, create it
			createCmd := exec.Command("git", "checkout", "-b", branchName)
			createCmd.Dir = repoPath
			output, err = createCmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
			}
			return branchName, nil
		}
	}

	// Pull latest changes if branch already existed
	pullCmd := exec.Command("git", "pull", "origin", branchName)
	pullCmd.Dir = repoPath
	pullCmd.CombinedOutput()

	return branchName, nil
}

// createBranchOrSkip creates a new branch, or returns ErrBranchExists if it already exists locally or remotely.
func createBranchOrSkip(repoPath, branchName string) (string, error) {
	if branchExistsLocally(repoPath, branchName) || branchExistsRemotely(repoPath, branchName) {
		return "", fmt.Errorf("%w: %s", ErrBranchExists, branchName)
	}

	createCmd := exec.Command("git", "checkout", "-b", branchName)
	createCmd.Dir = repoPath
	output, err := createCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
	}
	return branchName, nil
}

func branchExistsLocally(repoPath, branchName string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func branchExistsRemotely(repoPath, branchName string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", fmt.Sprintf("origin/%s", branchName))
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

// createNewBranch creates a new branch with timestamp and slug
func createNewBranch(repoPath, prTitle string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	slug := util.CreateSlugFromTitle(prTitle)

	var newBranch string
	if slug != "" {
		newBranch = fmt.Sprintf("copycat-%s-%s", timestamp, slug)
	} else {
		// Fallback to just timestamp if slug is empty
		newBranch = fmt.Sprintf("copycat-%s", timestamp)
	}

	cmd := exec.Command("git", "checkout", "-b", newBranch)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
	}

	return newBranch, nil
}
