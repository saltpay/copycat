package input

import (
	"copycat/internal/config"
	"fmt"
	"strconv"
	"strings"

	"github.com/manifoldco/promptui"
)

func SelectProjects(projects []config.Project) ([]config.Project, error) {
	var selected []config.Project

	fmt.Println("\nAvailable projects:")
	for i, project := range projects {
		fmt.Printf("%d. %s\n", i+1, project.Repo)
	}

	prompt := promptui.Prompt{
		Label: "Enter project numbers separated by commas (e.g., 1,2) or 'all' for all projects",
	}

	input, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	input = strings.TrimSpace(input)

	if input == "" {
		return selected, nil
	}

	// Check if user wants to select all projects
	if strings.ToLower(input) == "all" {
		return projects, nil
	}

	indices := strings.Split(input, ",")
	for _, indexStr := range indices {
		indexStr = strings.TrimSpace(indexStr)
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 1 || index > len(projects) {
			fmt.Printf("Invalid selection: %s\n", indexStr)
			continue
		}

		project := projects[index-1]
		alreadySelected := false
		for _, sel := range selected {
			if sel.Repo == project.Repo {
				alreadySelected = true
				break
			}
		}

		if !alreadySelected {
			selected = append(selected, project)
		}
	}

	return selected, nil
}
