# go-sqlcipher

`github.com/omnilium/go-sqlcipher` — Omnilium's fork of [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3), a CGo `database/sql` driver for SQLite. It exists to provide a **SQLCipher** at-rest-encrypted SQLite driver.

For upstream documentation, FAQ, and the full feature/build-tag matrix, see the [upstream README](https://github.com/mattn/go-sqlite3); this file covers only what is specific to the fork.

## Relationship to upstream

The fork carries Omnilium's changes as a small patch stack on top of an upstream **release tag** — never on top of upstream's unreleased `master` tip. The current base is **`v1.14.45`**.

The one foundational patch, always present and rebased forward on every resync, is the `go.mod` module path: it declares `github.com/omnilium/go-sqlcipher` (not `github.com/mattn/go-sqlite3`) so the package is importable under its own path without a `replace` directive.

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

Standard CGo build; a C toolchain is required.

```sh
go build ./...
go vet .
go test .
```

Build tags follow upstream (e.g. `sqlite_fts5`, `sqlite_userauth`, `libsqlite3`); see the `sqlite3_opt_*.go` files.

## License

MIT, inherited from upstream `mattn/go-sqlite3`. See [`LICENSE`](./LICENSE).
