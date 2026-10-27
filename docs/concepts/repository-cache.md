# The repository cache

All workspaces on a machine share one repository cache: a bare mirror of each remote jig has cloned, kept in your
user cache directory.

When jig clones a repository, it first creates or refreshes the mirror, fetching only what is new, then clones from
the mirror locally. A local clone hardlinks git's object files instead of copying them, so a second checkout of the
same repository costs little time and little disk space, close to a git worktree.

Unlike a worktree, each checkout is a normal, independent repository with its `origin` pointing at the real remote.
Nothing in a workspace depends on the cache.

This is what makes task workspaces practical:

```sh
jig init git@github.com:acme/workspace.git ~/Code/acme-cx12 --clone services/checkout --switch feature/CX-12
```

## Managing the cache

```sh
jig cache                         # location, number of mirrors, size
jig cache clean                   # remove every mirror
jig cache clean --unused 30d      # remove mirrors unused for 30 days
```

The cache is safe to delete at any time. The next clone of a repository simply fetches it again.

`JIG_CACHE_DIR` moves the cache. Setting it to an empty string disables it. If anything goes wrong with the cache,
jig falls back to a normal network clone, so the cache never makes a clone fail.
