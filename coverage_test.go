// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
)

// openDirect opens a connection through the driver itself (bypassing
// database/sql) so tests can drive the low-level driver.Conn / driver.Stmt
// methods that the database/sql layer never calls directly.
func openDirect(t *testing.T, dsn string) *SQLiteConn {
	t.Helper()
	conn, err := (&SQLiteDriver{}).Open(dsn)
	if err != nil {
		t.Fatalf("driver Open(%q): %v", dsn, err)
	}
	sc := conn.(*SQLiteConn)
	t.Cleanup(func() { _ = sc.Close() })
	return sc
}

// drainDriverRows iterates a driver.Rows to completion, returning the row count.
func drainDriverRows(t *testing.T, rows driver.Rows) int {
	t.Helper()
	dest := make([]driver.Value, len(rows.Columns()))
	n := 0
	for {
		err := rows.Next(dest)
		if errors.Is(err, io.EOF) {
			_ = rows.Close()
			return n
		}
		if err != nil {
			t.Fatalf("rows.Next: %v", err)
		}
		n++
	}
}

// TestDriverConnAPI exercises the driver.Conn / driver.Stmt surface
// (Begin, AutoCommit, Exec, Query, Prepare, GetFilename, GetLimit/SetLimit,
// SetFileControlInt64) that the database/sql layer reaches only indirectly.
func TestDriverConnAPI(t *testing.T) {
	path := TempFilename(t)
	defer os.Remove(path)
	sc := openDirect(t, path)

	if !sc.AutoCommit() {
		t.Fatal("expected autocommit on a fresh connection")
	}

	if _, err := sc.Exec("CREATE TABLE t (a INTEGER, b TEXT)", nil); err != nil {
		t.Fatalf("Exec create: %v", err)
	}

	// Begin a transaction: autocommit flips off until the tx resolves.
	tx, err := sc.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if sc.AutoCommit() {
		t.Fatal("expected autocommit off inside a transaction")
	}
	if _, err := sc.Exec("INSERT INTO t VALUES (?, ?)", []driver.Value{int64(1), "one"}); err != nil {
		t.Fatalf("Exec insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !sc.AutoCommit() {
		t.Fatal("expected autocommit back on after commit")
	}

	// Rollback path.
	tx, err = sc.Begin()
	if err != nil {
		t.Fatalf("Begin (rollback): %v", err)
	}
	if _, err := sc.Exec("INSERT INTO t VALUES (?, ?)", []driver.Value{int64(2), "two"}); err != nil {
		t.Fatalf("Exec insert (rollback): %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// driver.Conn.Query and a prepared driver.Stmt.Query / Exec.
	rows, err := sc.Query("SELECT a, b FROM t", nil)
	if err != nil {
		t.Fatalf("conn Query: %v", err)
	}
	if got := drainDriverRows(t, rows); got != 1 {
		t.Fatalf("rolled-back row leaked: got %d rows, want 1", got)
	}

	stmt, err := sc.Prepare("SELECT b FROM t WHERE a = ?")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer stmt.Close()
	// The non-context driver.Stmt.Query/Exec are deprecated in the interface but
	// the driver must still implement them; exercise them directly on purpose.
	srows, err := stmt.Query([]driver.Value{int64(1)}) //nolint:staticcheck // legacy driver.Stmt API under test
	if err != nil {
		t.Fatalf("stmt Query: %v", err)
	}
	if got := drainDriverRows(t, srows); got != 1 {
		t.Fatalf("stmt Query rows: got %d, want 1", got)
	}

	istmt, err := sc.Prepare("INSERT INTO t VALUES (?, ?)")
	if err != nil {
		t.Fatalf("Prepare insert: %v", err)
	}
	defer istmt.Close()
	res, err := istmt.Exec([]driver.Value{int64(3), "three"}) //nolint:staticcheck // legacy driver.Stmt API under test
	if err != nil {
		t.Fatalf("stmt Exec: %v", err)
	}
	if id, err := res.LastInsertId(); err != nil || id == 0 {
		t.Fatalf("LastInsertId: id=%d err=%v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("RowsAffected: n=%d err=%v", n, err)
	}

	// GetFilename resolves "" to "main" and reports the on-disk path.
	if got := sc.GetFilename(""); got != sc.GetFilename("main") {
		t.Fatalf("GetFilename(\"\")=%q != GetFilename(\"main\")=%q", got, sc.GetFilename("main"))
	}
	if got := sc.GetFilename("main"); got == "" {
		t.Fatal("GetFilename(main) returned empty path for a file-backed db")
	}

	// GetLimit / SetLimit round-trip.
	orig := sc.GetLimit(SQLITE_LIMIT_LENGTH)
	if orig <= 0 {
		t.Fatalf("GetLimit(LENGTH) = %d, want > 0", orig)
	}
	prev := sc.SetLimit(SQLITE_LIMIT_LENGTH, orig-1)
	if prev != orig {
		t.Fatalf("SetLimit returned prior %d, want %d", prev, orig)
	}
	if got := sc.GetLimit(SQLITE_LIMIT_LENGTH); got != orig-1 {
		t.Fatalf("GetLimit after SetLimit = %d, want %d", got, orig-1)
	}

	// SetFileControlInt64 with the SIZE_LIMIT opcode, which the memdb VFS honours.
	mem := openDirect(t, "file:/coverage_fcntl?vfs=memdb")
	if err := mem.SetFileControlInt64("", SQLITE_FCNTL_SIZE_LIMIT, 1<<30); err != nil {
		t.Fatalf("SetFileControlInt64(SIZE_LIMIT): %v", err)
	}
}

// TestCustomFuncTypeConversions registers scalar functions covering every
// argument and return converter (bool, []byte, string, float, int64, blob,
// text, nil, and the error path) so callbackArg*/callbackRet*/callbackError
// are all exercised.
func TestCustomFuncTypeConversions(t *testing.T) {
	sc := openDirect(t, ":memory:")

	reg := func(name string, impl any) {
		if err := sc.RegisterFunc(name, impl, true); err != nil {
			t.Fatalf("RegisterFunc(%q): %v", name, err)
		}
	}
	reg("cf_not", func(b bool) bool { return !b })                // bool arg, bool->int ret
	reg("cf_blen", func(b []byte) int64 { return int64(len(b)) }) // []byte arg
	reg("cf_blob", func() []byte { return []byte{1, 2, 3} })      // []byte ret -> blob
	reg("cf_emptyblob", func() []byte { return nil })             // []byte nil -> null
	reg("cf_text", func(s string) string { return s + "!" })      // string arg + text ret
	reg("cf_dbl", func(f float64) float64 { return f * 2 })       // float arg + ret
	reg("cf_i64", func(i int64) int64 { return i + 1 })           // int64 arg + ret
	reg("cf_nilret", func() error { return nil })                 // error-typed result -> retNil
	reg("cf_err", func() (int64, error) { return 0, errors.New("boom") })

	// Each scalar function, driven through a query that supplies the right
	// argument SQLITE type.
	mustQuery := func(q string) {
		rows, err := sc.Query(q, nil)
		if err != nil {
			t.Fatalf("Query %q: %v", q, err)
		}
		drainDriverRows(t, rows)
	}
	mustQuery("SELECT cf_not(1)")
	mustQuery("SELECT cf_blen(x'01020304')")
	mustQuery("SELECT cf_blob()")
	mustQuery("SELECT cf_emptyblob()")
	mustQuery("SELECT cf_text('hi')")
	mustQuery("SELECT cf_dbl(2.5)")
	mustQuery("SELECT cf_i64(41)")
	mustQuery("SELECT cf_nilret()")

	// The error-returning function reports its error when the row is stepped
	// (Query only prepares; the function body runs during Next).
	rows, err := sc.Query("SELECT cf_err()", nil)
	if err != nil {
		t.Fatalf("Query cf_err: %v", err)
	}
	dest := make([]driver.Value, len(rows.Columns()))
	if err := rows.Next(dest); err == nil {
		t.Fatal("expected error from cf_err(), got nil")
	}
	_ = rows.Close()
}

// TestColumnTypeMetadata exercises ColumnTypeDatabaseTypeName /
// ColumnTypeNullable / ColumnTypeScanType across every declared-type branch in
// scanType / databaseTypeConvSqlite.
func TestColumnTypeMetadata(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const ddl = `CREATE TABLE typed (
		c_int   INTEGER,
		c_big   BIGINT,
		c_text  TEXT,
		c_clob  CLOB,
		c_char  VARCHAR(10),
		c_blob  BLOB,
		c_real  REAL,
		c_float FLOAT,
		c_dbl   DOUBLE,
		c_num   NUMERIC,
		c_dec   DECIMAL(10,2),
		c_bool  BOOLEAN,
		c_date  DATE,
		c_dt    DATETIME,
		c_ts    TIMESTAMP,
		c_other JSON
	)`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create typed table: %v", err)
	}

	rows, err := db.Query("SELECT * FROM typed")
	if err != nil {
		t.Fatalf("query typed: %v", err)
	}
	defer rows.Close()

	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("ColumnTypes: %v", err)
	}

	want := map[string]reflect.Type{
		"c_int":   reflect.TypeFor[sql.NullInt64](),
		"c_big":   reflect.TypeFor[sql.NullInt64](),
		"c_text":  reflect.TypeFor[sql.NullString](),
		"c_clob":  reflect.TypeFor[sql.NullString](),
		"c_char":  reflect.TypeFor[sql.NullString](),
		"c_blob":  reflect.TypeFor[sql.RawBytes](),
		"c_real":  reflect.TypeFor[sql.NullFloat64](),
		"c_float": reflect.TypeFor[sql.NullFloat64](),
		"c_dbl":   reflect.TypeFor[sql.NullFloat64](),
		"c_num":   reflect.TypeFor[sql.NullFloat64](),
		"c_dec":   reflect.TypeFor[sql.NullFloat64](),
		"c_bool":  reflect.TypeFor[sql.NullBool](),
		"c_date":  reflect.TypeFor[sql.NullTime](),
		"c_dt":    reflect.TypeFor[sql.NullTime](),
		"c_ts":    reflect.TypeFor[sql.NullTime](),
		"c_other": reflect.TypeFor[*any](),
	}

	for _, ct := range cts {
		if _, ok := ct.Nullable(); !ok {
			t.Errorf("%s: Nullable reported not ok", ct.Name())
		}
		if ct.DatabaseTypeName() == "" {
			t.Errorf("%s: empty DatabaseTypeName", ct.Name())
		}
		if got, w := ct.ScanType(), want[ct.Name()]; got != w {
			t.Errorf("%s: ScanType = %v, want %v", ct.Name(), got, w)
		}
	}
}

// TestUserAuthOmitStubs calls every no-op method of the !sqlite_userauth build
// (the default build) so the omit shim is covered. They all succeed because the
// userauth extension was removed upstream; this just pins the no-op contract.
func TestUserAuthOmitStubs(t *testing.T) {
	sc := openDirect(t, ":memory:")

	if err := sc.Authenticate("u", "p"); err != nil {
		t.Errorf("Authenticate: %v", err)
	}
	if err := sc.AuthUserAdd("u", "p", true); err != nil {
		t.Errorf("AuthUserAdd: %v", err)
	}
	if err := sc.AuthUserChange("u", "p", false); err != nil {
		t.Errorf("AuthUserChange: %v", err)
	}
	if err := sc.AuthUserDelete("u"); err != nil {
		t.Errorf("AuthUserDelete: %v", err)
	}
	if sc.AuthEnabled() {
		t.Error("AuthEnabled: expected false in the omit build")
	}

	// The unexported variants back the SQL-facing API and return SQLITE_OK (0).
	if got := sc.authenticate("u", "p"); got != 0 {
		t.Errorf("authenticate = %d, want 0", got)
	}
	if got := sc.authUserAdd("u", "p", 1); got != 0 {
		t.Errorf("authUserAdd = %d, want 0", got)
	}
	if got := sc.authUserChange("u", "p", 1); got != 0 {
		t.Errorf("authUserChange = %d, want 0", got)
	}
	if got := sc.authUserDelete("u"); got != 0 {
		t.Errorf("authUserDelete = %d, want 0", got)
	}
	if got := sc.authEnabled(); got != 0 {
		t.Errorf("authEnabled = %d, want 0", got)
	}
}
