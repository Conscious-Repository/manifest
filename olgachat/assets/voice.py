"""One Liber turn with images: Hermes's one-shot path with a multimodal message.
Usage: python voice-shim.py <request.json>  (prompt, images [{mime, path}], home, model, provider, usage)."""
import base64, json, os, sys
req = json.load(open(sys.argv[1]))
os.environ["HERMES_HOME"] = req["home"]          # the profile, before Hermes imports
from hermes_cli.oneshot import run_oneshot
parts = [{"type": "text", "text": req["prompt"]}]
for im in req.get("images", []):
    data = base64.b64encode(open(im["path"], "rb").read()).decode()
    parts.append({"type": "image_url", "image_url": {"url": f"data:{im['mime']};base64,{data}"}})
sys.exit(run_oneshot(parts, model=req.get("model"), provider=req.get("provider"), toolsets=req.get("toolsets", "memory"), usage_file=req.get("usage")))
