#!/usr/bin/env python3
"""Generate the deterministic KMX agent-creator concept asciicast."""

from __future__ import annotations

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "docs" / "assets" / "agent-creator-copilot-demo.cast"

events: list[list[object]] = []
clock = 0.0


def emit(text: str, pause: float = 0.0) -> None:
    global clock
    events.append([round(clock, 3), "o", text])
    clock += pause


def wait(seconds: float) -> None:
    global clock
    clock += seconds


def type_text(text: str, delay: float = 0.025) -> None:
    global clock
    for char in text:
        events.append([round(clock, 3), "o", char])
        clock += delay


def command(text: str) -> None:
    emit("\033[1;32m❯\033[0m ")
    type_text(text)
    emit("\r\n")
    wait(0.45)


def user(text: str) -> None:
    emit("\033[1;36mYou\033[0m  ")
    type_text(text, 0.018)
    emit("\r\n")
    wait(0.55)


def copilot(text: str) -> None:
    emit("\033[1;35mCopilot\033[0m  " + text + "\r\n")
    wait(0.7)


def tool(name: str, detail: str) -> None:
    emit("\033[2m  ↳ MCP " + name + "\033[0m  " + detail + "\r\n")
    wait(0.55)


def heading(text: str) -> None:
    emit("\r\n\033[1;34m" + text + "\033[0m\r\n")
    wait(0.5)


emit("\033[2J\033[H")
emit("\033[1;33mKMX agent creator — Copilot + explicit deployment contexts\033[0m\r\n")
emit("\033[2mConcept recording: output is simulated; no Kubernetes resources are changed.\033[0m\r\n\r\n")
wait(1.0)

command("copilot --agent kmx-agent-creator")
emit("GitHub Copilot CLI\r\n")
emit("Agent: kmx-agent-creator  •  MCP: kmx-agent-creator connected\r\n\r\n")
wait(0.8)

user("Create a release-notes agent. It can run JavaScript, read the repo, and call GitHub. Run it on my local kind cluster.")
copilot("I’ll describe the workload and let KMX choose the runtime and sandbox.")
tool("kmx_agent_schema", "loading the current AgentIntent contract")
tool("kmx_agent_contexts", "reading available kube contexts")
emit("    kind-kaimahi-p1        local kind\r\n")
emit("    kind-kaimahi-staging   local kind\r\n")
emit("    aks-team-west           remote\r\n")
wait(0.7)
copilot("Which kube context should own the first deployment?")
emit("  \033[1m1\033[0m  kind-kaimahi-p1  \033[2m(local)\033[0m\r\n")
emit("  2  kind-kaimahi-staging\r\n")
emit("  3  aks-team-west\r\n")
emit("\033[1;36mYou\033[0m  ")
type_text("1")
emit("\r\n")
wait(0.6)

tool("kmx_agent_plan", "validating intent and selecting placement")
heading("KMX creation plan")
emit("  context    kind-kaimahi-p1\r\n")
emit("  namespace  agents\r\n")
emit("  runtime    orka\r\n")
emit("  sandbox    hyperlight-js\r\n")
emit("  reason     JavaScript-only; no shell, native packages, image, or device\r\n")
emit("  mutations  Provider/release-notes, Agent/release-notes\r\n")
emit("  digest     sha256:7d924f…b31a\r\n")
wait(1.1)
copilot("KMX selected Hyperlight JS as the smallest compatible sandbox. Deploy this exact plan to kind-kaimahi-p1?")
emit("\033[1;36mYou\033[0m  ")
type_text("yes, deploy it")
emit("\r\n")
wait(0.5)
tool("kmx_agent_apply", "approvedDigest=sha256:7d924f…b31a")
emit("  KMX guard: target kind-kaimahi-p1 / namespace agents\r\n")
emit("  ✓ Provider ready\r\n")
emit("  ✓ Agent ready\r\n")
emit("  receipt  kind-kaimahi-p1/agents/release-notes  revision sha256:7d924f…b31a\r\n")
wait(1.2)

command("kubectl --context kind-kaimahi-p1 -n agents get agent release-notes")
emit("NAME            READY   PROVIDER        SANDBOX\r\n")
emit("release-notes   True    release-notes   hyperlight-js\r\n")
wait(1.2)

heading("Later, in the same Copilot session")
user("Lift this agent to our other cluster.")
copilot("Lift is a mutation, so KMX needs the destination context explicitly.")
tool("kmx_agent_contexts", "excluding the current deployment target")
emit("  \033[1m1\033[0m  kind-kaimahi-staging\r\n")
emit("  2  aks-team-west\r\n")
copilot("Where should KMX deploy the reviewed revision?")
emit("\033[1;36mYou\033[0m  ")
type_text("aks-team-west")
emit("\r\n")
wait(0.55)
tool("kmx_agent_plan", "planning lift to context aks-team-west")
heading("KMX lift plan")
emit("  source      kind-kaimahi-p1/agents/release-notes\r\n")
emit("  destination aks-team-west/agents/release-notes\r\n")
emit("  revision    sha256:7d924f…b31a  \033[2m(unchanged)\033[0m\r\n")
emit("  sandbox     hyperlight-js\r\n")
emit("  mutations   reconcile Provider, reconcile Agent, verify Ready\r\n")
emit("  digest      sha256:bc118a…09ef\r\n")
wait(1.1)
copilot("Deploy this exact lift plan to aks-team-west?")
emit("\033[1;36mYou\033[0m  ")
type_text("approve sha256:bc118a…09ef")
emit("\r\n")
wait(0.5)
tool("kmx_agent_apply", "destination=aks-team-west approvedDigest=sha256:bc118a…09ef")
emit("  KMX guard: target aks-team-west / namespace agents\r\n")
emit("  ✓ Provider reconciled and Ready\r\n")
emit("  ✓ Agent reconciled and Ready\r\n")
emit("  receipt  aks-team-west/agents/release-notes  revision sha256:7d924f…b31a\r\n")
wait(1.2)

tool("kmx_agent_status", "comparing desired revision with both recorded targets")
emit("\r\n  CONTEXT             NAMESPACE  READY  REVISION\r\n")
emit("  kind-kaimahi-p1     agents     yes    sha256:7d924f…b31a\r\n")
emit("  aks-team-west       agents     yes    sha256:7d924f…b31a\r\n")
wait(1.2)
copilot("The same reviewed agent revision is Ready on both contexts.")
emit("\r\n\033[1;33mUser describes intent • Copilot converses • KMX plans, targets, and deploys\033[0m\r\n")
wait(2.0)

OUTPUT.parent.mkdir(parents=True, exist_ok=True)
with OUTPUT.open("w", encoding="utf-8") as stream:
    header = {
        "version": 2,
        "width": 112,
        "height": 34,
        "timestamp": 1790875800,
        "duration": round(clock, 3),
        "env": {"SHELL": "/bin/zsh", "TERM": "xterm-256color"},
        "title": "KMX agent creator with Copilot and explicit deployment contexts",
    }
    stream.write(json.dumps(header, separators=(",", ":")) + "\n")
    for event in events:
        stream.write(json.dumps(event, ensure_ascii=False, separators=(",", ":")) + "\n")

print(OUTPUT)
