# CLAUDE.md — go-sqlcipher

Guidance for Claude Code when working in this repository.

## What this repo is

`github.com/omnilium/go-sqlcipher` is Omnilium's fork of [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) — a CGo `database/sql` driver for SQLite. It exists to provide a **SQLCipher** at-rest-encrypted SQLite driver, and stands on its own as a library.

It is its own independent git repo. It is consumed as a Go module by downstream projects, so a change here that a consumer needs is not done until that consumer can pull a published fork tag.

## Upstream tracking & maintenance model

`master` = the latest upstream **release tag** + Omnilium's patch stack. We track release tags, **never** upstream's unreleased `master` tip.

- **Base:** currently `v1.14.45`. The `upstream` remote is `https://github.com/mattn/go-sqlite3.git`.
- **Foundational patches (always present):** both in `go.mod` — the module path declares `github.com/omnilium/go-sqlcipher` (importable under its own path, no `replace` directive), and the Go version floor is held at the Omnilium baseline (currently `go 1.26`, kept in step with the wider Omnilium Go toolchain) rather than upstream's lower minimum. Rebased forward on every resync. CI reads the version via `go-version-file: go.mod`, so bumping the floor there bumps CI too.
- **Resync procedure:** `git fetch --tags upstream` → rebase the patch stack onto the new release tag (`git rebase --onto vX.Y.Z <old-base> master`) → get the CI gates green (build / lint / race tests / vuln — see *Build / test / lint*) → force-push `master` → cut the next fork tag.
- **Versioning:** the fork has its **own independent semver line starting at `v0.1.0`**, not aligned to upstream's `v1.14.x` numbers. Consumers pin a fork **tag** (never a branch pseudo-version — a resync rebase rewrites `master`'s SHAs and would invalidate it).

`master` is force-pushed on every resync; this is expected and correct for a vendored mirror. Push directly without commentary on protected-branch bypass.

## Build / test / lint

Standard CGo build; a C toolchain is required. These are the same gates CI runs:

```sh
go build ./...
golangci-lint run ./...      # golangci-lint v2 (config in .golangci.yaml)
go test -race ./...
govulncheck ./...
```

Build tags follow upstream (`sqlite_fts5`, `sqlite_userauth`, `libsqlite3`, …); see the `sqlite3_opt_*.go` files.

## CI & linting

CI (`.github/workflows/ci.yml`) has four jobs — **Build**, **Lint**, **Test (race)**, **Vulnerability scan** — every job on a Blacksmith runner (house rule, see writing-workflows), with the Go version read from `go.mod`. It currently runs the **default build only**; the tag-gated test files are not yet exercised in CI (a planned feature-tag matrix — see the README).

Linting is golangci-lint v2 with the Omnilium house config (`.golangci.yaml`). The **entire tree — vendored upstream code included — is held to the bar**: findings are fixed, not exempted (see the workspace memory *forked code is ours*). The config carries a few documented, driver-intrinsic adjustments, each with a rationale comment in `.golangci.yaml`:

- `exhaustive` ignores `reflect.Kind` and the cgo `_Ctype_int` type — they are type-dispatch switches, not domain enums.
- `gocritic`'s `dupSubExpr` is excluded — it misfires on cgo wrapper calls (`C.sqlite3_*`).
- `revive`'s ALL_CAPS rule is excluded — the `SQLITE_*` constants mirror SQLite's C API and are public (drop-in) API.
- `unused` is excluded for `convert.go` — it's used only under the `sqlite_preupdate_hook` build tag, which the default-build linter can't see.
- The opt-in, deliberately-weak SHA1 crypt encoders are marked `//nolint:gosec` with justification (golangci honours `//nolint`, not gosec's native `//nosec`).
- Issue caps are disabled (`max-issues-per-linter`/`max-same-issues: 0`) so nothing is silently truncated.

Because the whole tree is lint-clean, **the lint/refactor fixes are part of the carried patch stack** and must be rebased forward on every upstream resync (the resync isn't done until the gates are green again).

## Conventions

- **Commits:** Conventional Commits 1.0.0 (Omnilium house style). Keep the Omnilium patch stack small, self-contained, and clearly labeled so it rebases cleanly.
- **Don't reintroduce upstream's open-source apparatus.** This fork deliberately drops mattn's `_example/`, OSS-Fuzz/Docker example workflows, `FUNDING.yml`, Codecov config, and `SECURITY.md` — they serve no purpose here. Don't restore them on resync.
- **Amalgamation source:** the C amalgamation (`sqlite3-binding.c/.h`, `sqlite3ext.h`) must come from **SQLCipher** (Zetetic), not vanilla `sqlite.org`. The inherited `upgrade/` tool fetches *vanilla* SQLite and is therefore wrong for this fork until reworked — do not run it as-is to regenerate the amalgamation.

## Keep this file and the README current — automatically

**`CLAUDE.md` and `README.md` are documentation that must track reality.** When a change alters anything they describe — the upstream base, the resync/rebase procedure, the versioning scheme, the foundational patch set, the build/test commands, or the SQLCipher integration status — update both files **as part of the same change**, never as a deferred follow-up. A stale rule here silently misleads every future session.
