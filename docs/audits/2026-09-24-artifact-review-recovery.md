# Review decision recovery

Pending artifact review submissions now freeze their decision, notes and immutable range while awaiting a receipt. Input changes, hunk/record selection and repeated submit calls cannot start another decision while the first is unresolved. The exact request is saved in browser storage before POST; storage failure prevents submission.

Reopening loads review history and matches the pending request ID, revision, state, note and range. A matching receipt marks the decision recorded without POST. A missing receipt offers explicit retry with the original request bytes and ID; uncertain responses retain the lock. Definitive validation rejection or stale-history conflict unlocks correction, preserving notes through reopening. A later corrected decision gets a fresh identity.

Recovered change requests offer an explicit draft action and do not automatically append text to a conversation. That action remains available through another reopening until used or replaced by a new decision. An acknowledgment arriving after its pane closes retains recovery information rather than drafting into another conversation. No agent message is sent by these controls.

## Evidence

Real Chromium recovery fixture covers loss before and after recording, receipt-only reopening, a second reopening before drafting, exact-ID retry, in-flight double submission and hunk-selection attempts, stale conflict correction/reopening and browser-storage failure with no POST. Existing review fixture covers exact ranges, lost-response retry, detached-pane isolation and 320/390/1440px layout. Existing hunk, table and metadata browser fixtures passed. JS syntax, diff checks and release build passed.

This closes local pending-decision recovery gaps. Unfinished review forms still use browser-local storage; this does not certify cross-device review draft synchronization or atomic composer handoff. The broader workbench recovery and integrated acceptance requirements remain active.

`make test` passed the server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files. Browser actions used fixture APIs, not real owner review records or provider messages.
