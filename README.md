# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

<img src="./copycat-logo.png"
     alt="Copycat logo"
     style="margin-bottom: 10px; animation: spin 2s linear infinite; transform-origin: center center;" />

Copycat is an automation tool that enables you to apply consistent changes across multiple GitHub repositories using Claude AI. It streamlines the process of creating issues or performing code changes at scale.

## Requirements

In order to run Copycat, you need to have the following installed:

- **Go** (1.16 or later)
- **GitHub CLI** (`gh`) - [Installation guide](https://cli.github.com/)
- **Claude CLI** (`claude`) - [Installation guide](https://docs.claude.com/en/docs/claude-code)

### Authentication Setup

Before using Copycat, ensure you're authenticated:

```bash
# Authenticate with GitHub
gh auth login

# Authenticate with Claude (if required)
claude auth login
```

## Configuration

Create a `projects.yaml` file in the same directory as `main.go` with your repository configuration:

```yaml
projects:
  - repo: "repo-name-1"
    slack_room: "#team-channel"
    in_cde: false
  - repo: "repo-name-2"
    slack_room: "#another-channel"
    in_cde: true
```

### Configuration Fields

- `repo`: Repository name (without the org prefix - org is hardcoded as "saltpay")
- `slack_room`: Associated Slack channel for the project
- `in_cde`: Boolean flag indicating if the project is in CDE (requires Jira ticket in PR title)

## Usage

### Quick Start

```bash
# Run the tool
go run main.go

# Or build and run
go build -o copycat
./copycat
```

### Workflow Options

Copycat offers two main workflows:

#### 1. Create GitHub Issues

Creates GitHub issues across selected repositories and assigns them to @copilot.

**Steps:**
1. Select repositories from the list (or type "all")
2. Choose "Create GitHub Issues"
3. Enter issue title
4. Enter issue description
5. Issues are created automatically

**Note:** The Copilot agent does not sign commits, so you'll need to fix unsigned commits before merging.

#### 2. Perform Changes Locally

Clones repositories, applies changes using Claude AI, and creates pull requests.

**Steps:**
1. Select repositories from the list (or type "all")
2. Choose "Perform Changes Locally"
3. If any selected project has `in_cde: true`, enter a Jira ticket (e.g., PROJ-123)
4. Enter PR title
5. Enter the Claude prompt:
   - **Single line**: Type or paste the prompt and press Enter
   - **Editor**: Opens your default editor (set via `$EDITOR` env var, defaults to vim)
6. Copycat will:
   - Clone all selected repositories to `repos/` directory
   - Create a timestamped branch (e.g., `copycat-20231015-150405`)
   - Run Claude CLI to analyze and apply changes
   - Generate PR description automatically
   - Commit and push changes
   - Create pull requests
   - Clean up cloned repositories

### Project Selection

When prompted for project numbers:
- Enter specific numbers: `1,3,5`
- Enter a range: `1,2,3,4`
- Select all projects: `all`
- Skip selection: press Enter

### Branch Naming

Branches are automatically named with the format: `copycat-YYYYMMDD-HHMMSS`

Example: `copycat-20231015-150405`

### Pull Request Titles

- **Regular projects**: Uses the PR title you provide
- **CDE projects** (where `in_cde: true`): Automatically prepends Jira ticket: `PROJ-123 - Your PR Title`

## How It Works

### Local Changes Workflow

1. **Input Collection Phase**
   - Collects all user inputs upfront (Jira ticket, PR title, Claude prompt)
   - Validates inputs before processing

2. **Repository Processing Phase**
   - Cleans up existing `repos/` directory
   - Clones selected repositories via SSH
   - Creates a new timestamped branch
   - Runs Claude CLI in non-interactive mode with `--permission-mode acceptEdits`
   - Captures Claude's output for PR description

3. **PR Generation Phase**
   - Uses Claude to generate a concise PR description (2-3 sentences)
   - Commits changes using the PR title as commit message
   - Pushes branch to origin
   - Detects default branch automatically
   - Creates PR using GitHub CLI
   - Cleans up local repository clone

### GitHub Issues Workflow

1. Collects issue title and description
2. Uses `gh issue create` to create issues
3. Automatically assigns issues to @copilot
4. Provides URLs of created issues

## Directory Structure

```
copycat/
├── main.go              # Main application code
├── projects.yaml        # Repository configuration
├── go.mod              # Go module dependencies
├── go.sum              # Go module checksums
├── README.md           # This file
├── copycat-logo.png    # Logo image
└── repos/              # Temporary directory for cloned repos (auto-cleaned)
```

## Troubleshooting

### Common Issues

**Git clone fails:**
- Ensure you have SSH access to the repositories
- Check your SSH keys: `ssh -T git@github.com`

**Claude CLI fails:**
- Verify Claude CLI is installed and in your PATH
- Check Claude authentication
- Ensure repositories have proper file permissions

**PR creation fails:**
- Verify you're authenticated with GitHub CLI: `gh auth status`
- Check that you have write access to the repositories
- Ensure the base branch exists

**No changes detected:**
- Claude may not have made any modifications
- Review your prompt for clarity
- Check if the prompt applies to the specific repository

## Best Practices

1. **Test with a single repository first** before running on all projects
2. **Use clear, specific prompts** for Claude
3. **Review changes** before merging PRs
4. **Keep Jira tickets handy** for CDE projects
5. **Use the editor option** for complex multi-line prompts

## Examples

### Example Claude Prompts

**Add a new dependency:**
```
Add the package "github.com/stretchr/testify" to go.mod and run go mod tidy
```

**Update documentation:**
```
Add a CONTRIBUTING.md file with guidelines for: code style (use gofmt), testing requirements (minimum 80% coverage), and PR process (requires 2 approvals)
```

**Refactor code:**
```
Rename all instances of the function 'processData' to 'transformData' across the codebase. Ensure all tests are updated accordingly.
```

## Security Notes

- Copycat uses SSH for git operations
- Credentials are managed via GitHub CLI and Claude CLI
- No credentials are stored by Copycat
- Repository clones are cleaned up after processing

## License

[Add your license information here]
