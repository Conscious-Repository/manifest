"""Single-step, tool-free local completion. No Hermes/config/plugin imports.
Linux Landlock denies filesystem opens; seccomp denies process creation/exec.
Only the already-open usage descriptor and standard pipes remain writable.
"""
import ctypes
import errno
import encodings.idna
import http.client
import json
import decimal
import os
import platform
import socket
import sys

HOST, PORT = '192.168.87.11', 8000
MODEL, PROVIDER = 'deepseek-v4.1-flash', 'deepseek-local'


def isolate():
    if sys.platform != 'linux' or platform.machine() not in ('x86_64', 'aarch64'):
        raise RuntimeError('isolation')
    libc = ctypes.CDLL(None, use_errno=True)
    sec = ctypes.CDLL('libseccomp.so.2', use_errno=True)
    sec.seccomp_init.argtypes = [ctypes.c_uint32]
    sec.seccomp_init.restype = ctypes.c_void_p
    sec.seccomp_rule_add.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int, ctypes.c_uint]
    sec.seccomp_load.argtypes = [ctypes.c_void_p]
    sec.seccomp_release.argtypes = [ctypes.c_void_p]
    sec.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
    # Landlock ABI >= 3 covers truncate as well as all reads/writes/exec.
    abi = libc.syscall(444, 0, 0, 1)
    if abi < 3 or libc.prctl(38, 1, 0, 0, 0) != 0:
        raise RuntimeError('isolation')
    rights = ctypes.c_uint64((1 << 15) - 1)
    fd = libc.syscall(444, ctypes.byref(rights), ctypes.sizeof(rights), 0)
    if fd < 0:
        raise RuntimeError('isolation')
    try:
        if libc.syscall(446, fd, 0) != 0:
            raise RuntimeError('isolation')
    finally:
        os.close(fd)
    ctx = sec.seccomp_init(0x7fff0000)  # allow; deny executable/process escape
    if not ctx:
        raise RuntimeError('isolation')
    try:
        for name in (b'execve', b'execveat', b'fork', b'vfork', b'clone', b'clone3',
                     b'ptrace', b'process_vm_writev', b'mount', b'setns', b'unshare',
                     b'io_uring_setup', b'chmod', b'fchmod', b'fchmodat', b'fchmodat2',
                     b'chown', b'lchown', b'fchown', b'fchownat', b'utime', b'utimes',
                     b'futimesat', b'utimensat', b'setxattr', b'lsetxattr', b'fsetxattr',
                     b'removexattr', b'lremovexattr', b'fremovexattr'):
            number = sec.seccomp_syscall_resolve_name(name)
            if number >= 0 and sec.seccomp_rule_add(ctx, 0x50000 | errno.EPERM, number, 0) != 0:
                raise RuntimeError('isolation')
        if sec.seccomp_load(ctx) != 0:
            raise RuntimeError('isolation')
    finally:
        sec.seccomp_release(ctx)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise RuntimeError('usage')
        result[key] = value
    return result


def main():
    packet = json.loads(sys.stdin.buffer.read(100001))
    a = packet['authority']
    if (a['provider'] != PROVIDER or a['model'] != MODEL or
            a['tools'] != ['none'] or a['mcp'] != 'no_mcp' or
            a.get('costPolicy') != 'local-zero-marginal' or
            a.get('providerBinding') != 'fixed-local-endpoint' or
            a.get('endpoint') != 'http://192.168.87.11:8000/v1' or
            a['ceilingUsd'] != 0 or a['maxSteps'] != 1 or
            not 1 <= a['timeoutSeconds'] <= 120):
        raise RuntimeError('authority')
    # Resolve the numeric address before lockdown (no DNS/provider discovery).
    address = socket.inet_aton(HOST)
    if len(address) != 4:
        raise RuntimeError('authority')
    with open(sys.argv[1], 'w', encoding='utf-8') as usage_file:
        isolate()
        # Exactly one request. No retries, redirects, fallback, tool dispatch,
        # config loading, or continuation loop exists in this executable.
        conn = http.client.HTTPConnection(HOST, PORT, timeout=a['timeoutSeconds'])
        # Sparks rejects tools=[]; omit both tool fields for this locally
        # enforced tool-free authority. Response scope checks still apply.
        body = json.dumps({'model': MODEL, 'messages': [{'role': 'user', 'content': packet['prompt']}],
                           'stream': False, 'max_tokens': 4096})
        conn.request('POST', '/v1/chat/completions', body, {'Content-Type': 'application/json'})
        response = conn.getresponse()
        if response.status != 200:
            raise RuntimeError('provider')
        raw = response.read(100001)
        conn.close()
        if len(raw) > 100000:
            raise RuntimeError('response')
        data = json.loads(raw, object_pairs_hook=unique_object, parse_float=decimal.Decimal)
        # Scope is enforced locally, but contradictory/uncertain provider claims
        # must never be ignored. Absence is not used as a usage attestation.
        if (('error' in data and data['error'] is not None) or
                ('failed' in data and data['failed'] is not False) or
                ('completed' in data and data['completed'] is not True) or
                ('outcome' in data and data['outcome'] != 'completed') or
                ('steps' in data and (type(data['steps']) is not int or data['steps'] != 1)) or
                ('cost_policy' in data and data['cost_policy'] != 'local-zero-marginal') or
                ('provider_binding' in data and data['provider_binding'] != 'fixed-local-endpoint') or
                ('cost_telemetry' in data and data['cost_telemetry'] != 'unavailable') or
                ('fallback' in data and data['fallback'] is not False) or
                ('tools' in data and data['tools'] != []) or
                ('mcp' in data and data['mcp'] != 'no_mcp') or
                ('tool_calls' in data and data['tool_calls'] != []) or
                ('function_call' in data and data['function_call'] is not None) or
                ('tool_choice' in data and data['tool_choice'] != 'none') or
                len(data['choices']) != 1):
            raise RuntimeError('completion')
        choice = data['choices'][0]
        message = choice['message']
        if (choice['finish_reason'] != 'stop' or
                ('tool_calls' in message and message['tool_calls'] not in (None, [])) or
                ('function_call' in message and message['function_call'] is not None) or
                ('tool_calls' in choice and choice['tool_calls'] != []) or
                ('function_call' in choice and choice['function_call'] is not None)):
            raise RuntimeError('completion')
        usage = data['usage']
        if not isinstance(usage, dict):
            raise RuntimeError('usage')
        tokens = {}
        for key in ('prompt_tokens', 'completion_tokens', 'total_tokens'):
            value = usage.get(key)
            if type(value) is not int or not 0 <= value <= 9007199254740991:
                raise RuntimeError('usage')
            tokens[key] = value
        if (tokens['total_tokens'] != tokens['prompt_tokens'] + tokens['completion_tokens'] or
                tokens['completion_tokens'] > 4096):
            raise RuntimeError('usage')
        # Zero marginal compute is owner policy, never provider billing telemetry.
        for reported in (data, usage):
            for key in ('cost_usd', 'estimated_cost_usd'):
                if key in reported:
                    cost = reported[key]
                    if type(cost) not in (int, decimal.Decimal) or cost != 0:
                        raise RuntimeError('cost')
        if data['model'] != MODEL:
            raise RuntimeError('model')
        if 'provider' in data and data['provider'] != PROVIDER:
            raise RuntimeError('provider_drift')
        reply = message['content']
        if not isinstance(reply, str) or not reply.strip() or len(reply.encode()) > 64000:
            raise RuntimeError('response')
        json.dump({'model': data['model'], 'provider': PROVIDER,
                   'provider_binding': 'fixed-local-endpoint',
                   'cost_policy': 'local-zero-marginal', 'cost_telemetry': 'unavailable',
                   'usage': tokens,
                   'completed': True, 'failed': False, 'steps': 1}, usage_file)
        usage_file.flush()
        sys.stdout.write(reply)


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        # Whitelist only; never serialize exception strings or provider bodies.
        code = str(exc) if type(exc) is RuntimeError else 'usage'
        if code not in ('isolation', 'authority', 'provider', 'completion', 'cost', 'model', 'provider_drift', 'response'):
            code = 'usage'
        sys.stderr.write(code)
        sys.exit(1)
