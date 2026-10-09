"""Bounded, model-backed A2A REST-shaped example; not a general A2A server."""
import errno
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import ipaddress
import json
import os
import socket
import ssl
import subprocess
import sys
import traceback
import uuid


def http_json(host, port, path, payload=None, timeout=120):
    connection = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        body = None if payload is None else json.dumps(payload).encode()
        connection.request("GET" if body is None else "POST", path, body,
                           {"Content-Type": "application/json", "A2A-Version": "1.0"})
        response = connection.getresponse()
        raw = response.read(1048577)
        if len(raw) > 1048576:
            raise ValueError("response exceeds 1 MiB")
        if response.status != 200:
            raise RuntimeError(f"HTTP {response.status} from {path}: {raw[:200]!r}")
        return json.loads(raw)
    finally:
        connection.close()


def registry_get(proxy, hostname="mcr.microsoft.com", port=443):
    connection = http.client.HTTPSConnection(proxy, 3000, timeout=15, context=ssl.create_default_context())
    try:
        connection.set_tunnel(hostname, port)
        connection.request("GET", "/v2/")
        response = connection.getresponse()
        body = response.read(4096).decode()
        if response.status != 200:
            raise RuntimeError(f"registry HTTP {response.status}")
        return {"host": hostname, "status": response.status, "body": body}
    finally:
        connection.close()


def infer(gateway, prompt):
    result = http_json(gateway, 3001, "/api/chat", {
        "model": "qwen3:0.6b", "stream": False, "think": False, "keep_alive": "10m",
        "messages": [{"role": "user", "content": prompt}],
        "options": {"temperature": 0, "num_predict": 128, "num_ctx": 2048, "num_thread": 2},
    })
    text = result["message"]["content"].strip()
    if not result.get("done") or not text or result.get("eval_count", 0) <= 0:
        raise RuntimeError("model did not produce a completed, nonempty inference")
    return text, {key: result[key] for key in ("model", "eval_count", "eval_duration", "prompt_eval_count")}


def message(text, role="ROLE_USER"):
    return {"messageId": str(uuid.uuid4()), "role": role, "parts": [{"text": text}]}


def input_text(body):
    msg = body.get("message", {})
    if not isinstance(msg, dict) or msg.get("role") != "ROLE_USER" or not isinstance(msg.get("messageId"), str) or not msg["messageId"]:
        raise ValueError("messageId and ROLE_USER are required")
    parts = msg.get("parts")
    if not isinstance(parts, list) or not parts or any(
            not isinstance(part, dict) or set(part) != {"text"} or not isinstance(part["text"], str)
            for part in parts):
        raise ValueError("this sample accepts text parts only")
    text = "\n".join(part["text"] for part in parts)
    if len(text) > 4000:
        raise ValueError("message text exceeds 4000 characters")
    return text


def workflow(agent, gateway, text):
    if agent == "registry-reader":
        observation = registry_get(gateway)
        answer, model = infer(gateway, "In one sentence explain this measured registry result. "
                              "Do not invent observations: " + json.dumps(observation))
        evidence = {"registry": observation, "summary": answer, "model": model}
    elif agent == "coordinator":
        peer = http_json(gateway, 3001, "/skills/inspect-registry/message:send", {"message": message(text)})
        observation = peer["message"]["metadata"]["policyExample"]
        answer, model = infer(gateway, "Summarize this registry check in one sentence. "
                              "Do not claim checks not in the evidence: " + json.dumps(observation))
        evidence = {"peer": observation, "model": model}
    else:
        raise ValueError("unknown agent")
    evidence["agent"] = agent
    response = message(answer, "ROLE_AGENT")
    response["metadata"] = {"policyExample": evidence}
    return {"message": response}


def probe(case, config):
    if case == "filesystem":
        results = []
        for path in ("/policy-write", "/tmp/policy-write", "/dev/shm/policy-write", "/dev/null"):
            try:
                fd = os.open(path, os.O_WRONLY | os.O_CREAT, 0o600)
            except OSError as error:
                if error.errno not in (errno.EACCES, errno.EROFS):
                    raise
                results.append({"path": path, "errno": error.errno})
            else:
                os.close(fd)
                raise AssertionError(f"write succeeded: {path}")
        return {"case": case, "denials": results}
    targets = {"direct-model": (config["modelIP"], 11434), "direct-peer": (config["peerIP"], 8080),
               "other-gateway": (config["otherGatewayIP"], 3001), "direct-mcr": (config["mcrIP"], 443)}
    if case in targets:
        host, port = targets[case]
        ipaddress.IPv4Address(host)
        try:
            with socket.create_connection((host, port), timeout=3):
                pass
        except TimeoutError:
            return {"case": case, "outcome": "timeout", "target": f"{host}:{port}"}
        raise AssertionError(f"{case}: direct connection succeeded")
    if case == "child-bypass":
        child = subprocess.run([sys.executable, "-B", "-c",
            "import socket,sys\n"
            "try: socket.create_connection((sys.argv[1],11434),timeout=3)\n"
            "except TimeoutError: sys.exit(42)\n"
            "sys.exit(1)", config["modelIP"]], capture_output=True, text=True, timeout=10)
        if child.returncode != 42:
            raise AssertionError(f"child bypass not denied: {child.returncode} {child.stderr}")
        return {"case": case, "outcome": "timeout"}
    if case == "dns-datagram":
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
            try:
                connection.sendto(b"probe", (config["dnsIP"], 53))
            except OSError as error:
                if error.errno == errno.EPERM:
                    return {"case": case, "errno": error.errno}
                raise
        raise AssertionError("agent DNS send succeeded")
    if case in ("model-admin", "forbidden-skill", "forbidden-peer", "wrong-method"):
        paths = {"model-admin": "/api/tags", "forbidden-skill": "/skills/delete-registry/message:send",
                 "forbidden-peer": "/skills/coordinate/message:send", "wrong-method": "/api/chat"}
        method = "GET" if case in ("model-admin", "wrong-method") else "POST"
        connection = http.client.HTTPConnection(config["gatewayIP"], 3001, timeout=10)
        try:
            connection.request(method, paths[case], "{}")
            response = connection.getresponse()
            response.read()
            if response.status != 403:
                raise AssertionError(f"{case}: expected 403, got {response.status}")
            return {"case": case, "status": response.status}
        finally:
            connection.close()
    if case == "unlisted-sni":
        try:
            registry_get(config["gatewayIP"], "example.com")
        except ssl.SSLError:
            return {"case": case, "outcome": "TLS rejected"}
        raise AssertionError("unlisted TLS authority was not rejected")
    raise ValueError("unknown probe")


class Handler(BaseHTTPRequestHandler):
    def respond(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/healthz":
            self.respond(200, {"ready": True})
        elif self.path == "/.well-known/agent-card.json":
            policy = self.server.policy
            skill = policy["capabilities"]["skills"][0]
            self.respond(200, {
                "name": policy["agent"], "description": skill["description"], "version": "0.1.0",
                "supportedInterfaces": [{"url": self.server.config["publicURL"], "protocolBinding": "HTTP+JSON", "protocolVersion": "1.0"}],
                "capabilities": {}, "defaultInputModes": ["text/plain"], "defaultOutputModes": ["text/plain"],
                "skills": [skill],
            })
        else:
            self.respond(404, {"error": "unsupported endpoint"})

    def do_POST(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 16384:
                raise ValueError("request must contain at most 16 KiB")
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict):
                raise ValueError("request must be an object")
            if self.path == "/probe":
                result = probe(body["case"], self.server.config)
            elif self.path == self.server.config["skillPath"]:
                result = workflow(self.server.policy["agent"], self.server.config["gatewayIP"], input_text(body))
            else:
                self.respond(404, {"error": "unsupported endpoint"})
                return
            self.respond(200, result)
        except (ValueError, KeyError) as error:
            self.log_error("%s", error)
            self.respond(400, {"error": str(error)})
        except (OSError, RuntimeError, AssertionError, subprocess.SubprocessError) as error:
            traceback.print_exc()
            self.respond(502, {"error": str(error)})


if __name__ == "__main__":
    with open("/example/policy.json") as source:
        policy = json.load(source)
    config = json.loads(os.environ["EXAMPLE_CONFIG"])
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    server.policy, server.config = policy, config
    server.serve_forever()
