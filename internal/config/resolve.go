package config

import (
	"sort"
	"strings"
)

// FindProjectByNameOrAlias resolves a project by name or alias.
// Returns the matched project, up to 3 suggestions if no match, and whether a match was found.
// Resolution order: exact repo name, exact alias, case-insensitive substring on repo name.
func FindProjectByNameOrAlias(name string, projects []Project) (Project, []string, bool) {
	nameLower := strings.ToLower(name)

	// 1. Exact match on Repo
	for _, p := range projects {
		if p.Repo == name {
			return p, nil, true
		}
	}

	// 2. Exact match on any Alias
	for _, p := range projects {
		for _, alias := range p.Aliases {
			if alias == name {
				return p, nil, true
			}
		}
	}

	// 3. Case-insensitive substring match on Repo
	for _, p := range projects {
		if strings.Contains(strings.ToLower(p.Repo), nameLower) {
			return p, nil, true
		}
	}

	// 4. No match — return Levenshtein-based suggestions
	suggestions := closestNames(name, projects, 3)
	return Project{}, suggestions, false
}

// closestNames returns up to n project repo names sorted by Levenshtein distance to the query.
func closestNames(query string, projects []Project, n int) []string {
	type scored struct {
		name string
		dist int
	}

	queryLower := strings.ToLower(query)
	var candidates []scored
	for _, p := range projects {
		d := levenshtein(queryLower, strings.ToLower(p.Repo))
		candidates = append(candidates, scored{name: p.Repo, dist: d})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	var result []string
	for i := 0; i < n && i < len(candidates); i++ {
		result = append(result, candidates[i].name)
	}
	return result
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
