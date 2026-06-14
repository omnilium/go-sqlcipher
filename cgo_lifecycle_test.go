// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"strings"
	"testing"
)

// TestStripDriverQueryParams checks that the driver's own "_"-prefixed query
// parameters (which carry secrets like _key and are meaningless to SQLite) are
// removed from a file: URI before it reaches sqlite3_open_v2, while
// SQLite-recognized URI parameters and their exact encoding are preserved.
func TestStripDriverQueryParams(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "strips _key keeps sqlite params",
			dsn:  "file:test.db?_key=secret&cache=shared&mode=ro",
			want: "file:test.db?cache=shared&mode=ro",
		},
		{
			name: "all driver params removed collapses to base",
			dsn:  "file:test.db?_key=x'AB'&_cipher_page_size=4096&_kdf_iter=64000",
			want: "file:test.db",
		},
		{
			name: "preserves order and encoding of kept params",
			dsn:  "file:t.db?mode=ro&_key=s&vfs=unix-none&_loc=auto",
			want: "file:t.db?mode=ro&vfs=unix-none",
		},
		{
			name: "no driver params is unchanged",
			dsn:  "file:t.db?cache=shared&immutable=1",
			want: "file:t.db?cache=shared&immutable=1",
		},
		{
			name: "strips auth secrets too",
			dsn:  "file:t.db?_auth&_auth_user=admin&_auth_pass=hunter2&cache=private",
			want: "file:t.db?cache=private",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := strings.IndexRune(tc.dsn, '?')
			got := stripDriverQueryParams(tc.dsn, pos)
			if got != tc.want {
				t.Fatalf("stripDriverQueryParams(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}
}

// TestCustomFuncPanicReturnsError verifies a panic inside a registered custom
// function is converted to a SQL error instead of unwinding across the cgo
// boundary and aborting the process.
func TestCustomFuncPanicReturnsError(t *testing.T) {
	sc := openDirect(t, ":memory:")
	if err := sc.RegisterFunc("go_panic", func() int {
		panic("boom from custom function")
	}, true); err != nil {
		t.Fatalf("RegisterFunc: %v", err)
	}
	queryStepErr(t, sc, "SELECT go_panic()")
}

// TestAggregatorPanicReturnsError verifies a panic inside a registered
// aggregator's Step is converted to a SQL error rather than crashing the
// process through the cgo boundary.
func TestAggregatorPanicReturnsError(t *testing.T) {
	sc := openDirect(t, ":memory:")
	if err := sc.RegisterAggregator("go_panic_agg", func() *panicAggregator {
		return &panicAggregator{}
	}, true); err != nil {
		t.Fatalf("RegisterAggregator: %v", err)
	}
	if _, err := sc.Exec("CREATE TABLE t(x)", nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := sc.Exec("INSERT INTO t VALUES (1)", nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	queryStepErr(t, sc, "SELECT go_panic_agg(x) FROM t")
}

type panicAggregator struct{}

func (a *panicAggregator) Step(int64)  { panic("boom from aggregator step") }
func (a *panicAggregator) Done() int64 { return 0 }

// TestDeleteHandleRemovesSingleEntry verifies deleteHandle removes exactly the
// one targeted handle and leaves the rest of the table intact.
func TestDeleteHandleRemovesSingleEntry(t *testing.T) {
	c := &SQLiteConn{}
	p1 := newHandle(c, "a")
	p2 := newHandle(c, "b")
	defer deleteHandle(p2)

	deleteHandle(p1)

	if v := lookupHandleVal(p1); v.val != nil {
		t.Fatalf("handle p1 still present after deleteHandle: %#v", v.val)
	}
	if v := lookupHandleVal(p2); v.val != "b" {
		t.Fatalf("handle p2 unexpectedly affected: got %#v, want \"b\"", v.val)
	}
}
