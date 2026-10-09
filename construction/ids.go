// Package construction is Manifest's private construction research and design
// domain: durable construction problems bound to an existing Home or property
// subject, canonical millimetre assemblies, deterministic geometry, sections
// and exports, evidence, research checkpoints and human decisions.
//
// It owns validation, command reduction, geometry and record shapes. It does
// not own a database, a scheduler, a transcript or a source record: canonical
// documents are immutable revisions in a private artifacts.Registry, a
// per-problem head.json is the only mutable pointer, and the property/Home
// records it is bound to are only ever read (by the server).
package construction

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// Domain id kinds. An id is "<kind>-<32 lowercase hex>", generated once from
// crypto/rand and never derived from labels, positions, geometry or SKUs.
// Randomness is identity, not authorization.
const (
	KindProblem    = "cp"
	KindAssembly   = "asm"
	KindComponent  = "cmp"
	KindJunction   = "jct"
	KindMaterial   = "mat"
	KindProduct    = "prd"
	KindSource     = "src"
	KindClaim      = "clm"
	KindEvidence   = "evd"
	KindDecision   = "dec"
	KindIssue      = "iss"
	KindRun        = "run"
	KindOperation  = "op"
	KindView       = "view"
	KindAnnotation = "ann"
	KindQuestion   = "dq"
)

var idKinds = map[string]bool{
	KindProblem: true, KindAssembly: true, KindComponent: true, KindJunction: true,
	KindMaterial: true, KindProduct: true, KindSource: true, KindClaim: true,
	KindEvidence: true, KindDecision: true, KindIssue: true, KindRun: true,
	KindOperation: true, KindView: true, KindAnnotation: true, KindQuestion: true,
}

var idRE = regexp.MustCompile(`^([a-z]{2,4})-([0-9a-f]{32})$`)

// NewID mints a fresh id of the given kind.
func NewID(kind string) string {
	if !idKinds[kind] {
		panic("construction: unknown id kind " + kind)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return kind + "-" + hex.EncodeToString(b)
}

// ValidID reports whether id is a well-formed id of kind ("" = any kind).
func ValidID(kind, id string) bool {
	m := idRE.FindStringSubmatch(id)
	if m == nil || !idKinds[m[1]] {
		return false
	}
	return kind == "" || m[1] == kind
}

// IDKind returns the kind prefix of a well-formed id ("" otherwise).
func IDKind(id string) string {
	if !ValidID("", id) {
		return ""
	}
	return id[:strings.IndexByte(id, '-')]
}

var requestRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

// ValidRequestID is the shape of a client request identity (idempotency key).
func ValidRequestID(id string) bool { return requestRE.MatchString(id) }

// ValidToken reports a revision token / content hash: 64 lowercase hex.
func ValidToken(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
