# Harness matrix

Where jig writes each kind of agent context, for each harness. Jig only writes for the harnesses in use on the machine
(detected, or set with `jig init --harness`). See [Agent context](../concepts/agent-context.md) for the `agents`
block itself.

Harness names, as used by `--harness` and `.jig/config.json`: `claude` (Claude Code), `codex` (Codex), `copilot`
(GitHub Copilot), `cursor` (Cursor), `opencode` (OpenCode), `gemini` (Gemini CLI), `windsurf` (Windsurf), `kiro`
(Kiro).

## Overview

| | context | skills | instructions | subagents | commands | MCP | hooks |
| --- | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| Claude Code | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Codex | ✓ | ✓ | folded | ✓ | | ✓ | ✓ |
| GitHub Copilot | ✓ | ✓ | ✓ | ✓ | | ✓ | ✓ |
| Cursor | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| OpenCode | ✓ | ✓ | folded | ✓ | ✓ | ✓ | |
| Gemini CLI | ✓ | ✓ | folded | | ✓ | ✓ | ✓ |
| Windsurf | ✓ | ✓ | ✓ | | ✓ | | ✓ |
| Kiro | ✓ | ✓ | ✓ | ✓ | | ✓ | |

*folded*: the harness has no per-file rules, so the instructions are appended to its root context file under
`## Instructions`. An empty cell means the harness has no project-level location for it, and nothing is written.

Plugins are not a column: a plugin is kept whole in `.agents/plugins/<name>/`, and its skills, subagents, commands,
MCP servers and hooks go through the rows above.

## Context

| Harness | Path |
| --- | --- |
| Codex, Cursor, OpenCode, Windsurf, Kiro | `AGENTS.md` |
| GitHub Copilot | `AGENTS.md`, plus `.github/copilot-instructions.md` linked to it |
| Claude Code | `CLAUDE.md` |
| Gemini CLI | `GEMINI.md` |

When `AGENTS.md` is written for another harness, `CLAUDE.md` and `GEMINI.md` are links to it as long as their content
would be the same. They differ when instructions are folded: if Codex or OpenCode is in use, `AGENTS.md` carries the
folded instructions and `CLAUDE.md` stays a separate file, since Claude Code reads its rules from `.claude/rules`.

## Skills

| Harness | Path |
| --- | --- |
| Codex, Copilot, Cursor, OpenCode, Gemini CLI, Windsurf | `.agents/skills/` |
| Claude Code | `.claude/skills/` |
| Kiro | `.kiro/skills/` |

`.claude/skills` and `.kiro/skills` are real copies, not links, because some harnesses do not follow symlinked skill
directories reliably.

## Instructions

Each rule is a markdown file with a `description` and optional `paths` globs (`applyTo` is accepted too). Without
`paths`, it is always on.

| Harness | Path | Format |
| --- | --- | --- |
| Claude Code | `.claude/rules/<name>.md` | Claude rule frontmatter |
| GitHub Copilot | `.github/instructions/<name>.instructions.md` | `applyTo` frontmatter |
| Cursor | `.cursor/rules/<name>.mdc` | Cursor rule frontmatter |
| Windsurf | `.windsurf/rules/<name>.md` | Windsurf rule frontmatter |
| Kiro | `.kiro/steering/<name>.md` | Kiro steering frontmatter |
| Codex, OpenCode | folded into `AGENTS.md` | |
| Gemini CLI | folded into `GEMINI.md` | |

## Subagents

| Harness | Path | Format |
| --- | --- | --- |
| Claude Code | `.claude/agents/<name>.md` | markdown |
| Codex | `.codex/agents/<name>.toml` | TOML |
| GitHub Copilot | `.github/agents/<name>.agent.md` | markdown |
| Cursor | `.cursor/agents/<name>.md` | markdown |
| OpenCode | `.opencode/agents/<name>.md` | markdown |
| Kiro | `.kiro/agents/<name>.md` | markdown |

## Commands

| Harness | Path | Format |
| --- | --- | --- |
| Claude Code | `.claude/commands/<name>.md` | markdown |
| Cursor | `.cursor/commands/<name>.md` | markdown |
| OpenCode | `.opencode/commands/<name>.md` | markdown |
| Gemini CLI | `.gemini/commands/<name>.toml` | TOML, `$ARGUMENTS` becomes `{{args}}` |
| Windsurf | `.windsurf/workflows/<name>.md` | markdown |

## MCP servers

| Harness | Path |
| --- | --- |
| Claude Code | `.mcp.json` |
| Codex | `.codex/config.toml` |
| GitHub Copilot | `.github/mcp.json` and `.vscode/mcp.json` |
| Cursor | `.cursor/mcp.json` |
| OpenCode | `opencode.json` |
| Gemini CLI | `.gemini/settings.json` |
| Kiro | `.kiro/settings/mcp.json` |

Each document is written in the harness's own shape, and `${NAME}` / `${env:NAME}` references are translated to its
syntax: `${env:NAME}` for VS Code and Cursor, `{env:NAME}` for OpenCode, `${NAME}` for the others. Codex expands no
reference: an env value `${NAME}` under its own name becomes `env_vars`, a header `${NAME}` becomes `env_http_headers`,
and `Authorization: Bearer ${NAME}` becomes `bearer_token_env_var`. A server with any other reference is left out of
the Codex document and reported as `skipped mcp server ...` in the sync output. These files are shared with you: jig
only adds, updates and removes the servers it declared.

## Hooks

Events: `sessionStart`, `beforeToolUse`, `afterToolUse`, `beforePrompt`, `stop`. A `matcher` uses Claude Code's
tool names (`Edit|Write`, `Bash`) and is allowed on the two tool events.

| Harness | Path | Notes |
| --- | --- | --- |
| Claude Code | `.claude/settings.json` | |
| Codex | `.codex/hooks.json` | |
| GitHub Copilot | `.github/hooks/jig.json` | no matchers: hooks with one are skipped |
| Cursor | `.cursor/hooks.json` | tool names mapped (`Shell`, `Write`) |
| Gemini CLI | `.gemini/settings.json` | tool names mapped (`run_shell_command`, `replace`), timeouts in milliseconds |
| Windsurf | `.windsurf/hooks.json` | no `sessionStart`; events chosen from the matcher |

A hook a harness cannot take is reported as `skipped hook ...` in the sync output. Like MCP documents, jig only owns
the hooks it wrote.
