# Native interruption keyboard access

The original workbench brief requires a stop-run shortcut. Inspection found that
Ctrl+Alt+X selected only the terminal header stop control, although native
Hermes-backed conversations now have targeted interruption controls. The added
shortcut regression failed before the change because native interruption could
not receive focus.

The shortcut now falls back to the native interruption button in the current
transcript. For this button it focuses only; Enter performs the existing action.
Repeated shortcuts do not submit interruption. Terminal stop retains its existing
arm-then-confirm behavior. Disabled native controls are ignored. The native
button advertises the shortcut in its tooltip and `aria-keyshortcuts` attribute.

Validation covers keyboard navigation and terminal arming, native focus without
submission, disabled controls and typing/dialog isolation. Full-frontend Chromium
presses the shortcut twice and then Enter, checking one exact running request ID
against a definitive fixture conflict response. The shared transport retries 503
responses, so that response is deliberately not used to count user activations.
Existing native interruption retry/wait/cancelled-text component checks pass.

Full repository/build/deployed checks are recorded in the plan checkpoint. This
does not add interruption to unsupported adapters or certify a physical keyboard,
screen reader or live provider stop. No production run was interrupted.
