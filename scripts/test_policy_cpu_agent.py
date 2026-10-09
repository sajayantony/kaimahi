#!/usr/bin/env python3
"""Offline checks for the CPU example's contracts; not enforcement evidence."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("policy_cpu_agent", Path(__file__).parent / "ci" / "policy-cpu-agent.py")
agent = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent)


class PolicyCPUAgentTests(unittest.TestCase):
    def test_text_envelope_and_rejected_inputs(self):
        self.assertEqual(agent.input_text({"message": agent.message("check")}), "check")
        for bad in (None, [], {}, {"role": "ROLE_AGENT", "messageId": "x", "parts": [{"text": "x"}]},
                    {"role": "ROLE_USER", "messageId": "x", "parts": [{"url": "https://example.com"}]}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                agent.input_text({"message": bad})
        with self.assertRaises(ValueError):
            agent.input_text({"message": agent.message("x" * 4001)})

    def test_model_failures_cannot_become_successful_answers(self):
        for result in (
            {"message": {"content": ""}, "done": True, "eval_count": 1},
            {"message": {"content": "text"}, "done": False, "eval_count": 1},
            {"message": {"content": "text"}, "done": True, "eval_count": 0},
        ):
            with patch.object(agent, "http_json", return_value=result), self.assertRaises(RuntimeError):
                agent.infer("gateway", "prompt")
        with patch.object(agent, "http_json", side_effect=RuntimeError("model unavailable")), self.assertRaises(RuntimeError):
            agent.workflow("coordinator", "gateway", "check")

    def test_coordinator_uses_fixed_peer_route_and_real_peer_evidence(self):
        peer = {"message": {"metadata": {"policyExample": {
            "registry": {"status": 200}, "summary": "reader result", "model": {"eval_count": 3}}}}}
        with patch.object(agent, "http_json", return_value=peer) as http, \
                patch.object(agent, "infer", return_value=("coordinator result", {"eval_count": 4})) as model:
            result = agent.workflow("coordinator", "gateway", "inspect")
        self.assertEqual(http.call_args.args[:3], ("gateway", 3001, "/skills/inspect-registry/message:send"))
        self.assertIn("reader result", model.call_args.args[1])
        evidence = result["message"]["metadata"]["policyExample"]
        self.assertEqual(evidence["peer"]["registry"]["status"], 200)
        self.assertEqual(evidence["model"]["eval_count"], 4)

    def test_connection_refusal_is_not_policy_denial(self):
        with patch.object(agent.socket, "create_connection", side_effect=ConnectionRefusedError()), self.assertRaises(ConnectionRefusedError):
            agent.probe("direct-model", {"modelIP": "192.0.2.1", "peerIP": "192.0.2.2",
                "otherGatewayIP": "192.0.2.3", "mcrIP": "192.0.2.4"})

    def test_unknown_probe_fails_explicitly(self):
        with self.assertRaises(ValueError):
            agent.probe("unknown", {"modelIP": "192.0.2.1", "peerIP": "192.0.2.2",
                "otherGatewayIP": "192.0.2.3", "mcrIP": "192.0.2.4"})


if __name__ == "__main__":
    unittest.main()
