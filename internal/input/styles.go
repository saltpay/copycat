package input

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Semantic color constants using AdaptiveColor for light/dark terminal support.
var (
	colorDone      = lipgloss.AdaptiveColor{Light: "#04B575", Dark: "#04B575"} // green
	colorRunning   = lipgloss.AdaptiveColor{Light: "#0087D7", Dark: "#0087D7"} // cyan/blue
	colorWaiting   = lipgloss.AdaptiveColor{Light: "#626262", Dark: "#626262"} // gray
	colorFailed    = lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"} // red
	colorCancelled = lipgloss.AdaptiveColor{Light: "#D7AF00", Dark: "#FFAF00"} // yellow
	colorAccent    = lipgloss.AdaptiveColor{Light: "#FF5FAF", Dark: "#FF5FAF"} // pink (brand)
	colorDim       = lipgloss.AdaptiveColor{Light: "#626262", Dark: "#626262"} // dim gray
	colorSubtle    = lipgloss.AdaptiveColor{Light: "#4E4E4E", Dark: "#4E4E4E"} // subtle borders
	colorText      = lipgloss.AdaptiveColor{Light: "#BCBCBC", Dark: "#BCBCBC"} // normal text
)

// Status icon constants.
const (
	iconDone      = "✓"
	iconFailed    = "✗"
	iconWaiting   = "·"
	iconCancelled = "⊘"
)

// Pre-built lipgloss styles (st prefix avoids shadowing local vars in render methods).
var (
	stDone      = lipgloss.NewStyle().Foreground(colorDone)
	stRunning   = lipgloss.NewStyle().Foreground(colorRunning)
	stWaiting   = lipgloss.NewStyle().Foreground(colorWaiting)
	stFailed    = lipgloss.NewStyle().Foreground(colorFailed)
	stCancelled = lipgloss.NewStyle().Foreground(colorCancelled)
	stAccent    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	stDim       = lipgloss.NewStyle().Foreground(colorDim)
	stSubtle    = lipgloss.NewStyle().Foreground(colorSubtle)
	stText      = lipgloss.NewStyle().Foreground(colorText)
)

// repoStatusDisplay returns the appropriate icon and style for a repo based on its current state.
func repoStatusDisplay(repo string, results map[string]ProjectDoneMsg, cancelled map[string]bool, statuses map[string]string) (string, lipgloss.Style) {
	if result, done := results[repo]; done {
		if result.Success {
			return iconDone, stDone
		}
		return iconFailed, stFailed
	}
	if cancelled[repo] {
		return iconCancelled, stCancelled
	}
	if statuses[repo] == "Waiting..." {
		return iconWaiting, stWaiting
	}
	return "", stRunning // spinner rendered separately for in-progress
}

// formatRepoElapsed returns a human-friendly elapsed time string for a repo.
func formatRepoElapsed(repo string, startTimes map[string]time.Time, doneTimes map[string]time.Time) string {
	start, ok := startTimes[repo]
	if !ok {
		return ""
	}
	var elapsed time.Duration
	if doneTime, done := doneTimes[repo]; done {
		elapsed = doneTime.Sub(start)
	} else {
		elapsed = time.Since(start)
	}
	elapsed = elapsed.Round(time.Second)
	if elapsed < time.Minute {
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
}

// buildSummaryStats returns a formatted summary line like "✓ 4 done  •  ⠋ 2 running  •  · 3 waiting"
func buildSummaryStats(repos []string, results map[string]ProjectDoneMsg, cancelled map[string]bool, statuses map[string]string) string {
	var done, failed, running, waiting, cancelledCount int
	for _, repo := range repos {
		if result, ok := results[repo]; ok {
			if result.Success {
				done++
			} else {
				failed++
			}
		} else if cancelled[repo] {
			cancelledCount++
		} else if statuses[repo] == "Waiting..." {
			waiting++
		} else {
			running++
		}
	}

	var parts []string
	separator := stDim.Render("  •  ")
	if done > 0 {
		parts = append(parts, stDone.Render(fmt.Sprintf("%s %d done", iconDone, done)))
	}
	if running > 0 {
		parts = append(parts, stRunning.Render(fmt.Sprintf("⠋ %d running", running)))
	}
	if waiting > 0 {
		parts = append(parts, stWaiting.Render(fmt.Sprintf("%s %d waiting", iconWaiting, waiting)))
	}
	if failed > 0 {
		parts = append(parts, stFailed.Render(fmt.Sprintf("%s %d failed", iconFailed, failed)))
	}
	if cancelledCount > 0 {
		parts = append(parts, stCancelled.Render(fmt.Sprintf("%s %d cancelled", iconCancelled, cancelledCount)))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, separator)
}
