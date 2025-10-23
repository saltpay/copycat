package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Repo      string `yaml:"repo"`
	SlackRoom string `yaml:"slack_room"`
	InCDE     bool   `yaml:"in_cde"`
}

type ProjectConfig struct {
	Projects []Project `yaml:"projects"`
}

type AITool struct {
	Name        string   `yaml:"name"`
	Command     string   `yaml:"command"`
	CodeArgs    []string `yaml:"code_args"`
	SummaryArgs []string `yaml:"summary_args"`
}

type AIToolsConfig struct {
	Default string   `yaml:"default"`
	Tools   []AITool `yaml:"tools"`
}

func LoadProjects(filename string) ([]Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	var config ProjectConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return config.Projects, nil
}

func LoadAITools(filename string) (*AIToolsConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	var config AIToolsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(config.Tools) == 0 {
		return nil, fmt.Errorf("no AI tools defined in %s", filename)
	}

	toolNames := make(map[string]struct{}, len(config.Tools))
	for _, tool := range config.Tools {
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

	if config.Default == "" {
		config.Default = config.Tools[0].Name
	} else {
		if _, exists := toolNames[config.Default]; !exists {
			return nil, fmt.Errorf("default AI tool %q is not defined in %s", config.Default, filename)
		}
	}

	return &config, nil
}

func (c *AIToolsConfig) ToolByName(name string) (*AITool, bool) {
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			return &c.Tools[i], true
		}
	}

	return nil, false
}
