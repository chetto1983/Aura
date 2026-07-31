import json
import unittest
import urllib.request

from production_load_chaos import (
    MCPFixture,
    REQUIRED_CHAOS_SCENARIOS,
    Toxiproxy,
    validate_chaos_report,
    validate_load_report,
)


class LoadReportValidationTest(unittest.TestCase):
    def test_accepts_complete_report_at_budgets(self):
        report = {
            "passed": True,
            "supported_concurrency": 32,
            "resource": {
                "peak_rss_bytes": 100 * 1024 * 1024,
                "max_rss_bytes": 512 * 1024 * 1024,
            },
            "endpoints": {
                "healthz": {
                    "requests": 200,
                    "throughput_rps": 24.9,
                    "success_ratio": 1.0,
                    "p50_ms": 2.0,
                    "p95_ms": 8.0,
                    "p99_ms": 15.0,
                    "p95_budget_ms": 250.0,
                },
                "readyz": {
                    "requests": 200,
                    "throughput_rps": 24.8,
                    "success_ratio": 1.0,
                    "p50_ms": 15.0,
                    "p95_ms": 80.0,
                    "p99_ms": 120.0,
                    "p95_budget_ms": 2000.0,
                },
            },
        }
        validate_load_report(report)

    def test_rejects_missing_endpoint_or_resource_overrun(self):
        with self.assertRaisesRegex(ValueError, "readyz"):
            validate_load_report(
                {
                    "passed": True,
                    "supported_concurrency": 32,
                    "resource": {
                        "peak_rss_bytes": 1,
                        "max_rss_bytes": 2,
                    },
                    "endpoints": {
                        "healthz": {
                            "requests": 1,
                            "success_ratio": 1.0,
                            "p95_ms": 1.0,
                            "p95_budget_ms": 2.0,
                        }
                    },
                }
            )
        with self.assertRaisesRegex(ValueError, "RSS"):
            validate_load_report(
                {
                    "passed": True,
                    "supported_concurrency": 32,
                    "resource": {
                        "peak_rss_bytes": 513 * 1024 * 1024,
                        "max_rss_bytes": 512 * 1024 * 1024,
                    },
                    "endpoints": {
                        name: {
                            "requests": 1,
                            "success_ratio": 1.0,
                            "p95_ms": 1.0,
                            "p95_budget_ms": 2.0,
                        }
                        for name in ("healthz", "readyz")
                    },
                }
            )


class ChaosReportValidationTest(unittest.TestCase):
    def test_requires_every_scenario_and_postcondition(self):
        report = {
            "passed": True,
            "scenarios": [
                {
                    "id": scenario,
                    "executed": True,
                    "passed": True,
                    "cleanup_ok": True,
                }
                for scenario in REQUIRED_CHAOS_SCENARIOS
            ],
        }
        validate_chaos_report(report)
        report["scenarios"][0]["cleanup_ok"] = False
        with self.assertRaisesRegex(ValueError, "cleanup"):
            validate_chaos_report(report)


class MCPFixtureTest(unittest.TestCase):
    def test_streamable_http_memory_surface(self):
        with MCPFixture() as fixture:
            initialize = self.post(
                fixture.url,
                {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "initialize",
                    "params": {},
                },
            )
            self.assertEqual(
                initialize["result"]["protocolVersion"],
                "2025-06-18",
            )
            tools = self.post(
                fixture.url,
                {
                    "jsonrpc": "2.0",
                    "id": 2,
                    "method": "tools/list",
                    "params": {},
                },
            )
            self.assertEqual(
                [tool["name"] for tool in tools["result"]["tools"]],
                ["memory_search"],
            )
            called = self.post(
                fixture.url,
                {
                    "jsonrpc": "2.0",
                    "id": 3,
                    "method": "tools/call",
                    "params": {"name": "memory_search", "arguments": {}},
                },
            )
            text = called["result"]["content"][0]["text"]
            self.assertEqual(
                json.loads(text),
                {"results": {"preferences": []}},
            )

    @staticmethod
    def post(url, body):
        request = urllib.request.Request(
            url,
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=2) as response:
            return json.load(response)


class ToxiproxyConfigurationTest(unittest.TestCase):
    def test_host_ports_are_unique(self):
        proxy = Toxiproxy(19091, "unit")
        ports = [proxy.control_port, *proxy.host_ports.values()]
        self.assertEqual(len(ports), len(set(ports)))


if __name__ == "__main__":
    unittest.main()
