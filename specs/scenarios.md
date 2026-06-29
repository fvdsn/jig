# Jig Scenarios

This document describes example workflows for Jig. The scenarios are intentionally concrete so the CLI behavior can be refined before implementation.

Filesystem trees are shown relative to the workspace root. The workspace root is the directory containing `.jig.json`.

## Current Scenario A: Tree With Repositories And Files

### `.jig.json`

```json
{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:acme/platform-auth.git",
        "description": "Authentication service"
      }
    },
    "platform/billing": {
      "$repo": {
        "id": "billing-service",
        "git": "git@github.com:acme/platform-billing.git",
        "description": "Billing service"
      }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:acme/checkout.git",
        "description": "Checkout service",
        "dependsOn": [
          {
            "path": "platform",
            "reason": "checkout uses shared platform services"
          }
        ]
      }
    },
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
        "description": "Starts the local development stack",
        "executable": true
      }
    }
  }
}
```

### Command

```sh
jig clone services/checkout
```

### Expected Behavior

Jig clones `services/checkout` and its non-optional dependencies:

```text
platform/auth
platform/billing
services/checkout
```

`scripts/dev.sh` has no `onlyWhen`, so it is active for the workspace and written during clone or sync.

### Resulting Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
    billing/
      .git/
  services/
    checkout/
      .git/
  scripts/
    dev.sh
```

### `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "billing-service": {
      "path": "platform/billing",
      "git": "git@github.com:acme/platform-billing.git"
    },
    "checkout-service": {
      "path": "services/checkout",
      "git": "git@github.com:acme/checkout.git"
    }
  },
  "files": {
    "dev-script": {
      "path": "scripts/dev.sh",
      "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
      "sha256": "abc123"
    }
  }
}
```

## Current Scenario B: Conditional File With `onlyWhen`

### `.jig.json`

```json
{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:acme/platform-auth.git"
      }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:acme/checkout.git"
      }
    },
    ".agents/skills/platform": {
      "$file": {
        "id": "platform-skill",
        "src": "git:git@github.com:acme/workspace-config.git#agents/skills/platform.md",
        "description": "Agent skill for platform repositories",
        "onlyWhen": {
          "path": "platform",
          "reason": "Only useful when platform repositories are installed"
        }
      }
    }
  }
}
```

### Command

```sh
jig clone services/checkout
```

### Expected Behavior

Jig clones only:

```text
services/checkout
```

The file `.agents/skills/platform` is not written because no repository matching `platform` is active or installed.

### Follow-Up Command

```sh
jig clone platform
```

### Follow-Up Expected Behavior

Jig clones:

```text
platform/auth
```

The file `.agents/skills/platform` becomes active because `platform` is active, so Jig writes it and records it in state.

## Current Scenario C: Initialize And Clone In One Command

### Source Repository Definition

```json
{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:acme/platform-auth.git"
      }
    },
    "services/checkout": {
      "$repo": {
        "id": "checkout-service",
        "git": "git@github.com:acme/checkout.git",
        "dependsOn": [
          {
            "path": "platform/auth",
            "reason": "checkout requires authentication"
          }
        ]
      }
    }
  }
}
```

### Command

```sh
jig init git@github.com:acme/jig-definition.git ~/Code/acme --clone services/checkout
```

### Expected Behavior

Jig initializes the workspace at `~/Code/acme`, writes `.jig.json`, creates `.jig/state.json`, then clones:

```text
services/checkout
platform/auth
```

The clone step uses the same behavior as `jig clone services/checkout`.

## Current Scenario D: Conditional Repository With `onlyWhen`

### `.jig.json`

```json
{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@github.com:acme/platform-auth.git"
      }
    },
    "tools/platform-debug": {
      "$repo": {
        "id": "platform-debug-tools",
        "git": "git@github.com:acme/platform-debug-tools.git",
        "onlyWhen": {
          "path": "platform",
          "reason": "Debug tooling only needed when platform repositories are installed"
        }
      }
    }
  }
}
```

### Command

```sh
jig clone platform
```

### Expected Behavior

Jig first activates `platform/auth` because it matches the requested path.

Then Jig activates `tools/platform-debug` because its `onlyWhen.path` intersects the active repository set.

Jig clones:

```text
platform/auth
tools/platform-debug
```

## Current Scenario E: Tree Key Slash Shorthand

### Equivalent Definitions

Nested form:

```json
{
  "version": 1,
  "tree": {
    "platform": {
      "auth": {
        "$repo": {
          "git": "git@github.com:acme/platform-auth.git"
        }
      }
    }
  }
}
```

Slash shorthand form:

```json
{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "git": "git@github.com:acme/platform-auth.git"
      }
    }
  }
}
```

### Expected Behavior

Both forms declare the same repository:

```text
platform/auth
```

And the same local checkout path:

```text
platform/auth
```

## Current Scenario F: File Update Preserves Local Modifications

### Initial State

Jig previously wrote `scripts/dev.sh` and recorded its hash:

```json
{
  "version": 1,
  "repos": {},
  "files": {
    "dev-script": {
      "path": "scripts/dev.sh",
      "src": "git:git@github.com:acme/workspace-config.git#scripts/dev.sh",
      "sha256": "abc123"
    }
  }
}
```

### Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  scripts/
    dev.sh
```

The user edited `scripts/dev.sh`, so its current hash no longer matches `abc123`.

### Command

```sh
jig sync
```

### Expected Behavior

Jig fetches the latest source content for `scripts/dev.sh`, but does not overwrite the local file because it was modified locally.

### Example Output

```text
skipped:
  scripts/dev.sh: locally modified
```

## Current Scenario G: Invalid Workspace Paths

### `.jig.json`

```json
{
  "version": 1,
  "tree": {
    "../outside": {
      "$file": {
        "src": "git:git@github.com:acme/workspace-config.git#files/outside"
      }
    },
    "~/home-file": {
      "$file": {
        "src": "git:git@github.com:acme/workspace-config.git#files/home-file"
      }
    },
    "/absolute": {
      "$repo": {
        "git": "git@github.com:acme/absolute.git"
      }
    }
  }
}
```

### Command

```sh
jig validate
```

### Expected Behavior

Validation fails because workspace paths must be relative, must not start with `~`, and must not contain `..` segments.
