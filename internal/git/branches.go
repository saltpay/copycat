package git

import (
	"copycat/internal/config"
	"copycat/internal/util"
	"fmt"
	"log"
	"os/exec"
	"sort"
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

// findMostRecentBranch returns the most recent copycat branch from a list of branches
// Returns empty string if no branches are provided
func findMostRecentBranch(branches []string) string {
	if len(branches) == 0 {
		return ""
	}

	// Sort branches by timestamp (descending) to get most recent first
	sort.Slice(branches, func(i, j int) bool {
		// Extract timestamp from branch names
		// Format: copycat-YYYYMMDD-HHMMSS[-slug]
		partsI := strings.Split(branches[i], "-")
		partsJ := strings.Split(branches[j], "-")

		// Need at least copycat-YYYYMMDD-HHMMSS (3 parts)
		if len(partsI) < 3 || len(partsJ) < 3 {
			return branches[i] > branches[j]
		}

		// Compare date part (YYYYMMDD)
		if partsI[1] != partsJ[1] {
			return partsI[1] > partsJ[1]
		}

		// Compare time part (HHMMSS)
		if len(partsI) > 2 && len(partsJ) > 2 {
			return partsI[2] > partsJ[2]
		}

		return branches[i] > branches[j]
	})

	return branches[0]
}

func SelectOrCreateBranch(repoPath, prTitle, branchStrategy, specifiedBranch string) (string, error) {
	// Fetch latest branches from remote
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoPath
	if _, err := fetchCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: Failed to fetch from remote: %v", err)
	}

	// Handle "Specify branch name" strategy
	if strings.HasPrefix(branchStrategy, "Specify branch name") {
		return checkoutOrCreateBranch(repoPath, specifiedBranch)
	}

	// Handle "Always create new" strategy
	if strings.HasPrefix(branchStrategy, "Always create new") {
		return createNewBranch(repoPath, prTitle)
	}

	// Handle "Reuse if available" strategy
	if strings.HasPrefix(branchStrategy, "Reuse if available") {
		copycatBranches := listCopycatBranches(repoPath)

		if len(copycatBranches) > 0 {
			// Find most recent branch
			mostRecent := findMostRecentBranch(copycatBranches)
			if mostRecent != "" {
				// Try to checkout and use the most recent branch
				selectedBranch, err := checkoutAndPullBranch(repoPath, mostRecent)
				if err != nil {
					log.Printf("Warning: Failed to checkout most recent branch %s: %v. Creating new branch instead.", mostRecent, err)
					return createNewBranch(repoPath, prTitle)
				}
				return selectedBranch, nil
			}
		}

		// No copycat branches found, create new one
		return createNewBranch(repoPath, prTitle)
	}

	// Fallback: create new branch
	return createNewBranch(repoPath, prTitle)
}

// listCopycatBranches returns all copycat-* branches (local and remote)
func listCopycatBranches(repoPath string) []string {
	branchCmd := exec.Command("git", "branch", "-a", "--list", "*copycat-*")
	branchCmd.Dir = repoPath
	output, err := branchCmd.CombinedOutput()
	if err != nil {
		return []string{}
	}

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

	return copycatBranches
}

// checkoutAndPullBranch checks out a branch and pulls latest changes
func checkoutAndPullBranch(repoPath, branchName string) (string, error) {
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
			return "", fmt.Errorf("failed to checkout branch: %w\nOutput: %s", err, string(output))
		}
	}

	// Pull latest changes
	pullCmd := exec.Command("git", "pull", "origin", branchName)
	pullCmd.Dir = repoPath
	if _, err := pullCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: Failed to pull latest changes: %v", err)
	}

	return branchName, nil
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
