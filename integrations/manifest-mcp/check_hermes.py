#!/usr/bin/env python3
"""Verify real installed Hermes registration and Alfred's filtered tool schemas."""
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
HERMES = Path.home() / '.hermes/hermes-agent'
PYTHON = HERMES / 'venv/bin/python'
if Path(sys.executable).resolve() != PYTHON.resolve():
    os.execv(str(PYTHON), [str(PYTHON), __file__])
sys.path.insert(0, str(HERMES))
import yaml
from tools.mcp_tool import register_mcp_servers, shutdown_mcp_servers
from model_tools import get_tool_definitions

config = yaml.safe_load((Path.home() / '.hermes/config.yaml').read_text())
app = json.loads((ROOT / 'config.json').read_text())
expected = {t['name'] for t in json.loads((ROOT / 'manifestmcp/catalog.json').read_text())['tools']}
try:
    registered = register_mcp_servers({'manifest': config['mcp_servers']['manifest']})
    defs = get_tool_definitions(enabled_toolsets=app['hermes']['readToolsets'].split(','), quiet_mode=True, skip_tool_search_assembly=True)
    names = {d['function']['name'] for d in defs if d['function']['name'].startswith('mcp__manifest__')}
    wanted = {'mcp__manifest__' + n.replace('.', '_') for n in expected}
    # Hermes preserves dots in some versions and normalizes in others.
    normalized = {n.replace('.', '_') for n in names}
    if normalized != wanted:
        raise SystemExit(f'Filtered discovery mismatch: expected {sorted(wanted)}, got {sorted(names)}')
    print(f'HERMES_FILTER_OK: {len(names)} tools discovered through installed Hermes and Alfred filter')
    print('\n'.join(sorted(names)))
finally:
    shutdown_mcp_servers()
