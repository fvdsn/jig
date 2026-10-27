# Join an existing workspace

Your team keeps its workspace schema in a git repository. In this guide it is `git@github.com:acme/workspace.git`.

## Create the workspace

```sh
jig init git@github.com:acme/workspace.git ~/Code/acme --clone services/checkout
```

This:

1. clones the schema repository into `~/Code/acme/.jig/source`;
2. clones `services/checkout` and every repository it depends on, at the paths the schema gives them;
3. installs the agent context (skills, `AGENTS.md`, MCP servers, ...) for the coding agents found on your machine.

The output ends with a `harnesses:` line naming the agents jig installed context for. See
[Choosing harnesses](#choosing-harnesses) if it is not what you want.

Some variants:

```sh
jig init <url> ~/Code/acme                          # only the schema, clone nothing yet
jig init <url> ~/Code/acme --clone                  # everything
jig init <url> ~/Code/acme --clone services --tags api   # the api-tagged repos under services
jig init <url> ~/Code/acme-cx12 --clone services/checkout --switch feature/CX-12
```

The last one is a task workspace: every cloned repository is put on `feature/CX-12`, created where it does not exist.
Thanks to the [repository cache](../concepts/repository-cache.md), a second workspace like this is quick to create.

## Look around

```sh
cd ~/Code/acme
jig status        # what is installed, and its state
jig list          # everything the schema offers, installed or not
jig info platform # what a group or repository is
jig deps services/checkout
```

Add more of the workspace whenever you need it:

```sh
jig clone platform
```

## Start your agent

Start your coding agent at the workspace root. It finds the jig skill and knows how to use jig, so you can ask it to
"pull everything", "create a branch for CX-12 in the repos I touched", or "clone the billing services".

## Stay up to date

```sh
jig pull    # pull every repository
jig sync    # follow changes to the schema: renamed repos, new dependencies, updated agent context
```

`jig sync` never clones repositories you did not ask for. When the schema adds new ones, sync tells you, and you
`jig clone` the ones you want.

## Choosing harnesses

By default jig installs agent context for every harness it detects on your machine, by looking for directories like
`~/.claude` or `~/.codex`. To pick them yourself:

```sh
jig init <url> ~/Code/acme --harness claude,codex
jig init <url> ~/Code/acme --harness none
```

The choice is stored in `.jig/config.json`. Edit its `harnesses` list later and run `jig sync`. The supported
harnesses are `claude`, `codex`, `copilot`, `cursor`, `opencode`, `gemini`, `windsurf` and `kiro`. See the
[harness matrix](../reference/harness-matrix.md) for what each one gets.
