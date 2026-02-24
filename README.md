# Copycat 😸

Welcome to Copycat, a way to copy changes from one git repository to another.

<img src="./logo.png"
     alt="Copycat logo"
     style="margin-bottom: 10px; animation: spin 2s linear infinite; transform-origin: center center;" />

Copycat is an automation tool that enables you to apply consistent changes across multiple GitHub repositories using AI coding assistants. It streamlines the process of creating issues or performing code changes at scale with support for multiple AI tools including Claude, Codex, Qwen, and others.

## Demo

![Copycat Demo](./demo.gif)

## Installation

### Using Go

```bash
go install github.com/saltpay/copycat@latest
```

### From GitHub Releases

Download the latest binary for your platform from the [Releases page](https://github.com/saltpay/copycat/releases).

### Build from Source

```bash
git clone git@github.com:saltpay/copycat.git
cd copycat
go build -o copycat .
```

## Requirements

In order to run Copycat, you need to have the following installed:

- **Go** (1.25 or later)
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

Copycat stores its configuration in a platform-specific location:

| Platform   | Config Path                                         | Projects Path                                         |
|------------|-----------------------------------------------------|-------------------------------------------------------|
| Linux      | `~/.config/copycat/config.yaml`                     | `~/.config/copycat/projects.yaml`                     |
| macOS      | `~/Library/Application Support/copycat/config.yaml` | `~/Library/Application Support/copycat/projects.yaml` |
| Windows    | `%AppData%/copycat/config.yaml`                     | `%AppData%/copycat/projects.yaml`                     |

Projects are stored in a separate `projects.yaml` file so it can be symlinked independently (e.g., different project lists per team or context).

### First Run

On first run, Copycat will guide you through setup:

```
Welcome to Copycat!
Configuration now follows XDG structure: ~/Library/Application Support/copycat/config.yaml

Found existing local config files:
  - config.yaml
  - .projects.yaml

How would you like to proceed?
> Migrate existing config
  Start fresh
```

If no existing local config is found, you'll be prompted for your GitHub organization.

### Subcommands

```bash
copycat                # Run the interactive TUI
copycat edit config    # Open config.yaml in $EDITOR
copycat edit projects  # Open projects.yaml in $EDITOR
copycat migrate        # Migrate from old local config files
copycat reset          # Delete configuration files and start fresh
```

### Config File Structure

**`config.yaml`** — tool and organization settings:

```yaml
github:
  organization: my-org
  auto_discovery_topic: copycat

agent_instructions:
  - CLAUDE.md
  - .claude
  - .cursorrules
  - .github/copilot-instructions.md

tools:
  - name: claude
    command: claude
    code_args: [--print, --permission-mode, acceptEdits, --setting-sources, user]
    summary_args: [--print, --setting-sources, user]
    allowed_tools:
      - Edit
      - "List(*)"
      - "Read(*)"
      - "Bash(tree:*)"
      - "Bash(grep:*)"
    disallowed_tools:
      - WebFetch
      - Task
    supports_permission_prompt: true
  - name: codex
    command: codex
    code_args: [exec, --full-auto]
    summary_args: []
  - name: gemini
    command: gemini
    code_args: [--approval-mode, auto_edit]
    summary_args: []
```

**`projects.yaml`** — repository list (separate file, can be symlinked):

```yaml
projects:
  - repo: service-a
    slack_room: "#team-a"
  - repo: service-b
    slack_room: "#team-b"
```

### Configuration Fields

**`config.yaml`:**

- `github.organization`: GitHub organization to scan for repositories
- `github.auto_discovery_topic` (optional): GitHub topic Copycat passes to `gh repo list`; when omitted Copycat lists all repositories
- `agent_instructions` (optional): List of files/directories to remove from cloned repos when "Ignore Agent Instructions" is enabled. Defaults to `CLAUDE.md`, `.claude`, `.cursorrules`, `.github/copilot-instructions.md`. Files are deleted before the AI tool runs and restored via `git checkout` before committing, so they never appear in the PR.
- `tools`: List of AI tools available in the selector
  - `name`: Identifier for the tool
  - `command`: CLI command to execute
  - `code_args`: Arguments passed when making code changes
  - `summary_args`: Arguments passed when generating PR descriptions (optional)
  - `allowed_tools` (optional, Claude-specific): Allowlist of tools the AI can use
  - `disallowed_tools` (optional, Claude-specific): Blocklist of tools
  - `supports_permission_prompt` (optional, Claude-specific): Enable interactive permission prompting for non-allowlisted commands

**`projects.yaml`:**

- `projects`: List of repositories (synced from GitHub or added manually)
  - `repo`: Repository name
  - `slack_room`: Slack channel for notifications (optional)

When Copycat lists repositories it uses the configured discovery topic if provided, otherwise it fetches every unarchived repository in the organization. Press 'r' in the project selector to sync repositories from GitHub.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for full details on the security model, permission prompting architecture, and allowlist customization.

## Usage

### Quick Start

```bash
copycat

# On first run, you'll be guided through setup
# Then use subcommands to manage your config:
copycat edit config    # Edit config.yaml
copycat edit projects  # Edit projects.yaml
copycat reset    # Start fresh
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
- Configure `slack_room` per project in `projects.yaml` (use `copycat edit projects`)

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
3. Enter PR title (you'll be reminded to include a ticket reference if needed)
4. Enter the AI prompt:
   - **Single line**: Type or paste the prompt and press Enter
   - **Editor**: Opens your default editor (set via `$EDITOR` env var, defaults to vim)
5. Optionally enable **Ignore Agent Instructions** to remove repo-level AI instruction files (e.g., `CLAUDE.md`, `.cursorrules`) before the AI runs, so it follows only your prompt
6. Copycat will:
   - Clone all selected repositories to `repos/` directory
   - Create a timestamped branch (e.g., `copycat-20231015-150405`)
   - Run your chosen AI tool to analyze and apply changes
   - Generate PR description automatically
   - Commit and push changes
   - Create pull requests
   - Clean up cloned repositories

### Project Selection

The project selector is an interactive multi-select TUI:
- **Navigate**: Arrow keys or `h/j/k/l`
- **Toggle selection**: `Space`
- **Select/deselect all**: `a`
- **Filter by topic**: `f`, then type to filter
- **Refresh from GitHub**: `r`
- **Confirm**: `Enter`

### Branch Naming

Branches are automatically named with the format: `copycat-YYYYMMDD-HHMMSS`

Example: `copycat-20231015-150405`

### Pull Request Titles

Uses the PR title you provide. You may include a ticket or issue reference directly in the title (e.g., `PROJ-123 - Your PR Title`).

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
4. **Use the editor option** for complex multi-line prompts
5. **Configure AI tool arguments properly** in `config.yaml` for optimal results

## Examples

### Example AI Prompts

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

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, architecture overview, code style, commit conventions, and security details.

## Security Notes

- Copycat uses SSH for git operations
- Credentials are managed via GitHub CLI and Claude CLI
- No credentials are stored by Copycat
- Repository clones are cleaned up after processing
