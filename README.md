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
jig fetch [path] [--archived] [--tags a,b]
jig rm <path>... [-r|--recursive] [-f|--force]
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

## Clone Cache

Jig keeps a bare mirror of each remote it clones under the user cache directory (`~/Library/Caches/jig/repos` on macOS, `~/.cache/jig/repos` on Linux). Clones freshen the mirror with a cheap fetch and then clone locally from it, so a repository's history only transfers over the network once per machine — repeat clones (new workspaces, restores) run at disk speed.

Clones are fully independent of the cache (local clones hardlink immutable object files), so deleting the cache directory is always safe. Any cache failure falls back to a direct network clone. Set `JIG_CACHE_DIR` to relocate the cache, or set it to an empty string to disable it.

`jig cache` shows the cache location and size; `jig cache clean --unused 30` removes mirrors not used in the last 30 days (omit `--unused` to remove everything).

## Concepts

- Paths use workspace-style `/` separators, such as `services/checkout` or `platform`.
- A path may refer to one repository or a group of repositories.
- `jig clone [path]` clones/materializes all entries, or matching repositories/files when a path is provided.
- `jig sync [path]` converges the workspace to the schema: it moves renamed checkouts, fixes origins, refreshes files, and restores tracked repositories whose directory was deleted. It never uninstalls anything.
- `jig rm <path>...` uninstalls: it deletes the checkout and stops tracking it, refusing to delete dirty or unpushed repositories unless `--force` is given. Removing a group requires `-r`.
- `jig pull [path]` runs `git pull --ff-only` in installed repositories.
- `jig fetch [path]` runs `git fetch` in installed repositories without touching working trees.
- `jig status` shows one line per entry: glyph, path, branch, and notes including dirty state and ahead/behind counts against upstream (run `jig fetch` first for fresh counts).
- `jig update` fast-forwards the schema checkout from its remote.
- `jig update --sync` updates the schema, then syncs the workspace.
- Archived entries are excluded by default unless they are already installed. Pass `--archived` to include uninstalled archived entries too.
- `jig sync` updates generated files when their source repository changed, using a cheap blob comparison against the clone cache; locally modified files are never overwritten. `--refresh` forces a rewrite.
- Files and dirs without an explicit `onlyWhen` follow the repositories around them: they are materialized when any repository in their scope (the nearest ancestor path containing repositories, or the whole workspace for root-level entries) is active or installed. Once installed, they stay maintained until `jig rm`.
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

## Files and Directories

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

Whole subtrees can be materialized with `$dir`. Executable bits come from the git tree, and jig keeps a manifest of every file it wrote: updates overwrite only untouched files, files removed upstream are deleted only when untouched, and files you add inside the directory are never touched. Omit the `#path` to materialize a whole repository's tree.

```json
{
  "tree": {
    "tools/ci-scripts": {
      "$dir": {
        "id": "ci-scripts",
        "src": "git:git@github.com:acme/workspace-config.git#scripts/ci"
      }
    }
  }
}
```
