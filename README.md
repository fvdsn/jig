# jig

[![ci](https://github.com/fvdsn/jig/actions/workflows/ci.yml/badge.svg)](https://github.com/fvdsn/jig/actions/workflows/ci.yml)
[![version](https://img.shields.io/github/v/tag/fvdsn/jig?label=version&sort=semver)](https://github.com/fvdsn/jig/tags)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> A jig holds workpieces in place — jig holds your repos in place.

Jig manages a local workspace made of many related Git repositories, driven by a single shared schema. Your organization describes its repositories once — layout, dependencies, tags, generated support files — in a schema hosted in its own Git repository. Everyone then materializes just the parts they need, and jig keeps the checkouts converged, restored, and up to date.

```sh
jig clone services/checkout   # clones the service and everything it depends on
```

## Why

- **One shared map.** Hundreds of repos, one versioned schema: where everything lives, what depends on what, what is archived, how it is tagged.
- **Partial by design.** Nobody needs the whole org. Clone one service, a group, or a tag — dependencies come along automatically. Ideal for spinning up task-scoped workspaces with all the context an AI agent needs.
- **Safe by default.** Jig never deletes work: dirty checkouts, unpushed commits, and locally modified files are always detected and preserved.
- **Fast.** Git operations run in parallel, and a machine-wide clone cache makes repeat clones nearly instant.

## Install

With Homebrew (macOS or Linux):

```sh
brew install fvdsn/tap/jig
```

With Go:

```sh
go install github.com/fvdsn/jig@latest
```

Or grab a prebuilt binary for macOS, Linux, or Windows from the [releases page](https://github.com/fvdsn/jig/releases).

## Quick start

```sh
jig init git@github.com:acme/jig-schema.git ~/Code/acme   # create a workspace from a shared schema
cd ~/Code/acme                  # (or start from scratch in an empty directory: jig init)
jig list                        # browse the catalog
jig clone services/checkout     # install a service + its dependencies
jig setup                       # run each repo's bootstrap → a runnable workspace
jig status                      # branches, dirty state, ahead/behind
jig diff --stat                 # what changed, per dirty repo (jig diff for the patch)
jig fetch && jig status         # what changed across the workspace?
jig pull                        # fast-forward everything installed
jig checkout -b fix-x           # branch across repos, edit away…
jig fmt && jig lint && jig test # …then check every repo, each with its own tooling
jig push -u                     # publish across repos
jig checkout --default          # …and back to each repo's own default branch
jig git stash                   # anything else git does, verbatim, in every repo in scope
jig rm services/checkout        # uninstall
```

## Commands

| Command | Description |
| --- | --- |
| `init [<git-url\|file> [dir]]` | Create a workspace from a schema repository or a local draft file; no args starts a fresh starter schema (`--harness a,b` picks the coding-agent harnesses, otherwise detected) |
| `list [path]` | List the catalog: groups, repos, files, dirs |
| `tags [path]` | List the tags in scope, with entry counts |
| `info <path>` | Show one entry's metadata |
| `deps <path>` | Show a repo's recursive dependencies (`--reverse` for its direct dependents) |
| `graph [path]` | Print the dependency graph as a Mermaid flowchart |
| `clone [path]` | Install repos/files matching a path, plus dependencies (`--no-deps` to skip them) |
| `sync` | Update the schema, then converge the installed checkouts: moves, origins, file updates, restores (`--no-update` skips the fetch, `--prune` deletes what left the schema) |
| `setup` / `fmt` / `lint` / `test` | Run each repo's own schema-declared command behind a standard verb; `setup` runs in dependency order |
| `pull` | `git pull --ff-only` across installed repos, in parallel |
| `fetch` | `git fetch` across installed repos, in parallel |
| `push [-u]` | `git push` (never forced) across installed repos, in parallel; `-u` sets missing upstreams |
| `checkout [-b] <branch>` | Switch installed repos to a branch (`-b` creates it); never discards local changes |
| `checkout --default` | Switch each installed repo to its own remote default branch (main, master, …) |
| `status` | One line per installed entry — branch, dirt counts, ahead/behind — plus a summary |
| `diff` | Uncommitted changes across installed repos as one workspace-wide diff, through your git pager and colors (`--stat` for one line per dirty repo) |
| `git <git args...>` | Run any git command verbatim across installed repos; raw git, no safety net, one report per repo |
| `rm <path>...` | Uninstall: delete the checkout and stop tracking it |
| `update` | Fast-forward the schema from its remote without touching checkouts — review changes, then apply with `sync` |
| `validate [file]` | Validate the schema — also usable in the schema repo's CI |
| `skill` | Print the jig skill the binary carries and installs in every workspace |
| `cache [clean]` | Inspect or prune the clone cache |

Two kinds of commands. The schema commands (`list`, `tags`, `info`, `deps`, `graph`, `clone`) select entries that may not be on disk yet, so they take a path (one entry or a whole subtree), `--tags a,b` (entries carrying all listed tags), `--id <id>` (exactly one entry by stable identity), and `--archived`. `jig tags` lists the tags in scope with entry counts, so filter values are discoverable without reading the schema. Everything else acts on what is installed under the current directory and takes no selector at all: `cd` is the selector. That is also why `jig git` needs no separator: every token after `git` belongs to git.

Commands are position-aware: `jig status` in `services/` shows just your services, `jig pull` anywhere inside a checkout pulls just that repo, and path arguments resolve like filesystem paths — `.`, `..`, and a leading `/` for the workspace root all work.

## The schema

A tree where paths are the directory layout, written in JSON or YAML (same structure, chosen by the file extension; a bare `jig init` starts you with `jig.yaml`). Repos, files, and dirs are leaves; any level can carry group metadata that children inherit.

```yaml
version: 4
tree:
  platform:
    $group: { description: Shared platform services, tags: [backend] }
    auth:
      $repo: { id: auth-service, git: git@github.com:acme/auth.git }
    dev.sh:
      $file: { src: git@github.com:acme/config.git#scripts/dev.sh, executable: true }
  services/checkout:
    $repo:
      id: checkout-service
      git: git@github.com:acme/checkout.git
      tags: [go, api]
      dependsOn:
        - path: platform/*
          reason: uses platform services
  tools/ci:
    $dir: { src: git@github.com:acme/config.git#scripts/ci }
```

- **`$repo`** — a Git checkout. `id` is a stable identity that survives renames: move the entry in the schema and `jig sync` moves the checkout on disk.
- **`$file`** — a single generated file fetched from a source repo (or a symlink to another file via `link`). Updated on sync when the source changes; never overwritten if locally modified. `src` is `<clone-url>#<path>`, or simply a file URL pasted from the forge web UI (`https://github.com/o/r/blob/main/…`, GitLab/Bitbucket/Gitea equivalents; default branch only). `src` may also be a list of sources concatenated in order — e.g. one `AGENTS.md` assembled from a base section plus sections that follow the installed repositories; List entries can be `{ "src": ..., "onlyWhen": ... }` objects gating individual sections, local sources (`{ "file": "~/.config/acme/AGENTS.md", "optional": true }`) folding per-user content in on machines that provide it, and a source that fails to resolve is reported while the rest still materialize.
- **`$dir`** — a whole subtree materialized from a source repo (`<clone-url>#<subtree>`, or a pasted `…/tree/main/…` web URL). `src` may also be a list of sources merged in order (last wins on conflicts — base layers first, overrides after) — e.g. one `.agents/skills` assembled from several skill repositories; list entries can be `{ "src": ..., "onlyWhen": ... }` objects to gate individual sources. List entries can also be local directories (`{ "dir": "~/.config/acme/skills", "optional": true }`) for per-user additions. A `$dir` can instead declare `link` to become a relative symlink to another `$dir`, or `copy` to materialize a second real directory for consumers that do not follow symlinks — one real skills directory, aliased into every harness path. Jig tracks a manifest of what it wrote, so updates touch only unmodified files and user files inside are never touched.
- **`$group`** — metadata on a directory: `description`, `tags`, `meta`, `dependsOn`, `archived`, `onlyWhen` are inherited by everything beneath it.
- **References** — `dependsOn`, `onlyWhen`, and `link` all use one structured form with exactly one selector: `{"id": ...}` (one entry, stable across moves), `{"path": ...}` (one declared entry, exact; append `/*` for the subtree below it), or `{"tags": [...]}` (every repo carrying all the tags). Dangling references are validation errors, so renames fail loudly instead of silently matching less.
- **`dependsOn`** — cloning a repo brings its dependency closure along (`optional: true` deps only with `--with-optional-deps`).
- **`onlyWhen`** — conditional entries, active only when some active or installed repository is selected by the reference — e.g. API skills materialize whenever anything tagged `api` is installed.
- **`archived`** — hidden and skipped by default, kept synced if already installed.
- **`setup` / `fmt` / `lint` / `test`** — each repo's own lifecycle commands behind four standard verbs, so `jig test` runs every installed repo's suite without anyone knowing per-repo tooling. Inherited from groups; run only when you invoke them, never automatically.
- **`meta`** — user-defined string keys and values jig carries but never interprets, e.g. `"meta": { "github-mirror": "git@github.com:acme/auth.git" }` on a GitLab repo with a synced GitHub clone. Shown by `jig info`, filtered with `jig list --meta key[=value]`, inherited per key from groups.

Files and dirs follow the repositories around them: a support file placed inside a group is materialized whenever any repo of that group is installed; root-level files follow the workspace as a whole.

## Agent context

Coding agents read skills and root instructions from harness-specific places: `.agents/skills` or `.claude/skills`, `AGENTS.md` or `CLAUDE.md`. The `agents` block declares what the workspace provides once, with no harness path in it, and jig materializes it for the harnesses used on each machine:

```yaml
version: 4
agents:
  skills:
    - git@github.com:anthropics/skills.git#skills/frontend-design
    - src: git@github.com:acme/billing-skills.git#skills
      onlyWhen: { path: billing/* }
    - dir: ~/.config/acme/skills
      optional: true
  context:
    - git@github.com:acme/meta.git#context/workspace.md
    - file: ~/.config/acme/AGENTS.md
      optional: true
  instructions:
    - git@github.com:acme/meta.git#instructions
  subagents:
    - git@github.com:acme/meta.git#agents
    - src: git@github.com:acme/meta.git#agents/api-architect.md
      onlyWhen: { tags: [api] }
  commands:
    - git@github.com:acme/meta.git#commands
  plugins:
    - src: git@github.com:acme/pdlc-plugins.git
      onlyWhen: { tags: [pdlc] }
  mcp:
    github:
      command: npx
      args: [-y, "@modelcontextprotocol/server-github"]
      env: { GITHUB_TOKEN: "${GITHUB_TOKEN}" }
    billing-api:
      url: https://mcp.example.com/sse
      onlyWhen: { path: billing/* }
  hooks:
    - event: afterToolUse
      matcher: Edit|Write
      command: jig fmt
      timeout: 5
tree: {}
```

`mcp` declares MCP servers once, in the standard shape, and jig writes each harness's own configuration document (`.mcp.json`, `.vscode/mcp.json` and `.github/mcp.json`, `.cursor/mcp.json`, `opencode.json`, `.gemini/settings.json`, `.kiro/settings/mcp.json`, `.codex/config.toml`) in that harness's shape, with `${NAME}` references translated to `${env:NAME}` where the harness wants them. These documents are the user's: jig owns only the keys it wrote, keeps and reports a key that conflicts with or was edited by the user, removes its keys when a server leaves the schema, and deletes a document only if it created it. A plugin's `.mcp.json` feeds the same map.

`hooks` declares commands run on lifecycle events, in a neutral vocabulary (`sessionStart`, `beforeToolUse`, `afterToolUse`, `beforePrompt`, `stop`, with a tool-name `matcher` on the tool events), and jig writes them into each harness's hook document in its own event names and shape (`.claude/settings.json`, `.codex/hooks.json`, `.cursor/hooks.json`, `.gemini/settings.json`, `.windsurf/hooks.json`, `.github/hooks/jig.json`), with the same ownership rules: jig owns the elements it wrote and nothing else. A hook a harness cannot express is reported as skipped, never silently dropped. A plugin's `hooks/hooks.json` feeds the same list.

A repository can export its own agent context: `agents` on a `$repo`, or on a `$group` for every repository beneath it, names a path inside the checkout per primitive (`agents: { skills: .agents/skills }`), and that path joins the block's sources while the repository is installed, read from the local checkout so an edit flows to the harness directories on the next sync before it is pushed. A repository without the path offers nothing, so one line on a group sets the convention for a whole fleet. The same checkout source is available to any `$file` or `$dir` as `{ repo: { id: x }, path: compose }`.

`plugins` sources are plugins in the Claude Code plugin layout, or marketplaces of them: each plugin is kept whole under `.agents/plugins/`, and its skills, agents, and commands feed the lists above as if declared there, with the links from a skill or agent to the plugin's shared `references/` rewritten so they resolve from wherever the harness reads them. One line installs an organization's whole plugin library for every harness, Copilot and Codex included.

`instructions` sources are directories of rule files, markdown with a `description` and a `paths` glob list in the frontmatter, written under each harness's own frontmatter (`.claude/rules`, `.cursor/rules/*.mdc`, `.github/instructions`, `.windsurf/rules`, `.kiro/steering`) and folded into the root context file of the harnesses without per-file rules (Codex, OpenCode, Gemini). `subagents` and `commands` sources are directories of markdown files or single files, landing under their own names in each harness's directory (`.claude/agents`, `.cursor/commands`, ...), converted where the harness wants its own format: Codex subagents and Gemini commands become TOML, Copilot subagents get the `.agent.md` suffix. `skills` sources are skill packs (directories of `<name>/SKILL.md`) or single skills (a directory whose root is a `SKILL.md`, landing under its own name), merged into `.agents/skills` with the `$dir` rules: last wins on conflicts, `onlyWhen` gates per source, local sources extend the set per user. `context` sources are concatenated into `AGENTS.md` with the `$file` rules. Jig itself ships a skill that teaches agents how to drive it, embedded in the binary and installed as `.agents/skills/jig` in every workspace, agents block or not, so the skill always matches the installed version; `jig skill` prints it.

Which harnesses are materialized is not in the schema: it depends on the person, not the team. A Claude-only machine gets `.claude/skills` and `CLAUDE.md` and nothing else; a machine that also runs Codex gets the canonical entries too, with `.claude/skills` as a real copy and `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md` as links to them. `jig init` detects the harnesses installed on the machine from their home directories and prints the list; `jig init --harness claude,codex` (or `none`) records a choice in `.jig/config.json` instead. The derived entries show up in `jig list` and `jig status` like declared ones, and a tree entry can still `link` to them by identity (`agents-skills`, `agents-context`) for a harness jig does not know.

## How it works

Everything jig manages lives under `.jig/` at the workspace root:

```text
.jig/source/       Git checkout of the schema repository — jig reads the schema live from it
.jig/config.json   which file inside the checkout is the schema
.jig/state.json    what is installed, and where
```

- **State records intent.** Deleting a checkout by hand doesn't uninstall it — `jig sync` restores it. `jig rm` is the uninstall verb, with `rm`-like ergonomics (`-r` for groups, `-f` to override the dirty/unpushed safety checks). `jig sync --prune` batch-deletes entries that were removed from the schema, with the same safety checks — anything dirty, unpushed, or locally modified is kept and reported.
- **The schema is a working copy.** Edit `.jig/source/<schema>`, test immediately with `jig sync --no-update`, then commit and push it like any repo. Teammates pick it up with their next `jig sync`. Conflicts are plain Git conflicts.
- **Clone cache.** Jig keeps a bare mirror per remote in the user cache directory; clones hardlink from it, so history transfers over the network once per machine. Checkouts stay fully independent — deleting the cache is always safe. `JIG_CACHE_DIR` relocates it (empty disables), `jig cache clean --unused 30` prunes it.
- **Terminal-aware output.** `list` and `status` align and truncate on a terminal; piped output stays full and tab-separated for scripts.

## Compatibility

- **Schemas are versioned.** The current schema version is 3 (local sources and `optional`); older versions are refused with a one-line migration message rather than misparsed.
- **Versioned formats fail loudly.** When jig meets a schema, workspace config, or state file with a version it does not understand, it stops with an "upgrade jig" error instead of guessing (or silently dropping fields a newer jig wrote).
- **Concurrent-safe.** Commands that mutate the workspace take a lock (`.jig/lock`); state files are written atomically.
- **Exit codes tell the truth.** Commands exit non-zero when any entry could not be brought to its desired state, so scripts and agents never mistake a partial run for success.
- **Platforms.** macOS and Linux are supported and tested in CI; Windows binaries ship with each release.

## Validating the schema in CI

In the schema repository:

```yaml
validate:
  image: golang:1.26
  script:
    - go install github.com/fvdsn/jig@latest
    - jig validate jig.json
```
