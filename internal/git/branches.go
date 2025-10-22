package git

import (
	"copycat/internal/config"
	"copycat/internal/util"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
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

func SelectOrCreateBranch(repoPath, prTitle string) (string, error) {
	// Fetch latest branches from remote
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoPath
	if _, err := fetchCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: Failed to fetch from remote: %v", err)
	}

	// Get all branches (local and remote) that match copycat-*
	branchCmd := exec.Command("git", "branch", "-a", "--list", "*copycat-*")
	branchCmd.Dir = repoPath
	output, err := branchCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list branches: %w", err)
	}

	// Parse the branches
	var copycatBranches []string
	if len(output) > 0 {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Remove the "remotes/origin/" prefix if present
			line = strings.TrimPrefix(line, "remotes/origin/")
			// Remove asterisk if it's the current branch
			line = strings.TrimPrefix(line, "* ")
			// Skip HEAD references
			if strings.Contains(line, "HEAD") {
				continue
			}
			// Deduplicate
			exists := false
			for _, existing := range copycatBranches {
				if existing == line {
					exists = true
					break
				}
			}
			if !exists {
				copycatBranches = append(copycatBranches, line)
			}
		}
	}

	// If there are existing copycat branches, let user choose
	if len(copycatBranches) > 0 {
		fmt.Printf("\nFound %d existing copycat branch(es):\n", len(copycatBranches))
		for i, branch := range copycatBranches {
			fmt.Printf("%d. %s\n", i+1, branch)
		}

		// Add option to create a new branch
		options := append(copycatBranches, "Create new branch")

		prompt := promptui.Select{
			Label: "Select a branch or create a new one",
			Items: options,
		}

		idx, _, err := prompt.Run()
		if err != nil {
			return "", fmt.Errorf("branch selection failed: %w", err)
		}

		// If user selected an existing branch
		if idx < len(copycatBranches) {
			selectedBranch := copycatBranches[idx]

			// Try to checkout the branch
			checkoutCmd := exec.Command("git", "checkout", selectedBranch)
			checkoutCmd.Dir = repoPath
			output, err := checkoutCmd.CombinedOutput()
			if err != nil {
				// If local checkout fails, try checking out from remote
				checkoutCmd = exec.Command("git", "checkout", "-b", selectedBranch, fmt.Sprintf("origin/%s", selectedBranch))
				checkoutCmd.Dir = repoPath
				output, err = checkoutCmd.CombinedOutput()
				if err != nil {
					return "", fmt.Errorf("failed to checkout branch: %w\nOutput: %s", err, string(output))
				}
			}

			// Pull latest changes
			pullCmd := exec.Command("git", "pull", "origin", selectedBranch)
			pullCmd.Dir = repoPath
			if _, err := pullCmd.CombinedOutput(); err != nil {
				log.Printf("Warning: Failed to pull latest changes: %v", err)
			}

			return selectedBranch, nil
		}
	}

	// Create a new branch with timestamp and slug
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
	output, err = cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create branch: %w\nOutput: %s", err, string(output))
	}

	return newBranch, nil
}
