// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
)

// --- aggregator shapes used to drive RegisterAggregator's validation ---

type aggValid struct{ sum int64 }

func (a *aggValid) Step(v int64) { a.sum += v }
func (a *aggValid) Done() int64  { return a.sum }
func newAggValidVal() aggValid   { return aggValid{} } // value, not pointer/interface

type aggNoStep struct{}

func (aggNoStep) Done() int64 { return 0 }

type aggStepBadRet struct{}

func (aggStepBadRet) Step() (int, int) { return 0, 0 }
func (aggStepBadRet) Done() int64      { return 0 }

type aggStep1NotErr struct{}

func (aggStep1NotErr) Step() int   { return 0 }
func (aggStep1NotErr) Done() int64 { return 0 }

type aggStepBadArg struct{}

func (aggStepBadArg) Step(complex128) {}
func (aggStepBadArg) Done() int64     { return 0 }

type aggNoDone struct{}

func (aggNoDone) Step() {}

type aggDoneArgs struct{}

func (aggDoneArgs) Step()          {}
func (aggDoneArgs) Done(int) int64 { return 0 }

type aggDoneNoRet struct{}

func (aggDoneNoRet) Step() {}
func (aggDoneNoRet) Done() {}

type aggDoneBadRet struct{}

func (aggDoneBadRet) Step()            {}
func (aggDoneBadRet) Done() complex128 { return 0 }

// TestRegisterFuncErrors covers RegisterFunc's input validation branches.
func TestRegisterFuncErrors(t *testing.T) {
	sc := openDirect(t, ":memory:")
	cases := map[string]any{
		"non_function":           5,
		"too_many_returns":       func() (int, int, int) { return 0, 0, 0 },
		"second_not_error":       func() (int, int) { return 0, 0 },
		"bad_arg_type":           func(complex128) int64 { return 0 },
		"bad_return_type":        func() complex128 { return 0 },
		"bad_variadic_type":      func(...complex128) int64 { return 0 },
		"iface_with_methods":     func(fmt.Stringer) int64 { return 0 },
		"slice_return_not_bytes": func() []string { return nil },
	}
	for name, impl := range cases {
		if err := sc.RegisterFunc(name, impl, true); err == nil {
			t.Errorf("%s: expected RegisterFunc error, got nil", name)
		}
	}
}

// TestRegisterAggregatorErrors covers RegisterAggregator's validation branches.
func TestRegisterAggregatorErrors(t *testing.T) {
	sc := openDirect(t, ":memory:")
	cases := map[string]any{
		"non_function":       5,
		"too_many_returns":   func() (int, int, int) { return 0, 0, 0 },
		"second_not_error":   func() (int, int) { return 0, 0 },
		"constructor_args":   func(int) *aggValid { return nil },
		"not_pointer":        newAggValidVal,
		"no_step":            func() *aggNoStep { return &aggNoStep{} },
		"step_bad_returns":   func() *aggStepBadRet { return &aggStepBadRet{} },
		"step_ret_not_error": func() *aggStep1NotErr { return &aggStep1NotErr{} },
		"step_bad_arg":       func() *aggStepBadArg { return &aggStepBadArg{} },
		"no_done":            func() *aggNoDone { return &aggNoDone{} },
		"done_args":          func() *aggDoneArgs { return &aggDoneArgs{} },
		"done_no_returns":    func() *aggDoneNoRet { return &aggDoneNoRet{} },
		"done_bad_return":    func() *aggDoneBadRet { return &aggDoneBadRet{} },
	}
	for name, impl := range cases {
		if err := sc.RegisterAggregator(name, impl, true); err == nil {
			t.Errorf("%s: expected RegisterAggregator error, got nil", name)
		}
	}
}

// --- aggregators that register cleanly but fail at run time ---

type aggRuntime struct{}

func (aggRuntime) Step(int64)  {}
func (aggRuntime) Done() int64 { return 0 }

type aggStepErr struct{}

func (aggStepErr) Step(int64) error { return errors.New("step boom") }
func (aggStepErr) Done() int64      { return 0 }

type aggDoneErr struct{}

func (aggDoneErr) Step(int64)           {}
func (aggDoneErr) Done() (int64, error) { return 0, errors.New("done boom") }

// TestAggregatorRuntimeErrors covers the agg()/Step()/Done() error reporting
// paths: a constructor returning nil, a constructor returning an error, a Step
// returning an error, and a Done returning an error.
func TestAggregatorRuntimeErrors(t *testing.T) {
	run := func(name string, ctor any) error {
		sc := openDirect(t, ":memory:")
		if err := sc.RegisterAggregator(name, ctor, true); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		if _, err := sc.Exec("CREATE TABLE t (v INTEGER)", nil); err != nil {
			t.Fatalf("create for %s: %v", name, err)
		}
		if _, err := sc.Exec("INSERT INTO t VALUES (1)", nil); err != nil {
			t.Fatalf("insert for %s: %v", name, err)
		}
		rows, err := sc.Query("SELECT "+name+"(v) FROM t", nil)
		if err != nil {
			return err
		}
		// The aggregate runs while the single result row is stepped.
		dest := make([]driver.Value, len(rows.Columns()))
		err = rows.Next(dest)
		_ = rows.Close()
		return err
	}

	cases := map[string]any{
		"agg_ctor_err": func() (*aggRuntime, error) { return nil, errors.New("ctor boom") },
		"agg_ctor_nil": func() *aggRuntime { return nil },
		"agg_step_err": func() *aggStepErr { return &aggStepErr{} },
		"agg_done_err": func() *aggDoneErr { return &aggDoneErr{} },
	}
	for name, ctor := range cases {
		if err := run(name, ctor); err == nil {
			t.Errorf("%s: expected a runtime error, got nil", name)
		}
	}
}

// TestHookRemoval covers the nil-callback (removal) branch of each connection
// hook registrar.
func TestHookRemoval(t *testing.T) {
	sc := openDirect(t, ":memory:")
	sc.RegisterCommitHook(func() int { return 0 })
	sc.RegisterCommitHook(nil)
	sc.RegisterRollbackHook(func() {})
	sc.RegisterRollbackHook(nil)
	sc.RegisterUpdateHook(func(int, string, string, int64) {})
	sc.RegisterUpdateHook(nil)
	sc.RegisterAuthorizer(func(int, string, string, string) int { return SQLITE_OK })
	sc.RegisterAuthorizer(nil)
}

// TestConnErrorPaths covers driver error branches: malformed SQL through
// Exec/Query, a failed extension load, and a database path that cannot be
// created.
func TestConnErrorPaths(t *testing.T) {
	sc := openDirect(t, ":memory:")
	if _, err := sc.Exec("THIS IS NOT SQL", nil); err == nil {
		t.Error("Exec of bad SQL: expected error")
	}
	if _, err := sc.Query("ALSO NOT SQL", nil); err == nil {
		t.Error("Query of bad SQL: expected error")
	}
	if err := sc.LoadExtension("/nonexistent/extension.so", "entry"); err == nil {
		t.Error("LoadExtension of missing library: expected error")
	}

	// A path under a nonexistent directory cannot be created (SQLITE_CANTOPEN),
	// which also drives lastError's system-errno branch.
	if _, err := (&SQLiteDriver{}).Open("/no/such/dir/cannot.db"); err == nil {
		t.Error("Open of uncreatable path: expected error")
	}
}
