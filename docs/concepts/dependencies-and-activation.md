# Dependencies and activation

Two fields decide what ends up in a workspace beyond what you ask for: `dependsOn` pulls repositories in, and
`onlyWhen` makes entries appear only when something else is there.

## References

Both use the same reference form, an object with exactly one of:

| Reference | Selects |
| --- | --- |
| `id: auth-service` | one entry by its stable id |
| `path: platform/auth` | one entry, exact path |
| `path: platform/*` | every repository below `platform` |
| `tags: [api, go]` | every repository carrying all those tags |

A reference that matches nothing is a validation error, so a rename that breaks a dependency fails loudly.

## Dependencies

```yaml
services/checkout:
  $repo:
    git: git@github.com:acme/checkout.git
    dependsOn:
      - path: platform/*
        reason: checkout calls the platform services
      - path: observability/tracing
        optional: true
        reason: useful when debugging traces locally
```

Cloning a repository clones its dependencies, recursively. Optional dependencies are skipped unless you pass
`--with-optional-deps`; once installed, sync keeps them up to date. `--no-deps` clones only what you selected.

A group's `dependsOn` applies to every repository under it.

To explore the graph:

```sh
jig deps services/checkout             # what checkout needs, recursively
jig deps platform/auth --reverse       # who depends on auth
jig graph services > deps.mmd          # a Mermaid flowchart
```

## Activation

An entry is **active** when it should be in the workspace. Repositories are active when you clone them. Files,
directories and agent context are active when the repositories they serve are installed:

- a file with no `onlyWhen` follows the repositories around it: a file under `platform/` is active when a
  repository under `platform/` is installed, and a file at the root is active when any repository is;
- `onlyWhen` sets the condition explicitly.

```yaml
scripts/seed-billing.sh:
  $file:
    src: git@github.com:acme/workspace-config.git#scripts/seed-billing.sh
    executable: true
    onlyWhen:
      path: billing/*
      reason: only useful with billing repos installed
```

Tag conditions make context follow capabilities rather than locations. A skill gated on `tags: [api]` appears
whenever any api-tagged repository is installed, with no schema edit as new api repositories are added.

Conditions add up: an entry's own `onlyWhen` and those inherited from its groups must all match. The same conditions
work on each source of a file, on skills and MCP servers in the `agents` block, and on whole group `agents` blocks.
See [Agent context](agent-context.md).
