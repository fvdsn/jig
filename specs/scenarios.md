# Jig Scenarios

This document describes example workflows for Jig. The scenarios are intentionally concrete so the CLI behavior can be refined before implementation.

## Notation

Filesystem trees are shown relative to the workspace root.

The workspace root is the directory containing `.jig.json`.

Example:

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
```

## Scenario 1: Clone One Repository With Required Dependencies

### Initial Filesystem

```text
workspace/
  .jig.json
```

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git",
      "web": "https://github.com/acme/platform-auth",
      "description": "Authentication service"
    },
    "platform.billing": {
      "id": "billing-service",
      "git": "git@github.com:acme/platform-billing.git",
      "web": "https://github.com/acme/platform-billing",
      "description": "Billing service"
    },
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git",
      "web": "https://github.com/acme/checkout",
      "description": "Checkout service",
      "dependsOn": [
        {
          "path": "platform",
          "reason": "checkout uses shared platform services"
        }
      ]
    }
  }
}
```

### Command

```sh
jig clone services.checkout
```

### Expected Behavior

Jig clones `services.checkout` and its non-optional dependencies.

The dependency path `platform` expands to:

```text
platform.auth
platform.billing
```

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
  }
}
```

## Scenario 2: Optional Dependencies Are Not Cloned By Default

### Initial Filesystem

```text
workspace/
  .jig.json
```

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "observability.tracing": {
      "id": "tracing-service",
      "git": "git@github.com:acme/tracing.git"
    },
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git",
      "dependsOn": [
        {
          "path": "platform.auth",
          "reason": "checkout requires authentication"
        },
        {
          "path": "observability",
          "optional": true,
          "reason": "useful when debugging traces locally"
        }
      ]
    }
  }
}
```

### Command

```sh
jig clone services.checkout
```

### Expected Behavior

Jig clones:

```text
services.checkout
platform.auth
```

Jig does not clone:

```text
observability.tracing
```

### Resulting Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
  services/
    checkout/
      .git/
```

### Follow-Up Command

```sh
jig clone services.checkout --with-optional-deps
```

### Follow-Up Expected Behavior

Jig sees that `services.checkout` and `platform.auth` are already installed, then clones `observability.tracing`.

### Follow-Up Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  observability/
    tracing/
      .git/
  platform/
    auth/
      .git/
  services/
    checkout/
      .git/
```

## Scenario 3: Commands Work From Subdirectories

### Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
  services/
    checkout/
      .git/
      src/
        api/
```

### Current Working Directory

```text
workspace/services/checkout/src/api
```

### Command

```sh
jig info services.checkout
```

### Expected Behavior

Jig walks up from the current working directory until it finds `.jig.json`.

The workspace root is resolved as:

```text
workspace
```

The command behaves the same as if it had been run from the workspace root.

## Scenario 4: Pull All Locally Installed Repositories

### Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
  platform/
    billing/
      .git/
  services/
    checkout/
      .git/
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
  }
}
```

### Command

```sh
jig pull
```

### Expected Behavior

Jig runs standard `git pull` behavior in each locally installed repository.

Repositories defined in `.jig.json` but not present locally are ignored.

### Example Output

```text
pulled:
  platform.auth
  services.checkout

skipped:
  platform.billing: merge conflict
```

If `platform.billing` hits a merge conflict, Jig skips it and continues with the remaining repositories.

## Scenario 5: Pull A Group

### Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  observability/
    tracing/
      .git/
  platform/
    auth/
      .git/
  platform/
    billing/
      .git/
  services/
    checkout/
      .git/
```

### Command

```sh
jig pull platform
```

### Expected Behavior

The path `platform` matches:

```text
platform.auth
platform.billing
```

It does not match:

```text
observability.tracing
services.checkout
```

Jig runs `git pull` only in the locally installed matching repositories.

## Scenario 6: Update The Shared Definition

### Initial `.jig.json`

```json
{
  "version": 1,
  "source": {
    "type": "git",
    "url": "git@github.com:acme/jig-definition.git",
    "ref": "main",
    "path": ".jig.json"
  },
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git",
      "dependsOn": [
        {
          "path": "platform.auth",
          "reason": "checkout requires authentication"
        }
      ]
    }
  }
}
```

### Incoming Source Definition

```json
{
  "version": 1,
  "source": {
    "type": "git",
    "url": "git@github.com:acme/jig-definition.git",
    "ref": "main",
    "path": ".jig.json"
  },
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "platform.billing": {
      "id": "billing-service",
      "git": "git@github.com:acme/platform-billing.git"
    },
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git",
      "dependsOn": [
        {
          "path": "platform",
          "reason": "checkout uses shared platform services"
        }
      ]
    }
  }
}
```

### Command

```sh
jig update
```

### Expected Behavior

Jig fetches the incoming definition, validates it, compares it to the current `.jig.json`, and replaces `.jig.json` if valid.

Jig reports:

```text
added:
  platform.billing

changed:
  services.checkout dependencies changed
```

`jig update` does not clone `platform.billing`.

`jig update` does not modify `.jig/state.json`.

## Scenario 7: Sync After Definition Update

This scenario continues from Scenario 6.

### Filesystem Before Sync

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
  services/
    checkout/
      .git/
```

### `.jig/state.json` Before Sync

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "checkout-service": {
      "path": "services/checkout",
      "git": "git@github.com:acme/checkout.git"
    }
  }
}
```

### Command

```sh
jig sync services.checkout
```

### Expected Behavior

The current `.jig.json` says `services.checkout` depends on `platform`.

The sync set is:

```text
services.checkout
platform.auth
platform.billing
```

Jig sees that `platform.billing` is missing and clones it.

### Filesystem After Sync

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
  platform/
    billing/
      .git/
  services/
    checkout/
      .git/
```

### `.jig/state.json` After Sync

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
  }
}
```

## Scenario 8: Sync A Moved Repository

### Initial `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Initial Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
```

### Initial `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Updated `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Command

```sh
jig sync identity.auth-service
```

### Expected Behavior

Jig compares the current expected path to the state path for identity `auth-service`.

Current expected path:

```text
identity/auth-service
```

State path:

```text
platform/auth
```

Jig moves the repository if safe:

```text
platform/auth -> identity/auth-service
```

Jig updates `.jig/state.json` after the move.

### Resulting Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  identity/
    auth-service/
      .git/
```

### Resulting `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "identity/auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

## Scenario 9: Sync A Moved Repository With A Hosting Change

### Initial State

The repository was previously cloned from GitHub:

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Current `.jig.json`

The repository now lives under a different logical path and Git hosting provider:

```json
{
  "version": 1,
  "repos": {
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@gitlab.com:acme/auth-service.git",
      "web": "https://gitlab.com/acme/auth-service"
    }
  }
}
```

### Filesystem Before Sync

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
```

### Command

```sh
jig sync identity.auth-service
```

### Expected Behavior

Jig uses the stable identity `auth-service` to find the old checkout in `.jig/state.json`.

Jig moves the local checkout:

```text
platform/auth -> identity/auth-service
```

Jig updates the local Git `origin` remote URL:

```text
git@github.com:acme/platform-auth.git -> git@gitlab.com:acme/auth-service.git
```

Jig updates `.jig/state.json`.

### Filesystem After Sync

```text
workspace/
  .jig.json
  .jig/
    state.json
  identity/
    auth-service/
      .git/
```

### `.jig/state.json` After Sync

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "identity/auth-service",
      "git": "git@gitlab.com:acme/auth-service.git"
    }
  }
}
```

## Scenario 10: Sync Skips Unsafe Move

### Current `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Filesystem Before Sync

```text
workspace/
  .jig.json
  .jig/
    state.json
  identity/
    auth-service/
      .git/
  platform/
    auth/
      .git/
```

### Command

```sh
jig sync identity.auth-service
```

### Expected Behavior

Jig wants to move:

```text
platform/auth -> identity/auth-service
```

But the target path already exists.

Jig skips the move and reports the conflict.

Jig does not update `.jig/state.json` for this repository.

### Example Output

```text
skipped:
  identity.auth-service: target path already exists: identity/auth-service
```

## Scenario 11: Sync Reports Stale Local Repository

### Current `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git"
    }
  }
}
```

### `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "checkout-service": {
      "path": "services/checkout",
      "git": "git@github.com:acme/checkout.git"
    },
    "old-reporting-service": {
      "path": "legacy/reporting",
      "git": "git@github.com:acme/reporting.git"
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
  legacy/
    reporting/
      .git/
  services/
    checkout/
      .git/
```

### Command

```sh
jig sync
```

### Expected Behavior

Jig sees `old-reporting-service` in state, but no repository in `.jig.json` has that identity.

Jig reports it as stale.

Jig does not delete it.

### Example Output

```text
stale:
  old-reporting-service at legacy/reporting is no longer defined
```

## Scenario 12: Validate Duplicate Repository Identity

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@gitlab.com:acme/auth-service.git"
    }
  }
}
```

### Command

```sh
jig validate
```

### Expected Behavior

Validation fails because two repository definitions resolve to the same identity:

```text
auth-service
```

### Example Output

```text
invalid .jig.json:
  duplicate repository identity auth-service:
    platform.auth
    identity.auth-service
```

## Scenario 13: Initialize Workspace In Current Directory

### Initial Filesystem

```text
workspace/
```

### Source Repository

Remote URL:

```text
git@github.com:acme/jig-definition.git
```

Remote default branch:

```text
main
```

Remote `.jig.json`:

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Current Working Directory

```text
workspace
```

### Command

```sh
jig init git@github.com:acme/jig-definition.git
```

### Expected Behavior

Jig discovers the remote default branch with:

```sh
git ls-remote --symref git@github.com:acme/jig-definition.git HEAD
```

Jig fetches `.jig.json` from `main`, validates it, writes it into the current directory, and creates empty local state.

Jig injects the source object into the written `.jig.json`.

Jig does not clone any repositories.

### Resulting Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
```

### Resulting `.jig.json`

```json
{
  "version": 1,
  "source": {
    "type": "git",
    "url": "git@github.com:acme/jig-definition.git",
    "ref": "main",
    "path": ".jig.json"
  },
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Resulting `.jig/state.json`

```json
{
  "version": 1,
  "repos": {}
}
```

## Scenario 14: Initialize Workspace In Explicit Directory

### Initial Filesystem

```text
home/
  fred/
    Code/
```

### Command

```sh
jig init git@github.com:acme/jig-definition.git /home/fred/Code/acme
```

### Expected Behavior

Jig creates the workspace directory if needed:

```text
/home/fred/Code/acme
```

Jig writes `.jig.json` and `.jig/state.json` inside that directory.

Jig does not clone repositories.

### Resulting Filesystem

```text
home/
  fred/
    Code/
      acme/
        .jig.json
        .jig/
          state.json
```

## Scenario 15: Initialize From Custom Definition Path

### Source Repository

Remote URL:

```text
git@github.com:acme/workspace-config.git
```

Remote default branch:

```text
master
```

Definition file path:

```text
definitions/backend.jig.json
```

### Command

```sh
jig init git@github.com:acme/workspace-config.git --path definitions/backend.jig.json
```

### Expected Behavior

Jig discovers `master` as the remote default branch.

Jig fetches `definitions/backend.jig.json` from `master`.

Jig writes the fetched definition to local `.jig.json` and records the source path.

### Resulting `.jig.json` Source Object

```json
{
  "source": {
    "type": "git",
    "url": "git@github.com:acme/workspace-config.git",
    "ref": "master",
    "path": "definitions/backend.jig.json"
  }
}
```

## Scenario 16: Init Fails If Workspace Already Exists

### Initial Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
```

### Command

```sh
jig init git@github.com:acme/jig-definition.git
```

### Expected Behavior

Jig fails because `.jig.json` already exists.

Jig does not overwrite `.jig.json`.

Jig does not modify `.jig/state.json`.

### Example Output

```text
workspace already initialized: .jig.json exists
```

## Scenario 17: Init Fails If Default Branch Cannot Be Determined

### Command

```sh
jig init git@github.com:acme/jig-definition.git
```

### Expected Behavior

If Jig cannot determine the remote default branch using `git ls-remote --symref`, it fails rather than guessing `main` or `master`.

Jig does not write `.jig.json`.

Jig does not create `.jig/state.json`.

### Example Output

```text
could not determine default branch for git@github.com:acme/jig-definition.git
```

## Scenario 18: Sync Adopts Existing Matching Repository

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Initial `.jig/state.json`

```json
{
  "version": 1,
  "repos": {}
}
```

### Initial Filesystem

`platform/auth` was cloned manually before Jig knew about it.

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
```

The local `origin` remote for `platform/auth` is:

```text
git@github.com:acme/platform-auth.git
```

### Command

```sh
jig sync platform.auth
```

### Expected Behavior

Jig sees that the expected path exists, contains a Git repository, and has a matching `origin` remote.

Jig adopts the repository by recording it in `.jig/state.json`.

Jig does not clone over it.

### Resulting `.jig/state.json`

```json
{
  "version": 1,
  "repos": {
    "auth-service": {
      "path": "platform/auth",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

## Scenario 19: Clone Skips Existing Path With Different Repository

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "platform.auth": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    }
  }
}
```

### Initial Filesystem

```text
workspace/
  .jig.json
  .jig/
    state.json
  platform/
    auth/
      .git/
```

The local `origin` remote for `platform/auth` is:

```text
git@github.com:other/auth.git
```

### Command

```sh
jig clone platform.auth
```

### Expected Behavior

Jig sees that the expected path already exists and contains a Git repository with a different origin.

Jig skips the repository and reports the mismatch.

Jig does not update `.jig/state.json` for this repository.

### Example Output

```text
skipped:
  platform.auth: existing Git repository has different origin at platform/auth
```

## Scenario 20: Status Shows Installed, Missing, Moved, Dirty, And Stale

### `.jig.json`

```json
{
  "version": 1,
  "repos": {
    "identity.auth-service": {
      "id": "auth-service",
      "git": "git@github.com:acme/platform-auth.git"
    },
    "platform.billing": {
      "id": "billing-service",
      "git": "git@github.com:acme/platform-billing.git"
    },
    "services.checkout": {
      "id": "checkout-service",
      "git": "git@github.com:acme/checkout.git"
    }
  }
}
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
    "checkout-service": {
      "path": "services/checkout",
      "git": "git@github.com:acme/checkout.git"
    },
    "old-reporting-service": {
      "path": "legacy/reporting",
      "git": "git@github.com:acme/reporting.git"
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
  legacy/
    reporting/
      .git/
  platform/
    auth/
      .git/
  services/
    checkout/
      .git/
```

Assume `services/checkout` has uncommitted changes.

### Command

```sh
jig status
```

### Expected Behavior

Jig reports:

- `identity.auth-service` is installed but at the old path `platform/auth`.
- `platform.billing` is missing.
- `services.checkout` is installed and dirty.
- `old-reporting-service` is stale because it is tracked in state but no longer defined.

### Example Output

```text
moved:
  identity.auth-service: platform/auth -> identity/auth-service

missing:
  platform.billing

dirty:
  services.checkout

stale:
  old-reporting-service at legacy/reporting is no longer defined
```

## Scenario 21: Status For A Group

### Command

```sh
jig status platform
```

### Expected Behavior

Jig reports status only for repositories whose path matches `platform` using segment-boundary path matching.

Example matches:

```text
platform.auth
platform.billing
```

Example non-matches:

```text
platforming.api
services.checkout
```
