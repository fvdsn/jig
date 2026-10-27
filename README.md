# Jig

[![ci](https://github.com/fvdsn/jig/actions/workflows/ci.yml/badge.svg)](https://github.com/fvdsn/jig/actions/workflows/ci.yml)
[![version](https://img.shields.io/github/v/tag/fvdsn/jig?label=version&sort=semver)](https://github.com/fvdsn/jig/tags)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Jig turns a pile of git repositories into one workspace your coding agent can work in.

You describe the workspace once, in a `jig.yaml` file kept in its own git repository: which repositories go where,
how they depend on each other, and which skills, plugins and `AGENTS.md` your agents should get. Anyone on the team
can then build the whole workspace, or just the part they need, with one command.

## Is jig for you?

Jig is probably a good fit if:

- your code is spread over many repositories (tens to thousands), and a task often touches several of them;
- you use coding agents (Claude Code, Codex, ...) and want them to see related repositories side by side, with the
  same skills and instructions for everyone on the team;
- you want a new teammate, or a fresh agent sandbox, to get a working checkout without a wiki page of `git clone`s.

It is probably not for you if you have a single repository or a monorepo, or if you want a build system. Jig places
and manages checkouts; it does not build your code.

Jig is made to be driven by your agent. Every workspace comes with a skill that teaches your agent how to use jig,
so most of the time you ask the agent and it runs the commands.

## Install

```sh
brew install fvdsn/tap/jig            # macOS or Linux
go install github.com/fvdsn/jig@latest
```

## Join an existing workspace

If your team already publishes a schema, for example in `git@github.com:acme/workspace.git`:

```sh
jig init git@github.com:acme/workspace.git ./acme --clone services/checkout
cd acme
```

This clones `services/checkout` and the repositories it depends on, and installs the agent context. Start your agent
in `./acme`.

## Start your own

```sh
mkdir workspace && cd workspace
jig init
```

This creates a starter schema in `.jig/source/jig.yaml`. Describe your repositories:

```yaml
version: 4

agents:
  context:
    - git@github.com:acme/agent-config.git#AGENTS.md
  skills:
    - git@github.com:acme/agent-config.git#skills
  mcp:
    github:
      command: npx
      args: [-y, "@modelcontextprotocol/server-github"]

tree:
  platform/auth:
    $repo:
      git: git@github.com:acme/platform-auth.git
  services/checkout:
    $repo:
      git: git@github.com:acme/checkout.git
      dependsOn:
        - path: platform/auth
```

Then run `jig sync` to build the workspace. To share it, push `.jig/source` to a git remote; teammates run
`jig init <url>`.

## Everyday commands

```sh
jig status          # what is checked out, dirty, ahead or behind
jig pull            # pull every repository
jig switch -c fix   # create a branch in every repository
jig git log -1      # run any git command in every repository
jig sync            # follow the latest version of the schema
jig list            # browse everything the schema offers
jig clone platform  # check out more of it
```

Commands that act on checkouts are scoped to the current directory: run `jig pull` inside `services/` and only
`services/` is pulled.

`jig --help` lists every command, and `jig skill` prints the full reference your agent reads.

## Agent context

The `agents` block of the schema declares everything your agents should get, once, in a harness-neutral way:

- `AGENTS.md` context, assembled from shared and per-project sections
- skills
- path-scoped rules (instructions)
- subagents
- slash commands
- plugins and marketplaces in the Claude Code layout
- MCP servers
- hooks

Jig writes each of these where and how each harness expects it: Claude Code, Codex, GitHub Copilot, Cursor,
OpenCode, Gemini CLI, Windsurf and Kiro. Context is only installed for the harnesses detected on your machine, or the
ones you pick with `--harness`, so a Claude-only machine gets `CLAUDE.md` and `.claude/` and nothing else. Settings
files you also edit by hand, like `.mcp.json` or `.claude/settings.json`, are merged with your changes rather than
overwritten.

If you know [APM](https://microsoft.github.io/apm/), jig covers the same kinds of context. What jig adds is the tie
to the workspace: context can follow what is checked out, so the billing MCP server and skills appear only when a
billing repository is in your workspace, and a repository can ship its own skills or `AGENTS.md` section for the
workspace to pick up.

## Fast at any size

Jig is built for workspaces of hundreds or thousands of repositories. Commands run across repositories in parallel,
so `jig pull`, `jig status` or `jig test` take about as long as the slowest repository, not the sum of all of them.

Every workspace on your machine shares one repository cache. Cloning a repository you already have in another
workspace only fetches what is new, and the clone hardlinks the cached git objects instead of copying them. A second
workspace is about as quick and as cheap on disk as a git worktree, but each checkout is a normal, independent
repository. The cache is safe to delete at any time (`jig cache clean`).

## More to explore

- **Task workspaces in one command.**
  `jig init <url> ~/Code/acme-cx12 --clone services/checkout --switch feature/CX-12` checks out a slice of the
  workspace with every repository on the task branch, in seconds thanks to the cache.
  [More](docs/getting-started/join-a-workspace.md)
- **Context that follows the checkout.** Put an `agents` block on a group, and its skills, MCP servers and
  `AGENTS.md` section appear only while a repository of that group is installed. Repositories can also export
  their own skills and context to the workspace. [More](docs/concepts/agent-context.md#scoping-context)
- **Select by capability.** Tag repositories (`api`, `go`, `frontend`), inherit tags from groups, then
  `jig clone services --tags api`, or gate a skill on `tags: [api]` so it follows every api repository, present and
  future. [More](docs/concepts/dependencies-and-activation.md#activation)
- **One verb for every stack.** Declare each repository's `setup`, `fmt`, `lint` and `test` commands, and
  `jig test` runs them all in parallel, whatever the language. [More](docs/getting-started/create-a-workspace.md#add-lifecycle-commands)
- **A dependency graph you can query.** `jig deps --reverse platform/auth` shows who depends on a repository, and
  `jig graph` draws the workspace as a Mermaid diagram. [More](docs/concepts/dependencies-and-activation.md)
- **Files assembled from sections.** Build one `AGENTS.md`, or any shared file, from several sources, each included
  only when it applies, plus optional local sources each developer can add to.
  [More](docs/concepts/the-tree.md#files-and-directories)
- **Safe across hundreds of repositories.** Jig never force-pushes, never drops local changes, and only touches the
  keys it wrote in settings files you share with it. [More](docs/concepts/safety.md)

## Documentation

The [documentation](docs/README.md) has:

- **Getting started**: install, join an existing workspace, or build your own schema step by step.
- **Concepts**: how the schema and workspace fit together, the tree, dependencies and activation, scoping, agent
  context, safety, and the repository cache.
- **Reference**: the [harness matrix](docs/reference/harness-matrix.md), showing exactly where each kind of context
  lands for each of the eight supported harnesses.
