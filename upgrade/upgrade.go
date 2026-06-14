// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

// Command upgrade regenerates the vendored SQLCipher amalgamation
// (sqlite3-binding.c, sqlite3-binding.h, sqlite3ext.h) from the SQLCipher
// source tree at a pinned release tag.
//
// Unlike vanilla SQLite, SQLCipher publishes no pre-built amalgamation download,
// so this tool clones the SQLCipher repository at the pinned tag and runs its
// build to produce sqlite3.c / sqlite3.h, then applies the same USE_LIBSQLITE3
// noop wrapper and sqlite3.h -> sqlite3-binding.h include rewrite that
// mattn/go-sqlite3 historically applied.
//
// Prerequisites: git, tclsh, and a C toolchain (the SQLCipher amalgamation build
// needs them). Run it from the repository root:
//
//	go run upgrade/upgrade.go
//
// The crypto provider is selected at compile time by the consuming package
// (-DSQLCIPHER_CRYPTO_OPENSSL in sqlite3_crypto_openssl.go); the amalgamation
// itself carries every provider, so no crypto choice is made here.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// sqlcipherTag is the pinned SQLCipher release the vendored amalgamation tracks.
// Bump this to upgrade; keep README.md and CLAUDE.md in step.
const (
	sqlcipherRepo = "https://github.com/sqlcipher/sqlcipher.git"
	sqlcipherTag  = "v4.14.0"
)

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("+ (%s) %s %v\n", dir, name, args)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// output runs a command and returns its trimmed stdout, for surfacing resolved
// state such as the cloned commit SHA.
func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// transform copies src to dst applying the vendoring wrapper: the file is
// guarded so that defining USE_LIBSQLITE3 turns it into a noop (for consumers
// who link the system library), and any `#include "sqlite3.h"` is rewritten to
// the vendored `#include "sqlite3-binding.h"` (plus a clang assert shim). When
// requireRewrite is set, src must contain at least one such include — guarding
// against an upstream include-format change silently producing a broken file.
func transform(src, dst string, requireRewrite bool) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	// Capture the close error on the writable handle: a failed final flush
	// would otherwise silently truncate the vendored amalgamation.
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err = io.WriteString(out, "#ifndef USE_LIBSQLITE3\n"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	rewrote := false
	for scanner.Scan() {
		text := scanner.Text()
		if text == `#include "sqlite3.h"` {
			rewrote = true
			text = `#include "sqlite3-binding.h"
#ifdef __clang__
#define assert(condition) ((void)0)
#endif
`
		}
		if _, err = fmt.Fprintln(out, text); err != nil {
			return err
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if _, err = io.WriteString(out, "#else // USE_LIBSQLITE3\n // If users really want to link against the system sqlite3 we\n// need to make this file a noop.\n #endif"); err != nil {
		return err
	}
	if requireRewrite && !rewrote {
		return fmt.Errorf(`%s: expected #include "sqlite3.h" not found; upstream include format may have changed`, src)
	}
	fmt.Printf("wrote %s\n", dst)
	return nil
}

func main() {
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}

// generate drives the regeneration end to end, returning an error instead of
// calling log.Fatal in helpers so the deferred temp-tree cleanup always runs.
func generate() (err error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("could not determine current file")
	}
	repoDir := filepath.Dir(filepath.Dir(file))

	tmp, err := os.MkdirTemp("", "sqlcipher-build-")
	if err != nil {
		return err
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil && err == nil {
			err = rerr
		}
	}()

	src := filepath.Join(tmp, "sqlcipher")
	fmt.Printf("Cloning SQLCipher %s into %s\n", sqlcipherTag, src)
	if err = run(tmp, "git", "clone", "--depth", "1", "--branch", sqlcipherTag, sqlcipherRepo, src); err != nil {
		return err
	}
	// Surface the resolved commit so the maintainer's diff review can confirm
	// what the pinned tag pointed at (git tags are server-side mutable).
	if head, herr := output(src, "git", "rev-parse", "HEAD"); herr == nil {
		fmt.Printf("Resolved %s to commit %s\n", sqlcipherTag, head)
	}

	// Generate the amalgamation. configure with no special crypto flags leaves
	// every crypto provider compiled in but gated by the consumer's
	// -DSQLCIPHER_CRYPTO_* define; `make sqlite3.c sqlite3.h` runs the
	// amalgamation generator (needs tclsh).
	if err = run(src, "./configure"); err != nil {
		return err
	}
	if err = run(src, "make", "sqlite3.c", "sqlite3.h"); err != nil {
		return err
	}

	// sqlite3ext.h is produced under the source tree, at the root or under src/.
	extSrc := filepath.Join(src, "sqlite3ext.h")
	if _, serr := os.Stat(extSrc); serr != nil {
		fallback := filepath.Join(src, "src", "sqlite3ext.h")
		if _, ferr := os.Stat(fallback); ferr != nil {
			return fmt.Errorf("sqlite3ext.h not found at %s or %s", extSrc, fallback)
		}
		extSrc = fallback
	}

	if err = transform(filepath.Join(src, "sqlite3.c"), filepath.Join(repoDir, "sqlite3-binding.c"), false); err != nil {
		return err
	}
	if err = transform(filepath.Join(src, "sqlite3.h"), filepath.Join(repoDir, "sqlite3-binding.h"), false); err != nil {
		return err
	}
	if err = transform(extSrc, filepath.Join(repoDir, "sqlite3ext.h"), true); err != nil {
		return err
	}

	fmt.Printf("Done. Vendored SQLCipher %s amalgamation into %s\n", sqlcipherTag, repoDir)
	return nil
}
