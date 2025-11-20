package git

import (
	"copycat/internal/config"
	"copycat/internal/util"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

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
	fmt.Printf("Committing changes...\n")
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
	fmt.Printf("Pushing branch to remote...\n")
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
	if _, err := fetchCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: Failed to fetch from remote: %v", err)
	}

	// Handle "Specify branch name" strategy (reuse if exists, create if doesn't)
	if strings.HasPrefix(branchStrategy, "Specify branch name") {
		return checkoutOrCreateBranch(repoPath, specifiedBranch)
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
	if _, err := pullCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: Failed to pull latest changes: %v", err)
	}

	return branchName, nil
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
