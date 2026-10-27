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
  skills:
    - git@github.com:acme/agent-skills.git

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
