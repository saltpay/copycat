package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RemoveInstructionFiles deletes the listed files/dirs from targetPath.
// Returns the list of paths that were actually removed (for restore).
func RemoveInstructionFiles(targetPath string, files []string) []string {
	var removed []string
	for _, f := range files {
		p := filepath.Join(targetPath, f)
		if _, err := os.Stat(p); err == nil {
			if err := os.RemoveAll(p); err == nil {
				removed = append(removed, f)
			}
		}
	}
	return removed
}

// RestoreInstructionFiles restores previously removed files using git checkout.
func RestoreInstructionFiles(ctx context.Context, targetPath string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"checkout", "--"}, files...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = targetPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %v (%s)", err, string(output))
	}
	return nil
}
