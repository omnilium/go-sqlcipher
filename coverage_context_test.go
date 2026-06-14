// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// coverCtxDriverOnce guards the one-time registration of the helper driver that
// exposes a slow SQL function used to drive the context-interrupt paths.
var coverCtxDriverOnce sync.Once

func coverCtxDB(t *testing.T) *sql.DB {
	t.Helper()
	coverCtxDriverOnce.Do(func() {
		sql.Register("sqlite3_cover_ctx", &SQLiteDriver{
			ConnectHook: func(c *SQLiteConn) error {
				return c.RegisterFunc("cover_sleep", func() int64 {
					time.Sleep(300 * time.Millisecond)
					return 1
				}, true)
			},
		})
	})
	db, err := sql.Open("sqlite3_cover_ctx", ":memory:")
	if err != nil {
		t.Fatalf("open cover_ctx db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestContextCompletes drives the ctx-aware (ctx.Done() != nil) goroutine paths
// of SQLiteStmt.exec and SQLiteRows.Next with a live-but-uncancelled context, so
// the "operation completed" select arms run.
func TestContextCompletes(t *testing.T) {
	db := coverCtxDB(t)
	ctx := t.Context() // live, uncancelled context (Done() != nil)

	if _, err := db.ExecContext(ctx, "CREATE TABLE t (a INTEGER)"); err != nil {
		t.Fatalf("ExecContext create: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO t VALUES (1), (2), (3)"); err != nil {
		t.Fatalf("ExecContext insert: %v", err)
	}
	rows, err := db.QueryContext(ctx, "SELECT a FROM t ORDER BY a")
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d rows, want 3", n)
	}
}

// TestContextInterrupt cancels a context while a slow SQL function runs, driving
// the interrupt arm of SQLiteRows.Next / SQLiteStmt.exec and isInterruptErr.
func TestContextInterrupt(t *testing.T) {
	db := coverCtxDB(t)
	if _, err := db.Exec("CREATE TABLE t (a INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	t.Run("query", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		rows, err := db.QueryContext(ctx, "SELECT cover_sleep()")
		if err != nil {
			return // interrupted before returning rows — also acceptable
		}
		defer rows.Close()
		for rows.Next() {
		}
		if rows.Err() == nil {
			t.Error("expected a context error from the interrupted query")
		}
	})

	t.Run("exec", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if _, err := db.ExecContext(ctx, "UPDATE t SET a = cover_sleep()"); err == nil {
			t.Error("expected a context error from the interrupted exec")
		}
	})
}

// TestCallbackRuntimeConversionErrors covers the converter error paths reachable
// at call time: a variadic argument of the wrong type, and an any-returning
// function yielding a type with no SQLite mapping.
func TestCallbackRuntimeConversionErrors(t *testing.T) {
	sc := openDirect(t, ":memory:")
	if err := sc.RegisterFunc("v_i64", func(xs ...int64) int64 { return int64(len(xs)) }, true); err != nil {
		t.Fatalf("register v_i64: %v", err)
	}
	if err := sc.RegisterFunc("bad_ret", func() any { return complex128(0) }, true); err != nil {
		t.Fatalf("register bad_ret: %v", err)
	}

	queryStepErr(t, sc, "SELECT v_i64(1, 2.5)") // variadic float into int64 converter
	queryStepErr(t, sc, "SELECT bad_ret()")     // unsupported concrete return type
}

// TestCloseEdges covers the idempotent / already-closed branches of Close on the
// connection.
func TestCloseEdges(t *testing.T) {
	sc := openDirect(t, ":memory:")

	// Double Close: the second call hits the c.db == nil short-circuit.
	if err := sc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestTxCommitRollback exercises the SQLiteTx commit/rollback wrappers and the
// commit-after-rollback error surface through database/sql.
func TestTxCommitRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (a INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatalf("tx insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// Committing an already-resolved transaction must report an error.
	if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("Commit after Rollback: got %v, want ErrTxDone", err)
	}

	var n int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rolled-back insert persisted: count=%d", n)
	}
}
