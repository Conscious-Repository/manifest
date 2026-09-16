"""Read-only subscription launcher for bounded migrated extraction duties.
The app prepares an isolated auth/config directory. Landlock admits runtime reads
and that scratch directory only; seccomp denies metadata and namespace escapes.
No engine imports, plugins, user hooks or project settings are loaded.
"""
import ctypes
import errno
import os
import platform
import sys


def main():
    if sys.platform != 'linux' or platform.machine() not in ('x86_64', 'aarch64'):
        raise RuntimeError('isolation')
    binary, scratch, budget, model = sys.argv[1:5]
    if model != 'claude-sonnet-5' or not 0 < float(budget) <= 4:
        raise RuntimeError('authority')
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
    read = (1 << 0) | (1 << 2) | (1 << 3)
    for path in ('/usr', '/lib', '/lib64', '/etc', '/proc/self', '/sys/devices/system/cpu'):
        if os.path.exists(path):
            allow(path, read)
    for path in ('/proc/cpuinfo', '/proc/meminfo'):
        if os.path.exists(path):
            allow(path, 1 << 2)
    for path in ('/dev/null', '/dev/urandom', '/dev/random'):
        if os.path.exists(path):
            allow(path, (1 << 1) | (1 << 2))
    allow(binary, (1 << 0) | (1 << 2))
    allow(scratch, all_rights)
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
        for name in ('ptrace','process_vm_writev','mount','setns','unshare',
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
    os.chdir(scratch)
    os.execv(binary, [binary, '-p', '--output-format', 'stream-json', '--verbose',
        '--model', model, '--tools', '', '--strict-mcp-config', '--mcp-config', '{"mcpServers":{}}',
        '--safe-mode', '--setting-sources', '', '--no-session-persistence',
        '--max-turns', '1', '--max-budget-usd', budget,
        '--system-prompt', 'Extract candidates from supplied data only. No tools or external effects. Return only the requested JSON.'])


try:
    main()
except BaseException:
    sys.stderr.write('isolation')
    sys.exit(1)
