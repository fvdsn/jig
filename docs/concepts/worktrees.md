# Worktrees

A worktree is a second checkout of part of the workspace, for one task, on a branch of its own. It lives inside the
workspace, under `worktrees/`, and leaves the root tree on its default branches.

```sh
jig worktree create feature/CX-12 services/checkout
```

```text
acme/
  services/checkout/                    the root tree, on its default branch
  platform/auth/
  worktrees/
    feature__CX-12/
      services/checkout/                on branch feature/CX-12
      platform/auth/                    on branch feature/CX-12
```

The path selects repositories as `jig clone` does, dependencies included. Every repository of the worktree is on
the branch the worktree is named after: an existing local branch is used as it is, a branch that exists on the
remote is tracked, and otherwise the branch is created at the remote default branch, freshly fetched. Whatever the
root tree has checked out or left uncommitted plays no part.

The directory is the branch name with each `/` written `__`, so a branch name may not contain `__`.

Run the command again with another path to add repositories to the worktree. Repositories missing from the root
tree are cloned there first.

## Working in a worktree

Each tree is its own scope. Inside a worktree, the commands that act on checkouts act on the worktree's, and paths
are taken from its root:

```sh
cd worktrees/feature__CX-12
jig status                 # the repositories of this worktree
jig test
jig push -u                # publishes the branch where it has commits
jig pull                   # untouched repositories follow their default branch
jig clone platform/billing # adds a repository to this worktree
jig rm platform/auth       # removes one from it
```

From the root tree, commands never reach into `worktrees/`.

Some things differ from the root tree:

- `jig switch --default` is refused by git, since the root tree holds the default branches. To go back to the
  mainline, `jig switch feature/CX-12` then `jig pull`.
- For a second merge request in one repository, create another branch there with `git switch -c`.
- A worktree holds repositories only. The agent context and the files of the tree exist in the root tree, and
  `jig sync` runs there.

## Removing a worktree

```sh
jig worktree remove feature/CX-12
```

This removes the checkouts and the directory. A checkout with uncommitted changes is kept, unless `-f` is given.

Commits are safe either way: they live in the root tree's clone. A branch that is merged, pushed or untouched is
deleted with its checkout. One with commits that exist nowhere else is kept in the root tree's clone and reported:

```text
removed: services/checkout
kept branch: services/checkout (feature/CX-12, 2 unpublished commits)
```

## Real git worktrees

Each checkout is a [git worktree](https://git-scm.com/docs/git-worktree) of the root tree's clone of the same
repository: they share branches, remotes and objects, and `git worktree list` shows them. A branch can be checked
out in one tree at a time.

Because the worktrees depend on it, `jig rm` and `jig sync --prune` never delete a root tree repository that has
worktrees. Remove the worktrees first.

The name `worktrees` is reserved at the top of the schema tree.
