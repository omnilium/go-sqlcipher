# CLAUDE.md — go-sqlcipher

Guidance for Claude Code when working in this repository.

## What this repo is

`github.com/omnilium/go-sqlcipher` is Omnilium's fork of [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) — a CGo `database/sql` driver for SQLite. It exists to provide a **SQLCipher** at-rest-encrypted SQLite driver, and stands on its own as a library.

It is its own independent git repo. It is consumed as a Go module by downstream projects, so a change here that a consumer needs is not done until that consumer can pull a published fork tag.

## Upstream tracking & maintenance model

`master` = the latest upstream **release tag** + Omnilium's patch stack. We track release tags, **never** upstream's unreleased `master` tip.

- **Base:** currently `v1.14.45`. The `upstream` remote is `https://github.com/mattn/go-sqlite3.git`.
- **Foundational patch (always present):** the `go.mod` module path declares `github.com/omnilium/go-sqlcipher`, so the package is importable under its own path with no `replace` directive. This patch is rebased forward on every resync.
- **Resync procedure:** `git fetch --tags upstream` → rebase the patch stack onto the new release tag (`git rebase --onto vX.Y.Z <old-base> master`) → `go build ./...` / `go vet .` / `go test .` → force-push `master` → cut the next fork tag.
- **Versioning:** the fork has its **own independent semver line starting at `v0.1.0`**, not aligned to upstream's `v1.14.x` numbers. Consumers pin a fork **tag** (never a branch pseudo-version — a resync rebase rewrites `master`'s SHAs and would invalidate it).

`master` is force-pushed on every resync; this is expected and correct for a vendored mirror. Push directly without commentary on protected-branch bypass.

## Build / test

Standard CGo build; a C toolchain is required.

```sh
go build ./...
go vet .
go test .
```

Build tags follow upstream (`sqlite_fts5`, `sqlite_userauth`, `libsqlite3`, …); see the `sqlite3_opt_*.go` files.

## Conventions

- **Commits:** Conventional Commits 1.0.0 (Omnilium house style). Keep the Omnilium patch stack small, self-contained, and clearly labeled so it rebases cleanly.
- **Don't reintroduce upstream's open-source apparatus.** This fork deliberately drops mattn's `_example/`, OSS-Fuzz/Docker example workflows, `FUNDING.yml`, Codecov config, and `SECURITY.md` — they serve no purpose here. Don't restore them on resync.
- **Amalgamation source:** the C amalgamation (`sqlite3-binding.c/.h`, `sqlite3ext.h`) must come from **SQLCipher** (Zetetic), not vanilla `sqlite.org`. The inherited `upgrade/` tool fetches *vanilla* SQLite and is therefore wrong for this fork until reworked — do not run it as-is to regenerate the amalgamation.

## Keep this file and the README current — automatically

**`CLAUDE.md` and `README.md` are documentation that must track reality.** When a change alters anything they describe — the upstream base, the resync/rebase procedure, the versioning scheme, the foundational patch set, the build/test commands, or the SQLCipher integration status — update both files **as part of the same change**, never as a deferred follow-up. A stale rule here silently misleads every future session.
