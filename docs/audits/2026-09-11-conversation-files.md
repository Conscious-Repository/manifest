# Conversation file discovery

The existing artifact-list endpoint now accepts a complete conversation backend/agent/id filter. The server resolves the existing provenance scope, including continuation inheritance; missing, mismatched or incomplete identities fail instead of returning the global list. Registry filtering uses exact session provenance, never matching titles or folder names.

A persistent Files tab lists conversation outputs alongside explicitly linked task inputs/outputs, deduplicated by artifact identity. Compact rows expose title, type, revision count and run/path metadata; clicking opens the revision listed. Filtering, empty/failure states, manual refresh and view restoration are supported. Closing aborts outstanding requests. Existing owner-only artifact routes and preview/edit/review components remain authoritative.

Validation: full Go tests and build pass; focused identity/continuation/agent tests; actual Chromium workspace tests cover scoped request, filter restoration, exact revision callback, empty filter and phone bounds. Phone screenshot inspected. No real provider calls or deployment.

This closes discovery for registered artifacts. It does not establish automatic registration of every provider-generated file, complete artifact activity provenance, or the remaining full-plan release gates.
