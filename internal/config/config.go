package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Repo      string   `yaml:"repo"`
	SlackRoom string   `yaml:"slack_room"`
	Topics    []string `yaml:"topics,omitempty"`
}

type GitHubConfig struct {
	Organization       string `yaml:"organization"`
	AutoDiscoveryTopic string `yaml:"auto_discovery_topic"`
}

type Config struct {
	GitHub        GitHubConfig `yaml:"github"`
	Parallelism   int          `yaml:"parallelism,omitempty"`
	AIToolsConfig `yaml:",inline"`
}

type AITool struct {
	Name                     string   `yaml:"name"`
	Command                  string   `yaml:"command"`
	CodeArgs                 []string `yaml:"code_args"`
	SummaryArgs              []string `yaml:"summary_args"`
	AllowedTools             []string `yaml:"allowed_tools,omitempty"`
	DisallowedTools          []string `yaml:"disallowed_tools,omitempty"`
	SupportsPermissionPrompt bool     `yaml:"supports_permission_prompt,omitempty"`
}

// CommandOptions holds optional flags for BuildCommand.
type CommandOptions struct {
	MCPConfigPath string
}

func (t *AITool) BuildCommand(prompt string, baseArgs []string, opts ...CommandOptions) *exec.Cmd {
	args := append([]string{}, baseArgs...)
	args = append(args, prompt)
	if len(t.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, t.AllowedTools...)
	}
	if len(t.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, t.DisallowedTools...)
	}
	if t.SupportsPermissionPrompt && len(opts) > 0 && opts[0].MCPConfigPath != "" {
		args = append(args, "--mcp-config", opts[0].MCPConfigPath)
		args = append(args, "--permission-prompt-tool", "mcp__copycat-auth__handle_permission")
	}
	return exec.Command(t.Command, args...)
}

func (t *AITool) BuildCommandContext(ctx context.Context, prompt string, baseArgs []string, opts ...CommandOptions) *exec.Cmd {
	args := append([]string{}, baseArgs...)
	args = append(args, prompt)
	if len(t.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, t.AllowedTools...)
	}
	if len(t.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, t.DisallowedTools...)
	}
	if t.SupportsPermissionPrompt && len(opts) > 0 && opts[0].MCPConfigPath != "" {
		args = append(args, "--mcp-config", opts[0].MCPConfigPath)
		args = append(args, "--permission-prompt-tool", "mcp__copycat-auth__handle_permission")
	}
	return exec.CommandContext(ctx, t.Command, args...)
}

type AIToolsConfig struct {
	Default string   `yaml:"default"`
	Tools   []AITool `yaml:"tools"`
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if cfg.GitHub.Organization == "" {
		return nil, fmt.Errorf("organization is required in %s", filename)
	}

	if cfg.Parallelism <= 0 {
		cfg.Parallelism = 3
	}
	if cfg.Parallelism > 10 {
		cfg.Parallelism = 10
	}

	if len(cfg.AIToolsConfig.Tools) == 0 {
		return nil, fmt.Errorf("no AI tools defined in %s", filename)
	}

	toolNames := make(map[string]struct{}, len(cfg.AIToolsConfig.Tools))
	for _, tool := range cfg.AIToolsConfig.Tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("an AI tool in %s is missing a name", filename)
		}
		if tool.Command == "" {
			return nil, fmt.Errorf("AI tool %q is missing a command in %s", tool.Name, filename)
		}
		if _, exists := toolNames[tool.Name]; exists {
			return nil, fmt.Errorf("duplicate AI tool name %q in %s", tool.Name, filename)
		}
		toolNames[tool.Name] = struct{}{}
	}

	if cfg.AIToolsConfig.Default == "" {
		cfg.AIToolsConfig.Default = cfg.AIToolsConfig.Tools[0].Name
	} else if _, exists := toolNames[cfg.AIToolsConfig.Default]; !exists {
		return nil, fmt.Errorf("default AI tool %q is not defined in %s", cfg.AIToolsConfig.Default, filename)
	}

	return &cfg, nil
}

func (c *AIToolsConfig) ToolByName(name string) (*AITool, bool) {
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			return &c.Tools[i], true
		}
	}

	return nil, false
}

// Save writes the configuration to a file with readable formatting.
func (c *Config) Save(filename string) error {
	// Marshal each section separately to add spacing between them
	githubData, err := yaml.Marshal(map[string]GitHubConfig{"github": c.GitHub})
	if err != nil {
		return fmt.Errorf("failed to encode github config: %w", err)
	}

	toolsData, err := yaml.Marshal(map[string][]AITool{"tools": c.Tools})
	if err != nil {
		return fmt.Errorf("failed to encode tools config: %w", err)
	}

	// Combine with blank lines between sections
	data := string(githubData) + "\n" + string(toolsData)

	if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", filename, err)
	}

	return nil
}

// LoadProjects reads and unmarshals a projects YAML file.
func LoadProjects(filename string) ([]Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Projects []Project `yaml:"projects"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse projects file %s: %w", filename, err)
	}

	return wrapper.Projects, nil
}

// SaveProjects marshals and writes projects to a YAML file.
func SaveProjects(filename string, projects []Project) error {
	data, err := yaml.Marshal(map[string][]Project{"projects": projects})
	if err != nil {
		return fmt.Errorf("failed to encode projects: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o600); err != nil {
		return fmt.Errorf("failed to write projects to %s: %w", filename, err)
	}

	return nil
}
