# Changelog

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
