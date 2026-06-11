// Copyright (C) 2026 OMNILIUM ADVANCED CYBERNETICS SRL. All rights reserved.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build cgo
// +build cgo

package sqlite3

// OpenSSL is the SQLCipher crypto provider. This fork links SQLCipher against
// the system OpenSSL (libcrypto), which is a HARD DEPENDENCY at both build time
// and run time:
//
//   - Build: the OpenSSL development headers and libcrypto must be present
//     (e.g. libssl-dev / openssl-devel, or `brew install openssl` on macOS).
//   - Run: the resulting binary dynamically links libcrypto, so libcrypto must
//     be available on the target host. `ldd` on the binary will list it. Plan
//     your deployment/base image accordingly (e.g. a distroless image must
//     include libcrypto, not just libc).
//
// This is a deliberate trade-off: OpenSSL is SQLCipher's reference,
// widely-audited, AES-NI-accelerated backend. A fully self-contained
// (libcrypto-free) build was evaluated against a vendored libtomcrypt backend
// and dropped — libtomcrypt has not had a stable release since 2018 and its
// integration was not reliable here — so OpenSSL is the single supported
// provider.

/*
#cgo CFLAGS: -DSQLCIPHER_CRYPTO_OPENSSL
#cgo LDFLAGS: -lcrypto
#cgo openbsd LDFLAGS: -lcrypto
#cgo darwin CFLAGS: -I/opt/homebrew/opt/openssl/include
#cgo darwin LDFLAGS: -L/opt/homebrew/opt/openssl/lib
*/
import "C"
