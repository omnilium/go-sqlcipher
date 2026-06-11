// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "correct horse battery staple"

// openKeyed opens a SQLCipher database at path using key via the _key DSN
// parameter.
func openKeyed(t *testing.T, path, key string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_key=%s", path, key)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open %q: %v", dsn, err)
	}
	return db
}

// mustClose closes db and fails the test if Close errors (e.g. a final flush
// failed), which matters before a reopen that asserts persistence.
func mustClose(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestEncryptedRoundTrip writes with the key, closes, and reopens with the same
// key. It locks down the open-order footgun: the key must be applied before the
// connection's first page read, otherwise reopening an existing encrypted file
// fails.
func TestEncryptedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt.db")

	db := openKeyed(t, path, testKey)
	if _, err := db.Exec(`CREATE TABLE t(x TEXT); INSERT INTO t VALUES('hello');`); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db = openKeyed(t, path, testKey)
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT x FROM t`).Scan(&got); err != nil {
		t.Fatalf("reopen read: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want hello", got)
	}
}

// TestEncryptedHeaderIsNotPlaintext verifies the file on disk does not begin
// with the plaintext SQLite header, i.e. encryption actually happened.
func TestEncryptedHeaderIsNotPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hdr.db")
	db := openKeyed(t, path, testKey)
	if _, err := db.Exec(`CREATE TABLE t(x)`); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustClose(t, db)

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head := make([]byte, 16)
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	if string(head[:15]) == "SQLite format 3" {
		t.Fatalf("file begins with plaintext SQLite header; not encrypted")
	}
}

// TestWrongKeyFails verifies that reopening with the wrong key fails on first
// access.
func TestWrongKeyFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrong.db")
	db := openKeyed(t, path, testKey)
	if _, err := db.Exec(`CREATE TABLE t(x)`); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustClose(t, db)

	db = openKeyed(t, path, "not the key")
	defer db.Close()
	if _, err := db.Exec(`SELECT count(*) FROM sqlite_master`); err == nil {
		t.Fatalf("expected failure with wrong key, got nil")
	}
}

// TestUnkeyedOpenOfEncryptedFails verifies that opening an encrypted file with
// no key at all fails.
func TestUnkeyedOpenOfEncryptedFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nokey.db")
	db := openKeyed(t, path, testKey)
	if _, err := db.Exec(`CREATE TABLE t(x)`); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustClose(t, db)

	plain, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, err := plain.Exec(`SELECT count(*) FROM sqlite_master`); err == nil {
		t.Fatalf("expected failure opening encrypted file without key, got nil")
	}
}

// TestCipherTuningParamsAreApplied writes a database with a non-default
// _cipher_page_size and proves the parameter is actually applied: reopening with
// only the key (default page size) must fail, while reopening with the matching
// page size succeeds. This would fail (the no-param reopen would succeed) if the
// cipher-tuning params were silently ignored.
func TestCipherTuningParamsAreApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuned.db")
	const pageSize = "8192"

	tuned := fmt.Sprintf("file:%s?_key=%s&_cipher_page_size=%s", path, testKey, pageSize)
	db, err := sql.Open("sqlite3", tuned)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t(x TEXT); INSERT INTO t VALUES('tuned');`); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustClose(t, db)

	// Key alone, default page size: must fail to read.
	def := openKeyed(t, path, testKey)
	if _, err := def.Exec(`SELECT count(*) FROM sqlite_master`); err == nil {
		_ = def.Close()
		t.Fatalf("expected failure reopening with default page size, got nil")
	}
	mustClose(t, def)

	// Key plus the matching page size: must read back.
	db, err = sql.Open("sqlite3", tuned)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT x FROM t`).Scan(&got); err != nil {
		t.Fatalf("reopen with matching page size: %v", err)
	}
	if got != "tuned" {
		t.Fatalf("got %q, want tuned", got)
	}
}

// sqlcipherCLI returns the path to the sqlcipher command-line shell, skipping
// the test if it is not installed.
func sqlcipherCLI(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("sqlcipher")
	if err != nil {
		t.Skip("sqlcipher CLI not on PATH; skipping interop test")
	}
	return p
}

// runCLI runs the sqlcipher shell against dbPath, feeding script on stdin, and
// returns its combined output.
func runCLI(t *testing.T, cli, dbPath, script string) (string, error) {
	t.Helper()
	cmd := exec.Command(cli, dbPath)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// cliKey renders a key for a PRAGMA key statement in a CLI script, matching the
// driver's quoteKey behaviour.
func cliKey(k string) string {
	return "'" + strings.ReplaceAll(k, "'", "''") + "'"
}

// TestInteropDriverWriteCLIRead verifies the sqlcipher CLI can read a database
// the driver wrote, using default cipher settings.
func TestInteropDriverWriteCLIRead(t *testing.T) {
	cli := sqlcipherCLI(t)
	path := filepath.Join(t.TempDir(), "dw.db")

	db := openKeyed(t, path, testKey)
	if _, err := db.Exec(`CREATE TABLE t(x TEXT); INSERT INTO t VALUES('interop');`); err != nil {
		t.Fatalf("driver write: %v", err)
	}
	mustClose(t, db)

	script := fmt.Sprintf("PRAGMA key=%s;\nSELECT x FROM t;\n", cliKey(testKey))
	out, err := runCLI(t, cli, path, script)
	if err != nil {
		t.Fatalf("cli read: %v (%s)", err, out)
	}
	if !strings.Contains(out, "interop") {
		t.Fatalf("cli output %q missing 'interop'", out)
	}
}

// TestInteropCLIWriteDriverRead verifies the driver can read a database the
// sqlcipher CLI wrote, using default cipher settings.
func TestInteropCLIWriteDriverRead(t *testing.T) {
	cli := sqlcipherCLI(t)
	path := filepath.Join(t.TempDir(), "cw.db")

	script := fmt.Sprintf("PRAGMA key=%s;\nCREATE TABLE t(x TEXT);\nINSERT INTO t VALUES('fromcli');\n", cliKey(testKey))
	if out, err := runCLI(t, cli, path, script); err != nil {
		t.Fatalf("cli write: %v (%s)", err, out)
	}

	db := openKeyed(t, path, testKey)
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT x FROM t`).Scan(&got); err != nil {
		t.Fatalf("driver read: %v", err)
	}
	if got != "fromcli" {
		t.Fatalf("got %q, want fromcli", got)
	}
}
