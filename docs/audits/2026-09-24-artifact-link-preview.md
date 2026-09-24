# Saved link artifact preview

Explicit link artifacts containing one HTTP(S) URL and .url Internet Shortcut files now show a destination preview in the artifact inspector. The complete normalized URL is the clickable label, including query and fragment; internationalized hostnames display their ASCII form. Credentials, unsupported protocols, ambiguous multiple targets and control/direction characters produce an explanatory source fallback. Source is limited to 16 KiB for this preview and retained byte-for-byte in its disclosure.

Inspection does not fetch the webpage, icon or metadata. Navigation requires clicking the destination and opens a new tab without an opener or referrer. The UI explains that review covers the saved link version, not live webpage contents. Existing immutable artifact review, source-line review and exact-version discussion remain available. Source disclosure restores with the selected version through workspace state.

Evidence: Node parser fixtures cover valid single URLs, BOM/CRLF shortcuts, URL query equals signs, ignored icon fields, international hostnames, invalid schemes, credentials, control characters, duplicate targets and limits. Real Chromium covers full destination, no outgoing destination/icon request, exact source, restored disclosure, version-bound review/discussion, unsupported fallback and both themes at 320/390/1440px. Phone screenshot inspected. Existing save-receipt browser fixture passed; build, syntax and diff checks passed.

This supports the named saved-link formats; it does not create a fetched webpage snapshot or claim that accepting a link approves its changing external content. Broader previews, adapter/context/approval coverage and integrated physical-device journeys remain open in the active workbench plan.

`make test` passed server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files. Review actions used mock APIs; no production reviews or external navigation occurred.
