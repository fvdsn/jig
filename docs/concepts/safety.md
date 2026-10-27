# Safety

Jig runs across many repositories at once, so its commands are built not to lose work.

## Repositories

- `jig sync` never deletes a repository. `jig sync --prune` deletes repositories removed from the schema, but keeps
  and reports those that are dirty, have unpushed commits, or whose origin changed.
- `jig rm` refuses to delete a dirty or unpushed repository unless you pass `-f`.
- `jig pull` only fast-forwards.
- `jig push` never forces. Rejected pushes are reported as skipped.
- `jig switch` never forces. A repository where the switch would overwrite local changes is skipped and left as it
  is.
- An existing directory at a repository's path that is not the expected git repository is skipped, not replaced.

Deleting a checkout by hand does not uninstall it: `jig sync` clones it again. Use `jig rm`.

## Files

- Jig never overwrites a generated file you edited. It keeps it and reports it as modified.
- A file removed from the schema is deleted only if you have not touched it.
- Files you add inside a generated directory are left alone.
- An existing file that jig did not write is never touched.
- In shared settings files (`.mcp.json`, `.claude/settings.json`, ...), jig only changes the keys it wrote.

## The exception: `jig git`

`jig git` runs a git command verbatim in every repository in scope, with none of the checks above.
`jig git reset --hard` does exactly what it says, everywhere. Prefer the jig verbs when there is one.
