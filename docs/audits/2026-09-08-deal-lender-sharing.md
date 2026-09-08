# Deal-scoped lender links

Implemented as a separate OODA listener route, outside team OAuth, with its own
password/session gate. Owner controls live in Manifest underwriting → Lender
links. Creation explicitly enables a live package for that selected deal;
new member expenses and linked documents update automatically. Default expiry
is 90 days, editable from 1–365 days. Revoke invalidates existing sessions at
the next request. The browser clears the rendered package on the next access
poll; previously downloaded documents cannot be recalled.

Passwords are generated from 24 cryptographically random bytes and shown once.
Only their SHA-256 hashes persist, in an atomically replaced mode-0600 file in
the application data directory. This fast hash is for generated 192-bit secrets,
not human-chosen passwords. Browser tokens use 32 random bytes; sessions last
eight hours, are memory-only, and are bound to a single link. Cookies are Secure,
HttpOnly, SameSite Strict and path-scoped. Revocation/expiry is checked on every
package and document request. Unlock attempts are bounded per link. Management
routes exist only on the private Manifest mux. No lender cookie is accepted as
a team identity. No email or lender-account changes were made.

Public JSON uses an allowlist: selected-deal facts, financial assumptions,
unit schedules, recorded expense fields, phase progress, scoped contracts,
document inventory and structured diligence fields. Internal correspondence,
corrections, follow-up notes, task chats and bank-import metadata are excluded.
Lender documents reuse the existing deal allowlist and vault path/symlink
checks. The renderer is shared with Manifest and the team portal. The public
shell has no team navigation or write controls. Content-derived asset versions
prevent a CDN-cached renderer from lagging future deployments.

Validation: full server and teamportal suites passed, shared JavaScript tests
passed, and new tests cover anonymous/wrong-password rejection, scoped data and
documents, cross-link sessions, denied team access, cross-deal revoke rejection,
durable revocation, expiry and private metadata exclusion. Live browser QA
created a temporary link, opened the password gate, unlocked the Rehab Quartet
package and confirmed no viewport overflow. Live HTTP QA confirmed member PDF
access, cross-deal 404 and revoked-session 404. Both temporary test links were
revoked; no active lender credentials were distributed.

Remaining broader objective items: owner decision on general-record budget
alignment versus separate scenarios, and supporting funding/title/permit/rent
reconciliations identified in the email audit. A password link supplies access;
it does not certify that these underlying diligence gaps are resolved.

Final requirement check: live membership includes 748 N Euclid plus all three
Bayard properties, display name is Rehab Quartet, and the communicated package
budget remains $1,258,500. The generic deal rate still reads 10% versus the
named ask's 6.25%; this is the pending scenario decision, not a completed
reconciliation. Shared document responses now sandbox active HTML/SVG content
so a linked attachment cannot execute scripts in the portal origin; a regression
test checks the served attachment policy. Full completion is not claimed while
financial-record alignment decisions remain outstanding.
