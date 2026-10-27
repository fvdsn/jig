# Agent context

The `agents` block declares what the workspace provides to coding agents, without saying where each agent reads it.
Jig writes it out for each harness in use, in that harness's paths and formats.

```yaml
agents:
  context:
    - git@github.com:acme/agent-config.git#context/workspace.md
    - src: git@github.com:acme/agent-config.git#context/billing.md
      onlyWhen: { path: billing/* }
  skills:
    - git@github.com:acme/agent-config.git#skills
    - dir: ~/.config/acme/skills
      optional: true
  instructions:
    - git@github.com:acme/agent-config.git#rules
  subagents:
    - git@github.com:acme/agent-config.git#agents
  commands:
    - git@github.com:acme/agent-config.git#commands
  plugins:
    - git@github.com:obra/superpowers.git
  mcp:
    github:
      command: npx
      args: [-y, "@modelcontextprotocol/server-github"]
      env: { GITHUB_TOKEN: "${GITHUB_TOKEN}" }
  hooks:
    - event: afterToolUse
      matcher: Edit|Write
      command: jig fmt
```

## The primitives

| Key | What it is |
| --- | --- |
| `context` | sections concatenated into the root context file (`AGENTS.md`, `CLAUDE.md`, ...) |
| `skills` | skill directories |
| `instructions` | rules with a `description` and optional `paths` globs, applied to matching files |
| `subagents` | agent definitions, one markdown file each |
| `commands` | slash commands, one markdown file each |
| `plugins` | plugins or marketplaces in the Claude Code plugin layout; their skills, agents, commands, MCP servers and hooks feed the lists above |
| `mcp` | MCP servers, local (`command`) or remote (`url`) |
| `hooks` | commands run on agent events: `sessionStart`, `beforeToolUse`, `afterToolUse`, `beforePrompt`, `stop` |

Sources are git URLs (`#path` points inside the repository) or local paths. A local source marked `optional: true`
is skipped on machines that do not have it, which makes per-user extension points possible.

The jig skill itself is always installed, so agents in any workspace know how to use jig.

## Harnesses

Jig supports Claude Code, Codex, GitHub Copilot, Cursor, OpenCode, Gemini CLI, Windsurf and Kiro. It installs
context only for the harnesses in use on your machine: the ones it detects from their directories in your home
(`~/.claude`, `~/.codex`, ...), or the ones chosen with `jig init --harness`, stored in `.jig/config.json`.

Where several harnesses read the same location, jig writes it once: most of them read skills from `.agents/skills`
and context from `AGENTS.md`. Where a harness reads its own location, jig writes that. On a machine
with only Claude Code, the workspace gets `CLAUDE.md` and `.claude/` and nothing else.

Formats are translated: rules get each harness's frontmatter, Codex subagents and Gemini commands become TOML, hook
event and tool names are mapped, and `${VAR}` references in MCP servers are rewritten in each harness's syntax.
Something a harness cannot take is reported as skipped. The [harness matrix](../reference/harness-matrix.md) has
the details.

Start the agent at the workspace root. Jig writes all agent context there, and the workspace root is usually not a
Git repository. Harnesses look for project configuration from the current directory up to its Git root. Thus a
session started inside a repository checkout stops at that checkout and does not read the workspace files. For MCP
servers, this is true of OpenCode, Codex and Copilot CLI. Codex and Copilot CLI also read project configuration only
after you trust the directory.

## Files you share with jig

Some files are both jig's and yours: `.mcp.json`, `.claude/settings.json`, `.codex/config.toml` and the like. Jig
only owns the keys it wrote. It adds and updates them, removes them when they leave the schema, and leaves the rest
of the file alone. A key you changed by hand is kept and reported, not overwritten.

## Scoping context

Context can follow what is checked out, in three ways.

### `onlyWhen` on a single item

Any source, MCP server, hook or plugin in an `agents` block can carry an `onlyWhen`. It is then active only while a
matching repository is installed:

```yaml
agents:
  skills:
    - src: git@github.com:acme/agent-config.git#skills/api-design
      onlyWhen: { tags: [api] }
```

### Group `agents` blocks

When several items belong to the same part of the workspace, put them in an `agents` block on that part's `$group`
instead of repeating the condition. It takes the same keys as the top-level block, and everything in it is active
while any repository under the group is installed:

```yaml
version: 4

agents:
  context:
    - git@github.com:acme/agent-config.git#AGENTS.md

tree:
  billing/:
    $group:
      agents:
        context:
          - git@github.com:acme/agent-config.git#context/billing.md
        skills:
          - git@github.com:acme/billing-skills.git#skills
        mcp:
          billing-api:
            url: https://mcp.acme.dev/billing
    invoices:
      $repo:
        git: git@github.com:acme/invoices.git
    ledger:
      $repo:
        git: git@github.com:acme/ledger.git

  frontend/web:
    $repo:
      git: git@github.com:acme/web.git
```

A workspace with only `frontend/web` gets the workspace context and nothing from billing. Clone
`billing/invoices`, and the next sync adds the billing skills and the `billing-api` server, and appends a section to
the context file:

```markdown
## billing

Applies to the repositories under `billing`.

(content of context/billing.md)
```

Where a group's skill or server has the same name as one in the top-level block, the group's wins while the group is
active, and the workspace's applies otherwise. Group blocks can be nested: each applies to the repositories under its
own group.

### Repository exports

A repository can offer some of its own files as context. `exports` maps a primitive (`skills`, `context`,
`subagents` or `commands`) to a path inside the checkout:

```yaml
services/checkout:
  $repo:
    git: git@github.com:acme/checkout.git
    exports:
      skills: .agents/skills
      context: AGENTS.md
```

While `services/checkout` is installed, its skills join the workspace's, and its `AGENTS.md` becomes a section of the
workspace context file, titled with the repository path. The files are read from the checkout, so a change on your
branch shows up on the next sync. Set `exports` on a group to apply it to every repository under it; repositories
that do not have the path simply offer nothing.

## Compared to APM

[APM](https://microsoft.github.io/apm/) manages the same kinds of context for a single project. Jig covers the same
primitives and harnesses, and adds the link to the workspace: context can depend on which repositories are installed,
and repositories can provide context themselves. One difference: jig does not follow the dependencies in a plugin's
`apm.yml`; declare them as sources instead.
