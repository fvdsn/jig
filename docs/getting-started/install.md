# Install

Jig is a single binary. It needs `git` on your `PATH`.

## Homebrew (macOS or Linux)

```sh
brew install fvdsn/tap/jig
```

## Go

```sh
go install github.com/fvdsn/jig@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

## Check the install

```sh
jig version
```

## Upgrading

Upgrade with `brew upgrade jig` or by running `go install` again. Every workspace carries a copy of the jig skill, the
reference your agent reads; the next `jig sync` in a workspace refreshes it to the version of the binary.

## Next

- [Join an existing workspace](join-a-workspace.md)
- [Create your own workspace](create-a-workspace.md)
