#!/usr/bin/env python3
"""Merge the local adapter into Hermes and Alfred's explicit toolset filter."""
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PYTHON = Path.home() / '.hermes/hermes-agent/venv/bin/python'
if Path(os.sys.executable).resolve() != PYTHON.resolve():
    os.execv(str(PYTHON), [str(PYTHON), __file__])
import yaml

binary = Path.home() / '.local/bin/manifest-mcp'
if not binary.is_file():
    raise SystemExit('Build first: go build -o ~/.local/bin/manifest-mcp ./cmd/manifest-mcp')
config_path = Path.home() / '.hermes/config.yaml'
config = yaml.safe_load(config_path.read_text()) or {}
config.setdefault('mcp_servers', {})['manifest'] = {
    'command': str(binary), 'args': ['--config', str(ROOT / 'config.json')],
    'tools': {'include': ['*']},
}
# Preserve unrelated configuration values via YAML parse/emit.
# Formatting/comments may change. The file remains owner-only.
tmp = config_path.with_suffix('.manifest-tmp')
fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, 'w') as f:
    yaml.safe_dump(config, f, sort_keys=False)
os.replace(tmp, config_path)
app_path = ROOT / 'config.json'
app = json.loads(app_path.read_text())
hermes = app.setdefault('hermes', {})
default = 'web,session_search,memory,x_search,skills,clarify,context_engine,vision'
toolsets = [s for s in (hermes.get('readToolsets') or default).split(',') if s]
if 'mcp-manifest' not in toolsets:
    toolsets.append('mcp-manifest')
hermes['readToolsets'] = ','.join(toolsets)
app_path.write_text(json.dumps(app, indent=2) + '\n')
print('Configured manifest MCP and Alfred mcp-manifest filter; restart Manifest for config reload.')
