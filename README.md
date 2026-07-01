# Jig

Jig is a CLI tool for managing a local workspace made of many related Git repositories and generated support files.

It uses a `.jig.json` file to describe the desired workspace tree. Repositories are declared with `$repo`, files are declared with `$file`, and paths map directly to where things should appear on disk.

Directory nodes may also declare `$group` metadata. Group metadata such as `description`, `web`, `dependsOn`, and `onlyWhen` is inherited by child repositories or files where applicable.

## Example

```json
{
  "version": 1,
  "tree": {
    "platform": {
      "$group": {
        "description": "Shared platform services"
      },
      "auth": {
        "$repo": {
          "id": "auth-service",
          "git": "git@github.com:acme/platform-auth.git"
        }
      }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
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

Running:

```sh
jig clone services/checkout
```

clones `services/checkout` and its dependencies under the workspace root.

## Install

Install directly from GitHub:

```sh
go install github.com/fvdsn/jig/cmd/jig@latest
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

Then run:

```sh
jig help
```

## Common Commands

```sh
jig init <git-url> [workspace-dir]
jig init <local-jig-file> [workspace-dir]
jig init <git-url> [workspace-dir] --clone [path]
jig validate
jig list [path] [--archived]
jig info <path> [--archived]
jig deps <path> [--with-optional-deps] [--archived]
jig clone [path] [--with-optional-deps] [--archived] [--refresh]
jig sync [path] [--with-optional-deps] [--archived] [--refresh]
jig pull [path] [--archived]
jig status [path] [--archived]
jig update
jig update --sync [path] [--with-optional-deps] [--archived] [--refresh]
```

## Concepts

- `.jig.json` is the shared workspace definition.
- `.jig/state.json` is local state used to track installed repositories and generated files.
- Paths use workspace-style `/` separators, such as `services/checkout` or `platform`.
- A path may refer to one repository or a group of repositories.
- `jig clone [path]` clones/materializes all entries, or matching repositories/files when a path is provided.
- `jig sync [path]` updates the local checkout shape without deleting local repositories.
- `jig pull [path]` runs `git pull` in installed repositories.
- `jig update` refreshes `.jig.json` from its configured source.
- `jig update --sync` refreshes `.jig.json`, then syncs the workspace.
- Generated files are only refetched when missing or when their `src` changes; pass `--refresh` to refetch them unconditionally.
- Archived entries are excluded by default unless they are already installed. Pass `--archived` to include uninstalled archived entries too.

## Remote Jig File

The `.jig.json` file is designed to be hosted in a remote Git repository and shared by a team.

Use `jig init` to create a local workspace from that remote definition:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme
```

This fetches the remote `.jig.json`, writes it into the workspace, records the source repository, and creates local Jig state.

You can also initialize and clone a path in one command:

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme --clone services/checkout
```

Later, run `jig update` to refresh the local `.jig.json` from the configured remote source.

You can also initialize from a local Jig file while testing changes before pushing them:

```sh
jig init ./draft.jig.json ~/Code/acme-test
```

When initialized from a local file, Jig does not record a remote source.

## Files

Jig can also materialize files into the workspace:

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
