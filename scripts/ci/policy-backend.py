"""Credential-free HTTP positive control; never invokes an actual model."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import ipaddress
import subprocess
import sys

hits = 0


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        global hits
        if self.path.startswith("/probe/") and len(sys.argv) == 2:
            target = str(ipaddress.ip_address(self.path.removeprefix("/probe/")))
            result = subprocess.run([sys.executable, "-B", "-c", sys.argv[1], target],
                                    capture_output=True, text=True, timeout=30)
            body = (result.stderr if result.returncode else result.stdout).encode()
            self.send_response(500 if result.returncode else 200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if self.path not in ("/healthz", "/hits"):
            hits += 1
        body = json.dumps({"hits": hits, "path": self.path}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.do_GET()

    def log_message(self, *_):
        pass


HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
