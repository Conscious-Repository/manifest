# Explicit side-chat return

Side-chat assistant responses now offer `Add to parent draft`. The parent accepts only a message from that mounted side-chat frame at the same origin, appends response text and the saved source route to the existing composer, and dispatches the ordinary input event for draft recovery. It never invokes send. Replayed request IDs are deduplicated for the mounted pane. Mobile hides the inspector to reveal the parent composer after acknowledgement. Closing the pane removes its message listener.

The browser fixture exercises the actual response action inside an iframe, parent acknowledgement, preserved existing draft, source rejection, and replay deduplication. The full repository Go suite passed at the start of this checkpoint; focused CSS validation and build also passed. No real provider send was exercised. Reload-spanning return-receipt deduplication and the larger integrated release gates remain outstanding.
