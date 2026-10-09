#!/usr/bin/env python3
"""Generate the checked-in bundled/external suite-policy examples."""
import copy
import hashlib
import json
from pathlib import Path
import sys

repo = Path(__file__).resolve().parents[2]
root = Path(sys.argv[1])
if root.exists() and sys.argv[2:] != ["--update"]:
    raise SystemExit("output must be new (or use --update to regenerate known fixture files)")
source = repo / "internal/kmx/agentsuite/testdata/incident-analyst"
policies = repo / "agentsuite/policy/examples/kind-cpu-agents"


def write(path, value):
    # ASCII keys and integer values: this fixture's compact sorted JSON is JCS.
    data = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(json.dumps(value, indent=2).encode() + b"\n")
    path.chmod(0o644)
    return "sha256:" + hashlib.sha256(data).hexdigest()


portable = {
    "apiVersion": "agentsuite.dev/suite-policy/v1alpha1", "suite": "registry-check",
    "defaults": {"filesystemWrite": "deny", "network": "deny", "invocations": "deny"},
    "resources": [{"id": "inference", "kind": "model"},
                  {"id": "registry", "kind": "https", "destination": {"scheme": "https", "host": "mcr.microsoft.com", "port": 443}}],
    "agents": [],
}
for name in ("coordinator", "registry-reader"):
    p = json.loads((policies / (name + ".json")).read_text())
    portable["agents"].append({"agent": name, "capabilities": p["capabilities"],
                              "resources": ["inference"] + (["registry"] if name == "registry-reader" else []),
                              "invocations": p["invocations"]["allow"]})
policy_digest = write(root / "suite-policy.json", portable)
for bundled in (True, False):
    directory = root / ("bundled" if bundled else "external")
    suite = json.loads((source / "agentsuite.json").read_text())
    base_agent = json.loads((source / suite["agents"][0]["path"]).read_text())
    base_composition = json.loads((source / suite["compositions"][0]["path"]).read_text())
    suite.update(name="registry-check", agents=[], compositions=[])
    for name in ("coordinator", "registry-reader"):
        agent = copy.deepcopy(base_agent)
        instruction = ("Report measured registry results briefly; never invent evidence.\n").encode()
        instruction_path = directory / "instructions" / (name + ".md")
        instruction_path.parent.mkdir(parents=True, exist_ok=True)
        instruction_path.write_bytes(instruction)
        instruction_path.chmod(0o644)
        agent.update(id=name, description=name, model={"protocol": "openai-compatible", "model": "inference"},
                     instructions={"path": "instructions/" + name + ".md", "digest": "sha256:" + hashlib.sha256(instruction).hexdigest()},
                     toolProviders=[], invokes=[{"agent": "registry-reader", "maxConcurrent": 1, "maxDepth": 1}] if name == "coordinator" else [])
        path = "agents/" + name + ".json"
        suite["agents"].append({"id": name, "path": path, "digest": write(directory / path, agent)})
        composition = copy.deepcopy(base_composition)
        composition.update(agent=name, toolProviders=[])
        path = "compositions/" + name + ".json"
        suite["compositions"].append({"agent": name, "platform": composition["platform"], "path": path,
                                      "digest": write(directory / path, composition)})
    for field in ("buildProfiles",):
        for ref in suite[field]:
            ref["digest"] = write(directory / ref["path"], json.loads((source / ref["path"]).read_text()))
    catalog = suite["toolProviderCatalog"]
    catalog["digest"] = write(directory / catalog["path"], {"schemaVersion": "1.0.0-draft",
        "mediaType": "application/vnd.agentsuite.tool.provider.catalog.v1+json", "toolProviders": []})
    if bundled:
        suite["policy"] = {"path": "policy/suite-policy.json", "digest": write(directory / "policy/suite-policy.json", portable)}
    digest = write(directory / "agentsuite.json", suite)
    binding = {
        "apiVersion": "kaimahi.dev/policy-application/v1alpha1", "suiteDigest": digest, "policyDigest": policy_digest,
        "namespace": "suite-policy-example", "adapter": "cilium-cpu-v1",
        "gatewayImage": "ghcr.io/agentgateway/agentgateway@sha256:482921556876a503ad3675b29223b1897b63a316983e46797ff99ebf83a2a6a2",
        "agents": {name: {"workload": name, "runtime": name,
                   "image": "docker.io/library/python@sha256:cfe2e24a75302a15934d37c2d86412893c0aa934dc3a97cbb439d04c01890ca9"}
                   for name in ("coordinator", "registry-reader")},
        "model": {"resource": "inference", "workload": "cpu-model", "model": "qwen3:0.6b",
                  "image": "docker.io/ollama/ollama@sha256:b86366bb528bbf7f1424435d165028497a5b69bf6ddb4fa5a87102e2b79f44fb"},
    }
    write(root / ("binding-bundled.json" if bundled else "binding-external.json"), binding)
