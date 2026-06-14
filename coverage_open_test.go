// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"database/sql"
	"os"
	"testing"
)

// openWith opens dsn through database/sql and forces the lazy driver Open by
// pinging; the connection (and any DSN-parse error) surfaces here.
func openWith(t *testing.T, dsn string) error {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		return err
	}
	var x int
	return db.QueryRow("SELECT 1").Scan(&x)
}

// TestOpenDSNOptionsValid walks every documented `_`-prefixed DSN option with a
// valid value so each parse-and-apply branch in Open runs.
func TestOpenDSNOptionsValid(t *testing.T) {
	opts := []string{
		"?_loc=auto",
		"?_loc=UTC",
		"?_mutex=no",
		"?_mutex=full",
		"?_txlock=immediate",
		"?_txlock=exclusive",
		"?_txlock=deferred",
		"?_auto_vacuum=full",
		"?_vacuum=incremental",
		"?_busy_timeout=1000",
		"?_timeout=1000",
		"?_case_sensitive_like=1",
		"?_cslike=0",
		"?_defer_foreign_keys=1",
		"?_defer_fk=0",
		"?_foreign_keys=1",
		"?_fk=0",
		"?_ignore_check_constraints=1",
		"?_journal_mode=WAL",
		"?_journal=MEMORY",
		"?_locking_mode=EXCLUSIVE",
		"?_locking=NORMAL",
		"?_query_only=1",
		"?_recursive_triggers=1",
		"?_rt=0",
		"?_secure_delete=fast",
		"?_secure_delete=on",
		"?_synchronous=FULL",
		"?_sync=OFF",
		"?_writable_schema=0",
		"?_cache_size=2000",
		"?_stmt_cache_size=16",
	}
	for _, opt := range opts {
		t.Run(opt, func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)
			if err := openWith(t, path+opt); err != nil {
				t.Fatalf("open %q: %v", opt, err)
			}
		})
	}
}

// TestOpenDSNOptionsInvalid covers the rejection branches: each bad value must
// make Open return an error.
func TestOpenDSNOptionsInvalid(t *testing.T) {
	opts := []string{
		"?_mutex=bogus",
		"?_txlock=bogus",
		"?_auto_vacuum=bogus",
		"?_busy_timeout=abc",
		"?_case_sensitive_like=bogus",
		"?_defer_fk=bogus",
		"?_fk=bogus",
		"?_ignore_check_constraints=bogus",
		"?_journal_mode=bogus",
		"?_locking_mode=bogus",
		"?_query_only=bogus",
		"?_recursive_triggers=bogus",
		"?_secure_delete=bogus",
		"?_synchronous=bogus",
		"?_writable_schema=bogus",
		"?_loc=Bogus/Zone",
		"?_cache_size=abc",
		"?_stmt_cache_size=-1",
		"?_stmt_cache_size=abc",
		"?_cipher_page_size=abc",
		"?_cipher_hmac_algorithm=not-an-identifier",
	}
	for _, opt := range opts {
		t.Run(opt, func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)
			if err := openWith(t, path+opt); err == nil {
				t.Fatalf("expected error for %q, got nil", opt)
			}
		})
	}
}

// TestOpenAuthCrypt covers the _auth_crypt encoder switch in Open (each SHA
// variant, the salted variants, and the missing-salt error branches). The
// userauth extension is a no-op in the default build, so a successful open just
// proves the crypt function registered.
func TestOpenAuthCrypt(t *testing.T) {
	//nolint:gosec // G101 false positive: a fixed test DSN fixture, not a real credential.
	const creds = "?_auth&_auth_user=admin&_auth_pass=secret&_auth_crypt="

	ok := []string{"SHA1", "SHA256", "SHA384", "SHA512"}
	for _, algo := range ok {
		t.Run(algo, func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)
			if err := openWith(t, path+creds+algo); err != nil {
				t.Fatalf("open _auth_crypt=%s: %v", algo, err)
			}
		})
	}

	salted := []string{"SSHA1", "SSHA256", "SSHA384", "SSHA512"}
	for _, algo := range salted {
		t.Run(algo+"+salt", func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)
			if err := openWith(t, path+creds+algo+"&_auth_salt=pepper"); err != nil {
				t.Fatalf("open _auth_crypt=%s: %v", algo, err)
			}
		})
		t.Run(algo+"+missing_salt", func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)
			if err := openWith(t, path+creds+algo); err == nil {
				t.Fatalf("expected missing-salt error for %s", algo)
			}
		})
	}
}

// TestCipherDSNRoundTrip writes an encrypted database with explicit cipher
// tuning, then reopens it with the same parameters — exercising quoteKey, the
// cipher-pragma application loop, and isCipherIdent's accept path for both a
// passphrase key and a raw hex key.
func TestCipherDSNRoundTrip(t *testing.T) {
	cases := map[string]string{
		"passphrase": "?_key=correct-horse&_cipher_page_size=4096&_kdf_iter=4000" +
			"&_cipher_hmac_algorithm=HMAC_SHA512&_cipher_kdf_algorithm=PBKDF2_HMAC_SHA512",
		"raw_hex_key": "?_key=x'2DD29CA851E7B56E4697B0E1F08507293D761A05CE4D1B628663F411A8086D99'" +
			"&_cipher_page_size=4096",
	}

	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			path := TempFilename(t)
			defer os.Remove(path)

			write, err := sql.Open("sqlite3", path+opt)
			if err != nil {
				t.Fatalf("open (write): %v", err)
			}
			if _, err := write.Exec(`CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('secret')`); err != nil {
				t.Fatalf("write encrypted db: %v", err)
			}
			if err := write.Close(); err != nil {
				t.Fatalf("close (write): %v", err)
			}

			read, err := sql.Open("sqlite3", path+opt)
			if err != nil {
				t.Fatalf("open (read): %v", err)
			}
			defer read.Close()
			var v string
			if err := read.QueryRow("SELECT v FROM t").Scan(&v); err != nil {
				t.Fatalf("read encrypted db: %v", err)
			}
			if v != "secret" {
				t.Fatalf("round-trip value = %q, want %q", v, "secret")
			}
		})
	}
}

// TestPreUpdateHookOmitStub calls the no-op RegisterPreUpdateHook present in the
// default (!sqlite_preupdate_hook) build.
func TestPreUpdateHookOmitStub(t *testing.T) {
	sc := openDirect(t, ":memory:")
	sc.RegisterPreUpdateHook(nil)
	sc.RegisterPreUpdateHook(func(SQLitePreUpdateData) {})
}
