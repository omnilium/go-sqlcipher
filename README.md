# go-sqlcipher

`github.com/omnilium/go-sqlcipher` — Omnilium's fork of [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3), a CGo `database/sql` driver for SQLite. It exists to provide a **SQLCipher** at-rest-encrypted SQLite driver.

For upstream documentation, FAQ, and the full feature/build-tag matrix, see the [upstream README](https://github.com/mattn/go-sqlite3); this file covers only what is specific to the fork.

> **Drop-in compatibility — with a caveat.** This fork aims to be a drop-in replacement for `mattn/go-sqlite3`: the public API (exported types, functions, constants, and SQLite C-API mirror names) is preserved. However, as part of the Go version bump and the associated linting/refactoring, there may be slight differences in the contract — for example, error message wording (lower-cased to satisfy `staticcheck`) and the avoidance of input-slice mutation in the salted crypt encoders. Behaviour-affecting differences are intentional and considered improvements; pin a specific fork tag and test before relying on exact byte-for-byte parity with upstream.

## Relationship to upstream

The fork carries Omnilium's changes as a small patch stack on top of an upstream **release tag** — never on top of upstream's unreleased `master` tip. The current base is **`v1.14.45`**.

The foundational patches, always present and rebased forward on every resync, both live in `go.mod`: the **module path** declares `github.com/omnilium/go-sqlcipher` (not `github.com/mattn/go-sqlite3`) so the package is importable under its own path without a `replace` directive, and the **Go version floor** is held at the Omnilium baseline (currently `go 1.26`) rather than upstream's lower minimum.

## Maintenance model

`master` = latest upstream release tag + Omnilium's patch stack. To resync to a newer upstream release:

1. `git fetch --tags upstream` (`upstream` = `https://github.com/mattn/go-sqlite3.git`).
2. Rebase the Omnilium patch stack onto the new release tag (e.g. `git rebase --onto vX.Y.Z <old-base> master`).
3. Build and verify (`go build ./...`, `go vet .`, `go test .`).
4. Force-push `master`.
5. Cut the next fork tag (see Versioning).

We deliberately track release tags, not upstream `master`, so the base is always a shipped SQLite version rather than unreleased churn.

## Versioning

The fork uses its **own independent semver line starting at `v0.1.0`** — it is *not* aligned to upstream's `v1.14.x` numbers (a bare `v1.14.45` here would falsely imply byte-equivalence with mattn's release). Each fork release notes the upstream base it sits on. Consumers pin a **fork tag** in their `go.mod` (never a branch pseudo-version, which a resync rebase would invalidate).

## Building and testing

Standard CGo build; a C toolchain is required. These are the gates CI enforces:

```sh
go build ./...
golangci-lint run ./...   # golangci-lint v2 (config in .golangci.yaml)
go test -race ./...
govulncheck ./...
```

Build tags follow upstream (e.g. `sqlite_fts5`, `sqlite_userauth`, `libsqlite3`); see the `sqlite3_opt_*.go` files.

> **Planned testing improvements.** CI currently runs the default build only. We plan to add (1) a CI matrix that builds with the optional feature tags so the tag-gated test files are actually exercised, (2) more statement coverage (currently ~61%), and (3) native Go fuzz targets for SQL and database-file parsing.

## License

MIT, inherited from upstream `mattn/go-sqlite3`. See [`LICENSE`](./LICENSE).
