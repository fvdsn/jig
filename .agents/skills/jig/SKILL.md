---
name: jig
description: Manage a multi-repository workspace with the jig CLI — initialize a workspace from a shared schema, clone repositories with their dependencies, sync, pull, and generate support files. Use when working inside a jig workspace or editing a jig schema (.jig.json / jig.json).
---

# Jig Usage Guide

Jig manages a local workspace made of many Git repositories and generated support files.

Use Jig when a team has many related repositories and wants one shared schema file to define how a workspace should be laid out, which repositories depend on which other repositories, and which helper files should be materialized locally.

## Core Idea

A workspace keeps everything Jig manages under `.jig/`:

```text
.jig/source/       Git checkout of the schema repository
.jig/config.json   which file inside the checkout is the schema
.jig/state.json    local machine state
```

The schema file (usually `.jig.json` or `jig.json`) lives in its own Git repository shared by the team. `jig init` clones that repository into `.jig/source/`, and Jig always reads the schema live from the checkout.

The `.jig/state.json` file is local and machine-owned. It tracks installed repositories and generated files so Jig can safely handle moves, remote changes, and local edits.

Jig also keeps a machine-wide clone cache (bare mirrors in the user cache directory), so cloning a repository into a second workspace is nearly instant. The cache is transparent and safe to delete; set `JIG_CACHE_DIR=` (empty) to disable it.

## Install

```sh
go install github.com/fvdsn/jig@latest
```

Make sure `$(go env GOPATH)/bin` is in `PATH`.

## Initialize A Workspace

Start a brand-new workspace from scratch (no schema yet):

```sh
jig init
```

This creates `.jig/source/` as a fresh local Git repository containing a starter `jig.json` and materializes it immediately. The starter schema pulls the official jig skill into `.agents/skills`, so coding agents in the workspace know how to use jig. Edit `.jig/source/jig.json` to describe your repositories, then push `.jig/source` to a shared remote so teammates can `jig init <url>`.

From a remote Git-hosted Jig definition:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme
```

Initialize and clone one path immediately:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme --clone services/checkout
```

Initialize and clone everything:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme --clone
```

Include optional dependencies during initial clone:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme --clone services/checkout --with-optional-deps
```

Initialize from a local file while testing a draft definition:

```sh
jig init ./draft.json ~/Code/acme-test
```

When initialized from a local file, `.jig/source/` is created as a fresh Git repository containing the schema as `jig.json`, with no remote configured.

## Definition Shape

The definition uses a top-level `tree`.

```json
{
  "version": 3,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:acme/platform-auth.git"
      }
    },
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "executable": true
      }
    }
  }
}
```

Tree paths use `/`, and the path is also the local filesystem path.

This:

```json
{
  "tree": {
    "platform/auth": {
      "$repo": {
        "git": "git@github.com:acme/platform-auth.git"
      }
    }
  }
}
```

creates:

```text
platform/auth
```

## Repository Nodes

Declare repositories with `$repo`.

```json
{
  "tree": {
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:acme/checkout.git",
        "web": "https://github.com/acme/checkout",
        "description": "Checkout service"
      }
    }
  }
}
```

Important fields:

- `id`: optional stable identity used to track moves and hosting changes.
- `git`: required clone URL.
- `web`: optional web URL.
- `description`: optional human description.
- `archived`: optional boolean; archived repos are excluded by default unless already installed. Pass `--archived` to include uninstalled archived repos too.
- `tags`: optional list of strings used for filtering with `--tags`.
- `meta`: optional object of user-defined string keys and values, carried but never interpreted by jig.
- `setup` / `fmt` / `lint` / `test`: optional lifecycle commands, run in the checkout by the matching jig command (never automatically).
- `dependsOn`: optional dependency list.
- `onlyWhen`: optional activation condition.

If `id` is omitted, the repository path is used as the identity.

## References

`dependsOn`, `onlyWhen` (including per-source conditions), `link`, and `copy` share one structured reference form: an object with **exactly one** of three selector fields.

- `{"id": "auth-service"}` — one entry by its stable identity; survives path moves. Ids are globally unique across all entry kinds.
- `{"path": "platform/auth"}` — one declared entry, exact match. A path naming nothing (or naming a group without `/*`) is a validation error, so renames fail loudly.
- `{"path": "platform/*"}` — every repository strictly below `platform`. The trailing `/*` is the only wildcard; bare `*` means the whole tree.
- `{"tags": ["api", "go"]}` — every repository carrying all the tags (inherited group tags included).

`link` and `copy` are single-target: only `id` or an exact `path`, resolving to one entry of the same kind.

## Dependencies

```json
{
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@github.com:acme/checkout.git",
        "dependsOn": [
          {
            "path": "platform/*",
            "reason": "checkout uses platform services"
          }
        ]
      }
    }
  }
}
```

The subtree selector `platform/*` matches all repositories under `platform/`, such as:

```text
platform/auth
platform/billing
```

Optional dependencies use `optional: true`.

```json
{
  "path": "observability/tracing",
  "optional": true,
  "reason": "useful when debugging traces locally"
}
```

By default, `clone` skips missing optional dependencies. Use `--with-optional-deps` to include them.

`sync` includes optional dependencies that are already installed, so installed optional repos stay up to date.

## Group Nodes

Declare group metadata with `$group` on directory nodes.

```json
{
  "tree": {
    "platform": {
      "$group": {
        "id": "platform-group",
        "description": "Shared platform services",
        "web": "https://github.com/acme/platform",
        "dependsOn": [
          {
            "path": "shared/config",
            "reason": "all platform repos use shared config"
          }
        ]
      },
      "auth": {
        "$repo": {
          "id": "auth-service",
          "git": "git@github.com:acme/platform-auth.git"
        }
      },
      "billing": {
        "$repo": {
          "id": "billing-service",
          "git": "git@github.com:acme/platform-billing.git"
        }
      }
    }
  }
}
```

Inherited behavior:

- `id` is the stable identity of the group and is not inherited.
- `description` is inherited by child repos/files when they do not define one.
- `web` is inherited by child repos when they do not define one.
- `archived` is inherited by child repos/files.
- `tags` are inherited additively by child repos/files.
- `meta` is inherited per key by child repos/files/dirs; the nearest declaration of a key wins.
- `setup`/`fmt`/`lint`/`test` are inherited by child repos that do not declare their own; nearest wins.
- `dependsOn` is inherited additively by child repos.
- `onlyWhen` is inherited additively by child repos/files.

## File Nodes

Declare files with `$file`.

```json
{
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "description": "Starts the local development stack",
        "executable": true
      }
    }
  }
}
```

The `src` format is:

```text
<repo-url>#<path-inside-source-repo>
```

`src` also accepts file URLs copied from a forge's web UI (GitHub, GitLab, Bitbucket, Gitea/Forgejo):

```text
https://github.com/acme/workspace-config/blob/main/scripts/dev.sh
https://gitlab.com/acme/workspace-config/-/blob/main/scripts/dev.sh
```

Such a URL is treated as the repository's https clone URL plus the in-repo path; line anchors and query parameters are ignored. The URL must point at the repository's default branch.

`src` may also be a list of sources; their contents are concatenated in order into the single generated file, with a newline inserted between parts when one is missing (intended for text content). List entries can be plain strings or objects with a per-source `onlyWhen`, gating just that section — the same shape `$dir` uses, but appending instead of merging trees. When every source is gated off, no file is generated (a previously written untouched file is removed). This assembles e.g. one `AGENTS.md` from sections that follow the installed repositories:

```json
{
  "$file": {
    "id": "agents-md",
    "src": [
      "git@github.com:acme/workspace-config.git#agents/base.md",
      { "src": "git@github.com:acme/workspace-config.git#agents/billing.md",
        "onlyWhen": { "path": "billing/*" } }
    ]
  }
}
```

Files are written during `clone` and `sync` when active. A file without an explicit `onlyWhen` is active when any repository in its scope is active or installed; the scope is the nearest ancestor path containing repositories (the whole workspace for root-level files). A support file placed next to a group of repos therefore follows those repos automatically. Installed files stay active until removed with `jig rm`.

Files can set `archived: true` to exclude them by default. Files already installed by Jig remain included; pass `--archived` to include uninstalled archived files too.

Jig records a hash for files it writes. If a user edits a generated file locally, Jig skips overwriting it and reports it as modified.

Files can also be symbolic links to other files in the same schema.

```json
{
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "executable": true
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": {"id": "dev-script"},
        "description": "Shortcut to the dev script"
      }
    }
  }
}
```

Rules for links:

- A `$file` defines exactly one of `src`, `link`, or `copy`.
- `link` is a reference object (`{"id": ...}` or exact `{"path": ...}`) naming another `$file` in the same schema.
- Jig creates relative symlinks.
- Link files are active only when their target file is active.
- Jig skips existing non-symlink paths instead of overwriting them.

A `$file` or `$dir` source list may include local sources — `{"file": "~/.codabox/MY-AGENTS.md", "optional": true}` for files, `{"dir": "~/.codabox/skills", "optional": true}` for directories (paths rooted at `~/` or `/`). Local content merges like any other source and local edits flow in on the next sync. With `optional`, a machine without the path simply composes without it — the schema can declare per-user extension points that most machines leave empty. Without `optional`, a missing source is reported and handled as unavailable.

A `$file` can declare `copy` instead of `link` with the same reference shape: Jig then materializes the target's sources as a real file rather than a symlink (the executable bit follows the target; the target must define `src`). Use `copy` when a consumer of the path does not follow symlinks reliably.

## Directory Nodes

Declare whole subtrees with `$dir`. The subtree of the source repository is materialized at the entry path; executable bits come from git. Omit `#path` to materialize the whole repository tree. Like `$file`, `src` also accepts directory URLs copied from a forge's web UI, such as `https://github.com/acme/workspace-config/tree/main/scripts/ci` (default branch only).

```json
{
  "tree": {
    "tools/ci-scripts": {
      "$dir": {
        "id": "ci-scripts",
        "src": "git@github.com:acme/workspace-config.git#scripts/ci"
      }
    }
  }
}
```

`src` may also be a list of sources; their trees are merged in order into the same directory, and when two sources provide the same file the last one wins (reported as shadowed) — list base layers first, overrides after. List entries can be plain strings or objects with a per-source `onlyWhen`, gating just that source's tree: when the condition stops matching, that source's untouched files are removed on the next sync. This assembles e.g. one `.agents/skills` directory from several skill repositories:

```json
{
  "$dir": {
    "id": "agent-skills",
    "src": [
      "git@github.com:acme/ez-skills.git#skills",
      { "src": "git@github.com:acme/billing-skills.git#skills",
        "onlyWhen": { "path": "billing/*" } }
    ]
  }
}
```

A `$dir` can instead declare `link` to become a relative symlink to another `$dir` entry, or `copy` to materialize the target's sources as a second real directory (for consumers that do not follow symlinks reliably — some agent harnesses reading skill directories) — for example one real `.agents/skills` aliased into every harness path:

```json
{
  "tree": {
    ".agents/skills":   { "$dir": { "id": "agent-skills", "src": ["git@github.com:acme/skills.git#skills"] } },
    ".opencode/skills": { "$dir": { "id": "opencode-skills", "link": {"path": ".agents/skills"} } },
    ".claude/skills":   { "$dir": { "id": "claude-skills", "copy": {"path": ".agents/skills"} } }
  }
}
```

Rules:

- Jig tracks a manifest of every file it wrote. Updates overwrite only untouched files; locally modified files are kept and reported.
- Files removed upstream are deleted locally only when untouched.
- Files the user adds inside the directory are never touched.
- A `$dir` defines exactly one of `src`, `link`, or `copy`. Link and copy dirs are active only when their target dir is active; removing a link dir removes only the symlink, and a copy converges on the target's sources on every sync.
- `$dir` supports `description`, `archived`, `tags`, `meta`, and `onlyWhen` like `$file`, but not `executable`.

## Conditional Nodes

Use `onlyWhen` to make a repo, file, or dir active only when some active or installed repository is selected by its reference — exactly one of `id`, `path`, or `tags` (see References), plus an optional `reason`:

```json
{ "onlyWhen": { "tags": ["api"], "reason": "API tooling for any API service" } }
{ "onlyWhen": { "id": "auth-service" } }
{ "onlyWhen": { "path": "services/*" } }
```

Tag conditions make support artifacts follow capabilities instead of locations: an API skill gated on `tags: ["api"]` materializes whenever any api-tagged repository is installed, with no schema edits as the catalog grows.

```json
{
  "tree": {
    ".agents/skills": {
      "$group": {
        "onlyWhen": {
          "path": "platform/*",
          "reason": "only useful when platform repos are installed"
        }
      },
      "platform": {
        "$file": {
          "id": "platform-skill",
          "src": "git@github.com:acme/workspace-config.git#agents/skills/platform.md"
        }
      }
    }
  }
}
```

In this example, `.agents/skills/platform` is only written when a repository under `platform/` is active or installed.

Inherited `onlyWhen` conditions are additive. All inherited and local conditions must match.

## Custom Metadata

Repos, files, dirs, and groups may carry a `meta` object: user-defined string keys and values that jig stores, displays (`jig info`), and filters on (`jig list --meta`), but never interprets. Use it for facts about entries that are not jig's business — for example, a GitLab repository can record where its synced GitHub mirror lives:

```json
{
  "platform/auth": {
    "$repo": {
      "git": "git@gitlab.com:acme/auth.git",
      "meta": { "github-mirror": "git@github.com:acme/auth.git" }
    }
  }
}
```

Keys must be non-empty, must not start with `$`, and must not contain spaces, commas, or `=`. Values are arbitrary strings. Meta never affects cloning, dependencies, or activation; external tooling (like a mirror-push script) reads it from `jig info` or `jig list --meta` output.

## Position-Relative Commands

Commands are aware of where they run inside the workspace:

- Pathless commands scope to the subtree of the current directory: `jig status` in `services/` reports only entries under `services`. Inside a repository checkout, pathless `status`, `pull`, and `checkout` address that one repository.
- Path arguments resolve like filesystem paths against the current directory: `.`, `..`, `../other`, and a leading `/` anchoring to the workspace root (`jig info /platform/auth`). Escaping the workspace is an error.
- Pathless `jig sync` in a subtree converges only what is installed there; `jig sync .` (an explicit path) materializes the whole subtree. `sync --prune` must run from the workspace root.
- Output always shows full workspace paths.

## Safe Paths

Workspace paths must be relative and use `/`.

Valid:

```text
platform/auth
services/checkout
.agents/skills/platform
```

Invalid:

```text
../outside
foo/../bar
~/file
/tmp/file
foo//bar
```

## Common Commands

Validate the definition:

```sh
jig validate
```

Validate a schema file directly (for example in the schema repository CI, no workspace needed):

```sh
jig validate jig.json
```

List defined groups, repositories, and files:

```sh
jig list
jig list services
jig list --archived
```

Discover which tags exist before filtering. `jig tags` lists the tags in scope with entry counts (pathless, it scopes to the current subtree like other commands):

```sh
jig tags
jig tags services
jig tags --archived
```

Filter by tags. `--tags a,b` keeps only entries carrying all the listed tags and works on `list`, `info`, `deps`, `clone`, `sync`, `pull`, `fetch`, `checkout`, and `status`. Dependencies of a selected repository are always included, tagged or not:

```sh
jig list --tags backend
jig clone services --tags backend,go
jig status --tags frontend
```

Select one entry by its stable id with `--id` on the same commands (plus the lifecycle verbs and `push`). It mirrors the schema's `{"id": ...}` selector: position-independent, includes archived entries, and cannot be combined with a path or `--tags`:

```sh
jig info --id auth-service
jig pull --id checkout-service
```

Path arguments select the node and everything under it; the explicit subtree form `services/*` is also accepted and equivalent.

Filter by custom metadata on `list`: `--meta key` keeps entries whose meta carries the key, `--meta key=value` additionally requires the exact value:

```sh
jig list --meta github-mirror
jig list --meta team=core
```

Show information about a repo, file, or group:

```sh
jig info platform
jig info services/checkout
jig info scripts/dev.sh
jig info legacy --archived
```

Show recursive dependencies for a path:

```sh
jig deps services/checkout
jig deps legacy --archived
```

Show the direct dependents instead — who consumes a repository (not recursive; edges through group paths count, so a repo depending on `platform` is a dependent of every repo under it). Optional edges need `--with-optional-deps`, as in the forward direction:

```sh
jig deps platform/auth --reverse
jig deps platform/auth --reverse --with-optional-deps
```

Print the dependency graph as a Mermaid flowchart (raw diagram, no markdown fence — wrap it in a ```` ```mermaid ```` fence for READMEs, or pipe to `mmdc` to render an image). Directories render as subgraphs, group dependencies point at the subgraph, optional edges are dashed:

```sh
jig graph
jig graph services
jig graph > docs/deps.mmd
```

Clone everything:

```sh
jig clone
```

Clone a path and its dependencies:

```sh
jig clone services/checkout
```

Clone or materialize files under a path:

```sh
jig clone .agents
```

Clone with optional dependencies:

```sh
jig clone services/checkout --with-optional-deps
```

Clone without any dependencies (just the selected repos):

```sh
jig clone services/checkout --no-deps
```

Clone uninstalled archived repositories and files too:

```sh
jig clone services --archived
```

Sync installed repositories and active files:

```sh
jig sync
```

Sync a specific path:

```sh
jig sync platform
```

Sync without pulling in dependencies (`--no-deps` also works on `clone` and `init --clone`):

```sh
jig sync platform --no-deps
```

Sync and delete entries that were removed from the schema (whole workspace only, cannot be combined with a path or `--tags`):

```sh
jig sync --prune
```

Pruning applies the same safety rules as `jig rm`: dirty or unpushed repositories, repositories whose origin changed, and locally modified files are kept and reported under `kept`; escalate per path with `jig rm -f` when you really mean it.

Renaming an entry's `id` in the schema is safe: sync readopts the existing checkout under the new identity (reported as `readopted`) instead of treating it as stale.

Sync uninstalled archived repositories and files too:

```sh
jig sync --archived
```

Run each repository's schema-declared lifecycle commands. This is how to work across many repos of mixed technologies without knowing each one's tooling: the schema maps the fixed verb (`setup`, `fmt`, `lint`, `test`) to each repo's own command, and jig runs it in every installed repo in scope. Repos without the command are counted, not failed. `setup` runs in dependency order to make fresh checkouts usable; the others run in parallel. Failures report the command's output and exit non-zero:

```sh
jig setup            # after clone: make the workspace runnable
jig fmt              # after bulk edits: run every repo's formatter
jig lint             # run every repo's checks, whatever they are
jig test services    # run the tests of one subtree
jig test --tags go   # run the tests of all go repos
```

Jig never runs these automatically — not on clone, sync, or update.

Pull installed repositories (fast-forward only):

```sh
jig pull
jig pull platform
jig pull --archived
```

Fetch installed repositories without touching working trees:

```sh
jig fetch
jig fetch platform
```

Push the current branch of installed repositories (never forced; rejected pushes are reported as skipped):

```sh
jig push
jig push services
jig push -u
```

Repositories with nothing to push report `up to date`. Branches with no upstream are skipped unless `-u` is passed, which pushes with `git push -u origin <branch>` and records the upstream — the natural follow-up to `jig checkout -b feature-x` and committing across repos. Only the current branch is pushed.

Switch installed repositories to a branch, mirroring `git checkout`:

```sh
jig checkout main
jig checkout -b feature-x services
jig checkout -b feature-x --tags backend
```

`-b` creates the branch at each repository's current HEAD, or just switches when it already exists. Repositories already on the branch report `present`. Checkouts are never forced: a repository where git refuses the switch (uncommitted changes that would be overwritten) is reported under `skipped` and left untouched.

Uninstall repositories or files (deletes the checkout and stops tracking it; `-r` for groups, `-f` to override the dirty/unpushed safety checks):

```sh
jig rm services/checkout
jig rm -r legacy
jig rm -r -f legacy
```

Deleting a repository directory by hand does not uninstall it: `jig sync` restores tracked repositories whose directory is missing. `jig rm` is the way to uninstall.

Show workspace status:

```sh
jig status
jig status services
jig status --archived
```

Status reports installed entries only; repos never installed are counted in the summary (pass `--all` to list them). Each line shows a glyph, path, branch, and notes; dirty repos spell out their counts, e.g. `dirty (14 changed, 3 untracked)`. Repositories with an upstream report ahead/behind commit counts (computed locally; run `jig fetch` first for fresh counts). `jig fetch && jig status` gives an overview of what changed across the workspace.

Show the uncommitted changes behind those dirty notes — one workspace-wide unified diff with workspace-relative paths (staged and unstaged against HEAD; untracked files are status's business). On a terminal the patch goes through the user's configured git pager (delta and friends) with their git color settings; piped output is plain, like git:

```sh
jig diff                  # the whole patch, like git diff over the tree
jig diff --stat           # one summary line per dirty repo: path, files, +/-
jig diff services --stat
jig diff codabox/core/codabox   # inspect one repo before keeping/discarding
```

Update the schema checkout from its Git remote (fast-forward only), without touching the workspace:

```sh
jig update
```

`jig sync` updates the schema itself before applying it, so a separate `jig update` is only needed to review incoming changes before syncing.

## Editing The Schema

The schema in `.jig/source/` is a normal Git working copy. To change the shared workspace definition:

```sh
$EDITOR .jig/source/.jig.json      # edit the schema
jig validate                       # check it
jig sync --no-update               # test it without fetching: jig reads the live file
git -C .jig/source commit -am "describe the change"
git -C .jig/source push            # publish to the team
```

If local schema edits conflict with upstream, `jig update` refuses to fast-forward; resolve with Git inside `.jig/source`.

## Update And Sync Model

Use `jig sync` to update the schema and apply it — the everyday command. When the schema cannot be updated (offline, broken upstream), sync reports it and applies the current schema instead.

Use `jig update` to update the schema alone, review the reported changes, then apply them with `jig sync`.

Use `jig sync --no-update` to apply the current definition without fetching — offline, or when testing local schema edits.

Use `jig pull` to update Git contents in already-installed repositories.

These are intentionally separate operations.

```text
jig update        -> update the map
jig update --sync -> update and apply the map
jig sync          -> apply the map to local checkout shape
jig pull          -> update Git repository contents
```

## Safety Rules

- Jig does not delete local repositories during `sync`, unless `--prune` is passed — and even then dirty or unpushed repositories and locally modified files are kept.
- Jig does not overwrite local file modifications.
- Jig skips existing directories that are not expected Git repositories.
- Jig skips existing files that it does not track in `.jig/state.json`.
- Jig records repo/file identities in `.jig/state.json` to handle moves safely.
