# Schema and workspace

A jig setup has two parts:

- the **schema**, a `jig.yaml` (or `jig.json`) file in its own git repository, shared by the team. It describes the
  workspace: which repositories go where, how they depend on each other, which files to generate, and which agent
  context to install.
- a **workspace**, a directory on your machine built from the schema. You can have several workspaces from the same
  schema, each with a different part of it checked out.

## Inside a workspace

Everything jig keeps for itself is under `.jig/`:

```text
.jig/source/       a git checkout of the schema repository
.jig/config.json   which file in the checkout is the schema, and which harnesses to install context for
.jig/state.json    what jig installed on this machine, used to handle moves and local edits safely
```

Jig reads the schema live from `.jig/source`. Editing it there and running `jig sync --no-update` is how you try a
change before pushing it. Jig never rewrites the schema file.

Everything else is your workspace: repository checkouts at the paths the schema gives them, generated files, and
agent context.

## Update, sync, pull

Three commands bring a workspace up to date, and each does one thing:

| Command | What it updates |
| --- | --- |
| `jig update` | the schema checkout, fast-forwarded from its remote. Nothing else changes. |
| `jig sync` | the workspace shape: runs `update`, then applies the schema to what is installed |
| `jig pull` | the contents of the installed repositories (`git pull --ff-only`) |

`jig sync` is the everyday command. It moves renamed repositories, clones new dependencies of what you have,
updates repository origins, regenerates files, and refreshes agent context. It never clones a repository you did not
ask for, and never deletes one unless you pass `--prune`. If the schema cannot be fetched (offline, broken upstream),
sync says so and applies the schema it has.

Use `jig update` alone when you want to review what changed in the schema before applying it.

## Schema format

JSON and YAML have the same structure, chosen by file extension. From a git repository, jig looks for `.jig.json`,
`jig.json`, `schema.json`, `jig.yaml` or `jig.yml` at the root, or the path given with `jig init --path`.

YAML is read strictly: one document, no anchors or aliases, no duplicate keys. A value YAML would read as a number
or boolean where a string is expected is reported with a hint to quote it.
