"""Fail-closed Linux/amd64 Python launcher for the experimental adapter."""
import ctypes
import os
import platform
import sys

if platform.machine() != "x86_64" or len(sys.argv) < 2:
    raise RuntimeError("policy launcher requires Linux amd64 and an explicit command")
libc = ctypes.CDLL(None, use_errno=True)
libc.syscall.restype = ctypes.c_long


def checked(result, operation):
    if result < 0:
        number = ctypes.get_errno()
        raise OSError(number, f"{operation}: {os.strerror(number)}")
    return result


abi = checked(libc.syscall(444, 0, 0, 1), "query Landlock ABI")
if abi < 3:
    raise RuntimeError(f"Landlock ABI >= 3 required for truncate denial; found {abi}")


class Ruleset(ctypes.Structure):
    _fields_ = [("handled_access_fs", ctypes.c_uint64)]


# Handle every filesystem mutation through ABI 3; reads and execution remain
# unhandled/allowed. No allow rules are added, including for temp or cache.
access = ((1 << 15) - 1) & ~((1 << 0) | (1 << 2) | (1 << 3))
ruleset = Ruleset(access)
fd = checked(libc.syscall(444, ctypes.byref(ruleset), ctypes.sizeof(ruleset), 0), "create Landlock ruleset")
try:
    checked(libc.prctl(38, 1, 0, 0, 0), "set no_new_privs")
    checked(libc.syscall(446, fd, 0), "restrict filesystem")
finally:
    os.close(fd)
os.execvp(sys.argv[1], sys.argv[1:])
