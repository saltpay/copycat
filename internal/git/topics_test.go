package git

import (
	"github.com/saltpay/copycat/internal/config"
	"reflect"
	"testing"
)

func TestDeduplicate(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  []string
	}{
		{
			name:  "empty list",
			items: []string{},
			want:  []string{},
		},
		{
			name:  "single item",
			items: []string{"topic-a"},
			want:  []string{"topic-a"},
		},
		{
			name:  "no duplicates",
			items: []string{"topic-a", "topic-b", "topic-c"},
			want:  []string{"topic-a", "topic-b", "topic-c"},
		},
		{
			name:  "with duplicates",
			items: []string{"topic-a", "topic-b", "topic-a", "topic-c", "topic-b"},
			want:  []string{"topic-a", "topic-b", "topic-c"},
		},
		{
			name:  "empty strings filtered out",
			items: []string{"topic-a", "", "topic-b", ""},
			want:  []string{"topic-a", "topic-b"},
		},
		{
			name:  "all empty strings",
			items: []string{"", "", ""},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicate(tt.items)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deduplicate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummarizeTopics(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{
			name:  "empty list",
			items: []string{},
			want:  "none",
		},
		{
			name:  "single topic",
			items: []string{"topic-a"},
			want:  "topic-a",
		},
		{
			name:  "multiple topics",
			items: []string{"topic-a", "topic-b", "topic-c"},
			want:  "topic-a, topic-b, topic-c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeTopics(tt.items)
			if got != tt.want {
				t.Errorf("summarizeTopics() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNotFoundResponse(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "empty string",
			output: "",
			want:   false,
		},
		{
			name:   "404 not found",
			output: "HTTP 404: Not Found (https://api.github.com/repos/org/repo)",
			want:   true,
		},
		{
			name:   "404 lowercase",
			output: "http 404: not found",
			want:   true,
		},
		{
			name:   "404 only",
			output: "404",
			want:   false,
		},
		{
			name:   "not found only",
			output: "not found",
			want:   false,
		},
		{
			name:   "success message",
			output: "Successfully updated topics",
			want:   false,
		},
		{
			name:   "other error",
			output: "permission denied",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotFoundResponse(tt.output)
			if got != tt.want {
				t.Errorf("isNotFoundResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeTopicChanges(t *testing.T) {
	tests := []struct {
		name        string
		existing    []string
		project     config.Project
		githubCfg   config.GitHubConfig
		wantAdd     []string
		wantRemove  []string
		description string
	}{
		{
			name:     "add auto-discovery topic when missing",
			existing: []string{},
			project:  config.Project{Repo: "service-a", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic: "copycat",
			},
			wantAdd:     []string{"copycat"},
			wantRemove:  nil,
			description: "Should add auto-discovery topic when not present",
		},
		{
			name:     "auto-discovery topic already exists",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic: "copycat",
			},
			wantAdd:     nil,
			wantRemove:  nil,
			description: "Should not add auto-discovery topic if already present",
		},
		{
			name:     "add requires-ticket topic",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", RequiresTicket: true},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			wantAdd:     []string{"requires-ticket"},
			wantRemove:  nil,
			description: "Should add requires-ticket topic when RequiresTicket is true",
		},
		{
			name:     "remove requires-ticket topic",
			existing: []string{"copycat", "requires-ticket"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			wantAdd:     nil,
			wantRemove:  []string{"requires-ticket"},
			description: "Should remove requires-ticket topic when RequiresTicket is false",
		},
		{
			name:     "sync project topics - add project topics",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false, Topics: []string{"backend", "golang"}},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "",
			},
			wantAdd:     []string{"backend", "golang"},
			wantRemove:  nil,
			description: "Should add topics from project's Topics field",
		},
		{
			name:     "sync project topics - remove non-project topics",
			existing: []string{"copycat", "backend", "golang", "frontend", "vue"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false, Topics: []string{"backend", "golang"}},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "",
			},
			wantAdd:     nil,
			wantRemove:  []string{"frontend", "vue"},
			description: "Should remove topics not in project's Topics field (excluding system topics)",
		},
		{
			name:     "sync project topics - add missing project topics, remove extra topics",
			existing: []string{"copycat", "frontend", "vue", "old-topic"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false, Topics: []string{"backend", "golang"}},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "",
			},
			wantAdd:     []string{"backend", "golang"},
			wantRemove:  []string{"frontend", "vue", "old-topic"},
			description: "Should add missing project topics and remove non-project topics",
		},
		{
			name:     "sync project topics - preserve system topics",
			existing: []string{"copycat", "requires-ticket", "backend", "frontend"},
			project:  config.Project{Repo: "service-a", RequiresTicket: true, Topics: []string{"backend"}},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			wantAdd:     nil,
			wantRemove:  []string{"frontend"},
			description: "Should preserve system topics and only remove non-project, non-system topics",
		},
		{
			name:     "empty config values",
			existing: []string{"some-topic"},
			project:  config.Project{Repo: "service-a", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "",
				RequiresTicketTopic: "",
			},
			wantAdd:     nil,
			wantRemove:  nil,
			description: "Should handle empty config values gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAdd, gotRemove := computeTopicChanges(tt.existing, tt.project, tt.githubCfg)

			if !equalSlices(gotAdd, tt.wantAdd) {
				t.Errorf("computeTopicChanges() add = %v, want %v\nDescription: %s", gotAdd, tt.wantAdd, tt.description)
			}

			if !equalSlices(gotRemove, tt.wantRemove) {
				t.Errorf("computeTopicChanges() remove = %v, want %v\nDescription: %s", gotRemove, tt.wantRemove, tt.description)
			}
		})
	}
}

func TestSyncTopicsWithCacheEmptyProjects(t *testing.T) {
	githubCfg := config.GitHubConfig{
		Organization:       "test-org",
		AutoDiscoveryTopic: "copycat",
	}

	err := SyncTopicsWithCache([]config.Project{}, githubCfg)
	if err != nil {
		t.Errorf("SyncTopicsWithCache() with empty projects should not error, got: %v", err)
	}
}

func TestSyncProjectSpecificTopics(t *testing.T) {
	tests := []struct {
		name           string
		existing       []string
		project        config.Project
		githubCfg      config.GitHubConfig
		expectedAdd    []string
		expectedRemove []string
		description    string
	}{
		{
			name:     "sync project topics exactly",
			existing: []string{"copycat", "frontend", "old-topic", "backend", "javascript"},
			project: config.Project{
				Repo:   "test-repo",
				Topics: []string{"backend", "golang", "microservice"},
			},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			expectedAdd:    []string{"golang", "microservice"},
			expectedRemove: []string{"frontend", "old-topic", "javascript"},
			description:    "Should sync project topics exactly, adding missing ones and removing non-project ones",
		},
		{
			name:     "project without Topics field (not managed)",
			existing: []string{"copycat", "frontend", "old-topic", "backend", "javascript"},
			project: config.Project{
				Repo:           "test-repo",
				RequiresTicket: false, // RequiresTicket is false, so won't add requires-ticket
				Topics:         nil,   // Explicitly nil
			},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			expectedAdd:    nil,
			expectedRemove: []string{}, // Should not remove non-system topics since Topics is nil
			description:    "Should not manage project-specific topics when Topics field is nil",
		},
		{
			name:     "project with empty Topics slice",
			existing: []string{"copycat", "frontend", "old-topic", "backend", "javascript"},
			project: config.Project{
				Repo:           "test-repo",
				RequiresTicket: false,      // RequiresTicket is false, so won't add requires-ticket
				Topics:         []string{}, // Empty but not nil
			},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			expectedAdd:    nil,
			expectedRemove: []string{"frontend", "old-topic", "backend", "javascript"}, // Remove all non-system topics
			description:    "Should remove all non-system topics when empty Topics slice is provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addTopics, removeTopics := computeTopicChanges(tt.existing, tt.project, tt.githubCfg)

			if !equalSlices(addTopics, tt.expectedAdd) {
				t.Errorf("computeTopicChanges() add = %v, want %v\nDescription: %s", addTopics, tt.expectedAdd, tt.description)
			}

			if !equalSlices(removeTopics, tt.expectedRemove) {
				t.Errorf("computeTopicChanges() remove = %v, want %v\nDescription: %s", removeTopics, tt.expectedRemove, tt.description)
			}
		})
	}
}

// Helper function to compare slices ignoring order
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	if len(a) == 0 && len(b) == 0 {
		return true
	}

	// Create maps for comparison
	aMap := make(map[string]int)
	bMap := make(map[string]int)

	for _, item := range a {
		aMap[item]++
	}

	for _, item := range b {
		bMap[item]++
	}

	return reflect.DeepEqual(aMap, bMap)
}
