# Cancelled context sharing boundary — September 25

Sharing review previously collected artifact references from every native delivery
receipt. With targeted queue cancellation, that included retained context from
instructions that never entered the conversation. The file staging/publication
path could consequently grant team access to a never-sent artifact.

The review now excludes cancelled deliveries from artifact collection. Their
private receipt/text/context remains intact and bound into source revision checks;
it is not imported as team conversation history or staged as shared files. Other
reviewed history, origin and completed-delivery artifacts retain their existing
sharing behavior. Missing cancelled references cannot block a share because those
private inputs are not read for publication.

Race-tested AION and OODA publication fixtures create and cancel an instruction
containing a unique private artifact plus a missing reference. They verify an
unblocked review, no cancelled artifact in the manifest, successful publication
of normal history, no private text/hash/identity in team message records, no new
domain file ownership, no team file-picker entry, and refusal to select the file
as shared agent context. The source cancelled receipt remains unchanged.
Existing sharing review and publication recovery tests also pass. Full-suite,
build and deployment verification are in the workbench plan checkpoint.

This changes newly reviewed sharing manifests. Existing owner-reviewed immutable
sharing envelopes are not rewritten or silently revoked. Broader workbench
requirements remain open.
