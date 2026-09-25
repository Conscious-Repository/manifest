# Research browser journey with real persistence endpoints

`TestResearchRevisionBrowserWithBackend` starts a temporary Go HTTP server with
the production artifact registry and chat-state stores. It runs the existing full
frontend research journey, routing artifact get/review/save and artifact editor
state requests to the real server. Conversation/inventory and workspace-layout
state remain fixture responses; no runtime is connected.

The browser commits a research revision through the actual artifact endpoint,
drops its successful response, reloads, retries the same request and compares the
saved version with the original. Two isolated browser contexts then generate real
draft-revision conflicts and explicitly resolve them through Keep this draft and
Use saved draft. The fixture's save interception reads the real persisted draft
before forwarding the artifact mutation and checks its save request identity.

After the browsers finish, the Go test reads the stores and checks the saved
head, exactly two revisions, original and revised immutable content bytes, and
the explicitly chosen phone draft with the correct base revision. This upgrades
the earlier client-only receipt/conflict evidence for these specific endpoints.
The backend save response contains metadata; the test reads current preview
content separately rather than assuming that response contains file text.

Run with:

```
NODE_PATH=/tmp/manifest-browser/node_modules go test -race ./server -run '^TestResearchRevisionBrowserWithBackend$' -count=1
```

The race-enabled integrated test and standalone browser mode pass. The Go wrapper
skips when Node or Playwright cannot resolve, following existing browser tests;
ordinary suite success alone does not prove this journey ran. No production code
change was required. Full repository/build/deployment evidence is in the plan
checkpoint. Real providers, physical devices, server restart, artifact discovery
and conversation/workspace-state backend integration remain separate work.
