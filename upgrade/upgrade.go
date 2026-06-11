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
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// sqlcipherTag is the pinned SQLCipher release the vendored amalgamation tracks.
// Bump this to upgrade; keep README.md and CLAUDE.md in step.
const (
	sqlcipherRepo = "https://github.com/sqlcipher/sqlcipher.git"
	sqlcipherTag  = "v4.14.0"
)

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("+ (%s) %s %v\n", dir, name, args)
	if err := cmd.Run(); err != nil {
		log.Fatalf("%s %v: %v", name, args, err)
	}
}

// transform copies src to dst applying the vendoring wrapper: the file is
// guarded so that defining USE_LIBSQLITE3 turns it into a noop (for consumers
// who link the system library), and any `#include "sqlite3.h"` is rewritten to
// the vendored `#include "sqlite3-binding.h"` (plus a clang assert shim).
func transform(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	if _, err := io.WriteString(out, "#ifndef USE_LIBSQLITE3\n"); err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		if text == `#include "sqlite3.h"` {
			text = `#include "sqlite3-binding.h"
#ifdef __clang__
#define assert(condition) ((void)0)
#endif
`
		}
		if _, err := fmt.Fprintln(out, text); err != nil {
			log.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	if _, err := io.WriteString(out, "#else // USE_LIBSQLITE3\n // If users really want to link against the system sqlite3 we\n// need to make this file a noop.\n #endif"); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", dst)
}

func main() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("could not determine current file")
	}
	repoDir := filepath.Dir(filepath.Dir(file))

	tmp, err := os.MkdirTemp("", "sqlcipher-build-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "sqlcipher")
	fmt.Printf("Cloning SQLCipher %s into %s\n", sqlcipherTag, src)
	run(tmp, "git", "clone", "--depth", "1", "--branch", sqlcipherTag, sqlcipherRepo, src)

	// Generate the amalgamation. configure with no special crypto flags leaves
	// every crypto provider compiled in but gated by the consumer's
	// -DSQLCIPHER_CRYPTO_* define; `make sqlite3.c sqlite3.h` runs the
	// amalgamation generator (needs tclsh).
	run(src, "./configure")
	run(src, "make", "sqlite3.c", "sqlite3.h")

	// sqlite3ext.h is produced under the source tree.
	extSrc := filepath.Join(src, "sqlite3ext.h")
	if _, err := os.Stat(extSrc); err != nil {
		extSrc = filepath.Join(src, "src", "sqlite3ext.h")
	}

	transform(filepath.Join(src, "sqlite3.c"), filepath.Join(repoDir, "sqlite3-binding.c"))
	transform(filepath.Join(src, "sqlite3.h"), filepath.Join(repoDir, "sqlite3-binding.h"))
	transform(extSrc, filepath.Join(repoDir, "sqlite3ext.h"))

	fmt.Printf("Done. Vendored SQLCipher %s amalgamation into %s\n", sqlcipherTag, repoDir)
}
