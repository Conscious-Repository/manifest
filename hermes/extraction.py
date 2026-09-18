"""Fail-closed filesystem boundary for the fixed Hermes extractor runtime."""
import ctypes
import errno
import os
import platform
import sys
import sysconfig


def main():
    if sys.platform != 'linux' or platform.machine() not in ('x86_64', 'aarch64'):
        raise RuntimeError('isolation')
    sys.dont_write_bytecode = True
    scratch = os.environ['HERMES_HOME']
    runtime = '/home/benjamin/.hermes/hermes-agent'
    if os.getcwd() != scratch or os.environ['HOME'] != scratch:
        raise RuntimeError('isolation')
    libc = ctypes.CDLL(None, use_errno=True)
    sec = ctypes.CDLL('libseccomp.so.2', use_errno=True)
    if libc.syscall(444, 0, 0, 1) < 3 or libc.prctl(38, 1, 0, 0, 0):
        raise RuntimeError('isolation')
    all_rights = (1 << 15) - 1
    rights = ctypes.c_uint64(all_rights)
    ruleset = libc.syscall(444, ctypes.byref(rights), ctypes.sizeof(rights), 0)
    if ruleset < 0:
        raise RuntimeError('isolation')
    class Rule(ctypes.Structure):
        _pack_ = 1
        _fields_ = [('allowed_access', ctypes.c_uint64), ('parent_fd', ctypes.c_int)]
    def allow(path, access):
        fd = os.open(path, os.O_PATH | os.O_CLOEXEC)
        try:
            rule = Rule(access, fd)
            if libc.syscall(445, ruleset, 1, ctypes.byref(rule), 0):
                raise RuntimeError('isolation')
        finally:
            os.close(fd)
    read = (1 << 2) | (1 << 3)
    for path in ('/usr', '/lib', '/lib64', '/sys/devices/system/cpu'):
        if os.path.exists(path):
            allow(path, read)
    for path in ('/proc/cpuinfo', '/proc/meminfo'):
        if os.path.exists(path):
            allow(path, 1 << 2)
    for path in ('/dev/null', '/dev/urandom', '/dev/random'):
        if os.path.exists(path):
            allow(path, (1 << 1) | (1 << 2))
    # No caller home, Hermes state, plugins, MCP configuration, or runtime
    # dotfiles. Only installed Python code/dependencies and public assets.
    for path in ('/etc/ld.so.cache', '/etc/localtime', '/etc/ssl/certs'):
        if os.path.exists(path):
            allow(path, read if os.path.isdir(path) else 1 << 2)
    allow(runtime, 1 << 3)  # directory listing only, no blanket file reads
    allow(sysconfig.get_path("stdlib"), read)
    for name in os.listdir(runtime):
        if name.endswith('.py') and not os.path.islink(os.path.join(runtime, name)):
            allow(os.path.join(runtime, name), 1 << 2)
    for name in ('agent', 'hermes_cli', 'tools', 'providers', 'gateway', 'cron',
                 'assets', 'locales', 'hermes_agent.egg-info', 'venv/lib'):
        allow(os.path.join(runtime, name), read)
    # The built-in custom provider is runtime code for lab-sparks, not a
    # caller-installed plugin. No other plugin source is admitted.
    custom = os.path.join(runtime, 'plugins/model-providers/custom')
    if os.path.isdir(custom):
        allow(custom, read)
    # No executable grants anywhere, including scratch: shell/subprocess exec
    # is forbidden. Hermes runs in this already-started isolated interpreter.
    allow(scratch, all_rights & ~(1 << 0))
    if libc.syscall(446, ruleset, 0):
        raise RuntimeError('isolation')
    os.close(ruleset)
    sec.seccomp_init.argtypes = [ctypes.c_uint32]
    sec.seccomp_init.restype = ctypes.c_void_p
    sec.seccomp_rule_add.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int, ctypes.c_uint]
    sec.seccomp_load.argtypes = [ctypes.c_void_p]
    sec.seccomp_release.argtypes = [ctypes.c_void_p]
    sec.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
    ctx = sec.seccomp_init(0x7fff0000)
    if not ctx:
        raise RuntimeError('isolation')
    try:
        for name in ('ptrace','process_vm_readv','process_vm_writev','mount','setns','unshare',
                     'execve','execveat',
                     'io_uring_setup','chmod','fchmod','fchmodat','fchmodat2',
                     'chown','lchown','fchown','fchownat','utime','utimes',
                     'futimesat','utimensat','setxattr','lsetxattr','fsetxattr',
                     'removexattr','lremovexattr','fremovexattr'):
            number = sec.seccomp_syscall_resolve_name(name.encode())
            if number >= 0 and sec.seccomp_rule_add(ctx, 0x50000 | errno.EPERM, number, 0):
                raise RuntimeError('isolation')
        if sec.seccomp_load(ctx):
            raise RuntimeError('isolation')
    finally:
        sec.seccomp_release(ctx)
    # A conservative byte budget also bounds tokens without a guessed ratio:
    # 512 KiB leaves half of the pinned 1,048,576 context for runtime/output.
    prompt = sys.stdin.buffer.read(524289)
    if not prompt or len(prompt) > 524288:
        raise RuntimeError('input')
    prompt = prompt.decode('utf-8', errors='strict')
    sys.path.insert(0, runtime + '/venv/lib/python%d.%d/site-packages' % sys.version_info[:2])
    sys.path.insert(0, runtime)
    sys.argv = ['hermes', 'chat', '-Q', '-q', prompt, '-m', 'sparks',
                '--provider', 'lab-sparks', '--safe-mode', '-t', 'none',
                '--max-turns', '1', '--source', 'tool', '--cli']
    # Observe the actual HTTP boundary. No prompt, response text or credentials
    # enter telemetry. A second request is refused, including SDK retries.
    import json
    import httpx
    send = httpx.Client.send
    receipt = {'provider': '', 'model': '', 'responseModel': '',
               'steps': 0, 'completed': False, 'status': 0}
    def save_receipt():
        with open('execution.json', 'w') as f:
            json.dump(receipt, f)
            f.flush()
            os.fsync(f.fileno())
    def observed_send(client, request, *args, **kwargs):
        if str(request.url) != 'http://192.168.87.11:8000/v1/chat/completions' or receipt['steps']:
            raise RuntimeError('unapproved provider request or retry')
        body = json.loads(request.content)
        if body.get('model') != 'deepseek-v4.1-flash' or body.get('tools'):
            raise RuntimeError('unapproved model or tools')
        receipt.update(provider='lab-sparks', model=body['model'], steps=1)
        save_receipt()
        kwargs['follow_redirects'] = False
        response = send(client, request, *args, **kwargs)
        raw = response.read()
        receipt.update(completed=True, status=response.status_code)
        try:
            receipt['responseModel'] = json.loads(raw).get('model', '')
        except (ValueError, AttributeError):
            for line in raw.splitlines():
                if line.startswith(b'data: ') and line != b'data: [DONE]':
                    item = json.loads(line[6:])
                    if item.get('model'):
                        receipt['responseModel'] = item['model']
        save_receipt()
        return response
    httpx.Client.send = observed_send
    from hermes_cli.main import main as hermes_main
    hermes_main()


try:
    main()
except Exception:
    sys.stderr.write('isolation')
    sys.exit(1)
