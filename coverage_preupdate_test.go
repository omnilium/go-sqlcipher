// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_preupdate_hook
// +build sqlite_preupdate_hook

package sqlite3

import (
	"database/sql"
	"testing"
)

// TestPreUpdateHookRowDecoding drives the pre-update hook accessors that the
// upstream test leaves uncovered: Depth, the FLOAT/BLOB/NULL arms of row(), and
// the wrong-operation guards (Old on INSERT, New on DELETE). The table mixes an
// integer, a real, a blob, text, and a NULL column so a single decode walks
// every storage-class branch.
func TestPreUpdateHookRowDecoding(t *testing.T) {
	type event struct {
		op    int
		depth int
		old   []any
		new   []any
	}
	var events []event

	sql.Register("sqlite3_cover_preupdate", &SQLiteDriver{
		ConnectHook: func(conn *SQLiteConn) error {
			conn.RegisterPreUpdateHook(func(d SQLitePreUpdateData) {
				ev := event{op: d.Op, depth: d.Depth()}

				switch d.Op {
				case SQLITE_INSERT:
					ev.new = make([]any, d.Count())
					if err := d.New(ev.new...); err != nil {
						t.Errorf("New on INSERT: %v", err)
					}
					// There is no old row for an INSERT.
					if err := d.Old(make([]any, d.Count())...); err == nil {
						t.Error("Old on INSERT: expected error, got nil")
					}
				case SQLITE_DELETE:
					ev.old = make([]any, d.Count())
					if err := d.Old(ev.old...); err != nil {
						t.Errorf("Old on DELETE: %v", err)
					}
					// There is no new row for a DELETE.
					if err := d.New(make([]any, d.Count())...); err == nil {
						t.Error("New on DELETE: expected error, got nil")
					}
				case SQLITE_UPDATE:
					ev.old = make([]any, d.Count())
					ev.new = make([]any, d.Count())
					if err := d.Old(ev.old...); err != nil {
						t.Errorf("Old on UPDATE: %v", err)
					}
					if err := d.New(ev.new...); err != nil {
						t.Errorf("New on UPDATE: %v", err)
					}
				}
				events = append(events, ev)
			})
			return nil
		},
	})

	db, err := sql.Open("sqlite3_cover_preupdate", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE t (i INTEGER, r REAL, b BLOB, x TEXT, n TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (7, 2.5, x'01020304', 'hello', NULL)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.Exec(`UPDATE t SET r = 3.5, x = 'world' WHERE i = 7`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM t WHERE i = 7`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 hook events, got %d", len(events))
	}

	insert := events[0]
	if insert.op != SQLITE_INSERT {
		t.Fatalf("first event op = %d, want INSERT", insert.op)
	}
	// Verify the decoded INSERT row covered every storage class: int, float,
	// blob, text (decoded as bytes), and NULL.
	if got, ok := insert.new[0].(int64); !ok || got != 7 {
		t.Errorf("new[0] = %v, want int64 7", insert.new[0])
	}
	if got, ok := insert.new[1].(float64); !ok || got != 2.5 {
		t.Errorf("new[1] = %v, want float64 2.5", insert.new[1])
	}
	if got, ok := insert.new[2].([]byte); !ok || string(got) != "\x01\x02\x03\x04" {
		t.Errorf("new[2] = %v, want blob 01020304", insert.new[2])
	}
	if got, ok := insert.new[3].([]byte); !ok || string(got) != "hello" {
		t.Errorf("new[3] = %v, want text 'hello'", insert.new[3])
	}
	if insert.new[4] != nil {
		t.Errorf("new[4] = %v, want nil (NULL)", insert.new[4])
	}

	if events[2].op != SQLITE_DELETE {
		t.Errorf("third event op = %d, want DELETE", events[2].op)
	}
}
