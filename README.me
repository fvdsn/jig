# Jig

Jig is a CLI tool for managing a local workspace made of many related Git repositories and generated support files.

It uses a `.jig.json` file to describe the desired workspace tree. Repositories are declared with `$repo`, files are declared with `$file`, and paths map directly to where things should appear on disk.

## Example

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

## Install For Development

Run directly from source:

```sh
go run . help
```

Or install the binary:

```sh
go install .
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

## Common Commands

```sh
jig init <git-url> [workspace-dir]
jig init <git-url> [workspace-dir] --clone <path>
jig validate
jig list
jig info <path>
jig deps <path>
jig clone <path>
jig sync [path]
jig pull [path]
jig status [path]
jig update
```

## Concepts

- `.jig.json` is the shared workspace definition.
- `.jig/state.json` is local state used to track installed repositories and generated files.
- Paths use workspace-style `/` separators, such as `services/checkout` or `platform`.
- A path may refer to one repository or a group of repositories.
- `jig clone <path>` clones matching repositories and their dependencies.
- `jig sync [path]` updates the local checkout shape without deleting local repositories.
- `jig pull [path]` runs `git pull` in installed repositories.
- `jig update` refreshes `.jig.json` from its configured source.

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

Jig tracks file hashes in `.jig/state.json` so it can update files it wrote while preserving local edits.
