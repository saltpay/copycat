package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RemovedFile tracks a removed instruction file and how to restore it.
type RemovedFile struct {
	RelPath   string // relative path within the repo
	BackupDir string // temp dir holding the backup (empty if git-tracked)
	Tracked   bool   // whether git tracks this file
}

// RemoveInstructionFiles deletes the listed files/dirs from targetPath.
// Untracked files (symlinks, gitignored, etc.) are backed up to a temp dir
// so they can be restored later. Returns metadata needed for restore.
func RemoveInstructionFiles(ctx context.Context, targetPath string, files []string) []RemovedFile {
	tracked := gitTrackedFiles(ctx, targetPath, files)

	var removed []RemovedFile
	for _, f := range files {
		p := filepath.Join(targetPath, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}

		if tracked[f] {
			if err := os.RemoveAll(p); err == nil {
				removed = append(removed, RemovedFile{RelPath: f, Tracked: true})
			}
		} else {
			// Back up untracked file to a temp dir before removing
			tmpDir, err := os.MkdirTemp("", "copycat-backup-*")
			if err != nil {
				continue
			}
			dst := filepath.Join(tmpDir, filepath.Base(f))
			if err := os.Rename(p, dst); err != nil {
				os.RemoveAll(tmpDir)
				continue
			}
			removed = append(removed, RemovedFile{RelPath: f, BackupDir: tmpDir})
		}
	}
	return removed
}

// RestoreInstructionFiles restores previously removed files.
// Git-tracked files are restored via git checkout; untracked files are
// restored from their temporary backup.
func RestoreInstructionFiles(ctx context.Context, targetPath string, files []RemovedFile) error {
	var trackedPaths []string
	var errs []string

	for _, f := range files {
		if f.Tracked {
			trackedPaths = append(trackedPaths, f.RelPath)
		} else if f.BackupDir != "" {
			src := filepath.Join(f.BackupDir, filepath.Base(f.RelPath))
			dst := filepath.Join(targetPath, f.RelPath)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				errs = append(errs, fmt.Sprintf("mkdir for %s: %v", f.RelPath, err))
				continue
			}
			if err := os.Rename(src, dst); err != nil {
				errs = append(errs, fmt.Sprintf("restore %s: %v", f.RelPath, err))
			}
			os.RemoveAll(f.BackupDir)
		}
	}

	if len(trackedPaths) > 0 {
		args := append([]string{"checkout", "--"}, trackedPaths...)
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = targetPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Sprintf("git checkout: %v (%s)", err, string(output)))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("restore failures: %s", strings.Join(errs, "; "))
	}
	return nil
}

// gitTrackedFiles checks which of the given files are tracked by git.
func gitTrackedFiles(ctx context.Context, repoPath string, files []string) map[string]bool {
	result := make(map[string]bool)
	if len(files) == 0 {
		return result
	}
	args := append([]string{"ls-files", "--"}, files...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			result[line] = true
		}
	}
	return result
}
