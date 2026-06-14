// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_vtable || vtable
// +build sqlite_vtable vtable

package sqlite3

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
)

// The upstream vtable test only ever emits int64 and string column values, so
// the SQLiteContext.Result* helpers for bool, blob, double, null, zeroblob, and
// the empty-text/empty-blob fast paths stay uncovered. This eponymous-only
// module returns one row whose columns exercise each Result* method exactly
// once.

type coverResultModule struct{}

type coverResultVTab struct{}

type coverResultCursor struct{ done bool }

func (coverResultModule) EponymousOnlyModule() {}

func (m coverResultModule) Create(c *SQLiteConn, _ []string) (VTab, error) {
	err := c.DeclareVTab(
		`CREATE TABLE x(
			c_bool_t, c_bool_f, c_blob, c_blob_empty, c_double,
			c_int, c_bigint, c_int64, c_null, c_text, c_text_empty, c_zeroblob
		)`,
	)
	if err != nil {
		return nil, err
	}
	return &coverResultVTab{}, nil
}

func (m coverResultModule) Connect(c *SQLiteConn, args []string) (VTab, error) {
	return m.Create(c, args)
}

func (coverResultModule) DestroyModule() {}

func (v *coverResultVTab) BestIndex(cst []InfoConstraint, _ []InfoOrderBy) (*IndexResult, error) {
	return &IndexResult{Used: make([]bool, len(cst))}, nil
}

func (v *coverResultVTab) Disconnect() error { return nil }
func (v *coverResultVTab) Destroy() error    { return nil }

func (v *coverResultVTab) Open() (VTabCursor, error) {
	return &coverResultCursor{}, nil
}

func (c *coverResultCursor) Close() error                    { return nil }
func (c *coverResultCursor) Filter(int, string, []any) error { c.done = false; return nil }
func (c *coverResultCursor) Next() error                     { c.done = true; return nil }
func (c *coverResultCursor) EOF() bool                       { return c.done }
func (c *coverResultCursor) Rowid() (int64, error)           { return 1, nil }

// Column maps each declared column to a distinct SQLiteContext.Result* call so
// every result helper — including the empty-text and empty-blob branches and
// ResultZeroblob — is exercised by a single SELECT.
func (c *coverResultCursor) Column(ctx *SQLiteContext, col int) error {
	switch col {
	case 0:
		ctx.ResultBool(true)
	case 1:
		ctx.ResultBool(false)
	case 2:
		ctx.ResultBlob([]byte{1, 2, 3})
	case 3:
		ctx.ResultBlob([]byte{}) // empty-blob fast path (nil data pointer)
	case 4:
		ctx.ResultDouble(2.5)
	case 5:
		ctx.ResultInt(42) // small int: SQLITE_result_int branch
	case 6:
		ctx.ResultInt(1 << 40) // > MaxInt32: widens to result_int64 on 64-bit
	case 7:
		ctx.ResultInt64(1 << 40)
	case 8:
		ctx.ResultNull()
	case 9:
		ctx.ResultText("hello")
	case 10:
		ctx.ResultText("") // empty-text fast path (placeholder pointer)
	case 11:
		ctx.ResultZeroblob(4)
	default:
		return fmt.Errorf("column index out of bounds: %d", col)
	}
	return nil
}

func TestVTabResultTypes(t *testing.T) {
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	sql.Register("sqlite3_cover_result", &SQLiteDriver{
		ConnectHook: func(conn *SQLiteConn) error {
			return conn.CreateModule("coverresult", coverResultModule{})
		},
	})
	db, err := sql.Open("sqlite3_cover_result", tempFilename)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var (
		boolT, boolF    int64
		blob, blobEmpty []byte
		dbl             float64
		i32             int64
		bigInt          int64
		i64v            int64
		null            any
		text, textEmpty string
		zeroblob        []byte
	)
	row := db.QueryRow(`SELECT c_bool_t, c_bool_f, c_blob, c_blob_empty, c_double,
		c_int, c_bigint, c_int64, c_null, c_text, c_text_empty, c_zeroblob FROM coverresult`)
	if err := row.Scan(
		&boolT, &boolF, &blob, &blobEmpty, &dbl,
		&i32, &bigInt, &i64v, &null, &text, &textEmpty, &zeroblob,
	); err != nil {
		t.Fatalf("scan: %v", err)
	}

	switch {
	case boolT != 1 || boolF != 0:
		t.Errorf("bool round-trip: t=%d f=%d", boolT, boolF)
	case string(blob) != "\x01\x02\x03":
		t.Errorf("blob = %v", blob)
	case len(blobEmpty) != 0:
		t.Errorf("empty blob len = %d", len(blobEmpty))
	case dbl != 2.5:
		t.Errorf("double = %v", dbl)
	case i32 != 42:
		t.Errorf("int = %d", i32)
	case bigInt != 1<<40:
		t.Errorf("big int = %d, want %d", bigInt, int64(1)<<40)
	case i64v != 1<<40:
		t.Errorf("int64 = %d", i64v)
	case null != nil:
		t.Errorf("null = %v", null)
	case text != "hello":
		t.Errorf("text = %q", text)
	case textEmpty != "":
		t.Errorf("empty text = %q", textEmpty)
	case len(zeroblob) != 4:
		t.Errorf("zeroblob len = %d, want 4", len(zeroblob))
	}
}
