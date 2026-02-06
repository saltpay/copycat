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

1. **GitHub Repository Topics**
   - Supports metadata: Slack channels

2. **Main Application (`main.go`)**
   - Interactive CLI using `promptui`
   - Two primary workflows: GitHub Issues and Local Changes
   - Stateless execution model
   - Auto-cleanup of temporary resources

3. **Integration Points**
   - **GitHub CLI (`gh`)**: Issue/PR management
   - **Claude CLI (`claude`)**: AI-powered code changes
   - **Git**: Repository operations

### Key Design Patterns

- **Input Collection Phase**: All user inputs gathered upfront before any operations
- **Batch Processing**: Iterates through selected repositories sequentially
- **Auto-cleanup**: Temporary `repos/` directory removed after processing
- **Error Tolerance**: Continues processing remaining repos if one fails
- **Non-interactive Mode**: Claude runs with `--permission-mode acceptEdits`

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

1. **Branch Naming**: `copycat-YYYYMMDD-HHMMSS` (timestamp-based)
2. **Commit Messages**: Use PR title as commit message
3. **PR Titles**: `Description` (users are reminded they may include a ticket reference)
4. **Repository Cloning**: Always use SSH URLs
5. **Directory Structure**: Temporary clones in `repos/` subdirectory

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
- `github.com/manifoldco/promptui`: Interactive CLI prompts
- `gopkg.in/yaml.v3`: YAML configuration parsing

Keep dependencies minimal. Prefer standard library when possible.

## Business Rules

### Pull Request Creation

- **Auto-generated descriptions**: Claude generates PR body from changes
- **Length limit**: PR descriptions truncated at 2000 characters
- **Base branch**: Dynamically detected (usually `main` or `master`)

### GitHub Issues Workflow

- **Auto-assignment**: Issues assigned to `@copilot`
- **Bulk creation**: Creates issues across all selected repos
- **Warning**: Copilot doesn't sign commits (displayed to user)

## AI Assistant Guidelines

When working on this codebase:

1. **Preserve the core workflow**: Input collection → Processing → Cleanup
2. **Maintain error tolerance**: Individual failures shouldn't block batch operations
3. **Keep it simple**: Avoid over-engineering for edge cases
4. **User experience matters**: Clear prompts and progress indicators
5. **Test thoroughly**: Especially git operations and cleanup
6. **Document business rules**: Clearly
7. **Consider scale**: Changes should work for 1 repo or 100 repos
8. **Security first**: Never compromise authentication or credentials
