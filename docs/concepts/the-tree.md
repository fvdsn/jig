# The tree

The `tree` of the schema maps workspace paths to entries. The path is the location on disk, always relative and
written with `/`. There are four kinds of entries:

| Entry | Declares |
| --- | --- |
| `$repo` | a git repository, cloned at that path |
| `$group` | metadata for a directory: description, tags, dependencies and commands shared by everything under it |
| `$file` | a file generated from one or more sources |
| `$dir` | a directory generated from one or more sources |

```yaml
version: 4

tree:
  platform/:
    $group:
      description: Shared platform services
      tags: [go]
      test: go test ./...
    auth:
      $repo:
        id: auth-service
        git: git@github.com:acme/platform-auth.git
    billing:
      $repo:
        git: git@github.com:acme/platform-billing.git

  scripts/dev.sh:
    $file:
      src: git@github.com:acme/workspace-config.git#scripts/dev.sh
      executable: true
```

Keys can be nested (`platform/` then `auth`) or written as one path (`platform/auth`). A trailing `/` marks a
directory key.

## Groups and inheritance

Children inherit from their groups:

- `tags`, `dependsOn` and `onlyWhen` add up along the path;
- `description`, `web`, `archived`, `meta` and the `setup`/`fmt`/`lint`/`test` commands apply unless the child sets
  its own (the nearest declaration wins).

A group's `id` is not inherited.

## Identity

Every entry has an identity. By default it is the entry's path; set `id` to give it a stable one. Jig uses the
identity to recognize an entry across schema changes. A repository with an `id` can move to another path, and
`jig sync` moves the checkout. Without an `id`, a moved repository is cloned again at its new path. Ids are unique
across the whole schema.

## Files and directories

`$file` and `$dir` sources are git URLs with `#path` pointing inside the repository, local paths (`~/...`), or
paths inside a checked-out repository of the workspace. A file can be assembled from several sources, each with its
own condition, which is how a single `AGENTS.md` can follow the repositories you have installed.

Jig tracks what it wrote. A generated file you edited by hand is kept and reported, never overwritten, and files you
add inside a generated directory are left alone.

## Tags, meta and archived

- `tags` group entries by capability (`api`, `go`, `frontend`) and work as filters: `jig list --tags api`,
  `jig clone services --tags api`.
- `meta` holds your own string keys and values. Jig shows and filters on them (`jig list --meta team=core`) but never
  interprets them.
- `archived: true` hides an entry from `list` and `clone` unless you pass `--archived`. Installed archived entries
  keep working.
