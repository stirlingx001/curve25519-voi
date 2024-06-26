// Copyright (c) 2016 The Go Authors. All rights reserved.
// Copyright (c) 2019-2021 Oasis Labs Inc. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//   * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Package x25519 provides an implementation of the X25519 function, which
// performs scalar multiplication on the elliptic curve known as Curve25519.
// See RFC 7748.
package x25519

import (
	"crypto/sha512"
	"crypto/subtle"
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/curve"
	"github.com/oasisprotocol/curve25519-voi/curve/scalar"
	_ "github.com/oasisprotocol/curve25519-voi/internal/toolchain"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
)

const (
	// ScalarSize is the size of the scalar input to X25519.
	ScalarSize = 32
	// PointSize is the size of the point input to X25519.
	PointSize = 32
)

// Basepoint is the canonical Curve25519 generator.
var Basepoint []byte

var basePoint = [32]byte{9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

// ScalarMult sets dst to the product in*base where dst and base are the x
// coordinates of group points and all values are in little-endian form.
//
// Deprecated: when provided a low-order point, ScalarMult will set dst to all
// zeroes, irrespective of the scalar. Instead, use the X25519 function, which
// will return an error.
func ScalarMult(dst, in, base *[32]byte) {
	var ec [ScalarSize]byte
	copy(ec[:], in[:])
	clampScalar(ec[:])

	var s scalar.Scalar
	if _, err := s.SetBits(ec[:]); err != nil {
		panic("x25519: failed to deserialize scalar: " + err.Error())
	}

	var montP curve.MontgomeryPoint
	if _, err := montP.SetBytes(base[:]); err != nil {
		panic("x25519: failed to deserialize point: " + err.Error())
	}

	montP.Mul(&montP, &s)
	copy(dst[:], montP[:])
}

// ScalarBaseMult sets dst to the product in*base where dst and base are
// the x coordinates of group points, base is the standard generator and
// all values are in little-endian form.
//
// It is recommended to use the X25519 function with Basepoint instead, as
// copying into fixed size arrays can lead to unexpected bugs.
func ScalarBaseMult(dst, in *[32]byte) {
	// There is no codepath to use `x/crypto/curve25519`'s version
	// as none of the targets use a precomputed implementation.

	var ec [ScalarSize]byte
	copy(ec[:], in[:])
	clampScalar(ec[:])

	var s scalar.Scalar
	if _, err := s.SetBits(ec[:]); err != nil {
		panic("x25519: failed to deserialize scalar: " + err.Error())
	}

	var (
		edP   curve.EdwardsPoint
		montP curve.MontgomeryPoint
	)
	montP.SetEdwards(edP.MulBasepoint(curve.ED25519_BASEPOINT_TABLE, &s))

	copy(dst[:], montP[:])
}

// X25519 returns the result of the scalar multiplication (scalar * point),
// according to RFC 7748, Section 5. scalar, point and the return value are
// slices of 32 bytes.
//
// scalar can be generated at random, for example with crypto/rand. point should
// be either Basepoint or the output of another X25519 call.
//
// If point is Basepoint (but not if it's a different slice with the same
// contents) a precomputed implementation might be used for performance.
func X25519(scalar, point []byte) ([]byte, error) {
	// Outline the body of function, to let the allocation be inlined in the
	// caller, and possibly avoid escaping to the heap.
	var dst [PointSize]byte
	return x25519(&dst, scalar, point)
}

func x25519(dst *[PointSize]byte, scalar, point []byte) ([]byte, error) {
	var in [ScalarSize]byte
	if l := len(scalar); l != ScalarSize {
		return nil, fmt.Errorf("bad scalar length: %d, expected %d", l, ScalarSize)
	}
	if l := len(point); l != PointSize {
		return nil, fmt.Errorf("bad point length: %d, expected %d", l, PointSize)
	}
	copy(in[:], scalar)
	if &point[0] == &Basepoint[0] {
		checkBasepoint()
		ScalarBaseMult(dst, &in)
	} else {
		var base, zero [PointSize]byte
		copy(base[:], point)
		ScalarMult(dst, &in, &base)
		if subtle.ConstantTimeCompare(dst[:], zero[:]) == 1 {
			return nil, fmt.Errorf("bad input point: low order point")
		}
	}
	return dst[:], nil
}

// EdPrivateKeyToX25519 converts an Ed25519 private key into a corresponding
// X25519 private key such that the resulting X25519 public key will equal
// the result from EdPublicKeyToX25519.
func EdPrivateKeyToX25519(privateKey ed25519.PrivateKey) []byte {
	h := sha512.New()
	_, _ = h.Write(privateKey[:32])
	digest := h.Sum(nil)
	h.Reset()

	clampScalar(digest)

	dst := make([]byte, ScalarSize)
	copy(dst, digest)

	return dst
}

// EdPublicKeyToX25519 converts an Ed25519 public key into the X25519 public
// key that would be generated from the same private key.
func EdPublicKeyToX25519(publicKey ed25519.PublicKey) ([]byte, bool) {
	var aCompressed curve.CompressedEdwardsY
	if _, err := aCompressed.SetBytes(publicKey); err != nil {
		return nil, false
	}

	var A curve.EdwardsPoint
	if _, err := A.SetCompressedY(&aCompressed); err != nil {
		return nil, false
	}

	var montA curve.MontgomeryPoint
	montA.SetEdwards(&A)

	return montA[:], true
}

func clampScalar(s []byte) {
	s[0] &= 248
	s[31] &= 127
	s[31] |= 64
}

func checkBasepoint() {
	if subtle.ConstantTimeCompare(Basepoint, []byte{
		0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}) != 1 {
		panic("x25519: global Basepoint value was modified")
	}
}

func init() {
	Basepoint = basePoint[:]
}
