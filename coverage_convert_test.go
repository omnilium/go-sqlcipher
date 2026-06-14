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
	"time"
)

// coverScanner is a sql.Scanner used to drive convertAssign's Scanner branch.
type coverScanner struct {
	got any
}

func (s *coverScanner) Scan(src any) error {
	s.got = src
	return nil
}

// coverBlob / coverInt / coverStr are named types used to drive the reflect
// assignable / convertible / string-destination branches of convertAssign.
type (
	coverBlob []byte
	coverInt  int
	coverStr  string
)

// TestConvertAssignFastPaths walks every concrete (no-reflect) src/dest pair in
// convertAssign's leading type switch, including each nil-destination guard.
func TestConvertAssignFastPaths(t *testing.T) {
	when := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	var (
		s     string
		bs    []byte
		rb    sql.RawBytes
		iface any
		tm    time.Time
	)

	ok := func(label string, dest, src any) {
		t.Helper()
		if err := convertAssign(dest, src); err != nil {
			t.Errorf("%s: unexpected error: %v", label, err)
		}
	}

	// string source.
	ok("string->*string", &s, "abc")
	ok("string->*[]byte", &bs, "abc")
	ok("string->*RawBytes", &rb, "abc")

	// []byte source.
	ok("bytes->*string", &s, []byte("xy"))
	ok("bytes->*any", &iface, []byte("xy"))
	ok("bytes->*[]byte", &bs, []byte("xy"))
	ok("bytes->*RawBytes", &rb, []byte("xy"))

	// time.Time source.
	ok("time->*time", &tm, when)
	ok("time->*string", &s, when)
	ok("time->*[]byte", &bs, when)
	ok("time->*RawBytes", &rb, when)
	if !tm.Equal(when) {
		t.Fatalf("time round-trip: got %v want %v", tm, when)
	}

	// nil source.
	ok("nil->*any", &iface, nil)
	ok("nil->*[]byte", &bs, nil)
	ok("nil->*RawBytes", &rb, nil)
}

// TestConvertAssignNilPointerGuards drives each "destination pointer is nil"
// branch by passing typed nil pointers for every fast-path destination kind.
func TestConvertAssignNilPointerGuards(t *testing.T) {
	cases := map[string]struct {
		dest any
		src  any
	}{
		"string->nil *string":   {(*string)(nil), "x"},
		"string->nil *[]byte":   {(*[]byte)(nil), "x"},
		"string->nil *RawBytes": {(*sql.RawBytes)(nil), "x"},
		"bytes->nil *string":    {(*string)(nil), []byte("x")},
		"bytes->nil *any":       {(*any)(nil), []byte("x")},
		"bytes->nil *[]byte":    {(*[]byte)(nil), []byte("x")},
		"bytes->nil *RawBytes":  {(*sql.RawBytes)(nil), []byte("x")},
		"time->nil *[]byte":     {(*[]byte)(nil), time.Now()},
		"time->nil *RawBytes":   {(*sql.RawBytes)(nil), time.Now()},
		"nil->nil *any":         {(*any)(nil), nil},
		"nil->nil *[]byte":      {(*[]byte)(nil), nil},
		"nil->nil *RawBytes":    {(*sql.RawBytes)(nil), nil},
	}
	for name, c := range cases {
		if err := convertAssign(c.dest, c.src); err == nil {
			t.Errorf("%s: expected a nil-pointer error, got nil", name)
		}
	}
}

// TestConvertAssignReflectPaths drives the reflection-based half of
// convertAssign: numeric->string, asBytes, *bool, *any, the Scanner branch, the
// assignable/convertible branches, pointer indirection, and the string-mediated
// numeric conversions with both success and parse-error outcomes.
func TestConvertAssignReflectPaths(t *testing.T) {
	ok := func(label string, dest, src any) {
		t.Helper()
		if err := convertAssign(dest, src); err != nil {
			t.Errorf("%s: unexpected error: %v", label, err)
		}
	}
	bad := func(label string, dest, src any) {
		t.Helper()
		if err := convertAssign(dest, src); err == nil {
			t.Errorf("%s: expected error, got nil", label)
		}
	}

	// numeric source into *string via asString — one case per reflect.Kind arm.
	var s string
	ok("int64->*string", &s, int64(123))
	ok("uint->*string", &s, uint(7))
	ok("float64->*string", &s, 1.5)
	ok("float32->*string", &s, float32(2.5))
	ok("bool->*string", &s, true)

	// numeric source into *[]byte / *RawBytes via asBytes — one per arm.
	var bs []byte
	var rb sql.RawBytes
	ok("int->*[]byte", &bs, 9)
	ok("uint->*[]byte", &bs, uint(9))
	ok("float32->*[]byte", &bs, float32(2.5))
	ok("float64->*[]byte", &bs, 1.5)
	ok("bool->*RawBytes", &rb, true)
	ok("named-string->*[]byte", &bs, coverStr("named")) // asBytes String arm

	// *bool via driver.Bool, success and failure.
	var b bool
	ok("int64->*bool", &b, int64(1))
	bad("garbage->*bool", &b, "not-a-bool")

	// *any catch-all.
	var iface any
	ok("anything->*any", &iface, struct{ X int }{1})

	// sql.Scanner destination.
	var sc coverScanner
	ok("->Scanner", &sc, int64(7))
	if sc.got != int64(7) {
		t.Errorf("scanner got %v, want 7", sc.got)
	}

	// assignable []byte clone branch (named []byte type).
	var cb coverBlob
	ok("bytes->named []byte", &cb, []byte{1, 2, 3})
	if len(cb) != 3 {
		t.Errorf("coverBlob len = %d, want 3", len(cb))
	}

	// convertible same-kind branch (int -> named int).
	var ci coverInt
	ok("int->named int", &ci, 5)
	if ci != 5 {
		t.Errorf("coverInt = %d, want 5", ci)
	}

	// string-destination reflect branch (named string from string and []byte).
	var cs coverStr
	ok("string->named string", &cs, "hi")
	ok("bytes->named string", &cs, []byte("yo"))

	// pointer indirection: allocate through a **int, and the nil-source zeroing.
	var pp *int
	ok("int->**int", &pp, int64(9))
	if pp == nil || *pp != 9 {
		t.Errorf("**int indirection failed: %v", pp)
	}
	pp = new(int)
	ok("nil->**int zeroes", &pp, nil)
	if pp != nil {
		t.Errorf("nil source should zero the pointer, got %v", pp)
	}

	// string-mediated numeric conversions, success and parse error.
	var i int
	ok("string->int", &i, "42")
	bad("garbage->int", &i, "nope")
	var u uint
	ok("string->uint", &u, "42")
	bad("garbage->uint", &u, "-1")
	var f float64
	ok("string->float", &f, "2.5")
	bad("garbage->float", &f, "xyz")

	// A struct source forced through the numeric reflect arm drives asString's
	// fmt.Sprintf fallback (and asBytes' false-returning default).
	var iDefault int
	bad("struct->int (asString default)", &iDefault, struct{ X int }{1})
	var bytesDefault []byte
	bad("struct->[]byte (asBytes default)", &bytesDefault, struct{ X int }{1})

	// unsupported destination and non-pointer destination.
	var st struct{ X int }
	bad("int->*struct", &st, int64(1))
	bad("non-pointer dest", 5, "x")
}
