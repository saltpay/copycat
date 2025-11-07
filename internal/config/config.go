package config

import (
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Repo           string   `yaml:"repo"`
	SlackRoom      string   `yaml:"slack_room"`
	RequiresTicket bool     `yaml:"requires_ticket"`
	Topics         []string `yaml:"topics,omitempty"`
}

type GitHubConfig struct {
	Organization         string `yaml:"organization"`
	AutoDiscoveryTopic   string `yaml:"auto_discovery_topic"`
	RequiresTicketTopic  string `yaml:"requires_ticket_topic"`
	SlackRoomTopicPrefix string `yaml:"slack_room_topic_prefix"`
}

type Config struct {
	GitHub        GitHubConfig `yaml:"github"`
	AIToolsConfig `yaml:",inline"`
}

type AITool struct {
	Name        string   `yaml:"name"`
	Command     string   `yaml:"command"`
	CodeArgs    []string `yaml:"code_args"`
	SummaryArgs []string `yaml:"summary_args"`
}

func (t *AITool) BuildCommand(prompt string, baseArgs []string) *exec.Cmd {
	args := append([]string{}, baseArgs...)
	args = append(args, prompt)
	return exec.Command(t.Command, args...)
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
