package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type cachedProject struct {
	Repo      string   `yaml:"repo"`
	SlackRoom string   `yaml:"slack_room"`
	Topics    []string `yaml:"topics,omitempty"`
}

type projectCache struct {
	Projects []cachedProject `yaml:"projects"`
}

type catalogEntry struct {
	Name           string `json:"name"`
	Communications struct {
		GeneralIM struct {
			Provider string `json:"provider"`
			Channel  string `json:"channel"`
		} `json:"general_im"`
	} `json:"communications"`
}

func main() {
	catalogPath := flag.String("catalog", "", "Path to saltpay-app-catalog directory")
	projectsFile := flag.String("projects", ".projects.yaml", "Path to .projects.yaml file")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatal("--catalog flag is required")
	}

	// Load existing projects
	projects, err := loadProjects(*projectsFile)
	if err != nil {
		log.Fatalf("Failed to load projects: %v", err)
	}

	// Build a map of repo name -> catalog entry
	catalog, err := loadCatalog(*catalogPath)
	if err != nil {
		log.Fatalf("Failed to load catalog: %v", err)
	}

	fmt.Printf("Loaded %d projects and %d catalog entries\n\n", len(projects.Projects), len(catalog))

	// Update slack rooms
	updated := 0
	notFound := 0
	noChannel := 0

	for i := range projects.Projects {
		project := &projects.Projects[i]
		entry, found := catalog[project.Repo]

		if !found {
			fmt.Printf("  [NOT FOUND] %s - no catalog entry\n", project.Repo)
			notFound++
			continue
		}

		channel := entry.Communications.GeneralIM.Channel
		if channel == "" {
			fmt.Printf("  [NO CHANNEL] %s - catalog entry has no general_im.channel\n", project.Repo)
			noChannel++
			continue
		}

		// Ensure channel has # prefix
		if !strings.HasPrefix(channel, "#") {
			channel = "#" + channel
		}

		oldRoom := project.SlackRoom
		if oldRoom == channel {
			fmt.Printf("  [UNCHANGED] %s - already set to %s\n", project.Repo, channel)
			continue
		}

		project.SlackRoom = channel
		updated++

		if oldRoom == "" || oldRoom == "#none" {
			fmt.Printf("  [UPDATED] %s - set to %s\n", project.Repo, channel)
		} else {
			fmt.Printf("  [UPDATED] %s - changed from %s to %s\n", project.Repo, oldRoom, channel)
		}
	}

	fmt.Printf("\nSummary: %d updated, %d not found in catalog, %d without channel\n", updated, notFound, noChannel)

	if updated == 0 {
		fmt.Println("\nNo changes to write")
		return
	}

	// Sort projects by repo name for stable output
	sort.Slice(projects.Projects, func(i, j int) bool {
		return projects.Projects[i].Repo < projects.Projects[j].Repo
	})

	// Write updated projects
	if err := saveProjects(*projectsFile, projects); err != nil {
		log.Fatalf("Failed to save projects: %v", err)
	}

	fmt.Printf("\nUpdated %s\n", *projectsFile)
}

func loadProjects(filename string) (*projectCache, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cache projectCache
	if err := yaml.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &cache, nil
}

func saveProjects(filename string, cache *projectCache) error {
	data, err := yaml.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func loadCatalog(catalogPath string) (map[string]catalogEntry, error) {
	catalog := make(map[string]catalogEntry)

	// Only read JSON files from the root directory (not subdirectories)
	entries, err := os.ReadDir(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog directory: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(catalogPath, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}

		var entry catalogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			// Skip files that don't match our expected structure
			continue
		}

		if entry.Name != "" {
			catalog[entry.Name] = entry
		}
	}

	return catalog, nil
}
