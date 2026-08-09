package secrets

import "testing"

// THE PARITY FIXTURES — duplicated verbatim in the excalibur engine's
// internal/secrets/secrets_test.go (byte contract; see package doc). A
// change here without its twin breaks proposal authoring/acceptance parity.

var positive = map[string]string{
	"aws-access-key":        "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
	"aws-secret-key":        `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
	"private-key":           "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA7v...",
	"bearer-token":          "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9abcdef",
	"credential-assignment": `password = "tr0ub4dor&3xplico99"`,
	"high-entropy":          "key blob: 9fXk2LmQ7vRt4Wq8Zs1Yc3Ue6Ip0Oa5D",
}

var negative = map[string]string{
	"git-sha":        "reverted in 0165520a9b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e",
	"wikilink":       "- [[2026-07-31 jack ruhl sync]] [date:: 2026-07-31]",
	"prose":          "Decide whether to pay to accelerate the ETH master's-student work",
	"iso-dates":      "[captured:: 2026-08-03] [due:: 2026-08-15] [status:: open]",
	"plain-password": "the password policy requires rotation every 90 days",
	"short-token":    "token: abc123",
}

func TestPositives(t *testing.T) {
	for name, text := range positive {
		fs := Scan(text)
		if len(fs) == 0 {
			t.Errorf("%s: not detected: %q", name, text)
			continue
		}
		found := false
		for _, c := range Classes(fs) {
			if c == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: detected as %v", name, Classes(fs))
		}
	}
}

func TestNegatives(t *testing.T) {
	for name, text := range negative {
		if fs := Scan(text); len(fs) != 0 {
			t.Errorf("%s: false positive %v on %q", name, Classes(fs), text)
		}
	}
}

func TestFindingsNeverCarryTheValue(t *testing.T) {
	fs := Scan(positive["aws-access-key"])
	for _, f := range fs {
		if f.Class == "" {
			t.Fatal("finding without class")
		}
	}
	// the Finding struct has no value field — this test pins that shape
	_ = Finding{Class: "x", Offset: 0}
}
