# File discovery preserves conversation context

The all-registered-files inspector previously passed an output's producing task into the current conversation's artifact selection. For an unrelated task, the displayed Discuss action led to a rejected send and left misleading composer context. The existing browser fixture used an output without a task and missed this case.

All-files results now open with an explicit message-context limitation and without a task selection or Discuss callback. Whole-version review and authorized owner editing remain available; request-changes reviews record notes without drafting into the current composer. Existing source links return to the producing work. Conversation-scoped file opening keeps its established context behavior, and server scope checks remain unchanged.

The browse context flag persists in the workspace tab specification. Its tab key is distinct from a contextual opening of the same artifact, so opening one mode cannot silently inherit the other's send behavior. No task links, artifact provenance or conversation records are changed by discovery.

Evidence: expanded Chromium workspace fixture includes a foreign task on an all-files result, verifies exact revision/source navigation, absent inherited task, disabled context callback and persisted browse spec, plus the rendered limitation and absent Discuss action after reopening. Existing draft/tab/mobile/search recovery assertions still run. This fixes misleading capability exposure; explicit cross-conversation context grants and general record reference/typeahead remain unfinished in the workbench plan.

Build and diff checks passed. `make test` passed server and other packages except the known Hermes authority/successor source-hash canary failure. Browser actions used mocked APIs; no actual context delivery or task modification occurred.
