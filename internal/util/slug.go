package util

import (
	"regexp"
	"strings"
)

// CreateSlugFromTitle converts a PR title to a git-safe slug
func CreateSlugFromTitle(title string) string {
	// Remove Jira ticket prefix if present (e.g., "JIRA-123 - ")
	re := regexp.MustCompile(`(?i)^[a-z]+-\d+\s*-\s*`)
	slug := re.ReplaceAllString(title, "")

	// Convert to lowercase
	slug = strings.ToLower(slug)

	// Replace spaces and special characters with hyphens
	re = regexp.MustCompile(`[^a-z0-9]+`)
	slug = re.ReplaceAllString(slug, "-")

	// Remove leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Limit length to 50 characters for readability
	if len(slug) > 50 {
		slug = slug[:50]
		// Remove trailing hyphen if truncation created one
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}
