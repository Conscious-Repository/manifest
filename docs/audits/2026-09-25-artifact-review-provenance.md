# Artifact review provenance — September 25

Exact-revision review now requests the existing verified source-link projection
and shows an expandable Origin and version details panel. It separates artifact
source/conversation/run/task identity from the selected revision's recorded actor,
timestamp, version number and hash. Links use the same identity resolution as
Files, including distinct conversation and producing execution routes. Missing
metadata says Not recorded; an unavailable recorded source retains its identity
and is not relinked by title. This presents existing provenance; it does not
invent missing producer metadata or add automatic capture for another adapter.

The private artifact-get endpoint adds sources only when requested. Existing
content/version lookup and source-link privacy/identity rules apply unchanged.
The panel survives the Markdown fallback; normal comparisons and editor modes
retain their established behavior. Recorded version actors describe the registry
record, not an assertion that the actor authored every byte.

Focused server race tests cover exact-preview source links alongside existing
same-title conversation/execution isolation. Chromium verifies historical actor
switching without changing origin, source navigation, unavailable-source labels,
and 320/390/1440px bounds; the phone screenshot was inspected. Existing generic
metadata review and save-receipt fixtures pass. Full-suite/build/deployment
results are recorded in the plan checkpoint. Other unchecked workbench scope,
including automatic provenance across all producers, remains open.

The older text-editor fixture required a system Chrome installation and loaded a
component slice that omitted current preview helpers. It now uses bundled
Chromium by default and loads the complete component file. Its edit/compare/save,
exact-version discussion and restored editor selection/scroll checks pass.
