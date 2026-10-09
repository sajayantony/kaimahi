"""Kernel-denial probes run inside the governed workload, not in the client."""
import errno
import json
import os
import socket
import subprocess
import sys

host = sys.argv[1]
results = []


def denied(name, action, expected=(errno.EPERM,)):
    try:
        value = action()
    except OSError as error:
        if error.errno not in expected:
            raise AssertionError(f"{name}: expected policy errno {expected}, got {error!r}") from error
        results.append({"case": name, "errno": error.errno, "outcome": "denied"})
        return
    if isinstance(value, int):
        os.close(value)
    raise AssertionError(f"{name}: operation unexpectedly succeeded")


for path in ["/policy-write", "/tmp/policy-write", "/dev/shm/policy-write", "/dev/null"]:
    denied(f"write-open:{path}", lambda path=path: os.open(path, os.O_WRONLY | os.O_CREAT, 0o600),
           (errno.EACCES, errno.EROFS))
denied("truncate-existing", lambda: os.truncate("/etc/hostname", 0))
denied("mkdir-tmp", lambda: os.mkdir("/tmp/policy-directory"))
denied("unlink-existing", lambda: os.unlink("/etc/hostname"))
denied("chmod-existing", lambda: os.chmod("/etc/hostname", 0o600))
with open("/etc/hostname") as source:
    assert source.read(), "read-only access must remain usable"
results.append({"case": "read-existing", "outcome": "allowed"})

for name, port in [("model", 8080), ("tool", 3000), ("a2a", 3001)]:
    with socket.socket() as connection:
        connection.settimeout(2)
        denied(f"direct-tcp:{name}", lambda: connection.connect((host, port)))
with socket.socket(socket.AF_INET6) as connection:
    denied("direct-ipv6", lambda: connection.connect(("::1", 8080)))
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
    denied("dns-datagram", lambda: connection.sendto(b"probe", (host, 53)))
    denied("udp-egress", lambda: connection.sendto(b"probe", (host, 8080)))
with socket.socket(socket.AF_UNIX) as connection:
    denied("unix-socket", lambda: connection.connect("/tmp/policy-socket"))

child = subprocess.run(
    [sys.executable, "-B", "-c",
     "import errno,socket,sys\n"
     "try: socket.create_connection((sys.argv[1],8080),timeout=2)\n"
     "except OSError as e: sys.exit(0 if e.errno==errno.EPERM else 2)\n"
     "sys.exit(1)", host],
    capture_output=True, text=True, timeout=10)
assert child.returncode == 0, f"child did not inherit denial: {child.stderr}"
results.append({"case": "child-process-egress", "outcome": "denied"})
print(json.dumps(results))
