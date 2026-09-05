#!/usr/bin/env python3
"""Headless Phase 2 loop against a real binary and temporary on-disk vault.
Usage: python3 integrations/manifest-mcp/exercise_loop.py /tmp/manifest-mcp-phase2
No owner vault/config or network source is touched.
"""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

with tempfile.TemporaryDirectory(prefix='manifest-operation-demo-') as tmp:
    root = Path(tmp)
    vault, data = root / 'vault', root / 'data'
    vault.mkdir()
    config = root / 'config.json'
    config.write_text(json.dumps({'vaultPath': str(vault), 'dataDir': str(data), 'systemRoot': 'system'}))
    binary = str(Path(sys.argv[1]).resolve())
    proc = subprocess.Popen([binary, '--config', str(config)], stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    seq = 0

    def rpc(method, params):
        global seq
        seq += 1
        proc.stdin.write(json.dumps({'jsonrpc': '2.0', 'id': seq, 'method': method, 'params': params}) + '\n')
        proc.stdin.flush()
        while True:
            line = proc.stdout.readline()
            if not line:
                raise RuntimeError(proc.stderr.read())
            response = json.loads(line)
            if response.get('id') == seq:
                if 'error' in response:
                    raise RuntimeError(response['error'])
                return response['result']

    def call(name, arguments, allow_error=False):
        result = rpc('tools/call', {'name': name, 'arguments': arguments})
        if result.get('isError'):
            if allow_error:
                return result
            raise RuntimeError(result)
        return result['structuredContent']

    try:
        rpc('initialize', {'protocolVersion': '2025-03-26', 'capabilities': {},
                           'clientInfo': {'name': 'phase2-demo', 'version': '1'}})
        proc.stdin.write(json.dumps({'jsonrpc': '2.0', 'method': 'notifications/initialized'}) + '\n')
        proc.stdin.flush()
        tools = rpc('tools/list', {})['tools']
        assert not any(t['name'] == 'operation.decide' for t in tools)
        source = call('source_run.prepare', {'request': {'source': 'manual', 'query': 'Phase Two Example', 'max': 1, 'role': '', 'dryRun': False},
                                            'conversation': 'headless-demo', 'turn': 'source'})
        assert source['status'] == 'prepared' and source['executable']
        run = call('operation.execute', {'operationId': source['operationId']})
        assert run['status'] == 'succeeded'
        run_id = run['result']['runId']
        draft_id = run['result']['run']['drafts'][0]['id']
        accept_args = {'runId': run_id, 'draftId': draft_id, 'conversation': 'headless-demo',
                       'turn': 'accept', 'idempotencyKey': 'demo-accept'}
        prepared = call('candidate_accept.prepare', accept_args)
        op_id = prepared['operationId']
        assert prepared['status'] == 'pending_approval'
        inspected = call('operation.get', {'operationId': op_id})
        assert inspected['record']['payload'] == prepared['record']['payload']
        assert call('operation.execute', {'operationId': op_id, 'actor': 'owner:local'}, True)['isError']
        assert call('operation.execute', {'operationId': op_id}, True)['isError']
        decision = json.loads(subprocess.check_output([binary, '--config', str(config), '--decide', op_id], text=True))
        assert decision['status'] == 'approved'
        done = call('operation.execute', {'operationId': op_id})
        assert done['status'] == 'succeeded', done
        for rel, content in prepared['record']['vaultFiles'].items():
            assert (vault / rel).read_text() == content, rel
        for rel, content in prepared['record']['payload']['preview']['cacheFiles'].items():
            assert (data / rel).read_text() == content, rel
        audit = (data / 'write-audit.log').read_text()
        assert 'approved-proposal' in audit and 'user-action' not in audit
        reprepared = call('candidate_accept.prepare', accept_args)
        assert reprepared['operationId'] == op_id and reprepared['status'] == 'succeeded'
        again = call('operation.execute', {'operationId': op_id})
        assert again['status'] == 'succeeded' and (data / 'write-audit.log').read_text() == audit
        print(json.dumps({'status': 'LOOP_OK', 'writer': 'real vaultwriter, temporary disk vault',
                          'runId': run_id, 'queueCounts': run['result']['queueCounts'],
                          'operationId': op_id, 'objectRefs': done['result']['objectRefs'],
                          'confirmedFiles': done['result']['confirmedFiles'],
                          'approvalActor': done['record']['approvalActor'],
                          'previewBytesMatch': True, 'retryDidNotWrite': True}, indent=2))
    finally:
        proc.stdin.close()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
