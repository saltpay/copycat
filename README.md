# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

<img src="./copycat-logo.png"
     alt="Copycat logo"
     style="margin-bottom: 10px; animation: spin 2s linear infinite; transform-origin: center center;" />

Copycat is an automation tool that enables you to apply consistent changes across multiple GitHub repositories using AI coding assistants. It streamlines the process of creating issues or performing code changes at scale with support for multiple AI tools including Claude, Codex, Qwen, and others.

## Requirements

In order to run Copycat, you need to have the following installed:

- **Go** (1.16 or later)
- **GitHub CLI** (`gh`) - [Installation guide](https://cli.github.com/)
- **AI Coding Assistant** - At least one of:
  - **Claude CLI** (`claude`) - [Installation guide](https://docs.claude.com/en/docs/claude-code)
  - **Codex** (`codex`)
  - **Qwen** (`qwen`)
  - **Gemini** (`gemini`)
  - Or any other AI tool configured in `config.yaml`

### Authentication Setup

Before using Copycat, ensure you're authenticated:

```bash
# Authenticate with GitHub
gh auth login

# Authenticate with your chosen AI tool (example with Claude)
claude auth login
```

## Configuration

Copycat reads all settings from a single `config.yaml` file in the project root:

```yaml
github:
  organization: saltpay
  auto_discovery_topic: copycat-subject

tools:
  - name: claude
    command: claude
    code_args:
      - --permission-mode
      - acceptEdits
    summary_args: []
  - name: codex
    command: codex
    code_args:
      - exec
      - --full-auto
    summary_args: []
  - name: qwen
    command: qwen
    code_args:
      - --approval-mode
      - auto-edit
      - -p
    summary_args: []
```

### Configuration Fields

- `github.organization`: GitHub organization to scan for repositories
- `github.auto_discovery_topic` (optional): GitHub topic Copycat passes to `gh repo list`; when omitted Copycat lists all repositories
- `tools`: List of AI tools available in the selector
  - `name`: Identifier for the tool
  - `command`: CLI command to execute
  - `code_args`: Arguments passed when making code changes
  - `summary_args`: Arguments passed when generating PR descriptions (optional)

When Copycat lists repositories it uses the configured discovery topic if provided, otherwise it fetches every unarchived repository in the organization. Slack channels for notifications are configured per-project in the `.projects.yaml` cache file (see Slack Notifications section).

## Usage

### Quick Start

```bash
go run main.go

# Or build and run
go build -o copycat
./copycat
```

### Slack Notifications

Copycat can send Slack notifications to inform teams when PRs are created for their repositories. To enable this feature, set the `SLACK_BOT_TOKEN` environment variable:

```bash
# Run with Slack notifications enabled
SLACK_BOT_TOKEN=xoxb-your-bot-token go run main.go

# Or export for the session
export SLACK_BOT_TOKEN=xoxb-your-bot-token
go run main.go
```

**Requirements:**
- A Slack app with the `chat:write` scope
- The bot must be invited to channels where it will post

**Behavior:**
- If `SLACK_BOT_TOKEN` is not set, Slack notifications are skipped entirely
- Notifications are grouped by Slack channel (one message per channel)
- You will be prompted to confirm before sending notifications

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

Clones repositories, applies changes using your configured AI coding assistant, and creates pull requests.

**Steps:**
1. Select repositories from the list (or type "all")
2. Choose "Perform Changes Locally"
3. Enter PR title (you may include a ticket reference, e.g., `PROJ-123 - Title`)
4. Enter the AI prompt:
   - **Single line**: Type or paste the prompt and press Enter
   - **Editor**: Opens your default editor (set via `$EDITOR` env var, defaults to vim)
6. Copycat will:
   - Clone all selected repositories to `repos/` directory
   - Create a timestamped branch (e.g., `copycat-20231015-150405`)
   - Run your chosen AI tool to analyze and apply changes
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

Pull requests use the title you provide. You may optionally include a ticket or issue reference (e.g., `PROJ-123 - Your PR Title`).

## How It Works

### Local Changes Workflow

1. **Input Collection Phase**
   - Collects all user inputs upfront (PR title, AI prompt)
   - Validates inputs before processing

2. **Repository Processing Phase**
   - Cleans up existing `repos/` directory
   - Clones selected repositories via SSH
   - Creates a new timestamped branch
   - Runs your configured AI tool with appropriate arguments (from `config.yaml`)
   - Captures AI output for PR description

3. **PR Generation Phase**
   - Uses your AI tool to generate a concise PR description (2-3 sentences)
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
├── main.go                     # Main application code
├── config.yaml                 # Combined configuration (GitHub + AI tools)
├── internal/
│   ├── ai/
│   │   └── ai.go              # AI tool integration logic
│   ├── config/
│   │   ├── config.go          # Configuration loading
│   │   └── config_test.go     # Configuration tests
│   ├── input/
│   │   └── ai_prompt.go       # User input handling
│   ├── git/
│   └── filesystem/
├── go.mod                      # Go module dependencies
├── go.sum                      # Go module checksums
├── README.md                   # This file
├── copycat-logo.png            # Logo image
└── repos/                      # Temporary directory for cloned repos (auto-cleaned)
```

## Troubleshooting

### Common Issues

**Git clone fails:**
- Ensure you have SSH access to the repositories
- Check your SSH keys: `ssh -T git@github.com`

**AI tool fails:**
- Verify your chosen AI tool is installed and in your PATH
- Check authentication with your AI tool
- Ensure repositories have proper file permissions
- Verify the AI tool configuration in `config.yaml` is correct

**PR creation fails:**
- Verify you're authenticated with GitHub CLI: `gh auth status`
- Check that you have write access to the repositories
- Ensure the base branch exists

**No changes detected:**
- Your AI tool may not have made any modifications
- Review your prompt for clarity
- Check if the prompt applies to the specific repository
- Verify the AI tool is configured correctly with appropriate arguments

## Best Practices

1. **Test with a single repository first** before running on all projects
2. **Use clear, specific prompts** for your AI tool
3. **Review changes** before merging PRs
4. **Include ticket references** in PR titles when relevant for tracking
5. **Use the editor option** for complex multi-line prompts
6. **Configure AI tool arguments properly** in `config.yaml` for optimal results

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

