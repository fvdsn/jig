# jig

[![ci](https://github.com/fvdsn/jig/actions/workflows/ci.yml/badge.svg)](https://github.com/fvdsn/jig/actions/workflows/ci.yml)
[![version](https://img.shields.io/github/v/tag/fvdsn/jig?label=version&sort=semver)](https://github.com/fvdsn/jig/tags)
[![go reference](https://pkg.go.dev/badge/github.com/fvdsn/jig.svg)](https://pkg.go.dev/github.com/fvdsn/jig)
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

```sh
go install github.com/fvdsn/jig@latest
```

Or grab a prebuilt binary for macOS or Linux from the [releases page](https://github.com/fvdsn/jig/releases).

## Quick start

```sh
jig init git@github.com:acme/jig-schema.git ~/Code/acme   # create a workspace from a shared schema
cd ~/Code/acme                  # (or start from scratch in an empty directory: jig init)
jig list                        # browse the catalog
jig clone services/checkout     # install a service + its dependencies
jig status                      # branches, dirty state, ahead/behind
jig fetch && jig status         # what changed across the workspace?
jig pull                        # fast-forward everything installed
jig rm services/checkout        # uninstall
```

## Commands

| Command | Description |
| --- | --- |
| `init [<git-url\|file> [dir]]` | Create a workspace from a schema repository or a local draft file; no args starts a fresh starter schema |
| `list [path]` | List the catalog: groups, repos, files, dirs |
| `info <path>` | Show one entry's metadata |
| `deps <path>` | Show a repo's recursive dependencies |
| `clone [path]` | Install repos/files matching a path, plus dependencies (`--no-deps` to skip them) |
| `sync [path]` | Converge the workspace: moves, origins, file updates, restores (`--prune` deletes what left the schema) |
| `pull [path]` | `git pull --ff-only` across installed repos, in parallel |
| `fetch [path]` | `git fetch` across installed repos, in parallel |
| `checkout [-b] <branch> [path]` | Switch installed repos to a branch (`-b` creates it); never discards local changes |
| `status [path]` | One line per installed entry, plus a summary |
| `rm <path>...` | Uninstall: delete the checkout and stop tracking it |
| `update [--sync]` | Fast-forward the schema from its remote (then sync) |
| `validate [file]` | Validate the schema — also usable in the schema repo's CI |
| `cache [clean]` | Inspect or prune the clone cache |

Most commands accept `--tags a,b` (entries carrying all listed tags), `--archived`, and paths that address a single entry or a whole subtree.

## The schema

A JSON tree where paths are the directory layout. Repos, files, and dirs are leaves; any level can carry group metadata that children inherit.

```json
{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "description": "Shared platform services", "tags": ["backend"] },
      "auth":   { "$repo": { "id": "auth-service", "git": "git@github.com:acme/auth.git" } },
      "dev.sh": { "$file": { "src": "git@github.com:acme/config.git#scripts/dev.sh", "executable": true } }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:acme/checkout.git",
        "tags": ["go", "api"],
        "dependsOn": [{ "path": "platform", "reason": "uses platform services" }]
      }
    },
    "tools/ci": {
      "$dir": { "src": "git@github.com:acme/config.git#scripts/ci" }
    }
  }
}
```

- **`$repo`** — a Git checkout. `id` is a stable identity that survives renames: move the entry in the schema and `jig sync` moves the checkout on disk.
- **`$file`** — a single generated file fetched from a source repo (or a symlink to another file via `link`). Updated on sync when the source changes; never overwritten if locally modified. `src` is `<clone-url>#<path>`, or simply a file URL pasted from the forge web UI (`https://github.com/o/r/blob/main/…`, GitLab/Bitbucket/Gitea equivalents; default branch only).
- **`$dir`** — a whole subtree materialized from a source repo (`<clone-url>#<subtree>`, or a pasted `…/tree/main/…` web URL). `src` may also be a list of sources merged in order (first wins on conflicts) — e.g. one `.agents/skills` assembled from several skill repositories; list entries can be `{ "src": ..., "onlyWhen": ... }` objects to gate individual sources. A `$dir` can instead declare `link` to become a relative symlink to another `$dir` — one real skills directory, symlinked into every harness path. Jig tracks a manifest of what it wrote, so updates touch only unmodified files and user files inside are never touched.
- **`$group`** — metadata on a directory: `description`, `tags`, `dependsOn`, `archived`, `onlyWhen` are inherited by everything beneath it.
- **`dependsOn`** — cloning a repo brings its dependency closure along (`optional: true` deps only with `--with-optional-deps`).
- **`onlyWhen`** — conditional entries, active only when some active or installed repository matches a `path`, carries all listed `tags`, or both — e.g. API skills materialize whenever anything tagged `api` is installed.
- **`archived`** — hidden and skipped by default, kept synced if already installed.

Files and dirs follow the repositories around them: a support file placed inside a group is materialized whenever any repo of that group is installed; root-level files follow the workspace as a whole.

## Agent skills

`$dir` is a natural fit for assembling one agent-skills directory for the whole workspace from several skill repositories. Jig itself ships a skill at `.agents/skills/jig/SKILL.md` that teaches agents how to drive it — use it as a source:

```json
{
  "tree": {
    ".agents/skills": {
      "$dir": {
        "id": "agent-skills",
        "src": [
          "https://github.com/fvdsn/jig/tree/master/.agents/skills",
          { "src": "git@github.com:acme/billing-skills.git#skills",
            "onlyWhen": { "path": "billing" } }
        ]
      }
    },
    ".claude/skills": { "$dir": { "id": "claude-skills", "link": ".agents/skills" } }
  }
}
```

`jig sync` merges the sources into `.agents/skills` (first wins on conflicts, `onlyWhen` gates per source) and symlinks it into harness-specific paths — every agent working in the workspace then knows how to use jig. Note the first source is a directory URL pasted straight from the GitHub UI; jig resolves it to the clone URL and subtree path.

## How it works

Everything jig manages lives under `.jig/` at the workspace root:

```text
.jig/source/       Git checkout of the schema repository — jig reads the schema live from it
.jig/config.json   which file inside the checkout is the schema
.jig/state.json    what is installed, and where
```

- **State records intent.** Deleting a checkout by hand doesn't uninstall it — `jig sync` restores it. `jig rm` is the uninstall verb, with `rm`-like ergonomics (`-r` for groups, `-f` to override the dirty/unpushed safety checks). `jig sync --prune` batch-deletes entries that were removed from the schema, with the same safety checks — anything dirty, unpushed, or locally modified is kept and reported.
- **The schema is a working copy.** Edit `.jig/source/<schema>`, test immediately with `jig sync`, then commit and push it like any repo. Teammates pick it up with `jig update --sync`. Conflicts are plain Git conflicts.
- **Clone cache.** Jig keeps a bare mirror per remote in the user cache directory; clones hardlink from it, so history transfers over the network once per machine. Checkouts stay fully independent — deleting the cache is always safe. `JIG_CACHE_DIR` relocates it (empty disables), `jig cache clean --unused 30` prunes it.
- **Terminal-aware output.** `list` and `status` align and truncate on a terminal; piped output stays full and tab-separated for scripts.

## Compatibility

- **Schemas are stable.** A `version: 1` schema keeps working across jig releases; schema and CLI behavior follow semver from v1.0.0.
- **Versioned formats fail loudly.** When jig meets a schema, workspace config, or state file with a version it does not understand, it stops with an "upgrade jig" error instead of guessing (or silently dropping fields a newer jig wrote).
- **Concurrent-safe.** Commands that mutate the workspace take a lock (`.jig/lock`); state files are written atomically.
- **Exit codes tell the truth.** Commands exit non-zero when any entry could not be brought to its desired state, so scripts and agents never mistake a partial run for success.
- **Platforms.** macOS and Linux are supported and tested in CI. Windows is not supported.

## Validating the schema in CI

In the schema repository:

```yaml
validate:
  image: golang:1.26
  script:
    - go install github.com/fvdsn/jig@latest
    - jig validate jig.json
```
