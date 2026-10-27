# Jig documentation

New to jig? Start with the [README](../README.md), then one of the quickstarts.

## Getting started

- [Install](getting-started/install.md)
- [Join an existing workspace](getting-started/join-a-workspace.md)
- [Create your own workspace](getting-started/create-a-workspace.md)

## Concepts

- [Schema and workspace](concepts/schema-and-workspace.md): where the schema lives, and `update` vs `sync`
- [The tree](concepts/the-tree.md): repositories, groups, files and directories
- [Dependencies and activation](concepts/dependencies-and-activation.md): what gets cloned, and when files appear
- [Scope](concepts/scope.md): the current directory picks what a command acts on
- [Agent context](concepts/agent-context.md): one declaration, written out for every harness
- [Safety](concepts/safety.md): what jig will and will not touch
- [The repository cache](concepts/repository-cache.md)

## Reference

- [Harness matrix](reference/harness-matrix.md): where each kind of agent context lands, per harness

For the full schema and command reference, run `jig skill` (the reference your agent reads) and `jig --help`.
