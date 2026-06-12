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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These are native Go fuzz targets (go test -fuzz). Their seed corpora also run
// as ordinary subtests under a plain `go test`, so they contribute coverage even
// without mutation fuzzing. The shared contract across all three is the only
// thing being asserted: any input — valid or hostile — must surface as a normal
// Go error at worst, never a panic or a process crash. A SIGSEGV out of the C
// codec/parser fails the worker, which is exactly the signal we want.

// fuzzOpenMemory opens a fresh keyed in-memory SQLCipher database. A new handle
// per fuzz iteration keeps inputs isolated and reproducible (no schema bleeds
// from one input into the next).
func fuzzOpenMemory(tb testing.TB) *sql.DB {
	tb.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_key="+testKey)
	if err != nil {
		tb.Fatalf("open in-memory db: %v", err)
	}
	return db
}

// FuzzExecSQL feeds arbitrary statement text through both the exec and the
// query/scan paths, so statement preparation, stepping, and value conversion all
// see the input. Scanning into *any also drives convert.go's decode arms.
func FuzzExecSQL(f *testing.F) {
	seeds := []string{
		"",
		";",
		"SELECT 1",
		"CREATE TABLE t(x);",
		"INSERT INTO t VALUES (1);",
		"SELECT * FROM t WHERE x = ?;",
		"PRAGMA cipher_version;",
		"-- comment\nSELECT 'x';",
		"SELECT zeroblob(16), randomblob(8);",
		"SELECT hex(x'00ff'), 1.5, NULL, 'text';",
		string([]byte{0x00, 0xff}), // non-UTF-8: exercises the C-string boundary
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, query string) {
		db := fuzzOpenMemory(t)
		defer db.Close()

		if _, err := db.Exec(query); err != nil {
			return
		}
		rows, err := db.Query(query)
		if err != nil {
			return
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return
		}
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = new(any)
		}
		for rows.Next() {
			if err := rows.Scan(dest...); err != nil {
				return
			}
		}
	})
}

// FuzzDSN fuzzes the connection-string option parser. DSN options are applied
// lazily on first use, so the target opens and pings; the fork's own _key and
// cipher-tuning options live in this same parse-and-apply path. A parse error is
// a fine outcome — a panic is not.
func FuzzDSN(f *testing.F) {
	seeds := []string{
		"",
		"_key=" + testKey,
		"_key=x'2DD29CA851E7B56E4697B0E1F08507293D761A05CE4D1B628663F411A8086D99'",
		"_cipher_page_size=4096&_kdf_iter=64000",
		"_cipher_hmac_algorithm=HMAC_SHA512&_cipher_kdf_algorithm=PBKDF2_HMAC_SHA512",
		"_busy_timeout=1000&_journal_mode=WAL&_synchronous=FULL",
		"_foreign_keys=1&_loc=UTC&_txlock=immediate",
		"mode=ro&cache=shared",
		"_auth&_auth_user=u&_auth_pass=p&_auth_crypt=SHA256",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, query string) {
		path := filepath.Join(t.TempDir(), "fuzz.db")
		db, err := sql.Open("sqlite3", "file:"+path+"?"+query)
		if err != nil {
			return
		}
		defer db.Close()
		_ = db.Ping() // forces the lazy connection open that applies the options
	})
}

// FuzzDatabaseFile writes the input to a file and opens it both plaintext and
// keyed, then walks the schema. A malformed file must come back as a clean error
// from the page parser or the codec, never a crash. Seeding with a real valid
// encrypted database gives the mutator a structurally meaningful starting point.
func FuzzDatabaseFile(f *testing.F) {
	f.Add([]byte("SQLite format 3\x00")) // valid plaintext header prefix
	f.Add([]byte("not a database at all"))
	f.Add([]byte{})
	if enc := encryptedDBSeed(f); enc != nil {
		f.Add(enc)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.db")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write fuzz file: %v", err)
		}

		for _, dsn := range []string{"file:" + path, "file:" + path + "?_key=" + testKey} {
			db, err := sql.Open("sqlite3", dsn)
			if err != nil {
				continue
			}
			rows, err := db.Query("SELECT name FROM sqlite_master")
			if err == nil {
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err != nil {
						break
					}
				}
				_ = rows.Close()
			}
			_ = db.Close()
		}
	})
}

// FuzzTimestampDecode drives the driver's timestamp parser: a value read from a
// column declared DATE/DATETIME/TIMESTAMP is run through time.ParseInLocation
// against every entry in SQLiteTimestampFormats (sqlite3.go), turning untrusted
// stored text into a time.Time. We insert the fuzzed text into such a column and
// scan it back into a time.Time; an unparseable value must surface as the zero
// time, never a panic. (DATETIME carries NUMERIC affinity, so inputs that look
// like a plain number are stored numeric and bypass the text parser — the bulk
// of mutated, non-numeric inputs still exercise it.)
func FuzzTimestampDecode(f *testing.F) {
	seeds := []string{
		"2026-06-12 10:00:00",
		"2026-06-12T10:00:00Z",
		"2026-06-12T10:00:00.000Z",
		"2026-06-12 10:00:00.000-07:00",
		"2026-06-12",
		"0000-00-00 00:00:00",
		"not a date",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, ts string) {
		db := fuzzOpenMemory(t)
		defer db.Close()

		if _, err := db.Exec(`CREATE TABLE t (ts DATETIME)`); err != nil {
			t.Fatalf("create table: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO t VALUES (?)`, ts); err != nil {
			return
		}
		var got time.Time
		_ = db.QueryRow(`SELECT ts FROM t`).Scan(&got)
	})
}

// FuzzQuoteKey asserts the SQL-injection invariant of the fork's quoteKey
// (sqlite3.go): for ANY key, the result must be exactly one well-formed SQL
// string literal that spans the whole output and decodes back to the original.
// If that holds, a key can never terminate the literal early and inject SQL into
// the PRAGMA key statement. Being in-package, this calls quoteKey directly, so it
// fuzzes the pure quoting logic at full speed with a real correctness property —
// not merely the absence of a panic.
func FuzzQuoteKey(f *testing.F) {
	seeds := []string{
		"",
		"correct horse",
		"x'2DD29CA851E7B56E'",
		"X'deadbeef'",
		"has'apostrophe",
		`has"quote`,
		"x'has'inner'quotes'",
		"';DROP TABLE x;--",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, key string) {
		q := quoteKey(key)
		decoded, ok := unquoteSQLLiteral(q)
		if !ok {
			t.Fatalf("quoteKey(%q) = %q is not a single well-formed SQL literal", key, q)
		}
		if decoded != key {
			t.Fatalf("quoteKey(%q) = %q decodes to %q, want the original", key, q, decoded)
		}
	})
}

// unquoteSQLLiteral parses one SQLite string literal that must span the entire
// input s (delimited by ' or "). A doubled delimiter is one literal delimiter; a
// lone delimiter before the end means the literal terminated early — the very
// escape an injection would exploit — so that is reported as not-well-formed. It
// returns the decoded content and whether s was exactly one valid literal.
func unquoteSQLLiteral(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	delim := s[0]
	if (delim != '\'' && delim != '"') || s[len(s)-1] != delim {
		return "", false
	}
	body := s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != delim {
			b.WriteByte(body[i])
			continue
		}
		// A delimiter inside the body is legal only as a doubled pair.
		if i+1 >= len(body) || body[i+1] != delim {
			return "", false
		}
		b.WriteByte(delim)
		i++
	}
	return b.String(), true
}

// encryptedDBSeed builds a small, valid SQLCipher database and returns its raw
// bytes for use as a FuzzDatabaseFile seed. It tolerates failure (returns nil)
// so a setup hiccup never aborts the whole fuzz target.
func encryptedDBSeed(f *testing.F) []byte {
	f.Helper()
	path := filepath.Join(f.TempDir(), "seed.db")
	db, err := sql.Open("sqlite3", "file:"+path+"?_key="+testKey)
	if err != nil {
		return nil
	}
	if _, err := db.Exec(`CREATE TABLE seed(x); INSERT INTO seed VALUES('hi');`); err != nil {
		_ = db.Close()
		return nil
	}
	if err := db.Close(); err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}
