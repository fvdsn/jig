# Jig Specification

## Purpose

Jig is a CLI tool for working with many related Git repositories and workspace files. It uses a declarative schema file to describe the desired workspace tree, then clones repositories, materializes files, updates local checkouts, and reports workspace status.

The primary goal is to make it easy to create and maintain a local workspace containing the repositories and support files needed for development.

## Workspace Definition File

The shared workspace definition (the schema) lives in its own Git repository. Each workspace keeps a full checkout of that repository at `.jig/source/`, and Jig reads the schema live from that checkout. `.jig/config.json` records which file inside the checkout is the schema.

The schema file is usually named `.jig.json`, `jig.json`, or `schema.json` at the root of the schema repository; `jig init --path` selects any other safe path.

Initial schema:

```json
{
  "version": 2,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:org/platform-auth.git",
        "web": "https://github.com/org/platform-auth",
        "description": "Authentication service"
      }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:org/checkout.git",
        "description": "Checkout service",
        "dependsOn": [
          {
            "path": "platform/*",
            "reason": "checkout uses shared platform services"
          }
        ]
      }
    },
    "tools": {
      "$group": {
        "description": "Developer tools",
        "onlyWhen": {
          "path": "platform/*",
          "reason": "Only useful when platform repositories are installed"
        }
      },
      "platform-debug": {
        "$repo": {
          "id": "platform-debug-tools",
          "git": "git@github.com:org/platform-debug-tools.git"
        }
      }
    },
    ".agents/skills/platform": {
      "$file": {
        "id": "platform-skill",
        "src": "git@github.com:org/workspace-config.git#agents/skills/platform.md",
        "description": "Agent skill for platform repositories",
        "onlyWhen": {
          "path": "platform/*",
          "reason": "Only useful when platform repositories are installed"
        }
      }
    }
  }
}
```

## Top-Level Fields

### `version`

Required integer.

Identifies the schema version used by the definition file.

Current version: `2`. Version 2 introduced structured references (see References); version 1 schemas are refused with an error pointing at the reference format change.

### `source`

Deprecated and ignored. The schema's remote is the `origin` of the `.jig/source/` checkout, and the tracked branch is whatever the checkout is on, exactly as with any Git clone. Older schemas that still carry a `source` object continue to parse.

### `tree`

Required object.

Describes the workspace file tree.

Tree nodes may be directories, repositories, or files.

Repository and file nodes use reserved marker keys:

- `$repo`
- `$file`
- `$group`

Keys starting with `$` are reserved for Jig.

Tree keys may contain `/` as shorthand for nested directories.

These two definitions are equivalent:

```json
{
  "tree": {
    "platform": {
      "auth": {
        "$repo": {
          "git": "git@github.com:org/platform-auth.git"
        }
      }
    }
  }
}
```

```json
{
  "tree": {
    "platform/auth": {
      "$repo": {
        "git": "git@github.com:org/platform-auth.git"
      }
    }
  }
}
```

After expansion, `platform/auth` is both the logical repository path and the local checkout path.

## Safe Paths

Jig paths are relative workspace paths using `/` as the separator.

This applies to:

- Expanded tree paths.
- Repository paths.
- File destination paths.
- Reference `path` selectors (before their optional trailing `/*`; see References).
- CLI path arguments.
- Paths stored in `.jig/state.json`.

Workspace paths must:

- Be non-empty.
- Be relative.
- Not start with `/`.
- Not start with `~`.
- Not contain empty segments.
- Not contain `.` or `..` segments.

Hidden directories are allowed. For example, `.agents/skills/platform` is valid.

Invalid examples:

```text
.
..
../outside
foo/../bar
~/file
/tmp/file
foo//bar
```

Source repo file paths use the same safety rules, but are interpreted relative to the source Git repository.

## References

Schema entries reference each other in `dependsOn`, `onlyWhen` (including per-source conditions), and `link`. Every reference is a structured object carrying **exactly one** selector field; there are no string reference forms in the schema. The selector fields are:

- `id`: the identity of one entry, stable across path changes. Ids are globally unique across all entry kinds, so an `id` selector is never ambiguous.
- `path`: an exact expanded tree path that must name a declared entry — or, with a trailing `/*`, the subtree strictly below it.
- `tags`: a list of tags; selects every repository carrying all of them (declared or inherited). Multiple tags are conjunctive, matching the `--tags` CLI flag.

Path selectors are exact: `{"path": "platform/auth"}` names the entry declared at `platform/auth` and nothing else. A path that names no declared entry is a validation error, so renames and typos fail loudly instead of silently matching less.

The subtree marker `/*` is the only wildcard. It may appear only as the entire final segment (`platform/*`, or bare `*` for the whole tree), it always means the full recursive subtree strictly below the path (not the node itself), and there is no other glob syntax. A subtree selector that matches nothing is a validation error.

A path selector naming a `$group` entry exactly is a validation error with a hint to use `path + "/*"`; groups are containers, and referencing their contents must be explicit.

Reference sites have one of two arities, enforced by validation:

- **Selector sites** (`dependsOn` entries, `onlyWhen` conditions) accept any of the three selector fields and may resolve to zero or more repositories, plus site-specific extras (`optional`, `reason`).
- **Single-target sites** (`$file.link`, `$dir.link`) accept only `id` or `path`, forbid `/*`, and must resolve to exactly one entry of the required kind.

The CLI mirrors the three selectors: the path positional, `--id`, and `--tags`. As a human interface the CLI keeps its filesystem feel — a bare path positional selects the node *and* its subtree, position-aware relative to the current directory — and also accepts an explicit trailing `/*`, which selects the same set. `--id` selects exactly one entry and cannot be combined with a path positional or `--tags`.

## Position-Relative CLI Paths

CLI path arguments and pathless commands are interpreted relative to the directory the command runs in (the subdirectory of the workspace root; `.jig` counts as the root):

- A pathless command uses the current subtree as its selection. At the workspace root this selects everything, preserving root behavior.
- Path arguments resolve like filesystem paths: `.` is the current subtree, `..` climbs, and a leading `/` anchors to the workspace root. A resolved path escaping the workspace is an error.
- Pathless `sync` converges installed entries within the current subtree only; an explicit path (including `.`) materializes the selection as before. `sync --prune` requires the workspace root.
- Resolution is CLI-side only: schema paths are unchanged and still reject `.`, `..`, and leading `/`.
- Reported paths remain workspace-relative regardless of where the command runs.

## Tree Node Rules

A tree node must be one of:

- Directory node.
- Repository node containing `$repo`.
- File node containing `$file`.

A directory node may contain `$group` alongside child nodes.

A node containing `$repo` or `$file` must not contain child nodes.

A node must not contain both `$repo` and `$file`.

## Group Nodes

Group nodes are declared with `$group` on directory nodes.

`$group` describes the group and provides inherited metadata and behavior for descendant `$repo` and `$file` nodes.

Example:

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
      }
    }
  }
}
```

### `$group.id`

Optional string.

Stable group identity. If omitted, the group path is used as its identity.

Identities are globally unique across all entry kinds; see the identity rules under `$repo.id`.

### `$group.description`

Optional string.

Human-readable group description.

Descendant repositories and files inherit this description when they do not define their own description.

Nearest ancestor wins.

### `$group.web`

Optional string.

Web URL for the group.

Descendant repositories inherit this value when they do not define their own `web` value.

Nearest ancestor wins.

### `$group.setup`, `$group.fmt`, `$group.lint`, `$group.test`

Optional strings.

Lifecycle commands inherited by descendant repositories that do not declare their own. Nearest ancestor wins, as for `web`. This lets a homogeneous group declare e.g. `"lint": "make check"` once.

### `$group.archived`

Optional boolean.

Default: `false`.

Marks the group and all descendant repositories and files as archived.

Archived repositories and files are excluded by default unless already installed. `--archived` includes uninstalled archived entries too.

### `$group.tags`

Optional list of strings.

Tags label entries for filtering with the `--tags` CLI flag. Tags must be non-empty and must not contain spaces or commas. Group tags are inherited additively by descendant repositories and files.

### `$group.meta`

Optional object mapping user-defined keys to string values.

Custom metadata inherited by all descendant repositories, files, and dirs. Inheritance is per key: a descendant's effective meta is the union of ancestor and local keys, and the nearest declaration of a key wins.

See [Custom Metadata](#custom-metadata).

### `$group.dependsOn`

Optional array.

Dependencies inherited by all descendant repositories.

Inherited dependencies are additive. Ancestor dependencies are applied before repository-local dependencies.

Files do not inherit dependencies because files are not dependency graph nodes.

### `$group.onlyWhen`

Optional object.

Condition inherited by all descendant repositories and files.

Inherited `onlyWhen` conditions are additive. A descendant is active only when all inherited and local conditions match.

## Group Inheritance

When flattening the tree:

- `description` is inherited by descendant repositories and files when they do not define one locally. The nearest value wins.
- `web` is inherited by descendant repositories when they do not define one locally. The nearest value wins.
- `setup`, `fmt`, `lint`, and `test` are inherited by descendant repositories when they do not define their own. The nearest value wins.
- `archived` is inherited by descendant repositories and files. A descendant cannot opt out of an archived ancestor.
- `tags` are inherited additively by descendant repositories and files. An entry effective tag set is the union of its declared tags and all ancestor group tags.
- `meta` is inherited per key by descendant repositories, files, and dirs. The nearest declaration of a key wins; other keys pass through.
- `dependsOn` is inherited additively by descendant repositories. Ancestor dependencies come before local dependencies.
- `onlyWhen` is inherited additively by descendant repositories and files. All conditions must match.

Inheritance applies to the expanded tree: a `$group` declared at `services` applies to `services/checkout` whether the child is written nested inside the group node or as a flat `services/checkout` key.

`jig info <group>` should show `$group` metadata when present.

## Repository Nodes

Repository nodes are declared with `$repo`.

Example:

```json
{
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:org/platform-auth.git",
        "web": "https://github.com/org/platform-auth",
        "description": "Authentication service",
        "dependsOn": [
          {
            "path": "platform/billing",
            "reason": "auth emits billing audit events"
          }
        ]
      }
    }
  }
}
```

### `$repo.id`

Optional string.

Stable repository identity.

The tree path is the current logical path and local path of the repository. The optional `id` field identifies the repository across path changes, renames, and hosting changes.

If `id` is omitted, the repository path is used as the identity.

Identities are globally unique across all entry kinds after applying this rule: no two entries — repositories, files, dirs, or groups — may resolve to the same identity. This is what makes an `id` reference selector unambiguous (see References).

### `$repo.git`

Required string.

The Git clone URL for the repository.

### `$repo.web`

Optional string.

The web URL for the repository.

### `$repo.description`

Optional string.

A short human-readable description of the repository.

### `$repo.setup`, `$repo.fmt`, `$repo.lint`, `$repo.test`

Optional strings.

The repository's lifecycle commands, run through the shell in the checkout by the matching jig command (`jig setup`, `jig fmt`, `jig lint`, `jig test`) — and only then, never as a side effect of clone, sync, or update.

See [Repository Lifecycle Commands](#repository-lifecycle-commands).

### `$repo.archived`

Optional boolean.

Default: `false`.

Archived repositories remain valid definition entries. They are excluded by default unless already installed; `--archived` includes uninstalled archived repositories too.

### `$repo.tags`

Optional list of strings.

Tags label entries for filtering with the `--tags` CLI flag. Tags must be non-empty and must not contain spaces or commas.

### `$repo.meta`

Optional object mapping user-defined keys to string values.

See [Custom Metadata](#custom-metadata).

### `$repo.dependsOn`

Optional array.

Lists dependencies for this repository. Each dependency is a reference selector — one repository by `id` or exact `path`, a subtree with `path` ending in `/*`, or every repository carrying `tags` — with optional `optional` and `reason` fields. See Dependency Fields.

If omitted, the repository has no declared dependencies.

### `$repo.onlyWhen`

Optional object.

Conditionally includes the repository only when another repository path or group is active.

If inherited `onlyWhen` conditions are present, all inherited conditions and the local condition must match.

See [Conditional Nodes](#conditional-nodes).

## File Nodes

File nodes are declared with `$file`.

Example:

```json
{
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git@github.com:org/workspace-config.git#scripts/dev.sh",
        "description": "Starts the local development stack",
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

### `$file.id`

Optional string.

Stable file identity.

If omitted, the file destination path is used as the identity.

Identities are globally unique across all entry kinds; see the identity rules under `$repo.id`.

### `$file.src`

Optional string or list.

Identifies where the file content comes from.

Source syntax:

```text
<repo-url>#<safe-source-repo-file-path>
```

Examples:

```text
git@github.com:org/workspace-config.git#compose/backend.yml
https://github.com/org/workspace-config.git#scripts/dev.sh
```

Parsing rules:

- The string is split at the last `#`.
- The left side is the Git URL.
- The right side is a safe source repo file path.
- The source repo file path must not contain `#`.
- A legacy `git:` prefix is accepted and ignored (real `git://` protocol URLs are untouched).

`src` may also be a list of such sources; their contents are concatenated in order into the single generated file. A list entry may be an object `{"src": ..., "onlyWhen": ...}`; the per-source condition gates just that source's content within the concatenation, evaluated against active and installed repositories — the same shape `$dir` uses, but appending instead of merging trees. Rules:

- All active sources are resolved before the file is written; parts are joined in list order.
- A source that fails to resolve (unreachable repository, file missing upstream) is excluded from that run and reported on the status line, mirroring `$dir`. A file not yet written is generated from the available parts; an already-written untouched file is left as is while any source is unavailable, since regenerating without the missing part would drop that source's content — the full rewrite happens once every source resolves. When no source resolves, an existing file is left untouched and reported present; a file never written is an error. A malformed source spec is always an error.
- A newline is inserted between two parts when the earlier one does not end with one, so text sections never run together. Multi-source files are therefore intended for text content.
- A condition flip changes the expected content: the file is rewritten on the next sync when untouched, and a locally modified file is reported instead of overwritten, as usual.
- When every source is gated off, no file is generated: a previously written untouched file is removed and dropped from state; a locally modified one is kept but left untracked.

This assembles e.g. one `AGENTS.md` from a base section plus sections that follow the installed repositories:

```json
{
  "$file": {
    "id": "agents-md",
    "src": [
      "git@github.com:org/workspace-config.git#agents/base.md",
      { "src": "git@github.com:org/workspace-config.git#agents/billing.md",
        "onlyWhen": { "path": "billing/*" } }
    ]
  }
}
```

### `$file.link`

Optional reference object.

Declares the file node as a symbolic link to another `$file` node in the same schema. The reference is a single-target selector (see References): exactly one of `id` and `path`, where `path` is exact — no `/*`, no `tags`.

Example:

```json
{
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git@github.com:org/workspace-config.git#scripts/dev.sh"
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": {"id": "dev-script"}
      }
    }
  }
}
```

Rules:

- A `$file` must define exactly one of `src` or `link`.
- `link` must resolve to exactly one other `$file` node in the same schema; referencing any other kind, or the link's own node, is a validation error.
- Link files are active only when their own conditions match and their target file is active.
- Jig creates relative symlinks so workspaces remain movable.
- `executable` applies only to `src` files.

### `$file.description`

Optional string.

A short human-readable description of the file.

### `$file.executable`

Optional boolean.

Default: `false`.

If true, Jig sets executable permissions after writing the file.

### `$file.archived`

Optional boolean.

Default: `false`.

Archived files remain valid definition entries. They are excluded by default unless already installed; `--archived` includes uninstalled archived files too.

If a link file points to an archived target file, the link is also skipped unless the target is already installed or `--archived` is passed.

### `$file.tags`

Optional list of strings.

Tags label entries for filtering with the `--tags` CLI flag. Tags must be non-empty and must not contain spaces or commas.

### `$file.meta`

Optional object mapping user-defined keys to string values.

See [Custom Metadata](#custom-metadata).

### `$file.onlyWhen`

Optional object.

Conditionally includes the file only when another repository path or group is active.

If inherited `onlyWhen` conditions are present, all inherited conditions and the local condition must match.

See [Conditional Nodes](#conditional-nodes).

## Directory Nodes

Directory nodes are declared with `$dir` and materialize a whole subtree of a source repository at the entry path.

```json
{
  "tree": {
    "tools/ci-scripts": {
      "$dir": {
        "id": "ci-scripts",
        "src": "git@github.com:org/workspace-config.git#scripts/ci"
      }
    }
  }
}
```

Fields: `id` (optional identity, defaults to the path), `src` (required, `<repo-url>[#<subtree-path>]` or a list of such sources; without a path the whole repository tree is materialized), `description`, `archived`, `tags`, `meta`, and `onlyWhen` behave as for `$file`. There is no `executable` field (modes come from the git tree). A directory node may declare `link` instead of `src`: a single-target reference (`{"id": ...}` or exact `{"path": ...}`, as for `$file.link`) making the node a relative symlink to another `$dir` entry. Exactly one of `src` and `link` is required; link dirs are active only when their target dir is active, targets are materialized before links, link cycles are validation errors, and removing a link dir removes only the symlink.

State records the source tree id and a manifest mapping each written file to its content hash. Rules:

- The subtree is extracted from the source repository's cache mirror without a checkout.
- With multiple sources, trees are merged in order into the same directory; when two sources provide the same file path, the last source wins and the shadowed file is reported, so a source list reads as base layers first and overrides after. All active sources are resolved before any file is written.
- A source that fails to resolve (unreachable repository, subtree missing upstream) is excluded from that run's merge and reported on the status line; the remaining sources still materialize. While any source is unavailable nothing is deleted, and files that vanished from the merge stay tracked so they update normally once the source resolves again. When no source resolves, an already-written directory is left untouched and reported present; a directory that was never written is an error. A malformed source spec is always an error.
- A list entry may be an object `{"src": ..., "onlyWhen": ...}`; the per-source condition gates just that source's tree within the merge, evaluated against active and installed repositories. Files of a source whose condition stops matching are removed on the next sync when untouched.
- Updates overwrite only files whose local content matches the manifest; locally modified files are kept and reported.
- Files that disappear upstream are deleted locally only when their content matches the manifest; modified ones are left behind as untracked.
- Files the user adds inside the directory are never touched or deleted.
- `jig rm` deletes only manifest-tracked files, refusing when any is locally modified unless forced.
- Status reports one line per directory entry with aggregated modified/missing counts.

## Dependency Fields

Each `dependsOn` entry is a reference selector (see References) plus the extras below. Exactly one of `id`, `path`, and `tags` is required.

### `id`

Selects one repository by identity, stable across path changes:

```json
{
  "id": "auth-service"
}
```

The id must belong to a repository; an id naming a file, dir, or group is a validation error.

### `path`

Selects one repository by exact path, or a subtree of repositories with a trailing `/*`:

```json
{
  "path": "platform/auth"
}
```

```json
{
  "path": "platform/*",
  "reason": "everything under platform"
}
```

An exact path must name a declared repository — a path matching nothing, or naming a group without `/*`, is a validation error. A subtree selector takes every repository strictly below the path and must match at least one (archived included).

### `tags`

Selects every repository carrying all the listed tags (declared or inherited):

```json
{
  "tags": ["base"]
}
```

At least one repository in the schema must carry all the tags (archived included), which catches misspelled tags.

### `optional`

Optional boolean.

Indicates whether this dependency is optional for normal local development.

Default: `false`.

Most dependencies should omit this field. Optional dependencies are represented with `optional: true`.

### `reason`

Optional string.

Explains why the dependency exists.

This is intended for humans and should not affect dependency resolution.

## Repository Lifecycle Commands

Working across many repositories of mixed technologies requires a standard vocabulary for per-repo operations: a Go repository lints with `golangci-lint run`, a Node repository with `npm run checks`, and nobody working fleet-wide (human or agent) should need to know which is which. The schema declares each repository's own implementation behind four fixed verbs:

```json
"$repo": {
  "git": "git@gitlab.com:acme/checkout.git",
  "setup": "make bootstrap",
  "fmt": "npm run fmt",
  "lint": "npm run checks",
  "test": "npm test"
}
```

- `setup` — bring a fresh checkout to a usable state (install dependencies, create local config).
- `fmt` — run the repository's formatter.
- `lint` — run the repository's checks.
- `test` — run the repository's test suite.

The vocabulary is deliberately fixed and closed. A verb belongs here only if it is a short-lived, repo-scoped command that terminates with a verdict; this excludes long-running commands (`start`, `watch` — process supervision is a different program shape), free-form script maps (which would only enable standardization where fixed verbs enforce it), and content-authoring steps (`commit`, PR creation — per-forge tooling is already uniform, so the schema adds nothing). `build` and `teardown` (undoing out-of-checkout setup state) are the reserved future additions, added only on real demand.

Execution rules:

- Commands run through the shell with the checkout as working directory.
- Jig runs them only when the user invokes the matching command — never as a side effect of `clone`, `sync`, or `update`. The schema carries a short, reviewable entrypoint; the logic belongs in the repository, reviewed by the people who own it.
- `jig setup` runs sequentially in dependency order: a repository's setup may rely on its dependencies being set up. `jig fmt`, `jig lint`, and `jig test` run in parallel.
- Selection matches `jig pull`: installed repositories under the path, with `--tags` and `--archived` as usual.
- Repositories without the verb's command are counted (`N repositories define no lint command`), not failed — the org can fill the schema in incrementally.
- One line per repository as it completes: `<verb>: <path>` on success, `failed: <path>` on failure — failures surface live, not only at the end. After the run, the `failed` group repeats each failure with its exit status and captured output, and the run exits non-zero.
- While repositories run, a transient status line on stderr shows the done counter and the entries in flight; it renders only when stderr is a terminal, so piped output stays clean.

## Custom Metadata

Repositories, files, dirs, and groups may carry a `meta` object: user-defined string keys mapped to string values. Jig stores, displays, and filters on meta, but never interprets it — it is the extension point for facts about entries that are not jig's business. For example, a GitLab repository can record where its synced GitHub clone lives:

```json
{
  "platform/auth": {
    "$repo": {
      "git": "git@gitlab.com:acme/auth.git",
      "meta": {
        "github-mirror": "git@github.com:acme/auth.git"
      }
    }
  }
}
```

Rules:

- Keys must be non-empty, must not start with `$` (reserved), and must not contain spaces, commas, or `=`.
- Values are arbitrary strings.
- `$group.meta` is inherited per key by descendant repositories, files, and dirs; the nearest declaration of a key wins.
- `jig info` shows an entry's effective meta.
- `jig list --meta <key>` keeps only entries whose effective meta carries the key; `--meta <key>=<value>` additionally requires the exact value.

Meta never affects planning, dependency resolution, or activation. External tooling that acts on meta (such as a mirror-push script) reads it from the schema or from `jig` output.

## Conditional Nodes

Use `onlyWhen` to make a repo, file, or dir active only when some active or installed repository satisfies the condition.

A condition is a reference selector (see References): exactly one of `id`, `path`, and `tags`, plus an optional `reason` documenting it. The condition holds when some active or installed repository is selected by it:

- `id`: that repository is active or installed.
- `path` (exact): the repository at that path is active or installed.
- `path` with `/*`: some repository strictly below the path is active or installed.
- `tags`: some active or installed repository carries all the tags.

```json
{ "onlyWhen": { "id": "auth-service" } }
{ "onlyWhen": { "path": "platform/auth" } }
{ "onlyWhen": { "path": "platform/*", "reason": "platform tooling" } }
{ "onlyWhen": { "tags": ["api"] } }
```

Validation requires each condition to be satisfiable by at least one repository in the schema (archived included), which catches path typos and misspelled tags; exact paths and ids must additionally name a declared repository.

Inherited `onlyWhen` conditions are additive. All inherited and local conditions must match.

## File And Dir Activation

Files and dirs are support artifacts: they are materialized as a side effect of the repositories they support, or by explicit selection.

A file or dir is active when the first of these rules applies:

- It is already installed (state records intent, mirroring repositories): it stays maintained until removed with `jig rm`.
- It has explicit `onlyWhen` conditions (own or inherited): it is active when all conditions match.
- Otherwise, it is active when any repository in its scope is active or installed. The scope is the nearest ancestor path that contains at least one repository; entries with no such ancestor use the workspace root as scope, meaning any repository in the workspace.

Explicitly selecting a file or dir path with `clone` or `sync` always materializes it. Link files additionally require their target file to be active.

## Dependency Resolution

When resolving dependencies for a repository, Jig expands every dependency selector to its repository paths: the one repository of an `id` or exact `path` selector, every repository strictly below a `path` ending in `/*`, or every repository carrying all the `tags` of a tag selector.

Non-optional dependencies are included by default. Optional dependencies are included when explicitly requested.

During `jig sync`, optional dependencies are also included when they are already installed locally. This keeps installed optional repositories up to date without causing Jig to clone missing optional repositories by default.

Dependency resolution is recursive by default. If a dependency has its own dependencies, those dependencies are included using the same optional dependency rules.

Dependency resolution should handle cycles safely. A repository should not be processed more than once during a single dependency traversal.

If multiple dependency paths resolve to the same repository, Jig should process that repository once using repository identity.

## Local State

Jig-managed local state is stored under `.jig/` at the workspace root.

Initial state file:

```text
.jig/state.json
```

`.jig/state.json` is local workspace metadata. It should not be treated as part of the shared repository definition.

Initial state schema:

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:org/platform-auth.git"
    }
  },
  "files": {
    "dev-script": {
      "path": "scripts/dev.sh",
      "src": "git@github.com:org/workspace-config.git#scripts/dev.sh",
      "sha256": "abc123"
    }
  }
}
```

State fields:

- `version`: required integer. Initial supported value: `1`.
- `repos`: required object mapping repository identity to local checkout metadata.
- `files`: required object mapping file identity to local file metadata.

Repository state fields:

- `path`: required safe workspace path relative to the workspace root.
- `git`: optional string. Git URL recorded when Jig cloned or last synced the repository.

File state fields:

- `path`: required safe workspace path relative to the workspace root.
- `src`: required string. Source recorded when Jig last wrote the file; for a multi-source file, the space-separated active sources.
- `link`: optional string. Link target recorded when Jig last created a symlink.
- `sha256`: required string. Hash of the file contents written by Jig.

For symlink files, `link` is required and `sha256` is omitted.

The local filesystem remains the authority for whether a repository or file currently exists.

For node selection, a repository is installed when a Git checkout exists at its tracked or current path. A file is installed when it is tracked in state and its tracked path exists. Untracked files are not considered installed.

## Workspace Discovery

The workspace root is defined by the presence of `.jig/config.json`.

All commands should work from the workspace root or any subdirectory inside the workspace. When a command starts, Jig walks up from the current working directory until it finds `.jig/config.json`.

If no workspace is found, the command should fail with a clear error. If a legacy root `.jig.json` is found instead, the error should say the layout is no longer supported and suggest re-running `jig init`.

`.jig/config.json` fields:

- `version`: required integer. Initial supported value: `1`.
- `schema`: required safe path of the schema file inside `.jig/source/`.

`.jig/` contains Jig-owned local workspace metadata.

## Existing Local Paths

When Jig needs to clone or sync a repository, it should handle the expected local path as follows:

- If the path does not exist, clone the repository.
- If the path exists, contains a Git repository, and its `origin` remote matches the definition Git URL, adopt it by recording it in `.jig/state.json`.
- If the path exists, contains a Git repository, and its `origin` remote does not match the definition Git URL, skip it and report the mismatch.
- If the path exists and is not a Git repository, skip it and report the conflict.

Jig should never overwrite an existing directory during clone or sync.

When Jig needs to write a file, it should handle the expected local path as follows:

- If the file does not exist, write it and record its hash in `.jig/state.json`, or create the symlink and record its link target.
- If the file exists and is not tracked in state, skip it and report the conflict.
- If the file exists, is tracked in state, and its current hash matches the state hash, overwrite it with the new source content and update state.
- If the file exists, is tracked in state, and its current hash differs from the state hash, skip it and report that it was locally modified.
- If a symlink exists and points to the expected target, adopt or update state.
- If a symlink exists with a different target, update it only if state shows Jig previously created it.
- If a symlink path exists and is not a symlink, skip it and report the conflict.

Jig should never overwrite local file modifications.

## Definition Updates

The schema checkout at `.jig/source/` is a normal Git working copy. Editing the shared definition is a plain Git workflow: edit the schema file, test with `jig sync` (Jig reads the live file), then commit and push inside `.jig/source/`.

Updating the definition and updating repository contents are separate operations:

```text
jig update        fast-forwards .jig/source from its remote
jig pull [path]   pulls existing local Git repositories
jig sync [path]   updates the schema, then applies it to the local checkout shape
```

`jig sync --no-update` applies the current schema without fetching, for offline use or for testing local schema edits before pushing them.

`jig update` should fetch from the checkout's upstream, validate the upstream schema, and fast-forward the checkout only if validation succeeds. If the checkout has diverged from upstream or local edits conflict, `jig update` should fail and direct the user to resolve with Git in `.jig/source/`.

`jig update` should not clone repositories, pull repositories, delete repositories, move directories, write files, or update Git remotes unless `--sync` is provided.

When reporting changes between the previous and updated definitions, Jig should use repository and file identities.

## Clone Cache

Jig maintains a bare mirror per remote URL in the user cache directory
(override with `JIG_CACHE_DIR`; empty value disables the cache). Cloning a
repository freshens its mirror with a fetch, clones locally from the mirror
(hardlinking immutable object files), and points `origin` at the real
remote. File source fetches read directly from the mirror.

Rules:

- Workspace clones must remain fully independent of the cache: deleting the
  cache directory never affects an existing checkout.
- Any cache failure falls back to a direct network clone; the cache can
  never cause an operation to fail.
- Mirror creation and updates are serialized with a lock file per mirror;
  locks older than ten minutes are treated as abandoned.
- Each successful use touches a `jig-last-used` marker inside the mirror.
  `jig cache` reports the cache location, mirror count, and size;
  `jig cache clean [--unused <days>]` removes mirrors (all of them, or only
  those unused for at least the given number of days), skipping mirrors
  locked by another process.

## Compatibility

The schema, workspace config, and state files each carry a `version` field. The current schema version is 2 (structured references); workspace config and state are at version 1. When jig encounters a version newer than it understands, it must fail with an error telling the user to upgrade jig; it must never guess at newer formats or rewrite a newer state file (which would silently strip unknown fields). Future format changes bump the corresponding version.

Schema version 1 (string `link` values, prefix-matching dependency paths, combined path-and-tags conditions) is refused with an error pointing at the reference format change; there is no in-tool migration.

## Operation Rules

Repository operations should use repository identity to avoid duplicate work.

File operations should use file identity to avoid duplicate work.

Operations may be processed sequentially in the initial implementation.

Output order should be deterministic. When there is no stronger command-specific ordering, entries should be reported by workspace path.

Commands should exit with a non-zero status when they fail. Validation failures should also use a non-zero exit status.

## Initial CLI Behavior

Target MVP commands:

```text
jig init
jig init <git-url-or-file> [workspace-dir]
jig init <git-url> [workspace-dir] --path <path>
jig init <git-url-or-file> [workspace-dir] --clone [path]
jig init <git-url-or-file> [workspace-dir] --clone [path] --with-optional-deps
jig init <git-url-or-file> [workspace-dir] --clone [path] --archived
jig validate
jig list [path]
jig list [path] --archived
jig tags [path]
jig tags [path] --archived
jig info <path>
jig info <path> --archived
jig deps <path>
jig deps <path> --archived
jig deps <path> --reverse
jig graph [path]
jig graph [path] --archived
jig clone [path]
jig setup [path]
jig fmt [path]
jig lint [path]
jig test [path]
jig pull [path]
jig pull [path] --archived
jig push [-u] [path]
jig checkout [-b] <branch> [path]
jig status [path]
jig status [path] --archived
jig update
jig sync [path]
jig sync [path] --no-update
jig clone [path] --no-deps
jig clone [path] --with-optional-deps
jig sync [path] --no-deps
jig sync [path] --with-optional-deps
jig sync --prune
jig clone [path] --archived
jig sync [path] --archived
```

### `jig init <git-url-or-file> [workspace-dir]`

Initializes a Jig workspace from a Git-hosted or local schema.

If the first argument is an existing local file, Jig creates `.jig/source/` as a fresh Git repository containing that file as `jig.json`, with no remote configured. This is useful for testing a schema before pushing it to a Git repository.

If the first argument is not an existing local file, Jig treats it as a Git URL.

If no argument is given, Jig starts a fresh workspace in the current directory: `.jig/source/` is created as a fresh Git repository containing a starter `jig.json` whose only entry pulls the official jig skill into `.agents/skills`. The workspace is then cloned immediately so the starter content materializes. A fetch failure during this initial clone does not fail the init (`jig sync` retries later), and the command prints next steps for describing repositories and sharing the schema.

If `workspace-dir` is omitted, the current working directory is used.

The command should:

- Resolve the workspace directory.
- Create the workspace directory if it does not exist.
- Replace a `.jig/source/` checkout found without `.jig/config.json`: the config is written only once the source and schema are good, so such a checkout is a leftover from an init that failed midway.
- When `.jig/config.json` exists and the requested Git source matches the checkout's origin, resume instead of failing: update the schema and run the clone step with the given flags. A different source fails, naming both; a bare init or local schema file (no verifiable source identity) fails as before.
- Clone the schema repository into `.jig/source/` (a full clone, so it can be pushed from).
- Locate the schema file: the `--path` value, or the first of `.jig.json`, `jig.json`, `schema.json` at the checkout root.
- Validate the schema.
- Write `.jig/config.json` recording the schema path.
- Create `.jig/state.json` with empty local state.

By default, the command should not clone repositories or write files declared in the tree.

`--with-optional-deps` and `--archived` are valid only when `--clone` is present.

Initial state:

```json
{
  "version": 1,
  "repos": {},
  "files": {}
}
```

### `jig init <git-url-or-file> [workspace-dir] --clone [path]`

Initializes a workspace, then clones repositories.

If `path` is omitted, Jig clones all repositories.

If `path` is provided, Jig clones repositories matching `path` and all non-optional dependencies.

Archived repositories and files are skipped unless they are already installed or `--archived` is provided.

The clone step should run only after `.jig/config.json` and `.jig/state.json` have been written successfully.

The clone behavior is the same as `jig clone <path>`.

### `jig init <git-url-or-file> [workspace-dir] --clone [path] --with-optional-deps`

Initializes a workspace, then clones all repositories, or repositories matching `path`, including non-optional dependencies, optional dependencies, and active files.

### `jig init <git-url-or-file> [workspace-dir] --clone [path] --archived`

Initializes a workspace, then clones uninstalled archived repositories and files in addition to the default selection.

### `jig validate`

Validates the definition file.

Validation should catch:

- Invalid JSON.
- Unsupported schema version.
- Missing top-level `version`.
- Missing top-level `tree`.
- Invalid tree node objects.
- Invalid safe paths.
- Invalid `$group` objects.
- Invalid `$repo` objects.
- Invalid `$file` objects.
- Invalid meta keys.
- Duplicate identities across all entry kinds.
- References carrying zero or more than one selector field.
- `id` selectors naming no entry, or an entry of the wrong kind.
- Exact `path` selectors naming no declared entry, or naming a group (with a hint to append `/*`).
- Subtree and tag selectors matching no repository.
- `/*` anywhere but as the entire final segment, or in a single-target reference.
- Invalid file `src` values.
- Invalid `link` references (wrong kind, self-reference, wrong arity).

Dependency cycles should be detected and reported, but they do not necessarily make the file invalid.

### `jig list [path]`

Lists known groups, repositories, and files.

If `path` is provided, only entries matching that path are listed.

`--meta <key>` keeps only entries whose effective meta carries the key; `--meta <key>=<value>` additionally requires the exact value.

Archived entries are skipped unless they are already installed or `--archived` is provided.

The output includes the entry type and is ordered by path across all entry types.

Example:

```text
file  .agents/skills/platform
group platform
repo  platform/auth
repo  services/checkout
file  scripts/dev.sh
```

### `jig tags [path]`

Lists the tag vocabulary of the entries matching `path`, so `--tags` filter values are discoverable without reading the schema.

If `path` is omitted, the current subtree is used, like other position-relative commands.

Output is one line per tag with the number of entries carrying it (effective tags, so group-inherited tags are counted), sorted by tag.

```text
api       4
backend   7
frontend  3
```

Counts match what filtering on the same tag selects, group entries included.

Tags of archived entries are hidden unless the entry is installed or `--archived` is provided, so the reported vocabulary matches what filtering will actually match.

### `jig info <path>`

Shows information for a repository, file, or group path.

For a repository, it should show metadata such as Git URL, web URL, description, custom meta, and direct dependencies.

For a file, it should show metadata such as source, description, executable flag, custom meta, and `onlyWhen` condition.

For a group, it should show matching groups, repositories, and files together in path order.

If the group has `$group` metadata, it should also show its identity and metadata.

Archived repositories, files, and groups are skipped unless they are already installed or `--archived` is provided.

### `jig deps <path>`

Shows the dependencies for repositories matching a path after expanding group paths.

If `path` matches multiple repositories, Jig resolves dependencies for all matching repositories and deduplicates the result by repository identity.

Files are ignored by `jig deps`.

By default, only non-optional dependencies are included.

Optional dependencies should be included only when requested.

Archived repositories are skipped unless they are already installed or `--archived` is provided.

### `jig deps <path> --reverse`

Shows the direct dependents of the repositories matching `path`: every repository with a `dependsOn` edge resolving to a matching repository, one per line in path order.

Unlike forward resolution, `--reverse` is deliberately not recursive: it answers "who are the declared consumers", not the transitive blast radius. Rules:

- Edges through group paths count. A repository depending on `platform` is a direct dependent of every repository under `platform/`, even though it never names them.
- A group `path` asks who depends on anything in the group. Intra-group edges count when they point at a different repository; a repository is never listed as its own dependent.
- Optional edges are hidden unless `--with-optional-deps` is provided, and uninstalled archived dependents are hidden unless `--archived` is provided, mirroring the forward flags.
- Files are ignored, as in forward `jig deps`.

### `jig graph [path]`

Prints the repository dependency graph as a Mermaid flowchart on stdout.

The output is the raw diagram (starting at `flowchart TD`, no markdown fence), so it pipes directly into mermaid tooling; wrap it in a ` ```mermaid ` fence when embedding in markdown.

Rules:

- The workspace tree is the visual skeleton: directories containing repositories render as nested `subgraph` blocks, and repositories are leaf nodes labeled with their last path segment.
- Dependency edges onto group paths point at the subgraph itself, matching what the schema declares, instead of fanning out to every member.
- Optional dependencies are dashed (`-.->`); non-optional dependencies are solid (`-->`). Both are always shown.
- If `path` is provided, the selected repositories are drawn along with any edge targets outside the selection, so no arrow dangles. A group target with no drawn repositories becomes a plain node.
- Archived repositories follow the usual rule: hidden unless installed or `--archived`.
- Repositories only; files, dirs, and `onlyWhen` relationships are not part of the graph.
- Node identifiers are derived from workspace paths with unsafe characters replaced, and Mermaid keywords (such as a path segment named `end`) are escaped.
- Output is deterministic: subgraphs, nodes, and edges are sorted.

### `jig clone [path]`

If `path` is omitted, clones all repositories and active files.

If `path` is provided, clones repositories and files matching that path. Matching repositories include all non-optional dependencies. Matching files are materialized directly.

If `path` matches multiple repositories, Jig clones all matching repositories and their deduplicated dependencies.

If `path` matches symlink files, Jig also materializes their target files.

Jig should also write active files whose `onlyWhen` condition matches the resulting active repository set.

Archived repositories and files are skipped unless they are already installed or `--archived` is provided.

After cloning each repository or writing each file, Jig should record it in `.jig/state.json` using its identity.

### `jig clone [path] --no-deps`

Clones only the repositories and files matching the selection, without expanding dependencies or activating conditional entries: the plan is exactly the selected roots. Files and dirs scoped to the selected repositories still materialize. `--no-deps` and `--with-optional-deps` are mutually exclusive.

### `jig clone [path] --with-optional-deps`

Clones all repositories, or repositories matching a path, including non-optional dependencies and optional dependencies.

### `jig clone [path] --archived`

Clones uninstalled archived repositories and files in addition to the default selection.

### `jig sync [path]`

Updates the schema as `jig update` does, then applies it to the local checkout shape. A schema update that fails (unreachable remote, invalid or diverged upstream) is reported on a `schema not updated:` line and the current schema is applied instead, so sync still converges offline. `--no-update` skips the update step: the current `.jig.json` is applied without fetching.

If `path` is provided, Jig syncs repositories and files matching that path. Matching repositories include non-optional dependencies. Matching files are materialized directly.

If `path` matches symlink files, Jig also materializes their target files.

If a matching repository has optional dependencies that are already installed locally, those optional dependencies are included in the sync set.

If `path` is omitted, Jig syncs the desired repositories: those installed locally plus those tracked in `.jig/state.json`, with their non-optional dependencies, then writes active files. Installed optional dependencies are included. It should not clone every repository in the schema by default.

State records intent: a tracked repository whose directory was deleted locally is restored by sync and reported as restored. `jig rm` is the way to uninstall.

Archived repositories and files are skipped unless they are already installed or `--archived` is provided.

`--no-deps` restricts the sync set to exactly the selected repositories and files, without dependency or condition expansion, as in `jig clone --no-deps`. It is mutually exclusive with `--with-optional-deps`.

Sync may perform these actions:

- Clone missing repositories in the sync set.
- Move a local repository when `.jig/state.json` records a path different from the current expected path. A move is a plain rename and carries uncommitted changes and unpushed commits along untouched, so dirty repositories move like clean ones. Skip messages for moves name both the recorded and the expected path.
- Update a repository's `origin` remote URL when the current definition Git URL differs from the local repository's `origin` remote URL.
- Write missing active files.
- Update active files that Jig previously wrote and that have not been locally modified. State records the source blob id of each written file; sync freshens each source repository's cache mirror once per run, compares blob ids, and rewrites only files whose source changed. When the source cannot be reached the file is reported as present but unchecked.
- Move tracked files when the same file identity has a new path and the file has not been locally modified.
- Update `.jig/state.json` after successful clone, move, origin update, or file write operations.
- Report repositories and files that exist locally but are no longer defined.
- Prune state entries that are no longer defined and whose checkout or file is gone from disk.

Sync must not delete local repositories or locally modified files, except under `--prune` as specified below.

Renamed identities are readopted: when a state entry's identity is no longer defined but a defined entry of the same kind expects the same path, the state record is transferred to the new identity (reported as `readopted`) before the plan is applied. The record's origin URL, file hash, or dir manifest follows the checkout, so an id rename in the schema is a no-op locally instead of producing a stale report.

### `jig sync --prune`

Deletes state-tracked entries that are no longer defined in the schema, after the normal sync. Pruning is a whole-workspace operation: `--prune` cannot be combined with a path or `--tags`, since stale entries have no schema entry to select on.

Safety rules match `jig rm` without `--force`:

- Repositories with uncommitted changes, unpushed commits, or a branch with no upstream are kept and reported under `kept`.
- Repositories whose `origin` no longer matches the recorded URL are kept.
- Files whose content differs from the recorded hash are kept.
- Inside pruned dirs, only untouched manifest files are deleted; user-added and modified files are kept.
- A path owned by a defined entry is never deleted; only the obsolete state record is dropped.

Successful deletions drop the state entry and are reported under `pruned`. There is no `--force`; escalate per path with `jig rm -f`.

Sync must skip and report any operation that is ambiguous or unsafe.

### `jig sync [path] --with-optional-deps`

Syncs repositories matching `path`, non-optional dependencies, optional dependencies, and active files.

### `jig sync [path] --archived`

Syncs uninstalled archived repositories and files in addition to the default selection.

### `jig setup [path]`, `jig fmt [path]`, `jig lint [path]`, `jig test [path]`

Run each installed repository's schema-declared lifecycle command. See [Repository Lifecycle Commands](#repository-lifecycle-commands) for the vocabulary, execution rules, and reporting.

### `jig pull [path]`

Pulls all locally installed Git repositories matching `path`.

If `path` is omitted, all locally installed repositories in the workspace are matched.

Files are ignored by `jig pull`.

Installed archived repositories are included by default. `--archived` applies the same selection semantics as other commands, although `pull` can only act on installed repositories.

### `jig fetch [path]`

Runs `git fetch` in installed repositories matching `path`, or in all installed repositories when `path` is omitted. Selection semantics match `jig pull`. Fetch never touches working trees or local branches.

### `jig push [-u] [path]`

Publishes the current branch of installed repositories matching `path` (all installed repositories when `path` is omitted), in parallel. Selection semantics match `jig pull`.

- Pushes are never forced; there is no force flag. A push the remote rejects (for example a non-fast-forward) is reported under `skipped` and the remote is left untouched — escalate per repository with plain Git when needed.
- Repositories whose branch is not ahead of its upstream report `up to date`, computed locally without network access.
- A branch with no upstream is skipped and reported, unless `-u` is passed, which pushes with `git push -u origin <branch>` and records the upstream. Re-running is therefore idempotent: the first `-u` push creates the upstream and later runs report `up to date`. This pairs with `jig checkout -b`, whose fresh branches have no upstream.
- Repositories on a detached HEAD are skipped and reported: there is no branch to push.
- The command exits non-zero when any repository was skipped.

Only the current branch is pushed. There is no branch argument or refspec support.

### `jig checkout [-b] <branch> [path]`

Switches installed repositories matching `path` (all installed repositories when `path` is omitted) to `<branch>`, in parallel. Selection semantics match `jig pull`.

- Repositories already on the branch report `present`.
- With `-b`, the branch is created at the repository's current HEAD when it does not exist; when it already exists, the repository just switches to it, so re-running is idempotent.
- Without `-b`, git's usual rules apply, including creating a local branch from a matching remote-tracking branch.
- Checkouts are never forced: a repository where git refuses the switch (for example uncommitted changes that would be overwritten) is reported under `skipped` and left untouched.
- The branch name is validated up front; an invalid name fails before touching any repository.

### `jig checkout --default [path]`

Switches each matching repository to its own remote default branch. This exists because default branches differ across repositories (`main`, `master`, `staging`, …), so no single branch name can express "go back to the mainline everywhere".

- The default branch is resolved per repository from `origin/HEAD`, which git records at clone time, so resolution is normally offline. When the ref is missing (an old clone, a hand-added remote), the remote is asked directly with `ls-remote --symref origin HEAD` and the answer is recorded via `git remote set-head`, making later runs offline again.
- Since the target branch differs per repository, report lines name it: `switched: services/api (main)`.
- Repositories whose default branch cannot be resolved (no `origin`, remote unreachable) are reported under `skipped`; switching rules and safety are otherwise identical to the branch form.
- `--default` replaces the `<branch>` positional and is incompatible with `-b`; combining them is a usage error.

### `jig rm <path>...`

Uninstalls repositories and files: deletes the checkout or file and drops it from `.jig/state.json`, so sync stops restoring it. Ergonomics follow `rm`:

- Multiple paths may be given.
- An exact repository or file path is removed directly.
- A path matching more than one entry (a group or prefix) requires `-r` / `--recursive`.
- Failing entries are reported and the rest proceed; the command exits non-zero if anything was not removed.

Safety: removal is refused for repositories with uncommitted changes, with unpushed commits, or on a branch with no upstream, and for locally modified files. `-f` / `--force` overrides.

Entries tracked in state whose directory is already gone can be removed too; this only drops the state entry.

### `jig status [path]`

Shows local checkout status for repositories and files matching `path`.

Status reports the state of the workspace, not the catalog: repositories that are neither installed nor tracked in state are only counted in the summary line as not installed, unless `--all` is given. A tracked repository whose directory was deleted locally is reported as missing (sync restores it). Inactive files and dirs are hidden.

If `path` is omitted, Jig reports status for the installed entries plus entries tracked in `.jig/state.json` that are no longer defined.

Archived repositories and files are skipped unless they are already installed or `--archived` is provided.

Output is a single list with one line per entry, ordered by path across repositories and files. Each line shows a status glyph, the path, the current branch (for repositories), and a note spelling out any notable state. For a repository the current branch is shown, or a short `@<sha>` when the checkout is on a detached HEAD.

Repositories with an upstream also report how many commits they are ahead of and behind it, computed locally without network access; run `jig fetch` first to compare against the latest remote state. Ahead/behind glyphs apply only when nothing more significant does, but the notes always spell out every state.

```text
✓ platform/argo-workflows        main
● platform/dagster               main    dirty, ahead 1
⇄ platform/terraform-operator    main    remote-changed
✗ platform/knative                       missing
→ platform/linkerd               main    moved from platform/old-linkerd
↑ platform/flux                  main    ahead 2
↓ platform/vault                 main    behind 3
⇅ platform/consul                main    ahead 1, behind 4
```

Glyphs: `✓` in sync, `●` uncommitted changes or a locally modified file, `⇄` origin differs from the definition, `→` checkout lives at a different path, `✗` defined but not present, `⚠` present but not what Jig expects.

Status should identify:

- Installed repositories and their current branch.
- Missing repositories.
- Written files.
- Missing active files.
- Repositories or files tracked in state but no longer defined.
- Repositories or files whose state path differs from the current expected path.
- Repositories whose local `origin` remote URL differs from the current definition Git URL.
- Repositories with uncommitted changes.
- Files with local modifications.
- Expected paths that cannot be adopted because they conflict with existing local content.

The initial implementation may omit ahead/behind information if computing it would require network access. Local-only status should not fetch from remotes.

### `jig update`

Fast-forwards the schema checkout at `.jig/source/` from its Git remote.

The command should:

- Fail with a clear error when the checkout has no `origin` remote.
- Fetch from the checkout's upstream.
- Validate the upstream schema before touching the checkout.
- Fast-forward the checkout only if the upstream schema is valid.
- Fail and direct the user to Git when the checkout has diverged or local edits conflict.
- Compare the previous and updated live definitions by repository and file identity.
- Report added, removed, moved, and changed groups, repositories, and files.

Uncommitted local schema edits that do not conflict with upstream are preserved by the fast-forward and do not appear in the reported changes.

The command should not change local repository checkouts, write files, or update `.jig/state.json`.

`jig update --sync` remains as an undocumented compatibility alias from before sync updated the schema itself: it updates the schema and then syncs, but unlike `jig sync` it fails outright when the schema update fails. The sync-step flags (`--no-deps`, `--with-optional-deps`, `--archived`, `--prune`, a node path) are valid only with `--sync`.

## Open Questions

- Should repository and file definitions support explicit local path overrides, or is tree position always the local path?
- Should inactive `onlyWhen` files that were previously written be reported only, or should a future `prune` command remove them if unmodified?
