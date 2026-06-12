# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.2.0] - 2026-06-12

### Changed
- Expanded the automated test suite — default-build coverage raised to 87.2% (88.7% with feature tags) — and added a feature-tag CI matrix so the optional build-tag suites are actually exercised.
- Added five coverage-guided fuzz targets over the driver's untrusted-input boundaries (arbitrary SQL, DSN options including `_key` and cipher tuning, database-file bytes, and timestamp text), plus a `FuzzQuoteKey` invariant proving the fork's key quoting is injection-safe — all run as a CI gate.

### Fixed
- Raw hex keys passed as `_key=x'<hex>'` now work. SQLCipher rejected the previously-emitted `PRAGMA key = x'...'` as a syntax error, so the documented raw-key DSN form never actually opened a database; the key is now quoted in the form SQLCipher requires.
- Custom aggregators whose constructor returns an error or a nil value no longer panic later in `Done()` — the aggregator slot is claimed only after the constructor succeeds.

## [0.1.0] - 2026-06-11

### Added
- SQLCipher 4.14.0 whole-database at-rest encryption compiled into the driver via the OpenSSL crypto provider. Encryption is opt-in through the DSN: `_key` takes either a passphrase or a raw `x'<hex>'` key, applied before the first page read so write → close → reopen round-trips work.
- Cipher-tuning DSN parameters (the `_cipher_*` / `_kdf_iter` family) for interoperating with non-default or `sqlcipher`-CLI-written databases.
- `upgrade/` tooling that regenerates the C amalgamation from a pinned SQLCipher source tag, reproducing the vendored files byte-for-byte.

### Changed
- Module path is now `github.com/omnilium/go-sqlcipher` — import the fork under its own path, no `replace` directive needed. The driver name (`sqlite3`) and the mattn-compatible public API are unchanged, so it stays drop-in.
- OpenSSL `libcrypto` is now a hard build- and run-time dependency (dynamically linked, `-lcrypto`).
- Supported targets narrowed to Linux (x86-64 / ARM64) and Apple Silicon macOS; Intel macOS and Windows are unsupported.
- Minimum Go version raised to 1.26.

### Fixed
- Salted SHA1 crypt encoders no longer alias their input slice (a latent aliasing bug), now building output with `slices.Concat`.

[0.2.0]: https://github.com/omnilium/go-sqlcipher/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/omnilium/go-sqlcipher/releases/tag/v0.1.0
