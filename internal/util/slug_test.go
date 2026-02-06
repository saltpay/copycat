package util

import "testing"

func TestCreateSlugFromTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple title",
			input:    "Update dependencies",
			expected: "update-dependencies",
		},
		{
			name:     "Title with ticket prefix",
			input:    "PROJ-123 - Fix authentication bug",
			expected: "fix-authentication-bug",
		},
		{
			name:     "Title with special characters",
			input:    "Add support for HTTP/2 & WebSockets",
			expected: "add-support-for-http-2-websockets",
		},
		{
			name:     "Title with multiple spaces",
			input:    "Update   the    database   schema",
			expected: "update-the-database-schema",
		},
		{
			name:     "Long title gets truncated",
			input:    "This is a very long PR title that should be truncated to ensure branch names remain readable and manageable",
			expected: "this-is-a-very-long-pr-title-that-should-be-trunca",
		},
		{
			name:     "Title with leading/trailing spaces",
			input:    "  Fix bug in parser  ",
			expected: "fix-bug-in-parser",
		},
		{
			name:     "Title with mixed case",
			input:    "Update NodeJS Dependencies",
			expected: "update-nodejs-dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateSlugFromTitle(tt.input)
			if result != tt.expected {
				t.Errorf("createSlugFromTitle(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
