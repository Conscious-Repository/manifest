# Retain an exact native reply as an artifact

Completed private native replies now offer **Save output**. The transcript GET
projects only receipt-backed completed replies with a delivery ID, reply turn and
content hash. Projection writes nothing. The private POST
`/api/agents/chat/{agent}/sessions/{id}/output` re-reads the source, checks that
exact completed receipt/hash and retains its spoken reply text in the existing
artifact registry. Failed, interrupted, cancelled, shared, legacy unreceipted and
empty replies do not offer capture. The source conversation is unchanged.

The retained report records the canonical conversation key, native delivery ID,
reply author and supplied task/input artifact IDs. Delivery is a new optional
provenance field, preserved by later edits, rendered in the inspector and included
in private search. It is separate from harness run IDs; the original conversation
link leads back to the receipt and its exact supplied context.

Capture identity includes source conversation, delivery and reply hash. The
registry's `Retain` operation creates a snapshot once under its existing lock;
repeat capture returns its first revision even after an owner edit changes the
head. Existing conditional save/receipt behavior is unchanged. Save output opens
that exact revision in the inspector, allowing existing comparison, editing,
review and discussion controls. Late results/errors are fenced by conversation
and route identity. Public portal routers do not register the endpoint.

Validation:

- Race tests check explicit capture, original-version retry after an owner edit,
  source links, delivery search, unchanged source transcript, stale/wrong targets,
  public-route isolation and excluded states.
- Registry race tests cover concurrent retained reads after editing/renaming, unchanged
  head/history, delivery-only provenance and conflicting snapshot identity.
- Full-frontend Chromium exercises Save output, its exact delivery/hash request,
  opening the retained reply and inspecting delivery provenance; existing native
  interruption, thread switching and phone coding-result journeys also pass.
  The output-inspector screenshot was inspected; its surrounding conversation
  and unavailable draft-sync notice are fixture data.
- Full repository, syntax/build/diff and live results are in the plan checkpoint.

Scope: capture is an explicit owner action, not automatic output classification
or proof that every significant run produces a file. Provider-created files and
other adapters retain their existing paths. Inputs list artifact IDs; their exact
revision selections remain on the linked delivery receipt. Skill inventory is
still unreported by the native runner and is not inferred from installed files.
