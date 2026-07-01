# Jig

Jig is a CLI tool for managing a local workspace made of many related Git repositories and generated support files.

A workspace is described by a schema file (usually `.jig.json` or `jig.json`) hosted in its own Git repository and shared by a team. Repositories are declared with `$repo`, files are declared with `$file`, and paths map directly to where things should appear on disk.

Directory nodes may also declare `$group` metadata. Group metadata such as `description`, `web`, `tags`, `dependsOn`, and `onlyWhen` is inherited by child repositories or files where applicable.

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
go install github.com/fvdsn/jig@latest
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

Then run:

```sh
jig help
```

## Common Commands

```sh
jig init <git-url> [workspace-dir] [--path <schema-path>]
jig init <local-schema-file> [workspace-dir]
jig init <git-url> [workspace-dir] --clone [path]
jig validate [schema-file]
jig list [path] [--archived] [--tags a,b]
jig info <path> [--archived] [--tags a,b]
jig deps <path> [--with-optional-deps] [--archived] [--tags a,b]
jig clone [path] [--with-optional-deps] [--archived] [--refresh] [--tags a,b]
jig sync [path] [--with-optional-deps] [--archived] [--refresh] [--tags a,b]
jig pull [path] [--archived] [--tags a,b]
jig remove <path>... [-r|--recursive] [-f|--force]
jig status [path] [--archived] [--tags a,b]
jig update
jig update --sync [path] [--with-optional-deps] [--archived] [--refresh] [--tags a,b]
```

## Workspace Layout

Everything jig manages lives under `.jig/` at the workspace root:

- `.jig/source/` is a Git checkout of the schema repository. The workspace always reads the schema live from this checkout.
- `.jig/config.json` records which file inside the checkout is the schema.
- `.jig/state.json` is local state tracking installed repositories and generated files.

The schema checkout works like any Git clone: the remote is its `origin`, and the tracked branch is whatever the checkout is on.

## Concepts

- Paths use workspace-style `/` separators, such as `services/checkout` or `platform`.
- A path may refer to one repository or a group of repositories.
- `jig clone [path]` clones/materializes all entries, or matching repositories/files when a path is provided.
- `jig sync [path]` converges the workspace to the schema: it moves renamed checkouts, fixes origins, refreshes files, and restores tracked repositories whose directory was deleted. It never uninstalls anything.
- `jig remove <path>...` uninstalls: it deletes the checkout and stops tracking it, refusing to delete dirty or unpushed repositories unless `--force` is given. Removing a group requires `-r`.
- `jig pull [path]` runs `git pull --ff-only` in installed repositories.
- `jig update` fast-forwards the schema checkout from its remote.
- `jig update --sync` updates the schema, then syncs the workspace.
- Archived entries are excluded by default unless they are already installed. Pass `--archived` to include uninstalled archived entries too.
- Generated files are only refetched when missing or when their `src` changes; pass `--refresh` to refetch them unconditionally.
- Entries may declare `tags: ["a", "b"]`; `--tags a,b` filters commands to entries carrying all the listed tags. Tags on groups are inherited by their children. Dependencies of a selected repository are always included, tagged or not.

## Editing the Schema

The schema in `.jig/source/` is a normal Git working copy, so testing and publishing changes is a plain Git workflow:

```sh
$EDITOR .jig/source/.jig.json   # edit the schema
jig sync                        # jig reads the live file, so test immediately
git -C .jig/source diff         # review
git -C .jig/source commit -am "add checkout service"
git -C .jig/source push         # publish to the team
```

Teammates pick the change up with `jig update --sync`. If your local schema edits conflict with upstream, `jig update` refuses to fast-forward and you resolve it with Git in `.jig/source` like any other repository.

## Initializing a Workspace

Create a local workspace from a schema repository:

```sh
jig init git@github.com:acme/jig-schema.git ~/Code/acme
```

This clones the schema repository into `.jig/source/`, finds the schema file (`.jig.json`, `jig.json`, or `schema.json` at the repository root, or the file given with `--path`), and creates local Jig state.

You can also initialize and clone a path in one command:

```sh
jig init git@github.com:acme/jig-schema.git ~/Code/acme --clone services/checkout
```

To experiment locally, initialize from a plain schema file:

```sh
jig init ./draft.json ~/Code/acme-test
```

This creates `.jig/source/` as a fresh Git repository containing your schema as `jig.json`. To promote the experiment to a shared schema later, add a remote and push:

```sh
git -C .jig/source remote add origin git@github.com:acme/jig-schema.git
git -C .jig/source push -u origin main
```

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
