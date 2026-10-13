import io
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch

import zapp


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        for name, text in {
            "main.go": 'package main\n//go:embed static migrations/*.sql\n',
            "static_assets.go": 'package main\n',
            "static_assets_dev.go": 'package main\n// development only',
            "main_test.go": 'package main\n',
            "static/index.html": '<html>Focus</html>',
            "migrations/001.sql": 'CREATE TABLE notes(id TEXT);',
            "internal/app/app.go": 'package app',
            "go.mod": 'module focus-space\ngo 1.25.0\n',
            "spec.yaml": 'name: plaud-action-dashboard\ncapabilities:\n  ai: {}\n',
            "data/app.db": 'private user data',
            ".dev-env": 'private token',
        }.items():
            p = self.source / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(text, encoding="utf-8")
        metadata = {"schema_version":1,"name":zapp.SLUG,"display_name":"焦点空间","description":"Focus","icon_emoji":"F","generation":{"created_by":"focus-space"},"owner_user_id":"private-owner","capabilities":{"device_data":["device_meetings_read"],"app_data":{"version":1,"operations":[]}}}
        (self.source / "metadata.json").write_bytes(zapp.json_bytes(metadata))
        self.binary = self.root / "app"
        elf = bytearray(64)
        elf[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", elf, 18, 183)
        self.binary.write_bytes(elf)  # fixture header only; never launched
        self.package = self.root / "focus.zapp"

    def pack(self):
        return zapp.package(self.source, self.binary, self.package, "test-revision")

    def test_stage_and_package_exclude_private_files_flow(self):
        staged = zapp.stage(self.source, self.root / "staged")
        self.assertTrue((staged / "src/main.go").exists())
        self.assertFalse((staged / "src/static_assets_dev.go").exists())
        first = self.pack()
        raw = self.package.read_bytes()
        self.assertEqual(first, zapp.inspect_package(self.package))
        self.pack()
        self.assertEqual(raw, self.package.read_bytes(), "package must be reproducible")
        with tarfile.open(self.package, "r:gz") as archive:
            names = archive.getnames()
            self.assertEqual(names[0], "manifest.json")
            self.assertIn("app/src/main.go", names)
            self.assertFalse(any("data/" in n or "_dev.go" in n or "_test.go" in n or ".dev-env" in n for n in names))
            metadata = json.load(archive.extractfile("app/metadata.json"))
            self.assertNotIn("owner_user_id", metadata)
            self.assertEqual(set(metadata["capabilities"]), {"app_data"})

    def test_reject_windows_binary(self):
        self.binary.write_bytes(b"MZ" + bytes(62))
        with self.assertRaisesRegex(ValueError, "Linux ELF"):
            self.pack()

    def test_import_preflights_capabilities_and_streams_without_retry_flow(self):
        self.pack()
        requests = []
        def api(base, token, method, route, body=None, size=None):
            requests.append((method, route))
            if method == "GET":
                return {"features":["explicit_device_data_v1","preserve_slug_v1"],"device_data":["device_meetings_read"]}
            self.assertTrue(hasattr(body, "read"))
            self.assertEqual(len(body.read()), size)
            self.assertIn("grant_device_data=device_meetings_read", route)
            self.assertIn("on_conflict=replace", route)
            return {"slug":zapp.SLUG,"renamed":False,"app":{"capabilities":{"granted":["app_ai","device_meetings_read"]}}}
        with patch.object(zapp, "api_request", api):
            zapp.import_package(self.package,"http://127.0.0.1:9090","private",["device_meetings_read"],True)
        self.assertEqual(len(requests),2)
        with patch.object(zapp,"api_request",return_value={"features":[]}) as api:
            with self.assertRaisesRegex(ValueError,"update the device"):
                zapp.import_package(self.package,"http://127.0.0.1:9090","private",[])
            self.assertEqual(api.call_count,1)

    def test_http_import_transport_and_redirect_failure_flow(self):
        self.pack()
        calls = []
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                calls.append(("GET", self.path))
                fixture.assertEqual(self.headers.get("Authorization"), "Bearer test-token")
                if self.path == "/redirect":
                    self.send_response(307)
                    self.send_header("Location", "/must-not-follow")
                    self.end_headers()
                    return
                self.reply({"features":["explicit_device_data_v1","preserve_slug_v1"],"device_data":["device_meetings_read"]})

            def do_POST(self):
                calls.append(("POST", self.path))
                fixture.assertEqual(self.headers.get("Content-Type"), "application/octet-stream")
                raw = self.rfile.read(int(self.headers["Content-Length"]))
                fixture.assertEqual(raw, fixture.package.read_bytes())
                self.reply({"slug":zapp.SLUG,"app":{"capabilities":{"granted":["app_ai","device_meetings_read"]}}})

            def reply(self, data):
                raw = zapp.json_bytes({"code":200,"data":data})
                self.send_response(200)
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}"
            zapp.import_package(self.package, base, "test-token", ["device_meetings_read"])
            self.assertEqual(len(calls), 2)
            with self.assertRaisesRegex(ValueError, "HTTP 307"):
                zapp.api_request(base, "test-token", "GET", "/redirect")
            self.assertEqual(len(calls), 3, "redirect must neither follow nor retry")
        finally:
            server.shutdown()
            server.server_close()
            worker.join(timeout=2)

    def test_import_missing_grants_and_oversized_upload_fail_closed(self):
        self.pack()
        features = {"features":["explicit_device_data_v1","preserve_slug_v1"],"device_data":["device_meetings_read"]}
        installed = {"slug":zapp.SLUG,"app":{"capabilities":{"granted":["app_ai"]}}}
        with patch.object(zapp, "api_request", side_effect=[features, installed]) as api:
            with self.assertRaisesRegex(ValueError, "installed but required capabilities"):
                zapp.import_package(self.package, "http://127.0.0.1:9090", "private", ["device_meetings_read"])
            self.assertEqual(api.call_count, 2, "do not retry a partial installation")
        with patch.object(zapp, "MAX_GATEWAY_UPLOAD", 1), patch.object(zapp, "api_request") as api:
            with self.assertRaisesRegex(ValueError, "32 MiB"):
                zapp.import_package(self.package, "http://127.0.0.1:9090", "private", [])
            api.assert_not_called()

    def test_tampered_archive_is_rejected(self):
        self.pack()
        contents=[]
        with tarfile.open(self.package,"r:gz") as archive:
            for member in archive:
                contents.append((member,archive.extractfile(member).read()))
        with tarfile.open(self.package,"w:gz") as archive:
            for member, raw in contents:
                if member.name == "app/src/main.go": raw = b"x" * len(raw)
                archive.addfile(member,io.BytesIO(raw))
        with self.assertRaisesRegex(ValueError,"does not match"):
            zapp.inspect_package(self.package)


if __name__ == "__main__":
    unittest.main()
