# Changelog

## v2.0.1 — 2026-08-13

- Fixed: a path move that only changes letter case (e.g.
  `codabox/Infrastructure` → `codabox/infrastructure`) was refused with
  "target path already exists" on case-insensitive filesystems (macOS
  default). Such a move now renames through a temporary name so the case
  change sticks; genuinely conflicting targets still refuse.

## v2.0.0 — 2026-08-12

- **Structured references** (schema version 2, breaking). Every schema
  reference — `dependsOn`, `onlyWhen` (including per-source conditions),
  and `link` — is now a structured object with exactly one selector:
  `{"id": ...}` (one entry, stable across moves), `{"path": ...}` (one
  declared entry, exact match), or `{"tags": [...]}` (every repository
  carrying all the tags). A trailing `/*` on a path selects the recursive
  subtree strictly below it — the only wildcard. Implicit prefix matching
  is gone: a path ref that names nothing, or names a group without `/*`,
  is a validation error with a hint, so moves and typos fail loudly
  instead of silently changing what a reference matches.
- `dependsOn` gains `id` and `tags` selectors; `link` becomes an object
  (`{"id": ...}` or `{"path": ...}`) and may target by id. Combined
  path-and-tags conditions are replaced by the exactly-one-selector rule.
- Identities are now globally unique across entry kinds, making id
  references unambiguous.
- CLI: every selection command accepts `--id <id>` alongside the existing
  `--tags`, mirroring the schema's three selectors. `--id` picks exactly
  one entry by identity, position-independent and including archived
  entries. Path positionals also accept an explicit `services/*` form.
- Version 1 schemas are refused with a migration hint; there is no
  in-tool migration (update refs, then set `"version": 2`).

## v1.12.0 — 2026-08-12

- `jig checkout --default` switches each installed repository to its own
  remote default branch — the way back to the mainline when repositories
  disagree on what it is called (`main`, `master`, `staging`, …). The
  branch is resolved per repo from `origin/HEAD` (offline for normal
  clones); when that ref is missing, the remote is asked once and the
  answer recorded so later runs are offline again. Report lines name the
  branch each repo landed on, e.g. `switched: services/api (main)`.

## v1.11.0 — 2026-08-06

- Repositories declare lifecycle commands in the schema — `setup`, `fmt`,
  `lint`, `test` — and the matching jig commands run them across installed
  repositories: each repo's own tooling behind a standard verb, so
  `jig test --tags go` needs no per-repo knowledge. `jig setup` runs in
  dependency order to make a fresh clone usable; the others run in
  parallel. Repos without a command are counted, not failed; failures
  report the command's output and exit non-zero. Commands are inherited
  from groups (nearest wins) and are only ever run by explicit
  invocation — never on clone, sync, or update.
- Changed: with multiple `$dir` sources, a conflicting file is now won by
  the **last** source instead of the first — a source list reads as base
  layers first, overrides after, matching how layered systems usually
  compose. Shadowed files are still reported. Schemas relying on the old
  first-wins order should reverse their source lists.

## v1.10.0 — 2026-08-05

- New `jig push [-u] [path]` command publishes the current branch of
  installed repositories in parallel, completing the multi-repo loop with
  `jig checkout -b` and per-repo commits. Pushes are never forced (no
  force flag exists); rejected pushes and detached HEADs are reported as
  skipped, and repositories with nothing to push report `up to date`
  without touching the network. `-u` sets the upstream on fresh branches
  (`git push -u origin <branch>`), so a first push after
  `jig checkout -b feature-x` is one command.
- New `jig graph [path]` command prints the repository dependency graph
  as a Mermaid flowchart: directories render as nested subgraphs, group
  dependencies point at the subgraph itself, and optional edges are
  dashed. The output is the raw diagram, ready to pipe into mermaid
  tooling or wrap in a ```` ```mermaid ```` fence for a README or PR.
- `jig deps <path> --reverse` shows the direct dependents of a repository
  or group: every repo whose `dependsOn` resolves to it, including edges
  through group paths that never name it. Deliberately not recursive —
  the answer is the declared consumers, not the transitive blast radius.
  `--with-optional-deps` and `--archived` mirror the forward direction.
- New `jig tags [path]` command lists the tags carried by the entries in
  scope, with entry counts — so `--tags` filter values are discoverable
  without reading the schema. Pathless, it scopes to the current subtree
  like other position-relative commands; tags of uninstalled archived
  entries are hidden unless `--archived`.

## v1.9.0 — 2026-08-04

- Repositories, files, dirs, and groups accept a `meta` object: user-defined
  string keys and values that jig stores, displays, and filters on but never
  interprets — e.g. a GitLab repo can record where its synced GitHub mirror
  lives. `$group.meta` is inherited per key by descendants, nearest
  declaration wins. `jig info` shows an entry's effective meta, and
  `jig list --meta key[=value]` filters on it. Keys must not start with `$`
  or contain spaces, commas, or `=`; `jig validate` checks them.

## v1.8.2 — 2026-07-14

- jig can now be installed with Homebrew: `brew install fvdsn/tap/jig`
  (macOS and Linux, Apple Silicon and Intel/amd64). The release workflow
  updates the [fvdsn/homebrew-tap](https://github.com/fvdsn/homebrew-tap)
  formula automatically on every release. No changes to jig itself.

## v1.8.1 — 2026-07-14

- Source repository mirrors are freshened in parallel before files and dirs
  are materialized, instead of one network round-trip at a time during the
  pass. A sync whose support files draw from several source repositories
  spends only the slowest fetch on the network, not the sum (measured ~8s
  to ~2.5s on a workspace with four source repositories).

## v1.8.0 — 2026-07-13

- `$file.src` accepts a list of sources, concatenated in order into the
  single generated file (a newline is inserted between parts when one is
  missing). List entries can be `{ "src": ..., "onlyWhen": ... }` objects
  gating individual sections — the same shape `$dir` uses, but appending
  instead of merging trees. One `AGENTS.md` can now be assembled from a
  base section plus sections that follow the installed repositories. When
  every source is gated off, no file is generated: a previously written
  untouched file is removed on sync.
- `jig info` renders per-source `onlyWhen` tag conditions (previously only
  the condition's path was shown).

## v1.7.0 — 2026-07-07

- Commands are position-aware: run from a subdirectory, pathless commands
  scope to that subtree (inside a checkout, they address that one repo), and
  path arguments resolve like filesystem paths — `.`, `..`, and a leading
  `/` anchoring to the workspace root. Pathless `sync` converges installed
  entries within the subtree only; `sync --prune` requires the root.
- Fixed: jig errored when run from inside a checkout containing a
  `.jig.json` at its root (such as a schema repository installed in the
  workspace).

## v1.6.0 — 2026-07-07

- `onlyWhen` conditions can select repositories by `tags` — carrying all
  listed tags, inherited group tags included — by `path`, or both combined.
  Support artifacts can follow capabilities instead of locations: an API
  skill gated on `tags: ["api"]` materializes whenever any api-tagged
  repository is active or installed. Validation requires every condition to
  be satisfiable by some repository in the schema.

## v1.5.0 — 2026-07-07

- Commands exit non-zero when any entry was skipped (`clone`, `sync`,
  `update --sync`, `pull`, `fetch`, `checkout`), so scripts and agents never
  mistake a partial run for success. State from the successful part is kept.
- Commands that mutate the workspace take an exclusive `.jig/lock`, so
  concurrent jig runs cannot silently drop each other's state updates.
- Per-command help: `jig <command> --help` and `jig help <command>`.
- Version guards: a jig older than the schema, config, or state format it
  meets stops with an "upgrade jig" error instead of guessing.
- `--tags` works with `jig init --clone`.
- The planner was rewritten as a documented worklist solver; behavior is
  unchanged (verified by differential testing on a 350-repo workspace).
- End-to-end integration test suite and GitHub Actions CI (macOS and Linux).
- Prebuilt binaries for macOS and Linux (amd64/arm64) attached to releases.
- Licensed under MIT.

## v1.4.0 — 2026-07-06

- Sources accept forge web URLs pasted from the browser
  (`https://github.com/o/r/tree/main/path` and GitLab/Bitbucket/Gitea
  equivalents) in addition to `<clone-url>#<path>`.
- Bare `jig init` starts a fresh workspace with a starter schema.
- `jig checkout [-b] <branch> [path]` switches branches across installed
  repositories; never discards local changes.
- `--no-deps` on `clone`, `sync`, `update --sync`, and `init --clone`.
- `sync --prune` deletes entries that left the schema, under `rm` safety
  rules; renamed identities are re-adopted instead of reported stale.
- The jig agent skill ships in-repo at `.agents/skills/jig`.
- The obsolete `--refresh` flag was removed (sync detects source changes).

## v1.3.0 — 2026-07-06

- `$dir` entries support `link`: one real directory (e.g. `.agents/skills`)
  symlinked into every harness path.

## v1.2.0 — 2026-07-06

- `$dir` sources can be a list, merged in order into one directory (first
  wins on conflicts); list entries can carry a per-source `onlyWhen`.

## v1.1.1 — 2026-07-06

- The `git:` prefix on `$file`/`$dir` sources is optional (still accepted).

## v1.1.0 — 2026-07-06

- `$dir` entries materialize whole subtrees with manifest-guarded updates.
- `sync` updates generated files when their source repository changed;
  locally modified files are never overwritten.
- Machine-wide clone cache (bare mirrors, hardlinked clones, always safe to
  delete) with `jig cache` and `jig cache clean --unused <days>`.
- Files and dirs follow the repositories around them (scope activation).
- `status` reports only installed entries by default (`--all` for the
  catalog) and shows ahead/behind counts against upstream.
- `list` aligns and truncates on terminals; piped output stays full.

## v1.0.0 — 2026-07-06

First release: schema-repository workspaces (`.jig/source`), dependency-aware
cloning, tags with `--tags` filtering, restore-on-sync with `jig rm`,
parallel git operations, `fetch` and ahead/behind status, and CI-friendly
schema validation.
