# Changelog

## v4.8.1 — 2026-10-20

- Fixes: `jig status` no longer reports as missing the entries sync
  would not write: a file or dir whose every source is gated off (a
  plugin library gated on a tag no installed repository carries, an
  MCP document whose servers are all conditional) is inactive, as sync
  reports it, and is left out of the status list.

## v4.8.0 — 2026-10-20

- `jig checkout` is now `jig switch`, after git's own verb for what it
  does, with `-c` to create a branch. It takes several branches: each
  repository switches to the first one it has, locally or on origin,
  and `--default` after the list falls back to the repository's own
  default branch, so `jig switch release/CX-12 feature/CX-12 --default`
  puts a workspace on the branches a piece of work touches. A
  repository with none of the branches is skipped. The old name and
  `-b` are gone. `-c` on a branch that exists on origin only switches
  to a tracking branch instead of creating a second one.
- `jig init --clone ... --switch <branch>` puts every cloned
  repository on the branch, created where it does not exist, so one
  command sets up a workspace for a piece of work.

## v4.7.0 — 2026-10-20

- Repository exports: `agents` on a `$repo`, or on a `$group` for every
  repository beneath it, names a path inside the checkout per agents
  block primitive (`skills`, `context`, `subagents`, `commands`), and
  that path joins the block's sources while the repository is
  installed, read from the local checkout's working tree. A repository
  shipping the skill of the tool it builds thus lands it at the
  workspace root and in every harness directory with no further step,
  and a repository without the path offers nothing. The checkout
  source is also available to `$file` and `$dir` as
  `{"repo": {"id": ...}, "path": ...}`.
- Multi-source files get one blank line between parts instead of a bare
  newline, so a section's opening heading stays a heading; every
  multi-source file is rewritten once on the next sync. An exported
  context section is framed with a `## <repository path>` heading and a
  line saying it applies to that project.
- Fixes: a link file or dir whose target has every source gated off is
  now removed with the target instead of failing the sync with "link
  target is missing".

## v4.6.0 — 2026-10-20

- The jig skill ships inside the binary and is installed in every
  workspace, agents block or not and whatever the schema version, as
  `.agents/skills/jig` and the harness copies the machine's harness
  list asks for, so the skill always matches the installed version. A
  schema that declares the skills directory itself takes it in as a
  source; an explicit `jig` skill shadows it. An upgraded jig rewrites
  it on the next sync. `jig skill` prints it. Schemas that pulled
  `https://github.com/fvdsn/jig/tree/master/.agents/skills` should
  drop that source, which now only shadows the embedded skill with
  whatever master holds. A bare `jig init` seeds a version and an
  empty tree, and needs no network.
- A schema can be written in YAML (`jig.yaml`, `jig.yml`, or any
  `--path` with those extensions) with the same structure as JSON.
  YAML is read strictly: one document, no anchors, aliases, merge keys,
  or local tags, no duplicate keys, and a scalar typed by YAML where a
  string is wanted gets a hint to quote it. A bare `jig init` now seeds
  a YAML starter, a local `.yaml` file keeps its format in the source
  checkout, and a Git source is probed for `jig.yaml` and `jig.yml`
  after the JSON names. Jig takes its first dependency for this,
  `github.com/goccy/go-yaml`.

## v4.5.0 — 2026-10-20

- The `agents` block gains `hooks`, its last primitive: commands run
  on lifecycle events, declared once in a neutral vocabulary
  (`sessionStart`, `beforeToolUse`, `afterToolUse`, `beforePrompt`,
  `stop`, with a tool-name `matcher` on the tool events) and written
  into each harness's hook document in its own event names and shape
  (`.claude/settings.json`, `.codex/hooks.json`, `.cursor/hooks.json`,
  `.gemini/settings.json`, `.windsurf/hooks.json`,
  `.github/hooks/jig.json`), tool names translated where the harness
  has its own. Hook documents follow the managed-document rules of
  `mcp`, owning elements by their matcher and command. A hook a harness
  cannot take is reported as skipped. A plugin's `hooks/hooks.json`
  feeds the list.

## v4.4.0 — 2026-10-20

- The `agents` block gains `mcp`: MCP servers declared once in the
  standard shape and written into each harness's own configuration
  document (`.mcp.json`, `.github/mcp.json` and `.vscode/mcp.json`,
  `.cursor/mcp.json`, `opencode.json`, `.gemini/settings.json`,
  `.kiro/settings/mcp.json`, `.codex/config.toml`) in that harness's
  shape, with environment references translated to the harness's
  syntax. These are managed documents: jig owns only the keys it wrote,
  keeps and reports conflicting or edited keys, removes its keys when
  a server leaves the schema, preserves the document's key order, and
  deletes a document only if it created it. A plugin's `.mcp.json`
  feeds the same map. Only `hooks` remains unimplemented.

## v4.3.1 — 2026-10-20

- Fixes: with two or more harnesses fed by a plugin source that has
  no content for a primitive (a plugin without commands, say), the
  second harness's directory failed with a git archive error instead
  of being reported inactive. Status no longer reports such a
  directory as missing before anything has been written to it.

## v4.3.0 — 2026-10-20

- The `agents` block gains `plugins`: plugins in the Claude Code plugin
  layout, or marketplaces of them, kept whole under `.agents/plugins/`
  and feeding the skills, subagents, and commands primitives, with the
  links from a skill or agent to the plugin's shared files rewritten
  to resolve from each harness's directory. One line installs an
  organization's plugin library for every harness.
- A primitive whose sources resolve but contribute nothing (plugins
  without a commands directory, say) is reported inactive instead of
  failing.
- A plugin source is resolved with one listing and one archive of the
  whole repository per run, instead of several git calls per plugin
  per entry: cloning a 25-plugin marketplace dropped from 7 to 2.5
  seconds on a warm cache.

## v4.2.0 — 2026-10-20

- The `agents` block gains `instructions`: rule files (markdown with a
  `description` and a `paths` glob list; `applyTo` works too) written
  under each harness's own frontmatter in `.claude/rules`,
  `.github/instructions`, `.cursor/rules` (`.mdc`), `.windsurf/rules`,
  and `.kiro/steering`, and folded into the root context file of the
  harnesses without per-file rules (Codex, OpenCode, Gemini) under an
  `## Instructions` heading. A root file folds when any harness in use
  that reads it has no per-file rules, as APM does; an alias whose
  folding differs from the canonical file's becomes a real file instead
  of a link. Only `mcp` and `hooks` remain unimplemented.
- Fixes: the per-harness entries written in a harness's own format
  (Codex subagents, Gemini commands, Copilot subagents) were reported
  as changed on every schema update.

## v4.1.0 — 2026-10-20

- The `agents` block gains `subagents` and `commands`: source lists
  whose entries are directories of markdown files or single files
  (git or local), each file landing under its own basename in every
  harness's own directory (`.claude/agents`, `.cursor/commands`,
  `.windsurf/workflows`, ...). Codex subagents and Gemini commands are
  written as TOML and Copilot subagents get the `.agent.md` suffix, so
  no harness is skipped. Only `instructions`, `mcp`, and `hooks` remain
  unimplemented.

## v4.0.1 — 2026-10-20

- The `agents` block derives one entry per location a harness in use
  reads, and nothing else: a Claude-only machine gets `.claude/skills`
  and `CLAUDE.md` as the real entries, with no canonical duplicate for
  its search tools to find twice. The canonical `.agents/skills` and
  `AGENTS.md` are derived when a harness in use reads them (Codex,
  Copilot, Cursor, OpenCode, Windsurf) or when no harness is in use;
  the other harness paths are then copies or links of them. Changing
  the harness list converges in place.

## v4.0.0 — 2026-10-20

- Schema version 4 adds the top-level `agents` block: `skills` and
  `context` source lists declare the workspace's agent context once,
  with no harness path, and jig derives the `.agents/skills` and
  `AGENTS.md` entries plus an alias per harness in use (`.claude/skills`
  as a copy, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`
  as links). A skills source whose root is itself a `SKILL.md` lands
  under its own name. Version 3 schemas keep working unchanged; the
  other primitives of the spec (instructions, subagents, commands, mcp,
  hooks) are refused by name until implemented.
- Which harnesses are materialized is per machine: `jig init` detects
  them from the harness directories in the home folder and prints the
  list, or `jig init --harness claude,codex` (or `none`) records a
  choice as `harnesses` in `.jig/config.json`.
- The starter schema of a bare `jig init` uses the `agents` block.

## v3.0.0 — 2026-10-19

- Breaking: the commands that act on checkouts (`sync`, `setup`,
  `fmt`, `lint`, `test`, `pull`, `fetch`, `push`, `checkout`, `status`,
  `diff`) no longer take a path, `--tags`, `--id`, or `--archived`.
  Their scope is the directory they run in: the whole workspace from
  the root, a subtree from a group directory, one repository from
  anywhere inside its checkout. The schema commands (`list`, `tags`,
  `info`, `deps`, `graph`, `clone`) keep the selectors, since their
  subjects may not be on disk yet. `sync` converges what is installed
  and never materializes an entry that was not cloned; use `jig
  clone` for that.
- A path below a repository or dir entry addresses that entry, the
  way git finds its repository from any directory inside a checkout.
- `jig git <git args...>` runs any git command verbatim in the
  installed repositories in scope, one report per repository in path
  order; it is raw git with none of the safety of the curated verbs.

## v2.6.0 — 2026-10-19

- Fixes: a workspace or mirror lock left behind by a jig process that
  no longer runs (killed, crashed, or its terminal closed) is taken
  over instead of being reported as stale; the lock error names the
  holder pid. A mirror lock whose holder is alive is no longer stolen
  after ten minutes; only a holderless one is. A hangup (closing the
  terminal) releases the held locks like an interrupt does.

## v2.5.2 — 2026-09-21

- The clone pass of `jig init --clone`, `jig clone`, and `jig sync`,
  and the mirror prefetch for file and dir sources, show the
  transient progress line the git verbs have, so a long clone no
  longer looks stuck on the last completed line.

## v2.5.1 — 2026-09-21

- Fixes: interrupting a jig command (ctrl-c, SIGTERM) removes the
  workspace and mirror lock files it holds, so the next command no
  longer reports a stale lock.

## v2.5.0 — 2026-09-07

- Fixes: `jig sync --id X` without a path now materializes the entry
  instead of silently ignoring the selector; `jig setup` orders
  through repositories that have no setup command; a `$dir` whose
  sources are all gated off converges to not existing (and is removed
  on deactivation) like a `$file`; turning `executable` off takes
  effect on a written file; read-only commands fail on a corrupt or
  newer state file instead of reporting an empty workspace; a tree
  path defined both nested and as a slash key is a validation error;
  a `$dir` merge that rejects a source entry no longer hangs; two jig
  processes can no longer both steal a stale mirror lock; `jig deps`
  no longer lists a conditional root as its own dependency; `jig
  status` lists stale entries in path order; `jig diff` exits non-zero
  when a repository was skipped; `jig update` detects a no-op under
  any locale and reports git's fast-forward error; a moved repository
  is reported once; Mermaid node ids stay distinct.
- `jig graph` and `jig tags` accept `--tags` and `--id` like every
  other scoped command.
- `jig info` on a group prints `path:` and `type: group` like the
  other kinds; `executable:` appears only when set.
- Applying a plan announces each skipped entry where it would have
  reported and lists the reasons in one `skipped:` group at the end,
  like the other commands; the git verbs announce a failed repository
  as it happens and show progress on a terminal; summary errors are
  worded uniformly (`pull: 2 repositories skipped`, `3 entries
  skipped`, `1 entries not removed`).
- A legacy root `.jig.json` produces the layout-no-longer-supported
  error the spec describes.

## v2.4.1 — 2026-09-02

- Schema version 3 (breaking): the local-source and `optional`
  additions bump the schema version. Version 2 schemas are refused
  with "set version: 3 (no other changes needed)" — the bump is the
  whole migration — and older jigs refuse version 3 schemas with
  their clear upgrade-jig error instead of a confusing invalid-src
  one.

## v2.4.0 — 2026-09-02

- `$file` and `$dir` source lists accept local sources — `{"file":
  "~/.codabox/MY-AGENTS.md"}` and `{"dir": "~/.codabox/skills"}`,
  rooted at `~/` or `/` — merged like any git source, with content
  hashes standing in for blob/tree ids so local edits flow in on the
  next sync. Any source may declare `"optional": true`: an optional
  source that fails to resolve is gated off silently (the artifact
  converges without it) instead of the degraded unavailable handling,
  enabling per-user extension points that most machines leave empty.

## v2.3.0 — 2026-09-02

- `$file` and `$dir` entries accept `copy`, a single-target reference
  materializing another entry's sources as a real file or directory
  instead of a symlink. Copies converge on the target's sources on every
  sync, follow link activation and ordering rules, and never chain (the
  target must define `src`). Switching an entry between `link` and
  `copy` converges on sync; locally modified content blocks converting
  back to a link. Real directories side-step symlink handling gaps in
  agent harnesses reading skill directories.

## v2.2.1 — 2026-09-02

- `jig diff` on a terminal now reuses the user's git diff
  presentation: the patch is piped through the pager git diff would
  use (`pager.diff`, then `GIT_PAGER` / `core.pager` / `PAGER`), and
  each repository's git runs with `GIT_PAGER_IN_USE` set so the
  user's color configuration applies — custom viewers such as delta
  work unchanged, now over the whole workspace tree. Piped output and
  `--stat` stay plain, matching git.

## v2.2.0 — 2026-09-02

- New `jig diff [path] [--stat] [--tags a,b] [--id x]`: the
  uncommitted changes of installed repositories as one workspace-wide
  unified diff, with the repository path folded into the `a/`/`b/`
  prefixes so the output reads as the diff of the whole tree. Diffs
  against `HEAD`, so staged changes appear; untracked files stay with
  `jig status`. `--stat` prints one summary line per dirty repository
  (files changed, +/- line totals) for triaging many dirty repos at a
  glance.
- `jig status` dirty notes now carry counts, e.g. `dirty (14 changed,
  3 untracked)`.

## v2.1.2 — 2026-09-02

- Rerunning `jig init` now converges instead of erroring, so a setup
  that failed midway is finished by running the same command again. A
  source checkout without `.jig/config.json` is a leftover from a
  failed init (the config is written only once the source and schema
  are good) and is replaced. An initialized workspace targeting the
  same source resumes: the schema is updated and the clone step runs
  with the given flags. A different source is a conflict and refuses,
  naming both; bare init and local schema files (no verifiable
  identity) refuse as before.
- Multi-source `$file` entries now tolerate unavailable sources the
  way `$dir` does: a source that fails to resolve is excluded from
  that run and reported on the status line instead of skipping the
  whole file (and any links targeting it). A file not yet written is
  generated from the available parts; an already-written untouched
  file is left as is while any source is unavailable, so the missing
  source's content is never dropped — the full rewrite happens once
  every source resolves again.

## v2.1.1 — 2026-09-02

- Selector typos now get did-you-mean errors. A `--tags` filter naming
  a tag no schema entry carries says so directly, suggesting the
  closest existing tag (or pointing at `jig tags`); a path where no
  entry is defined suggests the closest entry path. Applies to
  `clone`, `sync`, `deps`, `graph`, and `info`. The whole-workspace
  scope is no longer rendered as an empty `""` path in no-match
  errors.

## v2.1.0 — 2026-09-01

- `jig sync` now updates the schema before applying it, making it the
  single everyday command; `jig update` remains the schema-only step
  for reviewing incoming changes first. `jig sync --no-update` applies
  the current schema without fetching (offline, or testing local
  schema edits). When the schema cannot be updated (unreachable
  remote, invalid or diverged upstream), sync reports `schema not
  updated:` and applies the current schema instead. `jig update
  --sync` keeps working as an undocumented compatibility alias.

## v2.0.8 — 2026-09-01

- Fixed: a `$dir` source that fails to resolve (unreachable
  repository, subtree missing upstream) no longer blocks the whole
  directory — the remaining sources still materialize and the broken
  source is reported on the status line. While any source is
  unavailable nothing is deleted, and its files stay tracked so they
  update normally once the source resolves again. On `jig init` this
  also unblocks links targeting the directory.

## v2.0.7 — 2026-09-01

- Fixed: when a `$dir` entry merges multiple sources and one of them
  fails to resolve (bad spec, unreachable repository, missing subtree),
  the error now names the failing source instead of only the entry
  path.

## v2.0.6 — 2026-08-24

- Fixed: on Windows, `jig init` with an scp-style URL
  (`git@host:path.git`) failed with a `CreateFile ... syntax is
  incorrect` error instead of cloning. Any source argument that does not
  stat as a local file is now handed to git clone.
- The lifecycle commands (setup, fmt, lint, test) work from cmd and
  PowerShell: when `sh` is not on PATH, jig runs commands through the
  `sh.exe` that Git for Windows ships next to the git executable.
  Schemas keep declaring commands in sh syntax on every platform.
- Symlink creation failures caused by missing Windows privileges now
  explain the remedy (enable Developer Mode or use an administrator
  shell) instead of a bare "required privilege is not held".
- Releases include windows/amd64 and windows/arm64 binaries.

## v2.0.5 — 2026-08-13

- Lifecycle commands print `failed: <repo>` immediately when a
  repository's command fails, mirroring the success lines — failures no
  longer silently vanish from the progress line until the end summary.
  That summary group is now labeled `failed` instead of `skipped`, since
  the commands ran and failed rather than being skipped.

## v2.0.4 — 2026-08-13

- The lifecycle commands (setup, fmt, lint, test) show a transient
  progress line on stderr while repositories run — a done counter plus
  the entries currently in flight — so slow runs (a first pre-commit
  environment build, a cold npm ci) no longer look stuck. The line only
  appears when stderr is a terminal; piped output is byte-identical to
  before.

## v2.0.3 — 2026-08-13

- Fixed: repositories on machines using git `url.<base>.insteadOf`
  rewrites were flagged `remote-changed` even though their configured
  origin matched the schema. Origin comparisons now read the stored
  `remote.origin.url` instead of `git remote get-url`, which reports the
  rewritten form.

## v2.0.2 — 2026-08-13

- Changed: `jig sync` now moves repositories with uncommitted changes. A
  move is a plain rename that carries the working tree along untouched,
  so there was nothing to protect; the guard predated that insight.
  Destructive operations (`jig rm`, `sync --prune`) keep their dirty
  checks.
- Move failures now name the recorded on-disk path as well as the schema
  path ("moving from <old>: ..."), so a skip is traceable to the actual
  checkout location.

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
