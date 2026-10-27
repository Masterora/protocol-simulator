"""Release regressions: binary identity, payload boundaries, and user projects."""
from pathlib import Path
import hashlib
import json
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from delivery import (NATIVE_SCENARIOS, check_logs, copy_user_payload, digest,
                      preserve_user_files, source_hashes, verify_current_build, verify_licenses, license_sections)


class DeliveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        for name in ["frontend", "verification", "examples", "build/bin"]:
            (self.base / name).mkdir(parents=True)
        (self.base / "frontend/app.js").write_text("const realUI = true;")
        self.binary = self.base / "build/bin/protocol-simulator.exe"
        self.binary.write_bytes(b"PE fixture const realUI = true;")
        for name in ["README.md"]:
            (self.base / name).write_text("User documentation")
        (self.base / "THIRD-PARTY.txt").write_text("=== Module: Go runtime ===\nLicense body\n")
        (self.base / "examples/local.json").write_text('{"channels":[]}')

    def record(self, platform="windows"):
        logs = {"brand-go-tests.txt": "--- PASS: TestCore (0.01s)\nPASS\nok gytest-go/internal/core\n",
                "brand-ui-tests.txt": "tests 182\npass 182\nfail 0\n",
                "brand-window-smoke.txt": "verified_binary_sha256=" + digest(self.binary) + "\n" + "native process and frontend bridge ready\nvisible layout verified\n" * NATIVE_SCENARIOS + "--- PASS: TestNativeWindowSmoke\n",
                "vet.txt": "", "packaging-tests.txt": "Ran 8 tests\nOK\n",
                "quality-checks.txt": "All matched files use Prettier code style\n"}
        if platform == "macos":
            logs.update({"macos-race-tests.txt": "ok gytest-go/internal/core\n", "macos-serial-tests.txt": "--- PASS: TestSerialTransportLoopback\n"})
        for name, content in logs.items():
            (self.base / "verification" / name).write_text(content)
        sources = source_hashes(self.base)
        proof = {"platform": platform, "binarySHA256": digest(self.binary), "sourceSHA256": sources,
                 "sourceFingerprint": hashlib.sha256(json.dumps(sources, sort_keys=True).encode()).hexdigest(),
                 "logSHA256": {name: digest(self.base / "verification" / name) for name in logs}}
        (self.base / "verification/build-evidence.json").write_text(json.dumps(proof))
        return proof

    def test_each_platform_requires_matching_full_native_checks(self):
        for platform in ["windows", "macos"]:
            self.record(platform)
            verify_current_build(self.base, self.binary, platform)
            log = self.base / "verification/brand-window-smoke.txt"
            log.write_text(log.read_text().replace(digest(self.binary), "0" * 64))
            with self.assertRaisesRegex(ValueError, "different executable"):
                verify_current_build(self.base, self.binary, platform)

    def test_windows_powershell_utf16_logs_are_supported(self):
        self.record()
        for name in ["brand-ui-tests.txt", "brand-window-smoke.txt"]:
            path = self.base / "verification" / name
            path.write_text(path.read_text(encoding="utf-8"), encoding="utf-16")
        checks, _ = check_logs(self.base, self.binary, "windows")
        self.assertEqual(checks["uiTests"], 182)
        self.assertEqual(checks["nativeLayouts"], NATIVE_SCENARIOS)

    def test_linked_dependency_license_and_version_are_required(self):
        modules = [("example.com/serial", "v1.0.0")]
        with self.assertRaisesRegex(ValueError, "no license body"):
            verify_licenses(self.base, modules)
        (self.base / "THIRD-PARTY.txt").write_text("=== Module: example.com/serial v1.0.0 ===\n\n")
        with self.assertRaisesRegex(ValueError, "no license body"):
            verify_licenses(self.base, modules)
        (self.base / "THIRD-PARTY.txt").write_text("=== Module: example.com/serial v1.0.0 ===\nLicense\n=== LICENSE ===\nFull nested text\n")
        verify_licenses(self.base, modules)
        self.assertEqual(license_sections(self.base)["example.com/serial v1.0.0"], "License\n=== LICENSE ===\nFull nested text")
        with self.assertRaisesRegex(ValueError, "no license body"):
            verify_licenses(self.base, [("example.com/serial", "v2.0.0")])

    def test_partial_native_layout_run_is_rejected(self):
        self.record()
        log = self.base / "verification/brand-window-smoke.txt"
        log.write_text(log.read_text().replace("visible layout verified\n", "", 1))
        with self.assertRaisesRegex(ValueError, "Complete native"):
            verify_current_build(self.base, self.binary, "windows")

    def test_source_or_verification_log_drift_requires_rebuild(self):
        self.record()
        (self.base / "frontend/app.js").write_text("const realUI = false;")
        with self.assertRaisesRegex(ValueError, "Source changed"):
            verify_current_build(self.base, self.binary, "windows")
        self.record()
        log = self.base / "verification/brand-go-tests.txt"
        log.write_text(log.read_text() + "different run\n")
        with self.assertRaisesRegex(ValueError, "logs changed"):
            verify_current_build(self.base, self.binary, "windows")

    def test_stale_embedded_frontend_is_rejected(self):
        self.record()
        self.binary.write_bytes(b"old executable without UI")
        self.record()
        with self.assertRaisesRegex(ValueError, "stale frontend"):
            verify_current_build(self.base, self.binary, "windows")

    def test_payload_keeps_licenses_and_examples_without_historical_artifacts(self):
        proof = self.record()
        (self.base / "verification/large-project.json").write_bytes(b"large project" * 1000)
        stage = self.base / "stage"
        stage.mkdir()
        checks, _ = check_logs(self.base, self.binary, "windows")
        copy_user_payload(self.base, stage, proof, checks)
        self.assertIn("License body", (stage / "THIRD-PARTY.txt").read_text())
        self.assertTrue((stage / "examples/local.json").is_file())
        self.assertEqual([p.name for p in (stage / "verification").iterdir()], ["build.json"])
        self.assertTrue((self.base / "verification/large-project.json").exists())

    def test_missing_license_body_prevents_delivery(self):
        proof = self.record()
        (self.base / "THIRD-PARTY.txt").write_text("=== Module: Go runtime ===\n\n")
        stage = self.base / "stage"
        stage.mkdir()
        with self.assertRaisesRegex(ValueError, "missing license"):
            copy_user_payload(self.base, stage, proof, {})

    def test_user_projects_survive_repeated_package_updates(self):
        previous = self.base / "previous"
        previous.mkdir()
        (previous / "generated.txt").write_text("original")
        manifest = {"generated.txt": {"sha256": digest(previous / "generated.txt")}}
        (previous / "file-manifest.json").write_text(json.dumps(manifest))
        (previous / "generated.txt").write_text("user edits")
        (previous / "工程.json").write_text('{"name":"my project"}')
        stage = self.base / "stage"
        stage.mkdir()
        preserve_user_files(previous, stage)
        self.assertEqual((stage / "保留文件/工程.json").read_text(), '{"name":"my project"}')
        self.assertEqual((stage / "保留文件/generated.txt").read_text(), "user edits")
        new_manifest = {p.relative_to(stage).as_posix(): {"sha256": digest(p)} for p in stage.rglob("*") if p.is_file()}
        (stage / "file-manifest.json").write_text(json.dumps(new_manifest))
        again = self.base / "again"
        again.mkdir()
        preserve_user_files(stage, again)
        self.assertEqual((again / "保留文件/工程.json").read_bytes(), (stage / "保留文件/工程.json").read_bytes())
        self.assertFalse((again / "保留文件/保留文件").exists())

    def test_generated_runtime_does_not_change_source_identity(self):
        before = source_hashes(self.base)
        runtime = self.base / "frontend/wailsjs/runtime"
        runtime.mkdir(parents=True)
        (runtime / "runtime.js").write_text("generated code")
        self.assertEqual(source_hashes(self.base), before)


if __name__ == "__main__":
    unittest.main()
