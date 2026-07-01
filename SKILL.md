# Jig Usage Guide

Jig manages a local workspace made of many Git repositories and generated support files.

Use Jig when a team has many related repositories and wants one shared `.jig.json` file to define how a workspace should be laid out, which repositories depend on which other repositories, and which helper files should be materialized locally.

## Core Idea

A workspace contains:

```text
.jig.json        shared workspace definition
.jig/state.json  local machine state
```

The `.jig.json` file is usually hosted in a Git repository and shared by a team. Users run `jig init` to fetch it and create a local workspace.

The `.jig/state.json` file is local and machine-owned. It tracks installed repositories and generated files so Jig can safely handle moves, remote changes, and local edits.

## Install

```sh
go install github.com/fvdsn/jig@latest
```

Make sure `$(go env GOPATH)/bin` is in `PATH`.

## Initialize A Workspace

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
jig init ./draft.jig.json ~/Code/acme-test
```

When initialized from a local file, Jig does not record a remote `source` in `.jig.json`.

## Definition Shape

The definition uses a top-level `tree`.

```json
{
  "version": 1,
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
        "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
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
- `archived`: optional boolean; archived repos are skipped by clone/sync unless `--archived` is passed.
- `dependsOn`: optional dependency list.
- `onlyWhen`: optional activation condition.

If `id` is omitted, the repository path is used as the identity.

## Dependencies

Dependencies use workspace paths.

```json
{
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@github.com:acme/checkout.git",
        "dependsOn": [
          {
            "path": "platform",
            "reason": "checkout uses platform services"
          }
        ]
      }
    }
  }
}
```

The dependency path `platform` matches all repositories under `platform/`, such as:

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

- `description` is inherited by child repos/files when they do not define one.
- `web` is inherited by child repos when they do not define one.
- `archived` is inherited by child repos/files.
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
        "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "description": "Starts the local development stack",
        "executable": true
      }
    }
  }
}
```

The `src` format is:

```text
git:<repo-url>#<path-inside-source-repo>
```

Files are written during `clone` and `sync` when active.

Files can set `archived: true` to opt out of clone/sync unless `--archived` is passed.

Jig records a hash for files it writes. If a user edits a generated file locally, Jig skips overwriting it and reports it as modified.

Files can also be symbolic links to other files in the same schema.

```json
{
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "executable": true
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": "scripts/dev.sh",
        "description": "Shortcut to the dev script"
      }
    }
  }
}
```

Rules for links:

- A `$file` defines exactly one of `src` or `link`.
- `link` points to another `$file` path in the same `.jig.json`.
- Jig creates relative symlinks.
- Link files are active only when their target file is active.
- Jig skips existing non-symlink paths instead of overwriting them.

## Conditional Nodes

Use `onlyWhen` to make a repo or file active only when another repository path or group is active or installed.

```json
{
  "tree": {
    ".agents/skills": {
      "$group": {
        "onlyWhen": {
          "path": "platform",
          "reason": "only useful when platform repos are installed"
        }
      },
      "platform": {
        "$file": {
          "id": "platform-skill",
          "src": "git:git@github.com:acme/workspace-config.git#agents/skills/platform.md"
        }
      }
    }
  }
}
```

In this example, `.agents/skills/platform` is only written when a repository under `platform/` is active or installed.

Inherited `onlyWhen` conditions are additive. All inherited and local conditions must match.

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

List defined repositories and files:

```sh
jig list
jig list services
jig list --archived
```

Show information about a repo, file, or group:

```sh
jig info platform
jig info services/checkout
jig info scripts/dev.sh
```

Show recursive dependencies for a path:

```sh
jig deps services/checkout
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

Clone archived repositories and files too:

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

Sync archived repositories and files too:

```sh
jig sync --archived
```

Pull installed repositories:

```sh
jig pull
jig pull platform
```

Show workspace status:

```sh
jig status
jig status services
jig status --archived
```

Update `.jig.json` from its configured remote source:

```sh
jig update
```

Update `.jig.json` and immediately sync the workspace:

```sh
jig update --sync
```

## Update And Sync Model

Use `jig update` to update the definition file.

Use `jig update --sync` to update the definition file and then apply the updated map in one command.

Use `jig sync` to apply the current definition to the local workspace.

Use `jig pull` to update Git contents in already-installed repositories.

These are intentionally separate operations.

```text
jig update        -> update the map
jig update --sync -> update and apply the map
jig sync          -> apply the map to local checkout shape
jig pull          -> update Git repository contents
```

## Safety Rules

- Jig does not delete local repositories during `sync`.
- Jig does not overwrite local file modifications.
- Jig skips existing directories that are not expected Git repositories.
- Jig skips existing files that it does not track in `.jig/state.json`.
- Jig records repo/file identities in `.jig/state.json` to handle moves safely.
