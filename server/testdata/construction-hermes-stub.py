#!/usr/bin/python3 -I
# Fake `hermes` runner for construction native-delivery tests (P8). It is a
# protocol fake, not a model: it reads the -z prompt exactly as the server
# passed it on argv, re-hashes the construction packet between the markers,
# records what it received (declared vs computed hash, toolsets, model) for
# the test, and answers in the strict reply schema. No network, no files
# outside CX_STUB_OUT and the usage file. Modes (CX_STUB_MODE): normal, hang,
# fail, approve (a steward reply that tries to approve a decision).
import hashlib, json, os, sys, time

args = sys.argv[1:]
opts = {}
i = 0
while i < len(args):
    a = args[i]
    if a in ("-p", "-z", "-m", "-t", "--usage-file", "--provider", "--reasoning") and i + 1 < len(args):
        opts[a] = args[i + 1]
        i += 2
    else:
        i += 1
if args[:2] == ["profile", "list"]:
    sys.exit(0)
prompt = opts.get("-z", "")
mode = os.environ.get("CX_STUB_MODE", "normal")
observed = os.environ.get("CX_STUB_MODEL") or opts.get("-m") or "stub-model"
out_dir = os.environ.get("CX_STUB_OUT", "")

raw = os.fsencode(prompt)
begin = raw.find(b"BEGIN CONSTRUCTION PACKET sha256=")
packet, declared = b"", ""
if begin >= 0:
    line_end = raw.find(b"\n", begin)
    declared = raw[begin + len(b"BEGIN CONSTRUCTION PACKET sha256="):line_end].decode()
    end = raw.find(b"\nEND CONSTRUCTION PACKET", line_end)
    packet = raw[line_end + 1:end]
computed = hashlib.sha256(packet).hexdigest()
kind = ""
data = {}
try:
    data = json.loads(packet.decode()) if packet else {}
    kind = data.get("kind", "")
except Exception:
    kind = "unparsed"
if out_dir:
    n = len([f for f in os.listdir(out_dir) if f.startswith("received-")])
    with open(os.path.join(out_dir, "received-%03d.json" % n), "w") as fh:
        json.dump({"declared": declared, "computed": computed, "toolsets": opts.get("-t", ""), "model": opts.get("-m", ""), "kind": kind,
                   "mode": mode, "chatPreamble": "Manifest MCP" in prompt,
                   "brief": "CONSTRUCTION PROBLEM — you are the steward" in prompt}, fh)

if mode == "hang":
    time.sleep(120)
if mode == "fail":
    sys.stderr.write("stub failure\n")
    sys.exit(3)

reply = {}
if kind == "construction-extract-packet/1":
    trade = ""
    for s in data.get("sources", []):
        if "Headwall Flashing Practice Note" in s.get("title", ""):
            trade = s.get("sourceId", "")
    reply = {"passages": [
        {"sourceId": trade, "quote": "Where a metal roof terminates against a masonry wall, an apron flashing turned up the wall at least 150 mm shall be covered by a separate counterflashing.",
         "page": 2, "claim": "At a headwall, an apron flashing turned up the wall is covered by a separate counterflashing.", "relation": "supports",
         "topics": ["strategy:apron-surface-counterflashing"], "applicability": ["orientation:headwall"], "confidence": 0.6},
        {"sourceId": trade, "quote": "An invented sentence the source never contains.", "page": 2, "claim": "invented", "relation": "supports", "confidence": 0.9},
    ]}
elif kind == "construction-steward-packet/1":
    asm = data.get("assembly", {})
    ins = [c["id"] for c in asm.get("components", []) if c.get("type") == "insulation-board"]
    ops = [{"op": "SetDimension", "componentId": ins[0], "dimension": "thickness", "value": 150, "unit": "mm"}] if ins else []
    cmd = {"schemaVersion": 1, "requestId": "stub-" + computed[:16], "problemId": data.get("problemId"), "assemblyId": data.get("assemblyId"),
           "expectedAssemblyRevision": data.get("assemblyRevision"), "operations": ops}
    cmds = [cmd]
    if mode == "approve":
        cmds = [{"schemaVersion": 1, "requestId": "stub-approve-" + computed[:12], "problemId": data.get("problemId"),
                 "operations": [{"op": "ApproveDecision", "decisionId": "dec-" + "0" * 32, "expectedDecisionRevision": "0" * 64}]}]
    reply = {"summary": "increase the insulation to 150 mm (stub)", "commands": cmds}
elif "CONSTRUCTION PROBLEM — you are the steward" in prompt:
    # a problem chat: plain words, then one proposal block
    prop = {"summary": "one decision point", "changes": [{"operations": [{"op": "AddQuestion", "id": "dq-" + computed[:32],
            "text": "Is the wall solid brick or a cavity wall?", "options": ["solid", "cavity"], "why": "a cavity needs a through-wall flashing"}]}]}
    reply = None
    print("Two approaches fit; first we need to know the wall.\n\n```construction\n" + json.dumps(prop) + "\n```")
else:
    reply = {"summary": "no construction packet", "commands": []}

if reply is not None:
    print(json.dumps(reply))
usage = opts.get("--usage-file")
if usage:
    with open(usage, "w") as fh:
        json.dump({"model": observed, "session_id": "stub-session-" + computed[:8], "estimated_cost_usd": 0}, fh)
