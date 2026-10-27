# Scope

Jig commands act on part of the workspace, chosen in one of two ways depending on the command.

## Checkout commands: the current directory is the scope

`sync`, `setup`, `fmt`, `lint`, `test`, `pull`, `fetch`, `push`, `switch`, `status`, `diff` and `git` act on what is
installed under the current directory. They take no path argument.

```sh
cd ~/Code/acme && jig pull               # every repository
cd ~/Code/acme/services && jig pull      # only repositories under services/
cd ~/Code/acme/services/checkout && jig pull   # only checkout
```

To narrow a checkout command, `cd`.

## Schema commands: paths and filters

`list`, `tags`, `info`, `deps`, `graph` and `clone` work on the schema, including entries that are not on disk yet.
They take a path and filters:

```sh
jig list services
jig clone services --tags api
jig info --id auth-service
jig list --meta team=core
```

Without a path, they also scope to the current directory.

Paths resolve like filesystem paths: `.`, `..` and `../other` work, and a leading `/` starts at the workspace root
(`jig info /platform/auth`). Output always shows full workspace paths, so it is the same wherever you run it.
