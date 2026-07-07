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
  "version": 1,
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
            "path": "platform",
            "reason": "checkout uses shared platform services"
          }
        ]
      }
    },
    "tools": {
      "$group": {
        "description": "Developer tools",
        "onlyWhen": {
          "path": "platform",
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
          "path": "platform",
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

Initial supported version: `1`.

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
- `dependsOn.path`.
- `onlyWhen.path`.
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

Group identities must be unique among groups.

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

### `$group.archived`

Optional boolean.

Default: `false`.

Marks the group and all descendant repositories and files as archived.

Archived repositories and files are excluded by default unless already installed. `--archived` includes uninstalled archived entries too.

### `$group.tags`

Optional list of strings.

Tags label entries for filtering with the `--tags` CLI flag. Tags must be non-empty and must not contain spaces or commas. Group tags are inherited additively by descendant repositories and files.

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
- `archived` is inherited by descendant repositories and files. A descendant cannot opt out of an archived ancestor.
- `tags` are inherited additively by descendant repositories and files. An entry effective tag set is the union of its declared tags and all ancestor group tags.
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

Repository identities must be unique after applying this rule. Two repositories cannot resolve to the same identity.

### `$repo.git`

Required string.

The Git clone URL for the repository.

### `$repo.web`

Optional string.

The web URL for the repository.

### `$repo.description`

Optional string.

A short human-readable description of the repository.

### `$repo.archived`

Optional boolean.

Default: `false`.

Archived repositories remain valid definition entries. They are excluded by default unless already installed; `--archived` includes uninstalled archived repositories too.

### `$repo.tags`

Optional list of strings.

Tags label entries for filtering with the `--tags` CLI flag. Tags must be non-empty and must not contain spaces or commas.

### `$repo.dependsOn`

Optional array.

Lists dependencies for this repository. Each dependency points to either a specific repository or a group of repositories.

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
        "link": "scripts/dev.sh",
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

File identities must be unique after applying this rule. Two files cannot resolve to the same identity.

### `$file.src`

Optional string.

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

### `$file.link`

Optional string.

Declares the file node as a symbolic link to another `$file` node in the same schema.

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
        "link": "scripts/dev.sh"
      }
    }
  }
}
```

Rules:

- A `$file` must define exactly one of `src` or `link`.
- `link` must be a safe workspace path.
- `link` must resolve to another `$file` node in the same `.jig.json`.
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

Fields: `id` (optional identity, defaults to the path), `src` (required, `<repo-url>[#<subtree-path>]` or a list of such sources; without a path the whole repository tree is materialized), `description`, `archived`, `tags`, and `onlyWhen` behave as for `$file`. There is no `executable` field (modes come from the git tree). A directory node may declare `link` instead of `src`: it becomes a relative symlink to another `$dir` entry. Exactly one of `src` and `link` is required; link dirs are active only when their target dir is active, targets are materialized before links, link cycles are validation errors, and removing a link dir removes only the symlink.

State records the source tree id and a manifest mapping each written file to its content hash. Rules:

- The subtree is extracted from the source repository's cache mirror without a checkout.
- With multiple sources, trees are merged in order into the same directory; when two sources provide the same file path, the first source wins and the shadowed file is reported. All active sources are resolved before any file is written.
- A list entry may be an object `{"src": ..., "onlyWhen": ...}`; the per-source condition gates just that source's tree within the merge, evaluated against active and installed repositories. Files of a source whose condition stops matching are removed on the next sync when untouched.
- Updates overwrite only files whose local content matches the manifest; locally modified files are kept and reported.
- Files that disappear upstream are deleted locally only when their content matches the manifest; modified ones are left behind as untracked.
- Files the user adds inside the directory are never touched or deleted.
- `jig rm` deletes only manifest-tracked files, refusing when any is locally modified unless forced.
- Status reports one line per directory entry with aggregated modified/missing counts.

## Dependency Fields

### `path`

Required string.

Identifies the dependency target.

The path may refer to a specific repository:

```json
{
  "path": "platform/auth"
}
```

Or it may refer to a group of repositories:

```json
{
  "path": "platform"
}
```

Dependency paths are safe workspace paths.

Group paths are resolved by matching repository paths on `/` segment boundaries.

A dependency path matches a repository path when either:

```text
repoPath == path
```

or:

```text
repoPath starts with path + "/"
```

For example, `platform` matches:

```text
platform/auth
platform/billing
platform/events
```

But it does not match:

```text
platforming/api
```

### `optional`

Optional boolean.

Indicates whether this dependency is optional for normal local development.

Default: `false`.

Most dependencies should omit this field. Optional dependencies are represented with `optional: true`.

### `reason`

Optional string.

Explains why the dependency exists.

This is intended for humans and should not affect dependency resolution.

## Conditional Nodes

Use `onlyWhen` to make a repo, file, or dir active only when some active or installed repository satisfies the condition.

A condition has two selectors, of which at least one is required:

- `path`: a safe workspace path; a repository at or under it satisfies the condition.
- `tags`: a list of tags; a repository carrying all of them (declared or inherited from groups) satisfies the condition. Multiple tags are conjunctive, matching the `--tags` CLI flag.

When both selectors are given, one repository must satisfy both. An optional `reason` documents the condition.

```json
{ "onlyWhen": { "path": "platform" } }
{ "onlyWhen": { "tags": ["api"] } }
{ "onlyWhen": { "path": "services", "tags": ["frontend"], "reason": "frontend tooling" } }
```

Validation requires each condition to be satisfiable by at least one repository in the schema (archived included), which catches path typos and misspelled tags.

Inherited `onlyWhen` conditions are additive. All inherited and local conditions must match.

## File And Dir Activation

Files and dirs are support artifacts: they are materialized as a side effect of the repositories they support, or by explicit selection.

A file or dir is active when the first of these rules applies:

- It is already installed (state records intent, mirroring repositories): it stays maintained until removed with `jig rm`.
- It has explicit `onlyWhen` conditions (own or inherited): it is active when all conditions match.
- Otherwise, it is active when any repository in its scope is active or installed. The scope is the nearest ancestor path that contains at least one repository; entries with no such ancestor use the workspace root as scope, meaning any repository in the workspace.

Explicitly selecting a file or dir path with `clone` or `sync` always materializes it. Link files additionally require their target file to be active.

## Dependency Resolution

When resolving dependencies for a repository, Jig expands every dependency path to matching repository paths.

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
- `src`: required string. Source recorded when Jig last wrote the file.
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
jig update --sync updates the schema, then syncs the workspace
jig pull [path]   pulls existing local Git repositories
jig sync [path]   applies the current schema to the local checkout shape
```

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

The schema, workspace config, and state files each carry a `version` field. Version 1 is the current version of all three. When jig encounters a version newer than it understands, it must fail with an error telling the user to upgrade jig; it must never guess at newer formats or rewrite a newer state file (which would silently strip unknown fields). Future format changes bump the corresponding version.

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
jig info <path>
jig info <path> --archived
jig deps <path>
jig deps <path> --archived
jig clone [path]
jig pull [path]
jig pull [path] --archived
jig checkout [-b] <branch> [path]
jig status [path]
jig status [path] --archived
jig update
jig update --sync
jig update --sync [path]
jig sync [path]
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
- Fail if `.jig/config.json` or `.jig/source/` already exists in the workspace directory.
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
- Duplicate repository identities.
- Duplicate file identities.
- Duplicate group identities.
- Dependency paths that do not resolve to any repository.
- `onlyWhen.path` values that do not resolve to any repository.
- Invalid file `src` values.
- Invalid file `link` values.

Dependency cycles should be detected and reported, but they do not necessarily make the file invalid.

### `jig list [path]`

Lists known groups, repositories, and files.

If `path` is provided, only entries matching that path are listed.

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

### `jig info <path>`

Shows information for a repository, file, or group path.

For a repository, it should show metadata such as Git URL, web URL, description, and direct dependencies.

For a file, it should show metadata such as source, description, executable flag, and `onlyWhen` condition.

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

Applies the current `.jig.json` to the local checkout shape.

If `path` is provided, Jig syncs repositories and files matching that path. Matching repositories include non-optional dependencies. Matching files are materialized directly.

If `path` matches symlink files, Jig also materializes their target files.

If a matching repository has optional dependencies that are already installed locally, those optional dependencies are included in the sync set.

If `path` is omitted, Jig syncs the desired repositories: those installed locally plus those tracked in `.jig/state.json`, with their non-optional dependencies, then writes active files. Installed optional dependencies are included. It should not clone every repository in the schema by default.

State records intent: a tracked repository whose directory was deleted locally is restored by sync and reported as restored. `jig rm` is the way to uninstall.

Archived repositories and files are skipped unless they are already installed or `--archived` is provided.

`--no-deps` restricts the sync set to exactly the selected repositories and files, without dependency or condition expansion, as in `jig clone --no-deps`. It is mutually exclusive with `--with-optional-deps`.

Sync may perform these actions:

- Clone missing repositories in the sync set.
- Move a local repository when `.jig/state.json` records a path different from the current expected path.
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

### `jig pull [path]`

Pulls all locally installed Git repositories matching `path`.

If `path` is omitted, all locally installed repositories in the workspace are matched.

Files are ignored by `jig pull`.

Installed archived repositories are included by default. `--archived` applies the same selection semantics as other commands, although `pull` can only act on installed repositories.

### `jig fetch [path]`

Runs `git fetch` in installed repositories matching `path`, or in all installed repositories when `path` is omitted. Selection semantics match `jig pull`. Fetch never touches working trees or local branches.

### `jig checkout [-b] <branch> [path]`

Switches installed repositories matching `path` (all installed repositories when `path` is omitted) to `<branch>`, in parallel. Selection semantics match `jig pull`.

- Repositories already on the branch report `present`.
- With `-b`, the branch is created at the repository's current HEAD when it does not exist; when it already exists, the repository just switches to it, so re-running is idempotent.
- Without `-b`, git's usual rules apply, including creating a local branch from a matching remote-tracking branch.
- Checkouts are never forced: a repository where git refuses the switch (for example uncommitted changes that would be overwritten) is reported under `skipped` and left untouched.
- The branch name is validated up front; an invalid name fails before touching any repository.

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

`--no-deps`, `--with-optional-deps`, `--archived`, and a node path are valid only when `--sync` is present.

### `jig update --sync [path]`

Updates the schema, then applies the updated definition with the same behavior as `jig sync`.

If `path` is provided, only matching nodes are included in the sync step. The schema update itself is always global.

The sync step should run only after the schema has been fetched, validated, and fast-forwarded successfully.

`jig update --sync --with-optional-deps` includes optional dependencies during the sync step.

`jig update --sync --no-deps` restricts the sync step to the selected entries, as in `jig sync --no-deps`.

`jig update --sync --prune` prunes entries removed from the schema during the sync step, as in `jig sync --prune`.

`jig update --sync --archived` includes uninstalled archived repositories and files during the sync step.

## Open Questions

- Should repository and file definitions support explicit local path overrides, or is tree position always the local path?
- Should inactive `onlyWhen` files that were previously written be reported only, or should a future `prune` command remove them if unmodified?
