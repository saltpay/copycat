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
2. **Wizard** — collects PR title, AI prompt, branch strategy, Slack settings
3. **Processing** — clones repos, runs AI tool, creates PRs in parallel
4. **Done** — summary of results

### Package Layout

| Package | Purpose |
|---|---|
| `main.go` | Entry point, subcommand routing, repo processing orchestration |
| `internal/config/` | YAML config loading/saving, AI tool definitions, defaults |
| `internal/input/` | Bubble Tea models: dashboard, project selector, wizard, progress |
| `internal/ai/` | AI tool invocation (`VibeCode`, `GeneratePRDescription`) |
| `internal/git/` | Git/GitHub CLI operations (clone, branch, push, PR creation) |
| `internal/permission/` | Security hardening: repo sanitization, permission prompting |
| `internal/cmd/` | Subcommands (`edit`, `migrate`, `reset`) |
| `internal/filesystem/` | Workspace directory management |
| `internal/slack/` | Slack notification delivery |

### Bubble Tea Message Flow

The `dashboardModel` manages four phases. Sub-models communicate via custom messages:

```
projectSelectorModel  →  projectsConfirmedMsg  →  wizardModel
wizardModel           →  wizardCompletedMsg     →  startProcessing()
progressModel         →  processingDoneMsg      →  phaseDone
```

Background processing uses a channel-based approach: goroutines send `ProjectStatusMsg` / `ProjectDoneMsg` into `statusCh`, and `listenForStatus()` pumps them into the Bubble Tea event loop.

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

The `supports_permission_prompt: true` flag enables the interactive permission prompting. Set it to `false` to disable prompting (non-whitelisted commands will be silently denied by Claude).

## AI Tool Configuration

### Config Fields

```yaml
tools:
  - name: claude                           # Display name
    command: claude                         # CLI binary
    code_args: [...]                        # Args for code changes
    summary_args: [...]                     # Args for PR description generation
    allowed_tools: [...]                    # Whitelisted Claude tools
    disallowed_tools: [...]                 # Blocked Claude tools
    supports_permission_prompt: true        # Enable interactive permission TUI
```

`allowed_tools`, `disallowed_tools`, and `supports_permission_prompt` are Claude-specific. Other AI tools (codex, qwen, gemini) don't use these fields.

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
