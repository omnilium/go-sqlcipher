/*
Package sqlite3 provides an SQLCipher at-rest-encrypted SQLite3 driver for
database/sql.

It is Omnilium's fork of mattn/go-sqlite3: the bundled C amalgamation is
SQLCipher (Zetetic) with the encryption codec compiled in, backed by OpenSSL
(libcrypto) — a hard build- and run-time dependency. The public API and the
"sqlite3" driver name are unchanged from upstream; without an encryption key a
connection behaves as an ordinary, unencrypted SQLite database.

# Installation

	go get github.com/omnilium/go-sqlcipher

# Encryption

Supply a key in the DSN to open or create an encrypted database. The key is
applied (via PRAGMA key) before the connection's first page read, which is what
makes write → close → reopen round-trips work.

	import (
		"database/sql"

		_ "github.com/omnilium/go-sqlcipher"
	)

	// Passphrase key (run through SQLCipher's KDF):
	db, err := sql.Open("sqlite3", "file:app.db?_key=correct-horse-battery-staple")

	// Raw 256-bit key as an x'<hex>' blob literal (bypasses the KDF):
	db, err = sql.Open("sqlite3", "file:app.db?_key=x'2DD29CA851E7B56E4697B0E1F08507293D761A05CE4D1B628663F411A8086D99'")

For databases written with non-default cipher settings, the DSN parameters
_cipher_compatibility, _cipher_page_size, _kdf_iter, _cipher_hmac_algorithm,
_cipher_kdf_algorithm, and _cipher_plaintext_header_size are applied right after
the key. See the Open doc comment for details.

# Supported Types

This driver supports the following data types.

	+------------------------------+
	|go        | sqlite3           |
	|----------|-------------------|
	|nil       | null              |
	|int       | integer           |
	|int64     | integer           |
	|float64   | float             |
	|bool      | integer           |
	|[]byte    | blob              |
	|string    | text              |
	|time.Time | timestamp/datetime|
	+------------------------------+

# SQLite3 Extension

You can write your own extension module for sqlite3. For example, below is an
extension for a Regexp matcher operation.

	#include <pcre.h>
	#include <string.h>
	#include <stdio.h>
	#include <sqlite3ext.h>

	SQLITE_EXTENSION_INIT1
	static void regexp_func(sqlite3_context *context, int argc, sqlite3_value **argv) {
	  if (argc >= 2) {
	    const char *target  = (const char *)sqlite3_value_text(argv[1]);
	    const char *pattern = (const char *)sqlite3_value_text(argv[0]);
	    const char* errstr = NULL;
	    int erroff = 0;
	    int vec[500];
	    int n, rc;
	    pcre* re = pcre_compile(pattern, 0, &errstr, &erroff, NULL);
	    rc = pcre_exec(re, NULL, target, strlen(target), 0, 0, vec, 500);
	    if (rc <= 0) {
	      sqlite3_result_error(context, errstr, 0);
	      return;
	    }
	    sqlite3_result_int(context, 1);
	  }
	}

	#ifdef _WIN32
	__declspec(dllexport)
	#endif
	int sqlite3_extension_init(sqlite3 *db, char **errmsg,
	      const sqlite3_api_routines *api) {
	  SQLITE_EXTENSION_INIT2(api);
	  return sqlite3_create_function(db, "regexp", 2, SQLITE_UTF8,
	      (void*)db, regexp_func, NULL, NULL);
	}

It needs to be built as a so/dll shared library. And you need to register
the extension module like below.

	sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{
			Extensions: []string{
				"sqlite3_mod_regexp",
			},
		})

Then, you can use this extension.

	rows, err := db.Query("select text from mytable where name regexp '^golang'")

# Connection Hook

You can hook and inject your code when the connection is established by setting
ConnectHook to get the SQLiteConn.

	sql.Register("sqlite3_with_hook_example",
			&sqlite3.SQLiteDriver{
					ConnectHook: func(conn *sqlite3.SQLiteConn) error {
						sqlite3conn = append(sqlite3conn, conn)
						return nil
					},
			})

You can also use database/sql.Conn.Raw (Go >= 1.13):

	conn, err := db.Conn(context.Background())
	// if err != nil { ... }
	defer conn.Close()
	err = conn.Raw(func (driverConn any) error {
		sqliteConn := driverConn.(*sqlite3.SQLiteConn)
		// ... use sqliteConn
	})
	// if err != nil { ... }

# Go SQlite3 Extensions

If you want to register Go functions as SQLite extension functions
you can make a custom driver by calling RegisterFunction from
ConnectHook.

	regex = func(re, s string) (bool, error) {
		return regexp.MatchString(re, s)
	}
	sql.Register("sqlite3_extended",
			&sqlite3.SQLiteDriver{
					ConnectHook: func(conn *sqlite3.SQLiteConn) error {
						return conn.RegisterFunc("regexp", regex, true)
					},
			})

You can then use the custom driver by passing its name to sql.Open.

	var i int
	conn, err := sql.Open("sqlite3_extended", "./foo.db")
	if err != nil {
		panic(err)
	}
	err = db.QueryRow(`SELECT regexp("foo.*", "seafood")`).Scan(&i)
	if err != nil {
		panic(err)
	}

See the documentation of RegisterFunc for more details.
*/
package sqlite3
