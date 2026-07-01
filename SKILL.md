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

## Install

```sh
go install github.com/fvdsn/jig/cmd/jig@latest
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
jig init ./draft.json ~/Code/acme-test
```

When initialized from a local file, `.jig/source/` is created as a fresh Git repository containing the schema as `jig.json`, with no remote configured.

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
- `archived`: optional boolean; archived repos are excluded by default unless already installed. Pass `--archived` to include uninstalled archived repos too.
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

Files can set `archived: true` to exclude them by default. Files already installed by Jig remain included; pass `--archived` to include uninstalled archived files too.

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
- `link` points to another `$file` path in the same schema.
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

Sync uninstalled archived repositories and files too:

```sh
jig sync --archived
```

Pull installed repositories:

```sh
jig pull
jig pull platform
jig pull --archived
```

Show workspace status:

```sh
jig status
jig status services
jig status --archived
```

Update the schema checkout from its Git remote (fast-forward only):

```sh
jig update
```

Update the schema and immediately sync the workspace:

```sh
jig update --sync
jig update --sync services
```

## Editing The Schema

The schema in `.jig/source/` is a normal Git working copy. To change the shared workspace definition:

```sh
$EDITOR .jig/source/.jig.json      # edit the schema
jig validate                       # check it
jig sync                           # test it: jig reads the live file
git -C .jig/source commit -am "describe the change"
git -C .jig/source push            # publish to the team
```

If local schema edits conflict with upstream, `jig update` refuses to fast-forward; resolve with Git inside `.jig/source`.

## Update And Sync Model

Use `jig update` to update the schema from its remote.

Use `jig update --sync` to update the schema and then apply the updated map in one command.

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
