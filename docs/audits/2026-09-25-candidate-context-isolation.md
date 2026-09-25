# Candidate context source isolation — September 25

One malformed or oversized candidate file previously failed the whole Records
candidate listing and prevented previewing unrelated healthy candidates. Source
reads now fail independently. An affected row shows its canonical filename and
an unavailable reason, with no source link or body. Preview and retention refuse
that row. Healthy records remain searchable and selectable. Repairing a source
is reflected on the next read; an unavailable recruiting directory still reports
a directory-level failure.

Bounded source reads also explicitly reject invalid UTF-8 and NUL bytes before
parsing frontmatter. Existing identity, exact-version and source-path checks stay
in force. No file is changed by browsing, and no invalid record becomes an empty
context snapshot.

Focused race tests cover malformed identity, oversized text, NUL and invalid
UTF-8 beside a healthy candidate, then source repair. Existing candidate identity,
native exact-version delivery, duplicate request, shared/private boundary and
symlink checks pass. The real Records browser fixture also passes its keyboard,
selection recovery, source routing and responsive checks. Build and diff checks
pass. Full-suite and live verification are recorded in the plan checkpoint.

This is recovery within candidate context. Organization/schedule context and the
other unchecked workbench requirements remain open.
