# go-sqlcipher

`github.com/omnilium/go-sqlcipher` — Omnilium's fork of [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3), a CGo `database/sql` driver for SQLite. It exists to provide a **SQLCipher** at-rest-encrypted SQLite driver.

For upstream documentation, FAQ, and the full feature/build-tag matrix, see the [upstream README](https://github.com/mattn/go-sqlite3); this file covers only what is specific to the fork.

> **Drop-in compatibility — with a caveat.** This fork aims to be a drop-in replacement for `mattn/go-sqlite3`: the public API (exported types, functions, constants, and SQLite C-API mirror names) is preserved. However, as part of the Go version bump and the associated linting/refactoring, there may be slight differences in the contract — for example, error message wording (lower-cased to satisfy `staticcheck`) and the avoidance of input-slice mutation in the salted crypt encoders. Behaviour-affecting differences are intentional and considered improvements; pin a specific fork tag and test before relying on exact byte-for-byte parity with upstream.

## Encryption

The bundled C amalgamation is **SQLCipher** (currently **4.14.0**), not vanilla SQLite, with the encryption codec compiled in. The driver name stays `sqlite3` and the API is unchanged; you enable encryption by supplying a key in the DSN. The key is applied (via `PRAGMA key`) **before the connection's first page read**, which is what makes write → close → reopen round-trips work — a key applied after the first read silently produces a database that cannot be reopened.

```go
import (
	"database/sql"

	_ "github.com/omnilium/go-sqlcipher"
)

// Passphrase key (run through SQLCipher's KDF):
db, err := sql.Open("sqlite3", "file:app.db?_key=correct-horse-battery-staple")

// Raw 256-bit key as an x'<hex>' blob literal (bypasses the KDF):
db, err = sql.Open("sqlite3", "file:app.db?_key=x'2DD29CA851E7B56E4697B0E1F08507293D761A05CE4D1B628663F411A8086D99'")
```

Without `_key` the connection is an ordinary, unencrypted SQLite connection.

For opening databases written with non-default cipher settings (or by another SQLCipher version / the `sqlcipher` CLI), these DSN parameters are applied right after the key, in this order: `_cipher_compatibility`, `_cipher_page_size`, `_kdf_iter`, `_cipher_hmac_algorithm`, `_cipher_kdf_algorithm`, `_cipher_plaintext_header_size`. Algorithm values are bare SQLCipher identifiers (e.g. `HMAC_SHA512`, `PBKDF2_HMAC_SHA512`); the rest are integers. See the `Open` doc comment in `sqlite3.go` for details.

## Crypto provider and platform support

SQLCipher needs a cryptographic provider. This fork uses **OpenSSL** (`libcrypto`) — SQLCipher's reference, widely-audited, AES-NI-accelerated backend — linked **dynamically**. That makes `libcrypto` a **hard dependency at both build time and run time**:

- **Build:** the OpenSSL development headers and `libcrypto` must be present (`libssl-dev` / `openssl-devel` on Linux; `brew install openssl` on Apple Silicon macOS — the Homebrew `/opt/homebrew` include/lib paths are already wired in).
- **Run:** the binary dynamically links `libcrypto` (`ldd` will list `libcrypto.so`). Provision your deployment/base image accordingly — a distroless image, for instance, must include `libcrypto`, not just `libc`.

Supported targets:

| OS | Arch | Status |
| --- | --- | --- |
| Linux | x86-64, ARM64 | Supported |
| macOS | Apple Silicon (ARM64) | Supported (requires Homebrew OpenSSL). Intel macOS is **not** supported. |
| Windows | any | **Not supported.** It should in principle compile with a MinGW toolchain and an OpenSSL built for it, but we do not wire Windows OpenSSL paths, build it, or test it. Use at your own risk. |

> A fully self-contained (libcrypto-free) build using a vendored libtomcrypt backend was evaluated and **dropped**: libtomcrypt has had no stable release since 2018 and its SQLCipher integration was not reliable on a current toolchain. OpenSSL is the single supported provider.

## Relationship to upstream

The fork has **two upstreams**:

- The **Go binding layer** (the `*.go` files and the public API) tracks a **`mattn/go-sqlite3` release tag** — never its unreleased `master` tip. The current base is **`v1.14.45`**.
- The **C amalgamation** (`sqlite3-binding.c/.h`, `sqlite3ext.h`) is **SQLCipher** (Zetetic), currently **`v4.14.0`** — not vanilla `sqlite.org` SQLite. It is regenerated from the SQLCipher source tree by the `upgrade/` tool, not by mattn's old sqlite.org download.

The foundational patches, always present and rebased forward on every resync, both live in `go.mod`: the **module path** declares `github.com/omnilium/go-sqlcipher` (not `github.com/mattn/go-sqlite3`) so the package is importable under its own path without a `replace` directive, and the **Go version floor** is held at the Omnilium baseline (currently `go 1.26`) rather than upstream's lower minimum. On top of those sit the SQLCipher integration: the codec/crypto cgo flags, the OpenSSL provider wiring, and the `_key` DSN mechanism.

## Maintenance model

`master` = latest `mattn/go-sqlite3` release tag + Omnilium's patch stack (including the SQLCipher integration), with the SQLCipher amalgamation vendored in.

**To resync the Go binding layer** to a newer `mattn/go-sqlite3` release:

1. `git fetch --tags upstream` (`upstream` = `https://github.com/mattn/go-sqlite3.git`).
2. Rebase the Omnilium patch stack onto the new release tag (e.g. `git rebase --onto vX.Y.Z <old-base> master`).
3. Build and verify (see *Building and testing*).
4. Force-push `master`.
5. Cut the next fork tag (see Versioning).

**To upgrade the SQLCipher amalgamation** to a newer SQLCipher release: bump `sqlcipherTag` in `upgrade/upgrade.go`, run `sh upgrade/upgrade.sh` (it clones SQLCipher at that tag, builds the amalgamation, and re-vendors the three C files), then verify and commit. This needs `git`, `tclsh`, and a C toolchain.

We deliberately track release tags on both sides, not the `master` tips, so the base is always shipped code rather than unreleased churn.

## Versioning

The fork uses its **own independent semver line starting at `v0.1.0`** — it is *not* aligned to upstream's `v1.14.x` numbers (a bare `v1.14.45` here would falsely imply byte-equivalence with mattn's release). Each fork release notes the upstream base it sits on. Consumers pin a **fork tag** in their `go.mod` (never a branch pseudo-version, which a resync rebase would invalidate).

## Building and testing

CGo build; a C toolchain **and OpenSSL `libcrypto` (headers + library)** are required — see *Crypto provider and platform support* above. These are the core gates CI enforces (it adds a feature-tag matrix on top — see below):

```sh
go build ./...
golangci-lint run ./...   # golangci-lint v2 (config in .golangci.yaml)
go test -race ./...
govulncheck ./...
```

The SQLCipher round-trip / wrong-key / cipher-tuning / `sqlcipher`-CLI-interop tests live in `sqlcipher_test.go`; the CLI-interop tests skip themselves if no `sqlcipher` binary is on `PATH`.

Build tags follow upstream (e.g. `sqlite_fts5`, `sqlite_vtable`, `libsqlite3`); see the `sqlite3_opt_*.go` files.

CI builds and race-tests across every supported target — Linux x86-64, Linux ARM64, and Apple Silicon macOS — on Blacksmith runners, and lints + vuln-scans on Linux x86-64. A separate feature-tag matrix turns on each optional build tag whose test file the default build leaves uncompiled (`sqlite_column_metadata`, `sqlite_math_functions`, `sqlite_preupdate_hook`, `sqlite_unlock_notify`, `sqlite_vtable`, plus `sqlite_fts5`) and runs the suite under it on Linux x86-64, so those tag-gated tests are actually exercised. (`sqlite_userauth` is excluded: upstream neutered the tag — every connection now errors — so its test file can no longer pass.) Each runner gets OpenSSL provisioned first (`libssl-dev` on Linux, Homebrew `openssl` on macOS) so the SQLCipher cgo build links.

> **Planned testing improvements.** We still plan to add native Go fuzz targets for SQL and database-file parsing.

## License

MIT, inherited from upstream `mattn/go-sqlite3`. See [`LICENSE`](./LICENSE).
