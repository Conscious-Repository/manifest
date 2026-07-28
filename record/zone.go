package record

import "strings"

// Zone names — the vault's three lines (ARCHITECTURE §2): knowledge is the
// owner's hand-written thought, system is app-managed structure, extrinsic is
// content the owner did not originate (books, articles, papers).
const (
	ZoneKnowledge = "knowledge"
	ZoneSystem    = "system"
	ZoneExtrinsic = "extrinsic"
)

// UnderZoneRoot reports whether a vault-relative (slash) path is the zone
// root or anything inside it — matched by path prefix, never by base name.
// An empty root matches nothing (that zone is disabled).
func UnderZoneRoot(rel, root string) bool {
	if root == "" {
		return false
	}
	return rel == root || strings.HasPrefix(rel, root+"/")
}

// ZoneOf classifies a vault-relative (slash) path against the configured zone
// roots. Everything not under a configured root is knowledge — the owner's
// text is the default, structure is the exception.
func ZoneOf(rel, systemRoot, extrinsicRoot string) string {
	if UnderZoneRoot(rel, systemRoot) {
		return ZoneSystem
	}
	if UnderZoneRoot(rel, extrinsicRoot) {
		return ZoneExtrinsic
	}
	return ZoneKnowledge
}
