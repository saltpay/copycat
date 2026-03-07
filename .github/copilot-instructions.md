# Copycat - AI Coding Assistant Instructions

## Project Overview

**Copycat** is a Go-based CLI automation tool that applies consistent changes across multiple GitHub repositories using Claude AI. It serves as a force multiplier for platform engineering teams managing numerous repositories.

### Business Intent

The primary goal of Copycat is to:
- **Scale code changes** across multiple repositories efficiently
- **Maintain consistency** in codebases, dependencies, and configurations
- **Reduce manual toil** in repetitive cross-repository tasks
- **Accelerate platform engineering** initiatives
- **Enable AI-driven code transformations** at scale

### Use Cases

- Dependency updates across multiple services
- Security patches and vulnerability fixes
- Configuration standardization
- Documentation updates
- Code refactoring and modernization
- Policy enforcement (linting rules, CI/CD configs)
- Migration tasks (API changes, framework upgrades)

## Architecture & Design

### Core Components

1. **TUI (`internal/input/`)**
   - Interactive CLI using [Bubble Tea](https://github.com/charmbracelet/bubbletea)
   - Dashboard manages phases: Projects → Wizard → Processing → Done
   - Sub-models communicate via custom messages (not `tea.Quit`)

2. **Two Workflows**
   - **Perform Changes**: clones repos, runs AI, commits, creates PRs
   - **Run Assessment**: clones repos, runs AI with a question, collects findings, generates cross-repo summary (no PRs)

3. **Integration Points**
   - **GitHub CLI (`gh`)**: PR management, topic sync (calls serialized via mutex)
   - **AI CLIs**: Claude, Codex, Qwen, Gemini (tool-agnostic config)
   - **Git**: Repository operations
   - **Slack**: PR notifications, assessment findings/summaries

### Key Design Patterns

- **Input Collection Phase**: All user inputs gathered upfront before any operations
- **Batch Processing**: Parallel processing with configurable checkpoint pauses between batches
- **Auto-cleanup**: Temporary `repos/` directory removed after processing
- **Error Tolerance**: Continues processing remaining repos if one fails
- **MCP Permission Prompting**: Non-allowlisted tool calls routed to TUI for user approval

## Code Style & Conventions

### Go Style Guidelines

- **Standard Go formatting**: Use `gofmt` for all code
- **Error handling**: Always check errors, log with context
- **Variable naming**:
  - Descriptive names for complex logic
  - Short names (`err`, `cmd`) for common patterns
- **Function organization**: Group related functions together
- **Comments**: Explain "why" not "what", especially for business logic

### Project-Specific Conventions

1. **Branch Naming**: User-specified; strategies are "reuse if exists" or "skip if exists"
2. **Commit Messages**: Use PR title as commit message
3. **PR Titles**: `Description` (users are reminded they may include a ticket reference)
4. **Repository Cloning**: Always use SSH URLs
5. **Directory Structure**: Temporary clones in `repos/` subdirectory
6. **Config Paths**: XDG-compliant (`~/Library/Application Support/copycat/` on macOS, `~/.config/copycat/` on Linux)

### Error Handling Philosophy

- **Log and continue**: Don't let one repository failure block others
- **User-friendly messages**: Prefix with ✓ or ⚠️ for clarity
- **Contextual errors**: Include repository name in error messages
- **Graceful degradation**: Fallback to defaults when possible (e.g., default branch detection)

## Development Guidelines

### When Adding Features

1. **Consider both workflows**: GitHub Issues and Local Changes
2. **Validate inputs early**: Before any git operations
3. **Maintain idempotency**: Operations should be safe to retry
4. **Add progress indicators**: Users should know what's happening
5. **Test error paths**: Ensure graceful handling of failures

### Testing Considerations

- **Test with a single repo first**: Before running on "all"
- **Verify cleanup**: Ensure `repos/` directory is removed
- **Check authentication**: Both `gh` and `claude` must be authenticated
- **SSH keys**: Required for git clone operations
- **Permission checks**: Validate write access to repositories

### Dependencies

Current dependencies (from `go.mod`):
- `github.com/charmbracelet/bubbletea`: TUI framework
- `github.com/charmbracelet/bubbles`: TUI components (text input, viewport, etc.)
- `github.com/charmbracelet/lipgloss`: TUI styling
- `github.com/atotto/clipboard`: Clipboard access
- `github.com/google/uuid`: UUID generation
- `gopkg.in/yaml.v3`: YAML configuration parsing

Keep dependencies minimal. Prefer standard library when possible.

## Business Rules

### Pull Request Creation

- **Auto-generated descriptions**: AI tool generates PR body from changes
- **Length limit**: PR descriptions truncated at 2000 characters
- **Base branch**: Dynamically detected via `git symbolic-ref refs/remotes/origin/HEAD`
- **Labels**: PRs are labeled with `copycat` (purple label, auto-created if missing)

## AI Assistant Guidelines

When working on this codebase:

1. **Read `CONTRIBUTING.md` first**: Before making changes, read it to understand the architecture, design decisions, and conventions
2. **Update `CONTRIBUTING.md`**: When making architectural changes, adding new patterns, or changing significant behavior, update CONTRIBUTING.md to reflect those changes
3. **Preserve the core workflow**: Input collection → Processing → Cleanup
4. **Maintain error tolerance**: Individual failures shouldn't block batch operations
5. **Keep it simple**: Avoid over-engineering for edge cases
6. **User experience matters**: Clear prompts and progress indicators
7. **Test thoroughly**: Especially git operations and cleanup
8. **Document business rules**: Clearly
9. **Consider scale**: Changes should work for 1 repo or 100 repos
10. **Security first**: Never compromise authentication or credentials
