package construction

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ErrNonFinite is returned for NaN or ±Inf anywhere in a document.
var ErrNonFinite = errors.New("construction: non-finite number")

// Canonical encodes v as canonical JSON: object keys sorted, arrays kept in
// order (callers sort set-like id collections before encoding; ordered layers
// stay ordered), no insignificant whitespace, no HTML escaping, -0 written as
// 0 and every number in its shortest round-trip form. NaN/Inf are refused.
// Identical documents therefore have identical bytes on every platform, and
// a document's revision token is SHA-256 of these bytes (Token).
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		var unsupported *json.UnsupportedValueError
		if errors.As(err, &unsupported) {
			return nil, ErrNonFinite
		}
		return nil, err
	}
	return CanonicalizeJSON(raw)
}

// CanonicalizeJSON re-encodes arbitrary JSON bytes canonically.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var tree any
	if err := d.Decode(&tree); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("construction: trailing data after JSON value")
	}
	var b bytes.Buffer
	if err := writeCanonical(&b, tree); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		s, err := canonicalNumber(string(x))
		if err != nil {
			return err
		}
		b.WriteString(s)
	case string:
		writeString(b, x)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("construction: unexpected JSON value %T", v)
	}
	return nil
}

// canonicalNumber normalizes a JSON number literal: integers stay integers,
// everything else becomes the shortest float64 round-trip form, -0 → 0.
func canonicalNumber(s string) (string, error) {
	if !strings.ContainsAny(s, ".eE") {
		if s == "-0" {
			return "0", nil
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return s, nil
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return "", ErrNonFinite
	}
	if f == 0 {
		return "0", nil
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

func writeString(b *bytes.Buffer, s string) {
	var tmp bytes.Buffer
	e := json.NewEncoder(&tmp)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	b.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
}

// Token is the revision token of canonical bytes: SHA-256 hex.
func Token(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// TokenOf canonicalizes v and returns its bytes and token.
func TokenOf(v any) ([]byte, string, error) {
	b, err := Canonical(v)
	if err != nil {
		return nil, "", err
	}
	return b, Token(b), nil
}

// finite reports whether every float is finite (cheap pre-check for inputs).
func finite(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// normZero turns -0 into 0 so geometry never carries a signed zero.
func normZero(x float64) float64 {
	if x == 0 {
		return 0
	}
	return x
}

// sortedUnique returns the set-like id collection sorted with duplicates
// removed (blank entries dropped). Ordered collections never pass here.
func sortedUnique(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
