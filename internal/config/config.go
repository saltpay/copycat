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
