# Jig Specification

## Purpose

Jig is a CLI tool for working with many related Git repositories. It uses a declarative JSON structure to describe repositories, their metadata, and their dependencies, then uses that structure to clone, inspect, and navigate repository sets.

The primary goal is to make it easy to clone one repository together with the other repositories it needs for local development.

## Repository Definition File

The repository structure is defined in `.jig.json` at the workspace root.

Initial schema:

```json
{
  "version": 1,
  "source": {
    "type": "git",
    "url": "git@github.com:org/jig-definition.git",
    "ref": "main",
    "path": "jig.json"
  },
  "repos": {
    "org1.suborg1.repo1": {
      "id": "repo1",
      "git": "git@github.com:org1/repo1.git",
      "web": "https://github.com/org1/repo1",
      "description": "Service API",
      "dependsOn": [
        {
          "path": "org2.platform",
          "reason": "runtime platform dependency"
        }
      ]
    }
  }
}
```

## Top-Level Fields

### `version`

Required integer.

Identifies the schema version used by the definition file.

Initial supported version: `1`.

### `repos`

Required object.

Maps canonical repository IDs to repository definitions.

Repository IDs are dot-separated paths:

```text
org.suborg.repo
platform.auth
services.checkout
```

The dot-separated structure is used to infer groups. There is no separate group declaration in the initial schema.

Repository IDs must:

- Be non-empty.
- Use dot-separated segments.
- Not contain empty segments.
- Not start or end with a dot.
- Not contain slashes.

Segments should use simple filesystem-safe names. Initial recommended characters are letters, numbers, underscores, and hyphens.

### `source`

Optional object.

Describes where `.jig.json` can be updated from.

Initial supported source type: `git`.

Example:

```json
{
  "type": "git",
  "url": "git@github.com:org/jig-definition.git",
  "ref": "main",
  "path": "jig.json"
}
```

Fields:

- `type`: required string. Initial supported value: `git`.
- `url`: required string. Git URL of the repository containing the definition file.
- `ref`: optional string. Branch, tag, or revision to read from. `jig init` records the discovered default branch here.
- `path`: optional string. Path to the definition file inside the source repository. Default: `.jig.json`.

## Repository Fields

### `id`

Optional string.

Stable repository identity.

The repository map key is the current logical path of the repository. The optional `id` field identifies the repository across path changes, renames, and hosting changes.

If `id` is omitted, the repository path is used as the identity.

Repository IDs must be unique after applying this rule. Two repositories cannot resolve to the same identity.

The `id` value should be stable and should not be derived from the current hosting provider, organization, or local path.

Example:

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:org/platform-auth.git"
    }
  }
}
```

If this repository later moves, the `id` should stay the same:

```json
{
  "version": 1,
  "repos": {
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@gitlab.com:org/auth-service.git"
    }
  }
}
```

### `git`

Required string.

The Git clone URL for the repository.

Example:

```json
"git": "git@github.com:org/repo.git"
```

### `web`

Optional string.

The web URL for the repository.

Example:

```json
"web": "https://github.com/org/repo"
```

### `description`

Optional string.

A short human-readable description of the repository.

### `dependsOn`

Optional array.

Lists dependencies for this repository. Each dependency points to either a specific repository or a group of repositories.

If omitted, the repository has no declared dependencies.

## Dependency Fields

### `path`

Required string.

Identifies the dependency target.

The path may refer to a specific repository:

```json
{
  "path": "platform.auth"
}
```

Or it may refer to a group of repositories:

```json
{
  "path": "platform"
}
```

Group paths are resolved by matching repository IDs on dot-segment boundaries.

A dependency path matches a repository ID when either:

```text
repoId == path
```

or:

```text
repoId starts with path + "."
```

For example, `platform` matches:

```text
platform.auth
platform.billing
platform.events
```

But it does not match:

```text
platforming.api
```

### `optional`

Optional boolean.

Indicates whether this dependency is optional for normal local development.

Default: `false`.

Most dependencies should omit this field. Optional dependencies are represented with `optional: true`.

Example:

```json
{
  "path": "observability",
  "optional": true,
  "reason": "useful when debugging traces locally"
}
```

### `reason`

Optional string.

Explains why the dependency exists.

This is intended for humans and should not affect dependency resolution.

## Dependency Resolution

When resolving dependencies for a repository, Jig expands every dependency path to matching repository IDs.

Example definition:

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "git": "git@github.com:org/platform-auth.git"
    },
    "platform.billing": {
      "git": "git@github.com:org/platform-billing.git"
    },
    "platform.events": {
      "git": "git@github.com:org/platform-events.git"
    },
    "services.checkout": {
      "git": "git@github.com:org/checkout.git",
      "dependsOn": [
        {
          "path": "platform",
          "reason": "checkout uses shared platform services"
        }
      ]
    }
  }
}
```

Resolving dependencies for `services.checkout` produces:

```text
platform.auth
platform.billing
platform.events
```

Non-optional dependencies are included by default. Optional dependencies are included only when explicitly requested.

Dependency resolution is recursive by default. If a dependency has its own dependencies, those dependencies are included using the same optional dependency rules.

Dependency resolution should handle cycles safely. A repository should not be processed more than once during a single dependency traversal.

If multiple dependency paths resolve to the same repository, Jig should process that repository once using repository identity.

## Definition Updates

`.jig.json` is the active local definition file.

If `.jig.json` contains a `source` object, Jig can update it from that source.

Updating the definition and updating repository contents are separate operations:

```text
jig update       updates .jig.json
jig pull [path]  pulls existing local Git repositories
jig sync [path]  applies the current .jig.json to the local checkout shape
```

`jig update` should fetch the latest definition from `source`, validate it, compare it to the current local `.jig.json`, and replace the local `.jig.json` only if validation succeeds.

`jig update` should not clone repositories, pull repositories, delete repositories, move directories, or update Git remotes.

When comparing the current and incoming definitions, Jig should use repository identity. Repository identity is `id` when present, otherwise the repository path.

This allows Jig to report changes such as:

- Repository added.
- Repository removed.
- Repository path changed.
- Repository Git URL changed.
- Repository web URL changed.
- Repository dependencies changed.

Jig stores local mutable workspace state in `.jig/state.json`. This keeps `.jig.json` clean as the shared definition file while giving Jig enough information to safely track local checkouts across repository moves, renames, and hosting changes.

## Local State

Jig-managed local state is stored under `.jig/` at the workspace root.

Initial state file:

```text
.jig/state.json
```

`.jig/state.json` is local workspace metadata. It should not be treated as part of the shared repository definition.

The state file tracks locally installed repositories by repository identity.

Repository identity is `id` when present, otherwise the repository path.

Initial state schema:

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:org/platform-auth.git"
    }
  }
}
```

State fields:

- `version`: required integer. Initial supported value: `1`.
- `repos`: required object mapping repository identity to local checkout metadata.
- `path`: required string. Local path relative to the workspace root.
- `git`: optional string. Git URL recorded when Jig cloned or last synced the repository.

Jig should update `.jig/state.json` when it clones a repository, moves a repository, updates a repository's origin remote, or detects that a tracked repository no longer exists locally.

The local filesystem remains the authority for whether a repository currently exists. If `.jig/state.json` says a repository is installed but the directory no longer exists or is no longer a Git repository, Jig should treat it as missing and update or report stale state.

`.jig/state.json` must not cause Jig to modify a repository that does not exist on disk.

## Workspace Discovery

The workspace root is defined by the presence of a `.jig.json` file.

All commands should work from the workspace root or any subdirectory inside the workspace. When a command starts, Jig walks up from the current working directory until it finds `.jig.json`.

The directory containing `.jig.json` is the workspace root.

If no `.jig.json` file is found, the command should fail with a clear error.

The repository structure is read from `.jig.json`.

`.jig.json` is the shared, human-editable workspace definition. It may be updated from a remote `source` and should not contain local checkout metadata.

`.jig/` contains Jig-owned local workspace metadata. The initial local state file is `.jig/state.json`.

## Clone Layout

By default, repository IDs map to local paths by replacing dots with directory separators.

Example:

```text
org1.suborg1.repo1 -> org1/suborg1/repo1
platform.auth -> platform/auth
services.checkout -> services/checkout
```

Local repository paths are resolved relative to the workspace root.

## Local Repository Detection

A repository is considered locally installed when Jig can associate its repository identity with a local directory that exists and contains a Git repository.

Jig should first consult `.jig/state.json` for the repository identity. If state exists, the state path is the current known local checkout path.

If no state exists for a repository identity, the expected local directory is derived from the repository path unless a future schema version adds an explicit local path override.

For operations that need to detect repositories not yet recorded in state, Jig may inspect Git repositories inside the workspace and compare their configured remotes to repository definitions.

Jig should only move or modify a local repository when it can confidently identify it from `.jig/state.json`, the current `.jig.json`, or the checked out Git repository itself.

If Jig cannot confidently identify a local repository, it must skip the move or modification and report the ambiguity instead of guessing.

## Existing Local Paths

When Jig needs to clone or sync a repository, it should handle the expected local path as follows:

- If the path does not exist, clone the repository.
- If the path exists, contains a Git repository, and its `origin` remote matches the definition Git URL, adopt it by recording it in `.jig/state.json`.
- If the path exists, contains a Git repository, and its `origin` remote does not match the definition Git URL, skip it and report the mismatch.
- If the path exists and is not a Git repository, skip it and report the conflict.

Jig should never overwrite an existing directory during clone or sync.

Updating a repository's `origin` remote URL during sync is allowed even if the repository has uncommitted changes, because it does not modify the worktree.

## Operation Rules

Repository operations should use repository identity to avoid duplicate work.

Operations may be processed sequentially in the initial implementation.

Output order should be deterministic. When there is no stronger command-specific ordering, repositories should be reported by repository path.

Commands should exit with a non-zero status when they fail. Validation failures should also use a non-zero exit status.

## Initial CLI Behavior

The initial CLI should focus on validating the definition file, performing dependency-aware clone operations, updating the definition file, and keeping local checkouts aligned with the current definition.

Target MVP commands:

```text
jig init <git-url> [workspace-dir]
jig init <git-url> [workspace-dir] --path <path>
jig validate
jig list
jig info <path>
jig deps <repo>
jig clone <repo>
jig pull [path]
jig status [path]
jig update
jig sync [path]
jig clone <repo> --with-optional-deps
jig sync [path] --with-optional-deps
```

### `jig init <git-url> [workspace-dir]`

Initializes a Jig workspace from a Git-hosted definition file.

If `workspace-dir` is omitted, the current working directory is used.

The command should:

- Resolve the workspace directory.
- Create the workspace directory if it does not exist.
- Fail if `.jig.json` already exists in the workspace directory.
- Discover the source repository default branch with `git ls-remote --symref <git-url> HEAD`.
- Fetch the definition file from the discovered default branch.
- Validate the fetched definition.
- Write `.jig.json` in the workspace directory.
- Inject or replace the top-level `source` object in the written `.jig.json`.
- Create `.jig/state.json` with empty local state.

The command should not clone repositories.

The initial source object should be:

```json
{
  "type": "git",
  "url": "git@github.com:org/jig-definition.git",
  "ref": "main",
  "path": ".jig.json"
}
```

`ref` is the discovered remote default branch.

`path` defaults to `.jig.json`.

If the remote default branch cannot be determined, initialization should fail with a clear error rather than guessing `main` or `master`.

### `jig init <git-url> [workspace-dir] --path <path>`

Initializes a workspace from a definition file at a custom path inside the source repository.

Example:

```sh
jig init git@github.com:org/jig-definition.git ~/Code/org --path definitions/jig.json
```

The written source object should record the custom path:

```json
{
  "type": "git",
  "url": "git@github.com:org/jig-definition.git",
  "ref": "main",
  "path": "definitions/jig.json"
}
```

### `jig validate`

Validates the definition file.

Validation should catch:

- Invalid JSON.
- Unsupported schema version.
- Missing top-level `version`.
- Missing top-level `repos`.
- Invalid `source` object.
- Invalid repository IDs.
- Duplicate repository identities.
- Repository definitions missing `git`.
- Invalid dependency objects.
- Dependency paths that do not resolve to any repository.

Dependency cycles should be detected and reported, but they do not necessarily make the file invalid.

### `jig list`

Lists known repositories.

### `jig info <path>`

Shows information for a repository or group path.

For a repository, it should show metadata such as Git URL, web URL, description, and direct dependencies.

For a group, it should show matching repositories.

### `jig deps <repo>`

Shows the dependencies for a repository after expanding group paths.

By default, only non-optional dependencies are included.

Optional dependencies should be included only when requested.

### `jig clone <repo>`

Clones a repository and all non-optional dependencies.

After cloning each repository, Jig should record it in `.jig/state.json` using the repository identity.

### `jig clone <repo> --with-optional-deps`

Clones a repository, non-optional dependencies, and optional dependencies.

### `jig update`

Updates `.jig.json` from its configured `source`.

The command should:

- Fetch the incoming definition.
- Validate the incoming definition.
- Compare the current and incoming definitions by repository identity.
- Report added, removed, moved, and changed repositories.
- Replace `.jig.json` only if the incoming definition is valid.

The command should not change local repository checkouts.

The command should not update `.jig/state.json`.

### `jig sync [path]`

Applies the current `.jig.json` to the local checkout shape.

If `path` is provided, Jig syncs repositories matching that path plus their non-optional dependencies.

If `path` is omitted, Jig syncs locally installed repositories known to the current `.jig.json` plus their non-optional dependencies. It should not clone every repository in `.jig.json` by default.

Sync may perform these actions:

- Clone missing repositories in the sync set.
- Move a local repository when `.jig/state.json` records a path different from the current expected path.
- Update a repository's `origin` remote URL when the current definition Git URL differs from the local repository's `origin` remote URL.
- Update `.jig/state.json` after successful clone, move, or origin update operations.
- Report repositories that exist locally but are no longer defined.

Sync must not delete local repositories.

Sync must skip and report any operation that is ambiguous or unsafe.

Examples of unsafe operations:

- The target path already exists.
- The source repository cannot be confidently identified.
- Multiple local repositories appear to match the same definition.
- A local repository has uncommitted changes and would need to be moved.

### `jig sync [path] --with-optional-deps`

Syncs repositories matching `path`, non-optional dependencies, and optional dependencies.

### `jig pull [path]`

Pulls all locally installed Git repositories matching `path`.

If `path` is omitted, all locally installed repositories in the workspace are matched.

The path matching semantics are the same as dependency path matching. A path matches a repository ID when either:

```text
repoId == path
```

or:

```text
repoId starts with path + "."
```

For each matching locally installed repository, Jig runs the equivalent of a standard `git pull` from that repository's local directory.

Repositories that are defined in `.jig.json` but not present locally are ignored.

If pulling a repository would result in a merge conflict, Jig should skip that repository and continue pulling the remaining repositories.

Jig should report which repositories were pulled successfully and which repositories were skipped.

### `jig status [path]`

Shows local checkout status for repositories matching `path`.

If `path` is omitted, Jig reports status for all repositories known to `.jig.json` plus any repositories tracked in `.jig/state.json` that are no longer defined.

The path matching semantics are the same as dependency path matching.

Status should identify:

- Installed repositories.
- Missing repositories.
- Repositories tracked in state but no longer defined.
- Repositories whose state path differs from the current expected path.
- Repositories whose local `origin` remote URL differs from the current definition Git URL.
- Repositories with uncommitted changes.
- Repositories whose expected path exists but cannot be adopted because it is not a Git repository or has a different origin.

The initial implementation may omit ahead/behind information if computing it would require network access. Local-only status should not fetch from remotes.

## Open Questions

- Should repository definitions support an explicit local path override?
- Should `jig clone <group>` be supported in the MVP?
