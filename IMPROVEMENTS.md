# Copycat Proposed Improvements

Six improvement areas identified from team feedback and real-world usage.

## 1. Repo Resolution / Alias Handling (Priority: High)

**Problem**: Repo identification assumes exact name match. Users can't use service names, abbreviations, or aliases (e.g., `merchant-account` not found). Also a `.git` trimming bug where config values like `repo.git` produce clone URLs `repo.git.git`.

**Solution**:
- Add `aliases` field to Project config
- Strip `.git` suffix on load (defensive normalization)
- Extend filter matching to search repo names and aliases, not just topics
- Add fuzzy suggestion when exact match fails

## 2. Batch-Run Preflight Checks and Approval UX (Priority: High)

**Problem**: Users hit issues after batch approval — repos fail due to missing Jira tickets, auth issues, or inaccessible repos. One user reported a loop after batch approve.

**Solution**:
- Add preflight validation phase (check repo accessibility via `gh repo view`)
- Skip blocked repos gracefully instead of failing
- Fix batch-pause loop bug (`!m.paused` guard in progress.go:223)

## 3. Text Attachments / Richer Prompt Inputs (Priority: Medium)

**Problem**: Users can't attach files (migration guides, spec documents) to provide context for the AI.

**Solution**:
- Add `stepAttachments` wizard step after prompt input
- Concatenate file contents into the prompt sent to AI
- 50KB size limit per attachment

## 4. Better Ambiguity Handling Before Editing Code (Priority: Medium)

**Problem**: AI acts on ambiguous prompts without clarifying, leading to wrong changes (e.g., adding producer instead of consumer).

**Solution**:
- Add plan preview phase: clone first repo, run AI in read-only mode to describe planned changes
- User can proceed, edit prompt, or cancel before committing to batch
- Optional `--dry-run` CLI flag

## 5. More Robust PR Follow-up / Workspace Targeting (Priority: Medium)

**Problem**: Can't apply feedback to PRs in different repos/workspaces than the one currently open.

**Solution**:
- Add "Follow Up on PR" action in wizard
- Clone repo, checkout PR branch, apply changes, push (PR auto-updates)
- No new PR creation needed

## 6. Clearer Task Boundaries / Fallback Modes (Priority: Low)

**Problem**: When asked to "investigate", Copycat says it's outside its toolkit. Assessment mode exists but isn't surfaced well.

**Solution**:
- Rename actions with inline descriptions ("Investigate" instead of "Run Assessment")
- Add prompt templates for investigation use cases
- Pattern-match AI refusal output and suggest appropriate mode
