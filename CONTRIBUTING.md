# Contributing to Copycat

## Development Setup

```bash
git clone git@github.com:saltpay/copycat.git
cd copycat
go build -o copycat .
go test ./...
```

## Architecture Overview

Copycat is a Go CLI that uses [Bubble Tea](https://github.com/charmbracelet/bubbletea) for its TUI. The main entry point is `main.go`, which coordinates:

1. **Project selection** — interactive multi-select of GitHub repos
2. **Wizard** — collects action type, PR title/question, AI prompt, branch strategy, notification settings
3. **Processing** — clones repos, runs AI tool, creates PRs (or collects findings) in parallel batches
4. **Done** — summary of results

### Two Workflows

- **Perform Changes** (`action = "local"`) — clones repos, runs AI tool with a prompt, commits changes, creates PRs
- **Run Assessment** (`action = "assessment"`) — clones repos, runs AI tool with a question, collects per-repo findings, generates a cross-repo summary (no branches/commits/PRs)

### Package Layout

| Package | Purpose |
|---|---|
| `main.go` | Entry point, subcommand routing, repo processing orchestration |
| `internal/config/` | YAML config loading/saving, AI tool definitions, XDG path resolution, defaults |
| `internal/input/` | Bubble Tea models: dashboard, project selector, wizard, progress |
| `internal/ai/` | AI tool invocation (`VibeCode`, `Assess`, `GeneratePRDescription`, `SummarizeFindings`) |
| `internal/git/` | Git/GitHub CLI operations (clone, branch, push, PR creation, topic sync) |
| `internal/permission/` | MCP permission handler, HTTP permission server, repo sanitization |
| `internal/cmd/` | Subcommands (`edit`, `migrate`, `reset`) |
| `internal/filesystem/` | Workspace directory management |
| `internal/slack/` | Slack notification delivery (PR links, assessment findings, summaries) |

### Subcommands

| Command | Description |
|---|---|
| `copycat` | Main TUI (default) |
| `copycat edit config` | Opens config file in `$EDITOR` |
| `copycat edit projects` | Opens projects file in `$EDITOR` |
| `copycat migrate` | Migrates old config/projects to XDG paths |
| `copycat reset` | Deletes config and projects files (with confirmation) |
| `copycat permission-handler` | MCP server subprocess (internal, not for direct use) |

### Bubble Tea Message Flow

The `dashboardModel` manages phases. Sub-models communicate via custom messages:

```
projectSelectorModel  →  projectsConfirmedMsg  →  wizardModel
wizardModel           →  wizardCompletedMsg     →  startProcessing()
progressModel         →  processingDoneMsg      →  phaseDone
```

Background processing uses a channel-based approach: goroutines send `ProjectStatusMsg` / `ProjectDoneMsg` into `statusCh`, and `listenForStatus()` pumps them into the Bubble Tea event loop.

### Config Paths (XDG-compliant)

| OS | Config directory |
|---|---|
| macOS | `~/Library/Application Support/copycat/` |
| Linux | `~/.config/copycat/` |
| Windows | `%AppData%/copycat/` |

Config and projects are stored in **separate files**: `config.yaml` and `projects.yaml`.

## Architectural Decisions

### Why the MCP handler is a separate subprocess

Claude Code's `--permission-prompt-tool` requires an MCP server. MCP servers are separate processes that communicate over stdin/stdout (JSON-RPC 2.0). The `copycat permission-handler` subcommand is that process — Claude spawns it automatically via the temp MCP config file.

Because it's a separate process, it can't share memory with the main Copycat process. This drives two design decisions:

- **`COPYCAT_PERMISSION_PORT`**: The handler needs to reach the main process's HTTP permission server. The port is passed via env var in the MCP config.
- **`COPYCAT_PREAPPROVED_TOOLS`**: The handler auto-approves Bash commands matching allowed prefixes (e.g., `tree`, `cat`) without a round-trip to the TUI. These prefixes are parsed from the config's `allowed_tools` by `ParseBashPrefixes()` (converting `"Bash(tree:*)"` to `"tree"`) and passed via env var since the subprocess can't read the parent's config.
- **`COPYCAT_REPO_NAME`**: Set per-repo on the AI subprocess so the handler can include the repo name in TUI permission prompts.

Everything that isn't pre-approved goes through the HTTP permission server → Bubble Tea status channel → TUI prompt → user approval.

### Why gh CLI calls are serialized

All `gh` CLI invocations go through `runGhContext()` which holds a global `sync.Mutex` (`ghMu`). Even though repos are processed in parallel, GitHub API calls are serialized to avoid rate limiting. This is intentional — parallelism gains come from AI processing time, not GitHub API calls.

### Why user MCP servers are not duplicated in the temp config

The generated MCP config file only contains the `copycat-auth` server. User MCP servers are loaded via `--setting-sources user` instead. This avoids duplicating remote/SSE servers that may be unreachable, which would block Claude startup.

### Why AskUserQuestion is handled as a deny

When Claude uses the `AskUserQuestion` tool, the MCP handler routes it to the TUI for user input. The answer is returned as a **deny** response with `"User answered: <label>"`. This is because the MCP permission protocol only has allow/deny — deny with a message is the only way to pass information back to Claude without executing the original tool.

### Permission timeout

Unanswered permission prompts auto-deny after **5 minutes** (`permissionTimeout` in `server.go`).

### Batch processing and checkpoints

Both workflows (changes and assessments) process repos in batches. After each batch:
- The TUI pauses, asking the user to verify AI credits before continuing
- The user can press `e` to edit the prompt for remaining repos
- Resume is channel-based (`sender.ResumeCh` carries the possibly-updated prompt)

Batch size is controlled by `confirm_move_to_next_batch` in config (default: pause every 10 repos, or `"automatic"` to skip pauses).

### Agent instruction file handling

Files listed in `agent_instructions` config (defaults: `CLAUDE.md`, `AGENTS.md`, `.claude`, `.cursorrules`, `.github/copilot-instructions.md`) can be stripped from target repos before the AI runs (opt-in during wizard). Git-tracked files are restored via `git checkout --`; untracked files are backed up to a temp directory and restored by rename.

### Topic sync

`SyncTopicsWithCache` in `git/topics.go` provides bidirectional sync between the local `projects.yaml` cache and GitHub repo topics (used for project metadata like Slack channels).

## Security Hardening

Copycat runs AI tools inside cloned third-party repos. This creates security risks because those repos may contain files that escalate AI tool permissions. Copycat addresses this with multiple layers of defense.

### 1. Setting Source Restriction

Claude is invoked with `--setting-sources user`, which tells it to only load settings from the user's global config — not from per-project `.claude/settings.json` files. This is a defense-in-depth measure in case sanitization misses a file.

### 2. Tool Allowlisting and Permission Prompting

Claude is invoked with `--allowedTools` to whitelist only the tools needed:

```yaml
allowed_tools:
  - Edit
  - "List(*)"
  - "Read(*)"
  - "Bash(tree:*)"
  - "Bash(cat:*)"
  - "Bash(find:*)"
  - "Bash(wc:*)"
  - "Bash(grep:*)"
  - "Bash(./mvnw test:*)"
  - "Bash(./mvnw verify:*)"
  - "Bash(./mvnw compile:*)"
  - "Bash(./mvnw clean test:*)"
  - "Bash(./mvnw clean verify:*)"
```

Dangerous tools are explicitly blocked:

```yaml
disallowed_tools:
  - WebFetch
  - Task
```

When Claude wants to run a command not in the allowlist, the request is routed to Copycat's TUI via the `--permission-prompt-tool` flag. The user sees an inline prompt and can:

- **Approve (y)** — allow this one command
- **Deny (n)** — block this command
- **Approve all (a)** — auto-approve all future commands matching the same pattern (e.g., all `npm *` commands)

### Permission Prompt Architecture

```
Claude Code (subprocess, --print mode)
  ↕ MCP stdio (JSON-RPC 2.0)
copycat permission-handler (subcommand, spawned by Claude as MCP server)
  ↕ HTTP POST localhost:<port>
Copycat HTTP server (goroutine in main process)
  ↕ statusCh (channel into Bubble Tea)
Progress Model TUI
  ↕ keyboard
User approves/denies
```

The flow:

1. `startProcessing()` starts a `PermissionServer` on a random localhost port
2. A temp MCP config file is generated pointing Claude's `--permission-prompt-tool` at `copycat permission-handler`
3. When Claude hits a non-whitelisted tool, it calls the MCP tool instead of silently denying
4. The MCP handler (`permission-handler` subcommand) POSTs to the HTTP server
5. The HTTP server sends a `PermissionRequestMsg` into the Bubble Tea status channel
6. The progress model shows the prompt and waits for user input
7. The response flows back through the channel → HTTP → MCP → Claude

### Customizing the Allowlist

Edit your `config.yaml` (`copycat edit config`) to adjust `allowed_tools` and `disallowed_tools` for the Claude tool. Use Claude Code's tool permission syntax:

- `Edit` — file editing
- `Read(*)` — read any file
- `List(*)` — list directory contents
- `Bash(command:*)` — allow a specific command prefix
- `WebFetch` — fetch URLs (blocked by default)
- `Task` — spawn sub-agents (blocked by default)

Interactive permission prompting is always enabled for Claude. Non-allowlisted Bash commands and MCP tools will trigger a TUI prompt for user approval.

## AI Tool Configuration

### Supported AI Tools

Four tools are configured by default:

| Name | Binary | Notes |
|---|---|---|
| `claude` | `claude` | Full support: allowed/disallowed tools, MCP permission prompting, PR description generation |
| `codex` | `codex` | `--full-auto` mode |
| `qwen` | `qwen` | `--approval-mode auto-edit` |
| `gemini` | `gemini` | `--approval-mode auto_edit` |

The config is tool-agnostic — any CLI tool can be added. `allowed_tools` and `disallowed_tools` are Claude-specific fields; other tools ignore them. When `summary_args` is empty, `code_args` are used as fallback for PR descriptions and assessment summaries.

### Config Fields

```yaml
tools:
  - name: claude                           # Display name
    command: claude                         # CLI binary
    code_args: [...]                        # Args for code changes
    summary_args: [...]                     # Args for PR description generation
    allowed_tools: [...]                    # Whitelisted Claude tools
    disallowed_tools: [...]                 # Blocked Claude tools
```

## Code Style

- Standard Go formatting (`gofmt`)
- Tabs for indentation
- Error handling: log and continue for batch operations
- Prefer the standard library; keep dependencies minimal

## Commit Messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/) and [Semantic Versioning](https://semver.org/).

| Prefix | Version Bump |
|---|---|
| `feat:` | Minor (0.X.0) |
| `fix:` | Patch (0.0.X) |
| `feat!:` / `fix!:` | Major (X.0.0) |

Non-release prefixes: `docs:`, `chore:`, `refactor:`, `test:`, `ci:`

## Running Tests

```bash
go test ./...                          # All tests
go test ./internal/permission/... -v   # Permission package with verbose output
go vet ./...                           # Static analysis
```