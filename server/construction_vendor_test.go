package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
)

// The construction renderer's three.js is pinned by content: these are the
// bytes extracted from the npm registry tarball three@0.180.0 whose SHA-512
// matches the registry integrity (vendor/three-0.180.0/PROVENANCE.md). An edit,
// upgrade or truncation of a vendored file fails here, not in a browser.
func TestConstructionVendoredThreePinned(t *testing.T) {
	want := map[string]string{
		"web/vendor/three-0.180.0/three.module.min.js": "e2b5ee6bccd38fd6d8a2428546b83c5f2426d84b152ef82be8055556e3b40eb6",
		"web/vendor/three-0.180.0/three.core.min.js":   "61ba0df005b05991361d040d8ff670e1aadfd0ce7aeebd1fdb0725957a8957de",
		"web/vendor/three-0.180.0/LICENSE":             "bfe119ea4fd413f5f7ca3fcd63adb0c4a073ed39daa2fe7d3e6b769e21272601",
	}
	for name, sum := range want {
		b, err := fs.ReadFile(webFiles, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := sha256.Sum256(b)
		if hex.EncodeToString(got[:]) != sum {
			t.Fatalf("%s changed: sha256 %x, pinned %s", name, got, sum)
		}
	}
	// the module's only import is its sibling core build — no bare
	// specifier, no URL, nothing that could resolve off-origin
	mod, _ := fs.ReadFile(webFiles, "web/vendor/three-0.180.0/three.module.min.js")
	if strings.Count(string(mod), `from"./three.core.min.js"`) == 0 || strings.Contains(string(mod), "https://") {
		t.Fatal("three.module.min.js must import only ./three.core.min.js")
	}
	lic, _ := fs.ReadFile(webFiles, "web/vendor/three-0.180.0/LICENSE")
	if !strings.Contains(string(lic), "The MIT License") {
		t.Fatal("LICENSE must carry the MIT text")
	}
}
