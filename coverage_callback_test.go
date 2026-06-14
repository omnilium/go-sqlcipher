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
	"testing"
	"time"
)

// queryStepErr expects the statement to fail — either at prepare or when the
// first row is stepped (custom-function argument converters report their error
// during the step, not at prepare time).
func queryStepErr(t *testing.T, sc *SQLiteConn, q string) {
	t.Helper()
	rows, err := sc.Query(q, nil)
	if err != nil {
		return
	}
	dest := make([]driver.Value, len(rows.Columns()))
	if err := rows.Next(dest); err == nil {
		t.Errorf("expected error for %q, got none", q)
	}
	_ = rows.Close()
}

// queryStepOK expects the statement to run and produce its row(s).
func queryStepOK(t *testing.T, sc *SQLiteConn, q string) {
	t.Helper()
	rows, err := sc.Query(q, nil)
	if err != nil {
		t.Fatalf("Query %q: %v", q, err)
	}
	drainDriverRows(t, rows)
}

// TestCallbackArgConverterBranches drives every argument converter down both its
// accepting and rejecting paths by calling typed functions with matching and
// mismatching SQLITE argument types.
func TestCallbackArgConverterBranches(t *testing.T) {
	sc := openDirect(t, ":memory:")
	reg := func(name string, impl any) {
		if err := sc.RegisterFunc(name, impl, true); err != nil {
			t.Fatalf("RegisterFunc(%q): %v", name, err)
		}
	}
	reg("a_i64", func(i int64) int64 { return i })
	reg("a_bool", func(b bool) bool { return b })
	reg("a_f64", func(f float64) float64 { return f })
	reg("a_bytes", func(b []byte) int64 { return int64(len(b)) })
	reg("a_str", func(s string) int64 { return int64(len(s)) })
	reg("a_any", func(v any) any { return v })

	// Rejecting paths: each converter's type guard.
	queryStepErr(t, sc, "SELECT a_i64(1.5)")  // FLOAT -> int64 guard
	queryStepErr(t, sc, "SELECT a_bool('x')") // TEXT  -> bool guard
	queryStepErr(t, sc, "SELECT a_f64(1)")    // INT   -> float guard
	queryStepErr(t, sc, "SELECT a_bytes(1)")  // INT   -> bytes default
	queryStepErr(t, sc, "SELECT a_str(1)")    // INT   -> string default

	// Accepting paths, including the TEXT/BLOB alternates.
	queryStepOK(t, sc, "SELECT a_i64(7)")
	queryStepOK(t, sc, "SELECT a_bool(1)")
	queryStepOK(t, sc, "SELECT a_f64(2.5)")
	queryStepOK(t, sc, "SELECT a_bytes(x'01020304')") // BLOB branch
	queryStepOK(t, sc, "SELECT a_bytes('text')")      // TEXT branch
	queryStepOK(t, sc, "SELECT a_str('abc')")         // TEXT branch
	queryStepOK(t, sc, "SELECT a_str(x'010203')")     // BLOB branch

	// Generic converter across every SQLITE storage class.
	queryStepOK(t, sc, "SELECT a_any(42)")
	queryStepOK(t, sc, "SELECT a_any(3.5)")
	queryStepOK(t, sc, "SELECT a_any('s')")
	queryStepOK(t, sc, "SELECT a_any(x'00ff')")
	queryStepOK(t, sc, "SELECT a_any(NULL)")
}

// TestCallbackRetConverterBranches covers the return converters not reached by
// the simple-typed functions: the generic dispatch across concrete types, the
// float32 widening, and the unsigned-integer path.
func TestCallbackRetConverterBranches(t *testing.T) {
	sc := openDirect(t, ":memory:")
	reg := func(name string, impl any) {
		if err := sc.RegisterFunc(name, impl, true); err != nil {
			t.Fatalf("RegisterFunc(%q): %v", name, err)
		}
	}
	// func(any) any returns its argument, so the generic return converter
	// dispatches on whatever concrete type flows through.
	reg("r_any", func(v any) any { return v })
	reg("r_f32", func() float32 { return 1.5 })
	reg("r_u32", func() uint32 { return 7 })
	reg("r_nilany", func() any { return nil })

	queryStepOK(t, sc, "SELECT r_any(10)")      // -> integer
	queryStepOK(t, sc, "SELECT r_any(2.5)")     // -> float
	queryStepOK(t, sc, "SELECT r_any('txt')")   // -> text
	queryStepOK(t, sc, "SELECT r_any(x'0102')") // -> blob
	queryStepOK(t, sc, "SELECT r_any(NULL)")    // -> null (nil byte slice)
	queryStepOK(t, sc, "SELECT r_f32()")        // float32 widening
	queryStepOK(t, sc, "SELECT r_u32()")        // unsigned -> integer
	queryStepOK(t, sc, "SELECT r_nilany()")     // nil interface -> null
}

// TestBindValueTypes exercises bindValue/bind/bindText across every driver.Value
// kind the binder special-cases (nil, bool, int64, float64, string, []byte,
// time.Time, and the empty-string text path).
func TestBindValueTypes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE b (a, c, d, e, f, g, h DATETIME, i)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	when := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	_, err = db.Exec(
		`INSERT INTO b VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nil, true, int64(42), 3.5, "text", []byte{1, 2, 3}, when, "",
	)
	if err != nil {
		t.Fatalf("insert mixed types: %v", err)
	}

	var (
		a any
		c bool
		d int64
		e float64
		f string
		g []byte
		h time.Time
		i string
	)
	row := db.QueryRow(`SELECT a, c, d, e, f, g, h, i FROM b`)
	if err := row.Scan(&a, &c, &d, &e, &f, &g, &h, &i); err != nil {
		t.Fatalf("scan mixed types: %v", err)
	}
	if a != nil || !c || d != 42 || e != 3.5 || f != "text" || string(g) != "\x01\x02\x03" || !h.Equal(when) || i != "" {
		t.Fatalf("round-trip mismatch: a=%v c=%v d=%v e=%v f=%q g=%v h=%v i=%q", a, c, d, e, f, g, h, i)
	}
}

// TestBindNamedParams covers the named-parameter binding path (bindNamedIndices)
// and a wrong-argument-count error from bind.
func TestBindNamedParams(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE n (x INTEGER, y TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO n VALUES (:x, :y)`,
		sql.Named("x", 5), sql.Named("y", "five"),
	); err != nil {
		t.Fatalf("named insert: %v", err)
	}

	var x int
	var y string
	if err := db.QueryRow(`SELECT x, y FROM n WHERE x = :x`, sql.Named("x", 5)).Scan(&x, &y); err != nil {
		t.Fatalf("named query: %v", err)
	}
	if x != 5 || y != "five" {
		t.Fatalf("named round-trip: x=%d y=%q", x, y)
	}

	// A statement with a placeholder invoked with no argument must error.
	stmt, err := db.Prepare(`SELECT ?`)
	if err != nil {
		t.Fatalf("prepare placeholder: %v", err)
	}
	defer stmt.Close()
	if _, err := stmt.Query(); err == nil {
		t.Error("expected error querying a placeholder with no argument")
	}
}
