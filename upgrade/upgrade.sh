#!/bin/sh

# Regenerate the vendored SQLCipher amalgamation from the pinned SQLCipher
# release (see upgrade/upgrade.go for the pin and prerequisites: git, tclsh, a C
# toolchain). The fork tracks a SQLCipher *tag*, and its resync is a deliberate
# manual step, so this script only regenerates and reports the versions —
# reviewing the diff, running the CI gates, and committing / force-pushing master
# are left to the maintainer (see CLAUDE.md).

set -e

cd "$(dirname "$0")/.."

go run upgrade/upgrade.go

CIPHER_VERSION=$(grep -m1 '#define CIPHER_VERSION_NUMBER' sqlite3-binding.c | awk '{print $3}')
CIPHER_BUILD=$(grep -m1 '#define CIPHER_VERSION_BUILD' sqlite3-binding.c | awk '{print $3}')
SQLITE_VERSION=$(grep -m1 '#define SQLITE_VERSION ' sqlite3-binding.c | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$CIPHER_VERSION" ]; then
  echo "Error: could not detect SQLCipher version in the regenerated amalgamation" >&2
  exit 1
fi

echo
echo "Regenerated vendored amalgamation:"
echo "  SQLCipher: ${CIPHER_VERSION} (${CIPHER_BUILD:-unknown})"
echo "  Underlying SQLite: ${SQLITE_VERSION:-unknown}"
echo
echo "Next: review the diff, run the CI gates"
echo "  go build ./... && golangci-lint run ./... && go test -race ./... && govulncheck ./..."
echo "then commit and force-push master per CLAUDE.md."
