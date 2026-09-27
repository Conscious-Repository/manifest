# Live provider trials

Owner decision D7 (2026-09-27): live trials are **not authorized by default**. The live tests below skip unless the owner authorizes a run through the environment. Nothing in `make test` or `go test ./...` reaches a real provider.

A run spends provider tokens and sends short probe text to a real model. The probes ask the model not to search, write or change anything. The tests never touch the owner's Hermes state, vault or checkout.

These trials close the live halves of the workbench acceptance checklist:
- **Row 2:** a selected endpoint/model receives the exact context.
- **Row 13:** the planning→coding→result journey with a real coding agent.
- **Row 14:** concurrent live providers.

The Manifest-side halves are proven against fakes already, for example `TestNativeChatInstructionAndAttachmentsReachRunnerExactly`, `TestWorkbenchContextRelationshipsJourney` and `TestPlanningCodingResultJourney`.

## Hermes (native chat and task threads): `server/hermes_live_test.go`

### Prerequisites (the owner's actions)

1. **A scratch Hermes home**, never `~/.hermes`. The harness refuses the owner's own home. For example, `/tmp/hermes-live-home` with:
   - a `config.yaml` naming the provider and model(s) under trial;
   - a `.env` holding that provider's key.

   Copying a key into it is the authorization to spend.
2. **The Hermes CLI's absolute path**, for example `$(command -v hermes)`.
3. **The models to trial**, as a comma-separated list. Each model becomes its own subtest. Leave it empty to use the scratch home's configured default.

### One command

```sh
MANIFEST_HERMES_LIVE=authorized \
MANIFEST_HERMES_LIVE_BIN="$(command -v hermes)" \
MANIFEST_HERMES_LIVE_HOME=/tmp/hermes-live-home \
MANIFEST_HERMES_LIVE_MODELS=<model-a>,<model-b> \
go test ./server -run '^TestHermesLive' -count=1 -v -timeout 60m
```

Optional settings:
- `MANIFEST_HERMES_LIVE_PROFILE`: a `-p` profile inside the scratch home.
- `MANIFEST_HERMES_LIVE_TOOLSETS`: the `-t` scope. The default is production's read scope, `DefaultHermesReadToolsets` in `config.go`.
- `MANIFEST_HERMES_LIVE_EVIDENCE`: where the evidence JSON goes. The default is `/tmp/manifest-hermes-live`.

`MANIFEST_HERMES_LIVE` must be exactly `authorized`. Any other value skips the tests.

### What each test proves

| Test | Proves | Fails when |
| --- | --- | --- |
| `TestHermesLiveNativeChatExactContext/<model>` | Four tokens each travel their own channel: the tail of an ~8 KB instruction, an explicit artifact revision, an inlined thread attachment, and an owned file handed over as a path. The model returns all four. | A token does not come back (the message names the channel), or the delivery does not complete. A clear delivery error is recorded as evidence first. |
| `TestHermesLiveImageProbe/<model>` | Evidence only. A solid red PNG is sent. The evidence records whether the model named the colour or said it cannot open the image. | The delivery fails, or the image hand-off loses its "vision support unknown" label (D2). It never asserts vision. |
| `TestHermesLiveTaskThreadAsk` | One task-thread Ask, with exactly one hand-off, no re-dispatch (D4), and one reply carrying its token. | Any of those counts differ. |

Each test writes one JSON evidence file per model. It records the requested model, the reported model and Hermes session (the delivery result), the delivery state, the full transcript, and which tokens came back. Cite that file when checking row 2.

Watch the owned-file channel. Production's read scope has no file tool, so a native turn may be unable to open an owned file's path. If `OWNEDFILE` comes back `CANNOT_OPEN`, that is a real finding about the scope, not a harness fault.

### Harness self-test (not a trial)

The harness plumbing can be checked without a provider. Point `MANIFEST_HERMES_LIVE_BIN` at a scripted stand-in that reads the prompt and echoes the tokens, as was done on 2026-09-27 with `/tmp/opus-hermes-live-dryrun/hermes`. That proves the harness, not a model. Never cite it as row-2 evidence.

## herdr (Codex and Claude): `server/terminal_herdr_live_test.go`

Requirements:
- an isolated, independently running herdr 0.9.0 scratch daemon;
- authenticated Codex and Claude CLIs;
- a trusted scratch checkout.

```sh
MANIFEST_HERDR_TEST_SESSION=<scratch session> \
MANIFEST_HERDR_TEST_CWD=/path/to/scratch/checkout \
go test ./server -run '^TestHerdrLive' -count=1 -v -timeout 30m
```

`TestHerdrLiveCodexFileIdentity` also needs `MANIFEST_HERDR_TEST_CODEX_ID`, the known conversation ID of the one Codex pane in that daemon.

## Still not covered by any harness

- A physical phone, a screen reader and a physical keyboard (row 14). These need a person and a device.
- Deployed-asset verification (row 14). The integrator deploys, then checks the served asset stamps against the commit.
