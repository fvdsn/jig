# Jig

[![ci](https://github.com/fvdsn/jig/actions/workflows/ci.yml/badge.svg)](https://github.com/fvdsn/jig/actions/workflows/ci.yml)
[![version](https://img.shields.io/github/v/tag/fvdsn/jig?label=version&sort=semver)](https://github.com/fvdsn/jig/tags)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Jig is a many-git-repository agentic workspace manager. It lets you organize up to thousands of git repositories in a reproducible and self-contained directory structure as well as manage their related agentic context such as skills, mcps, AGENTS.md, etc for the target agent harnesses. A single command can then efficiently instanciate a workspace, or a subset of it. Jig also then helps with operating the workspace, such as branching, diffing, linting, testing, and keeping its structure up to date with remote changes.

Jig is *agentic first*, it ships with a self-describing skill and is made to be operated by your agent not by hand. 

## Installation

With Homebrew (macOS or Linux):

```sh
brew install fvdsn/tap/jig
```

With Go:

```sh
go install github.com/fvdsn/jig@latest
```

## Getting started

The starting point of jig is the jig schema, a yaml file that describes how the git repositories are composed into the desired workspace directory structure, as well as how to source the agentic context.

````
# jig.yaml

version: 4

agents:
  skills:
    - git@github.com:acme/workspace-config.git#skills
  plugins:
    - git@github.com:obra/superpowers.git

tree:
  platform/:
    auth:
      $repo:
        git: git@github.com:acme/platform-auth.git
        test: go test ./...
    billing:
      $repo:
        git: git@github.com:acme/platform-billing.git

  services/checkout:
    $repo:
      git: git@github.com:acme/checkout.git
      tags: [api]
      dependsOn:
        - path: platform/*
          reason: checkout calls the platform services

  scripts/dev.sh:
    $file:
      src: git@github.com:acme/workspace-config.git#scripts/dev.sh
      executable: true
````

Once your jig schema is published in a repository, for example at `git@github.com:acme/workspace-config.git`, you
can instanciate a workspace with a single command.

```
jig init git@github.com:acme/workspace-config.git --clone services/ ./workspace
```

This will create a directory `./workspace` that will contain all the repositories inside the `services/` subtree, with
their dependencies.

You can then launch your agent at the workspace root, where all the requested context will have been installed.

Jig will automatically install context about jig itself so that your agent can use it by itself. Jig is agentic-first,
you will not usually call its commands yourself.

## Main jig commands

 - `jig pull` will pull all your workspace git repositories
 - `jig sync` will update the context and workspace structure according to the latest schema version
 - `jig git ...` will execute the git command accross every repository in the workspace
 - `jig list` will list every repository available in the schema
 - `jig clone` will clone a part of the schema into the workspace
 - `jig status` will show you the status of all the repos in the workspace

These commands can be run anywhere in the workspace and will be scoped to your current directory

