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
            a['ceilingUsd'] != 0 or not 1 <= a['maxSteps'] <= 1000 or
            not 1 <= a['timeoutSeconds'] <= 3600):
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
        body = json.dumps({'model': MODEL, 'messages': [{'role': 'user', 'content': packet['prompt']}],
                           'tools': [], 'tool_choice': 'none', 'stream': False, 'max_tokens': 4096})
        conn.request('POST', '/v1/chat/completions', body, {'Content-Type': 'application/json'})
        response = conn.getresponse()
        if response.status != 200:
            raise RuntimeError('provider')
        raw = response.read(100001)
        conn.close()
        if len(raw) > 100000:
            raise RuntimeError('response')
        data = json.loads(raw, object_pairs_hook=unique_object, parse_float=decimal.Decimal)
        choice = data['choices'][0]
        message = choice['message']
        if choice['finish_reason'] != 'stop' or message.get('tool_calls') or message.get('function_call'):
            raise RuntimeError('completion')
        # Cost MUST be reported, never manufactured from an absent field.
        cost = data['usage']['cost_usd']
        if type(cost) not in (int, decimal.Decimal) or cost != 0:
            raise RuntimeError('cost')
        if data['model'] != MODEL:
            raise RuntimeError('model')
        if data['provider'] != PROVIDER:
            raise RuntimeError('provider_drift')
        reply = message['content']
        if not isinstance(reply, str) or not reply.strip() or len(reply.encode()) > 64000:
            raise RuntimeError('response')
        json.dump({'model': data['model'], 'provider': data['provider'], 'cost_usd': float(cost),
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
