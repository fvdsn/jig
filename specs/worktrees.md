# Worktrees — 2026-11-24

Proposal to add `jig worktree create` and `jig worktree remove`: a second checkout of part of the tree for one
task, inside the same workspace, built from real git worktrees on a branch of their own, so that the root tree
stays on default branches.

This is a first version, deliberately without per-worktree state. What it leaves out is listed under
[Not in this version](#not-in-this-version).

## Motivation

The root checkout is shared, and every task contends for it. Measured on the CodaBox workspace (326 repositories)
and its 256 OpenCode sessions:

- **Task branches pile up in the root tree.** 92 leftover local branches, 7 repositories off their default
  branch, 44 with a stash. One ticket, PLT-915, created 16 branches across six repositories over a month.
- **Agents read around the checkout.** Because the working copy is on someone's branch, agents get upstream
  content from `origin/` refs instead: `git diff` / `git log` against `origin/` in 38 sessions, `git show
  origin/<branch>:<file>` in 17, `git archive | tar -x` into a temporary directory in 3.
- **Or they move it.** Sessions stash, branch from `origin/master`, pop, and leave the checkout on their branch.
- **One session built a worktree by hand**, in a temporary directory, and copied a second repository next to it
  to render a chart: a clean multi-repository copy, without the workspace around it.
- **A separate task workspace does not help** an agent already running in the default one, and costs three
  network rounds and a full agent context install to create.

## Model

A worktree is a second instance of part of the schema tree, rooted at `worktrees/<name>/`, with every repository
on the branch `<name>`:

```text
workspace/
  .jig/
  CLAUDE.md                             agent context: root only
  services/checkout/                    root tree: the main clone, on its default branch
  platform/auth/
  worktrees/
    PLT-915/
      services/checkout/                git worktree of ../../services/checkout, on branch PLT-915
      platform/auth/                    git worktree of ../../platform/auth, on branch PLT-915
```

- **Real git worktrees.** Every checkout in a worktree is a `git worktree` of the root tree's clone of that
  repository. `git worktree list`, shared branches, shared remotes and shared objects behave as git documents.
- **The root tree holds the main clone.** Creating a worktree installs what is missing at the root. The clone
  cache is never a worktree's repository: `jig cache clean` stays safe at any time.
- **One branch, every repository.** The name is a branch name, and every repository of the worktree is on that
  branch, dependencies included. No checkout is detached, and there is no difference between the repositories
  asked for and the ones that came along. A task that needs more branches, one per merge request, creates them
  inside with `git switch -c`.
- **Repositories only.** The agent context and the tree's `$file` / `$dir` entries exist at the root only.
- **The disk is the record.** A repository is in a worktree when a git checkout exists at
  `worktrees/<name>/<its schema path>`. There is no per-worktree state.
- **Activation is unchanged.** The root tree is a superset of every worktree, so `onlyWhen`, group `agents`
  blocks and exports keep reading the root tree.

## `jig worktree create <name> <path>`

Creates the worktree `<name>` with the repositories matching `<path>`, or adds them to it when it exists.

- **`<name>`** is a branch name. The directory is the name with each `/` replaced by `__`: `feature/CX-12` is
  created at `worktrees/feature__CX-12/`. A name that contains `__` is refused, so a directory maps back to
  exactly one branch name.
- **`<path>`** is required, and selects schema entries as in `jig clone <path>`: position-relative, the node and
  its subtree, with non-optional dependencies. Omitting it is a usage error. `.` at the workspace root selects
  the whole schema, as it does for `jig clone`.

Steps:

1. **Root tree.** `jig clone <path>` semantics at the root: missing repositories are cloned through the cache,
   with everything that follows from a clone. Installed ones are left alone, whatever their branch or state.
2. **Fetch.** `git fetch origin` in the root clone of every repository involved, in parallel, one round. A fetch
   that fails is reported and the run continues from what the clone last fetched.
3. **Checkouts.** Each repository is added with `git worktree add` on the branch `<name>`:
   - the local branch exists: it is used as it is;
   - else `origin/<name>` exists: the branch is created from it and tracks it;
   - else the branch is created at the remote default branch (`origin/HEAD`, resolved as in
     `jig switch --default`) with `--no-track`, so that it never pushes to the default branch.

   The start point never depends on what the root clone has checked out.

Per repository, reported under `skipped` with the rest proceeding: the branch is checked out in another tree,
the root tree included; the target path exists and is not a worktree of the root clone; the root clone could
not be installed.

Re-running is idempotent: existing checkouts report `present`. Output follows `jig clone`, one line per checkout.

## `jig worktree remove <name> [-f]`

`<name>` is the branch name or the directory name. Removes every git checkout found under `worktrees/<name>/`, then the directory. Checkouts are found by walking
the directory, so one whose schema path has changed since is still removed.

Commits on a branch live in the root clone and survive removal, pushed or not. Removal is refused, per
checkout, only when it would lose work:

- uncommitted changes, untracked files included;
- a `HEAD` that no local branch and no remote-tracking branch contains (commits made while detached).

`-f` / `--force` overrides both. Checkouts are removed with `git worktree remove`, so the root clone's worktree
list stays accurate.

In each repository, the branch `<name>` and the branch the checkout was on are deleted from the root clone when
a remote-tracking branch contains their tip: untouched, pushed or merged, nothing is lost. Otherwise the branch
is kept and reported: `kept branch: services/checkout (PLT-915-rolling-update, 2 unpublished commits)`. `-f`
does not change this.

Anything else under `worktrees/<name>/` is kept and reported, and the command exits non-zero; `-f` deletes it.
Failing entries are reported and the rest proceed, as in `jig rm`.

There is no `jig worktree list`: `ls worktrees` answers it.

## Existing commands

**Scope.** Each tree is its own scope. From the root tree, every command behaves as today and never reaches into
`worktrees/`. From `worktrees/<name>/` or below, paths resolve relative to the worktree root, and commands that
act on checkouts act on the ones present there. From `worktrees/` itself, those commands fail and say to enter a
worktree. Reported paths inside a worktree are relative to the worktree root.

- **`status`, `diff`, `fetch`, `pull`, `push`, `switch`, `git`, `setup`, `fmt`, `lint`, `test`**: act on the
  worktree's checkouts. `status` lists what is present; it has no notion of missing there. `switch` to a branch
  checked out in another tree is refused by git and reported under `skipped`, like any refused switch. That
  includes `switch --default`, since the root tree normally holds the default branch: inside a worktree, going
  back to the mainline is `jig switch <name>` then `jig pull`. `switch -c` creates the branch in every
  repository under the current directory, untouched dependencies included, so run it from the subdirectory the
  change concerns.
- **`list`, `tags`, `info`, `deps`, `graph`, `validate`, `update`**: unchanged; they read the schema.
- **`clone [path]`** inside a worktree adds the selection to it, as `worktree create` does.
- **`rm <path>...`** inside a worktree removes those checkouts from it, with the `worktree remove` rules.
- **`sync`** inside a worktree is refused: sync applies the schema to the root tree, and says so.

**Untouched branches.** Most repositories of a worktree are on a branch with no upstream and no commits of its
own. Two verbs change so that those stay quiet and current, for all trees:

- **`pull`**: on a branch with no upstream whose tip the remote default branch contains, pull fetches and
  fast-forwards the branch to `origin/HEAD`. Untouched repositories therefore follow the default branch. A
  branch with no upstream and commits of its own fails as today.
- **`push`**: a branch with no upstream whose tip a remote-tracking branch contains reports `up to date`, with
  or without `-u`. `jig push -u` in a worktree therefore publishes the branch only where it has commits.

**Root tree guards.**

- `jig rm` of a repository that has linked git worktrees is refused, `-f` included, and names them, whether jig
  created them or not. `jig sync --prune` keeps such a repository and reports it under `kept`. This closes a way
  to lose work that exists today: `jig rm` only checks the current branch of the checkout it deletes.
- When sync moves a root clone, it runs `git worktree repair` there, so its worktrees keep their link.
- A schema whose tree has a top-level `worktrees` segment fails validation.

## Not in this version

All four follow from having no per-worktree state. Adding it later means a `.jig/worktrees/<name>/state.json`
shaped like the root's, on top of the disk discovery above, with one lock per tree taken worktree first.

- **Restore and path moves.** A deleted checkout comes back with `create`, not `sync`. After a schema path
  move, a worktree's checkout stays at its old path, unseen by jig until `git worktree move` or a recreate.
- **Tree files.** `$file` / `$dir` entries are not written in worktrees; tracking their hashes needs state.
- **`sync` in a worktree.**
- **Per-worktree locks.** Nothing in a worktree is written by jig except through git, which locks for itself.
  `create` takes the workspace lock while it clones at the root.

Add them when a schema needs its tree files in worktrees, or when path moves strand worktrees in practice.

## Documentation and changelog

- `specs/main.md`: a Worktrees section, the two commands, the scope rule under Position-Relative CLI Paths, the
  reserved path under Safe Paths, and the `rm`, `sync --prune`, `pull` and `push` changes.
- `docs/concepts/`: a worktrees page, linked from the README's "task workspaces" highlight, and a line in
  `repository-cache.md` saying worktrees hang off the root clone, never the cache.
- The skill:
  - never create a branch or leave changes in the root tree; it is the reference copy, on default branches;
  - for a task, `jig worktree create <branch> <path>` and work inside the directory it reports; every
    repository there is already on `<branch>`, and `jig push -u` publishes the ones with commits;
  - for a second merge request in the same repository, `git switch -c` there;
  - `jig switch --default` does not work inside a worktree; use `jig switch <branch>` then `jig pull`;
  - `jig worktree remove <branch>` when the task is merged;
  - search inside the worktree, not from the root, which now holds its repositories a second time.
- Changelog: the new commands; `worktrees` reserved as a top-level tree path (breaking for a schema that uses
  it); `jig rm` refusing repositories with linked worktrees; the `pull` and `push` changes for branches with no
  upstream and no commits of their own.

## Tests

- Create: a missing repository is cloned at the root and added; an installed one is not re-cloned or touched.
- The branch starts at `origin/HEAD` when the root clone is on another branch, dirty, or behind; an existing
  local branch is reused; `origin/<name>` is tracked; a branch checked out in another tree is skipped.
- A name with `/` maps to a `__` directory; a name with `__` is refused; remove accepts either form.
- Re-running create is a no-op; create with a second path extends the worktree.
- Activation at the root is the same before and after creating a worktree of already installed repositories.
- Remove: refused when dirty or with commits only on a detached `HEAD`; allowed with unpushed commits on a
  branch, which is kept; an untouched, pushed or merged branch is deleted, both `<name>` and the current one;
  `-f` semantics; untracked content is kept.
- Remove finds a checkout whose schema path has since moved.
- Root `jig rm` and `sync --prune` refuse a repository with a linked worktree, including one made by plain git.
- Scope: commands at the root do not touch worktrees; commands in a worktree do not touch the root or siblings.
- `pull` fast-forwards an untouched branch with no upstream and fails on one with its own commits; `push` and
  `push -u` report `up to date` for an untouched one and create no remote branch.
- A root clone moved by sync keeps working worktrees.
- `sync` inside a worktree is refused; validation rejects a top-level `worktrees` tree path.
- `jig cache clean` with worktrees present breaks nothing.

## Open questions

- **Branch cleanup in root clones.** Removal deletes only `<name>` and the branch a checkout was on. The other
  branches of a task stay in the root clone, which is the pile-up this proposal starts from. A rule such as "delete local
  branches that a remote-tracking branch contains and no tree has checked out" belongs with the parked `discard`
  design.
- **`pull` and `push` rules in the root tree.** The new rules also apply to a root checkout on a fresh local
  branch with no upstream: `pull` moves it to the default branch, where it fails today, and `push -u` no longer
  publishes an empty branch. Acceptable, or restrict to worktrees?
- **`--detach`.** Checkouts at the default branch with no branch at all, as `git worktree add --detach`. Left
  out: a detached `HEAD` confuses people, and one mode is simpler to teach. Add if agents need worktrees that
  leave no branch behind.
- **Minimum git version.** `git worktree repair` sets it (2.29, to confirm).
- **`--no-fetch`.** Skipping step 2 makes create fully local, about 0.8 s for codabox-core. Wait for demand.
