# Create your own workspace

This guide builds a workspace schema from scratch, tries it locally, and publishes it for your team.

## Start an empty workspace

```sh
mkdir ~/Code/acme && cd ~/Code/acme
jig init
```

This creates `.jig/source/`, a new local git repository holding a starter `jig.yaml`, and installs the jig skill for
your agents. The schema is read live from `.jig/source/jig.yaml`: edit it, and the next `jig sync` applies it.

## Describe your repositories

Repositories go under `tree`. The key is the path in the workspace, and the path is also the directory on disk:

```yaml
version: 4

tree:
  platform/auth:
    $repo:
      git: git@github.com:acme/platform-auth.git
  platform/billing:
    $repo:
      git: git@github.com:acme/platform-billing.git
  services/checkout:
    $repo:
      git: git@github.com:acme/checkout.git
      dependsOn:
        - path: platform/*
          reason: checkout calls the platform services
```

`platform/*` means every repository under `platform/`. Cloning `services/checkout` now clones both platform
repositories with it. See [Dependencies and activation](../concepts/dependencies-and-activation.md).

Check the schema and clone it:

```sh
jig validate
jig clone
```

## Add agent context

The `agents` block lists what your coding agents should get. Sources are git URLs, with `#path` pointing inside the
repository:

```yaml
agents:
  context:
    - git@github.com:acme/agent-config.git#AGENTS.md
  skills:
    - git@github.com:acme/agent-config.git#skills
  mcp:
    github:
      command: npx
      args: [-y, "@modelcontextprotocol/server-github"]
      env:
        GITHUB_TOKEN: ${GITHUB_TOKEN}
```

Run `jig sync --no-update` to apply it without fetching the schema from a remote (there is none yet). Jig writes the
context for each harness on your machine: `CLAUDE.md` and `.claude/skills` for Claude Code, `AGENTS.md` and
`.agents/skills` for Codex, and so on. See [Agent context](../concepts/agent-context.md).

## Add lifecycle commands

Give repositories their `setup`, `fmt`, `lint` and `test` commands, so anyone (or any agent) can run them across the
workspace without knowing each repository's tooling:

```yaml
  platform/auth:
    $repo:
      git: git@github.com:acme/platform-auth.git
      setup: go mod download
      test: go test ./...
```

`jig test` then runs every installed repository's tests in parallel. Commands on a group are inherited by the
repositories under it.

## Publish it

`.jig/source` is a normal git repository. Commit and push it to a remote:

```sh
git -C .jig/source add jig.yaml
git -C .jig/source commit -m "Initial workspace schema"
git -C .jig/source remote add origin git@github.com:acme/workspace.git
git -C .jig/source push -u origin HEAD
```

Teammates can now [join the workspace](join-a-workspace.md) with `jig init git@github.com:acme/workspace.git`.

## Change it later

Edit `.jig/source/jig.yaml`, check it with `jig validate`, try it with `jig sync --no-update`, then commit and push.
Teammates pick the change up on their next `jig sync`.

Give repositories you may move later an `id`. A repository with an `id` can change path in the schema, and sync moves
the existing checkout instead of cloning it again. Without an `id`, the path is the identity, so a moved repository
is cloned again at its new path and the old checkout is reported as stale.

To check the schema in its own repository's CI, without a workspace:

```sh
jig validate jig.yaml
```
