package git

import (
	"copycat/internal/config"
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
		name          string
		existing      []string
		project       config.Project
		githubCfg     config.GitHubConfig
		wantAdd       []string
		wantRemove    []string
		description   string
	}{
		{
			name:     "add auto-discovery topic when missing",
			existing: []string{},
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
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
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
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
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: true},
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
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:  "copycat",
				RequiresTicketTopic: "requires-ticket",
			},
			wantAdd:     nil,
			wantRemove:  []string{"requires-ticket"},
			description: "Should remove requires-ticket topic when RequiresTicket is false",
		},
		{
			name:     "add slack room topic",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"slack-team-alpha"},
			wantRemove:  nil,
			description: "Should add slack topic with prefix (# stripped)",
		},
		{
			name:     "update slack room topic",
			existing: []string{"copycat", "slack-old-channel"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#new-channel", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"slack-new-channel"},
			wantRemove:  []string{"slack-old-channel"},
			description: "Should replace old slack topic with new one",
		},
		{
			name:     "remove slack topic when slack room is empty",
			existing: []string{"copycat", "slack-old-channel"},
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     nil,
			wantRemove:  []string{"slack-old-channel"},
			description: "Should remove slack topic when project has no slack room",
		},
		{
			name:     "slack room with # prefix is stripped",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"slack-team-alpha"},
			wantRemove:  nil,
			description: "Should strip # prefix from slack room name",
		},
		{
			name:     "complex scenario with multiple changes",
			existing: []string{"copycat", "slack-old-channel", "other-topic"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#new-channel", RequiresTicket: true},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				RequiresTicketTopic:  "requires-ticket",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"requires-ticket", "slack-new-channel"},
			wantRemove:  []string{"slack-old-channel"},
			description: "Should handle multiple topic changes at once",
		},
		{
			name:     "no changes needed",
			existing: []string{"copycat", "requires-ticket", "slack-team-alpha"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#team-alpha", RequiresTicket: true},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				RequiresTicketTopic:  "requires-ticket",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     nil,
			wantRemove:  nil,
			description: "Should return empty when all topics are correct",
		},
		{
			name:     "empty config values",
			existing: []string{"some-topic"},
			project:  config.Project{Repo: "service-a", SlackRoom: "", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "",
				RequiresTicketTopic:  "",
				SlackRoomTopicPrefix: "",
			},
			wantAdd:     nil,
			wantRemove:  nil,
			description: "Should handle empty config values gracefully",
		},
		{
			name:     "whitespace in slack room",
			existing: []string{"copycat"},
			project:  config.Project{Repo: "service-a", SlackRoom: "  #team-alpha  ", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"slack-team-alpha"},
			wantRemove:  nil,
			description: "Should trim whitespace from slack room",
		},
		{
			name:     "multiple slack topics removed",
			existing: []string{"copycat", "slack-channel-a", "slack-channel-b"},
			project:  config.Project{Repo: "service-a", SlackRoom: "#new-channel", RequiresTicket: false},
			githubCfg: config.GitHubConfig{
				AutoDiscoveryTopic:   "copycat",
				SlackRoomTopicPrefix: "slack-",
			},
			wantAdd:     []string{"slack-new-channel"},
			wantRemove:  []string{"slack-channel-a", "slack-channel-b"},
			description: "Should remove all old slack topics with matching prefix",
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