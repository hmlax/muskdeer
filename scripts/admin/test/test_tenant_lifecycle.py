"""Guard the automation's failure semantics and credential artifacts."""

from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import stat
import tempfile
from threading import Thread
import unittest
from unittest.mock import patch

from tenant_lifecycle import Client, Report


class Responses(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/portal-echo":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"tenantCode": self.headers.get("X-Tenant-Code", "")}).encode())
            return
        if self.path == "/invalid-json":
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'{"code":404}Not Found')
            return
        status = 500 if self.path == "/server-error" else 403
        payload = {"code": status, "reason": "FORBIDDEN", "message": "unauthorized access"}
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(payload).encode())

    def log_message(self, *_args):
        # Local fake server traffic is asserted by tests; access logs add no evidence.
        return


class LifecycleAutomationTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.report = Report(self.directory.name)

    def client(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), Responses)
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()

        def close():
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
            self.assertFalse(thread.is_alive(), "fake server failed to stop")

        self.addCleanup(close)
        return Client("http://127.0.0.1:" + str(server.server_port), self.report)

    def test_server_error_never_passes_as_tenant_isolation(self):
        with self.assertRaisesRegex(AssertionError, "HTTP 500"):
            self.client().request("GET", "/server-error", expected=(403, 404))

    def test_wrong_reason_cannot_pass_a_quota_rejection(self):
        with self.assertRaisesRegex(AssertionError, "unexpected error message"):
            self.client().request("GET", "/quota", expected=403, message="API_CALL")

    def test_expected_rejection_is_accepted(self):
        payload = self.client().request("GET", "/denied", expected=403, reason="FORBIDDEN")
        self.assertEqual(payload["code"], 403)

    def test_merchant_requests_keep_the_portal_header_and_use_the_merchant_gateway(self):
        client = self.client()
        with patch.dict(os.environ, {"E2E_TENANT_BASE_URL": client.base}):
            client.tenant_code = "MerchantA"
            client.base = "http://127.0.0.1:1"
            self.assertEqual(client.request("GET", "/portal-echo")["tenantCode"], "MerchantA")

    def test_platform_requests_do_not_accept_the_merchant_base_as_the_platform(self):
        client = self.client()
        with patch.dict(os.environ, {"E2E_TENANT_BASE_URL": "http://127.0.0.1:1"}):
            self.assertEqual(client.request("GET", "/portal-echo")["tenantCode"], "")

    def test_malformed_error_body_is_an_informative_failure(self):
        with self.assertRaisesRegex(AssertionError, "not one JSON document.*HTTP 404"):
            self.client().request("GET", "/invalid-json", expected=404)

    def test_credentials_are_private_and_report_redacts_original_failure(self):
        secret = "generated-test-secret"
        self.report.secrets.append(secret)
        with redirect_stdout(io.StringIO()):
            self.report.save("http://localhost:15888", {"password": secret, "ready": False},
                             RuntimeError("login failed with " + secret))
        report = (self.report.directory / "report.json").read_text()
        self.assertNotIn(secret, report)
        self.assertIn("[REDACTED]", report)
        credentials = self.report.directory / "credentials.json"
        self.assertEqual(json.loads(credentials.read_text())["password"], secret)
        self.assertEqual(stat.S_IMODE(credentials.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(self.report.directory.stat().st_mode), 0o700)
        latest = json.loads((Path(self.directory.name) / "latest.json").read_text())
        self.assertEqual(latest["status"], "FAIL")
        self.assertFalse(latest["ready"])


if __name__ == "__main__":
    unittest.main()
