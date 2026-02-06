package config

import "fmt"

// ConfigTemplate is the default configuration template.
// Use fmt.Sprintf(ConfigTemplate, org) to fill in the organization.
const ConfigTemplate = `github:
  organization: %s
  auto_discovery_topic: copycat

tools:
  - name: claude
    command: claude
    code_args:
      - --print
      - --permission-mode
      - acceptEdits
    summary_args:
      - --print
  - name: codex
    command: codex
    code_args:
      - exec
      - --full-auto
    summary_args: []
  - name: qwen
    command: qwen
    code_args:
      - --approval-mode
      - auto-edit
      - -p
    summary_args: []
  - name: gemini
    command: gemini
    code_args:
      - --approval-mode
      - auto_edit
    summary_args: []

projects: []
`

// DefaultConfigContent returns the default config content with the given org.
func DefaultConfigContent(org string) string {
	return fmt.Sprintf(ConfigTemplate, org)
}

// DefaultConfig returns a Config struct with default values.
func DefaultConfig(org string) *Config {
	return &Config{
		GitHub: GitHubConfig{
			Organization:       org,
			AutoDiscoveryTopic: "copycat",
		},
		AIToolsConfig: AIToolsConfig{
			Tools: []AITool{
				{
					Name:        "claude",
					Command:     "claude",
					CodeArgs:    []string{"--print", "--permission-mode", "acceptEdits"},
					SummaryArgs: []string{"--print"},
				},
				{
					Name:        "codex",
					Command:     "codex",
					CodeArgs:    []string{"exec", "--full-auto"},
					SummaryArgs: []string{},
				},
				{
					Name:        "qwen",
					Command:     "qwen",
					CodeArgs:    []string{"--approval-mode", "auto-edit", "-p"},
					SummaryArgs: []string{},
				},
				{
					Name:        "gemini",
					Command:     "gemini",
					CodeArgs:    []string{"--approval-mode", "auto_edit"},
					SummaryArgs: []string{},
				},
			},
		},
		Projects: []Project{},
	}
}
