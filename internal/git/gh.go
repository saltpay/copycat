package git

import (
	"context"
	"os/exec"
	"sync"
)

// ghMu serializes all gh CLI calls to avoid GitHub API rate limiting.
var ghMu sync.Mutex

// runGh executes a gh CLI command with mutual exclusion.
// If dir is non-empty, the command runs in that directory.
func runGh(dir string, args ...string) ([]byte, error) {
	return runGhContext(context.Background(), dir, args...)
}

// runGhContext executes a gh CLI command with mutual exclusion and context support.
func runGhContext(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ghMu.Lock()
	defer ghMu.Unlock()

	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}
