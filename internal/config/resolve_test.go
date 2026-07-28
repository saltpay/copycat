package config

import (
	"testing"
)

func TestFindProjectByNameOrAlias_ExactRepoMatch(t *testing.T) {
	projects := []Project{
		{Repo: "merchant-account-service"},
		{Repo: "payment-gateway"},
	}

	got, _, found := FindProjectByNameOrAlias("merchant-account-service", projects)
	if !found {
		t.Fatal("expected match, got none")
	}
	if got.Repo != "merchant-account-service" {
		t.Fatalf("expected merchant-account-service, got %s", got.Repo)
	}
}

func TestFindProjectByNameOrAlias_ExactAliasMatch(t *testing.T) {
	projects := []Project{
		{Repo: "merchant-account-service", Aliases: []string{"merchant-account", "mas"}},
		{Repo: "payment-gateway"},
	}

	got, _, found := FindProjectByNameOrAlias("merchant-account", projects)
	if !found {
		t.Fatal("expected match via alias, got none")
	}
	if got.Repo != "merchant-account-service" {
		t.Fatalf("expected merchant-account-service, got %s", got.Repo)
	}

	got, _, found = FindProjectByNameOrAlias("mas", projects)
	if !found {
		t.Fatal("expected match via short alias, got none")
	}
	if got.Repo != "merchant-account-service" {
		t.Fatalf("expected merchant-account-service, got %s", got.Repo)
	}
}

func TestFindProjectByNameOrAlias_SubstringMatch(t *testing.T) {
	projects := []Project{
		{Repo: "merchant-account-service"},
		{Repo: "payment-gateway"},
	}

	got, _, found := FindProjectByNameOrAlias("merchant-account", projects)
	if !found {
		t.Fatal("expected substring match, got none")
	}
	if got.Repo != "merchant-account-service" {
		t.Fatalf("expected merchant-account-service, got %s", got.Repo)
	}
}

func TestFindProjectByNameOrAlias_CaseInsensitiveSubstring(t *testing.T) {
	projects := []Project{
		{Repo: "Payment-Gateway"},
	}

	got, _, found := FindProjectByNameOrAlias("payment", projects)
	if !found {
		t.Fatal("expected case-insensitive match, got none")
	}
	if got.Repo != "Payment-Gateway" {
		t.Fatalf("expected Payment-Gateway, got %s", got.Repo)
	}
}

func TestFindProjectByNameOrAlias_NoMatch_ReturnsSuggestions(t *testing.T) {
	projects := []Project{
		{Repo: "merchant-account-service"},
		{Repo: "merchant-api"},
		{Repo: "payment-gateway"},
		{Repo: "auth-service"},
	}

	_, suggestions, found := FindProjectByNameOrAlias("merchant-acount", projects)
	if found {
		t.Fatal("expected no match for typo")
	}
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions, got none")
	}
	if len(suggestions) > 3 {
		t.Fatalf("expected at most 3 suggestions, got %d", len(suggestions))
	}
	// The closest match should be merchant-account-service or merchant-api
	if suggestions[0] != "merchant-api" && suggestions[0] != "merchant-account-service" {
		t.Fatalf("expected a merchant-* suggestion first, got %s", suggestions[0])
	}
}

func TestFindProjectByNameOrAlias_EmptyProjects(t *testing.T) {
	_, suggestions, found := FindProjectByNameOrAlias("anything", nil)
	if found {
		t.Fatal("expected no match on empty list")
	}
	if len(suggestions) != 0 {
		t.Fatalf("expected no suggestions on empty list, got %d", len(suggestions))
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestGitSuffixNormalization(t *testing.T) {
	// This tests the normalization that happens in LoadProjects.
	// We test the TrimSuffix logic directly since LoadProjects needs a file.
	tests := []struct {
		input string
		want  string
	}{
		{"repo-name", "repo-name"},
		{"repo-name.git", "repo-name"},
		{"repo.git.git", "repo.git"},
	}
	for _, tt := range tests {
		got := trimGitSuffix(tt.input)
		if got != tt.want {
			t.Errorf("trimGitSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
