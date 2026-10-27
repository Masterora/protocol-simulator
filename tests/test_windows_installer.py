"""Installer data boundaries and upgrade/uninstall preservation rules."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from windows_installer import generate_file_lists, installer_files, nsis_string, verify_application_resources, windows_resource_spec


class WindowsInstallerTests(unittest.TestCase):
    def test_changed_version_reaches_manifest_and_numeric_and_string_resources(self):
        source = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="resource fixture ") as directory:
            root = Path(directory)
            (root / "build/windows").mkdir(parents=True)
            for name in ("icon.ico", "app.manifest"):
                shutil.copy2(source / "build/windows" / name, root / "build/windows" / name)
            metadata = json.loads((source / "wails.json").read_text())
            metadata["info"]["productVersion"] = "13.27.4095"
            config = root / "wails.json"
            config.write_text(json.dumps(metadata))
            stub, binary = root / "stub.go", root / "test.exe"
            stub.write_text("package main\nfunc main() {}\n")
            env = {**os.environ, "GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"}
            subprocess.run(["go", "build", "-o", str(binary), str(stub)], env=env, check=True, capture_output=True)
            subprocess.run(["go", "run", ".", str(root), str(binary)], cwd=source / "scripts/windows-resources", check=True, capture_output=True)
            verify_application_resources(root, binary)
            metadata["info"]["productVersion"] = "13.27.4096"
            config.write_text(json.dumps(metadata))
            with self.assertRaisesRegex(ValueError, "manifest resource mismatch"):
                verify_application_resources(root, binary)
            metadata["info"]["productVersion"] = "65536.0.0"
            config.write_text(json.dumps(metadata))
            with self.assertRaisesRegex(ValueError, "65535"):
                windows_resource_spec(root)

    def payload(self, root):
        for name in ("protocol-simulator.exe", "README.md", "THIRD-PARTY.txt", "examples/工程.json",
                     "verification/build.json", "保留文件/用户工程.json"):
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture")

    def test_only_runtime_and_maintained_resources_enter_installer(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.payload(root)
            files = {p.relative_to(root).as_posix() for p in installer_files(root)}
            self.assertEqual(files, {"protocol-simulator.exe", "README.md", "THIRD-PARTY.txt",
                                     "examples/工程.json", "verification/build.json"})

    def test_editable_examples_survive_upgrade_and_uninstall(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload, output = root / "payload", root / "output"
            output.mkdir()
            self.payload(payload)
            generate_file_lists(payload, output)
            install = (output / "install-files.nsh").read_text(encoding="utf-8-sig")
            uninstall = (output / "uninstall-files.nsh").read_text(encoding="utf-8-sig")
            self.assertIn('SetOverwrite off\nSetOutPath "$INSTDIR\\examples"', install)
            self.assertNotIn('Delete "$INSTDIR\\examples\\工程.json"', uninstall)
            self.assertIn('Delete "$INSTDIR\\protocol-simulator.exe"', uninstall)
            self.assertIn('Delete "$INSTDIR\\verification\\build.json"', uninstall)
            self.assertNotIn('RMDir /r', uninstall)
            self.assertNotIn('用户工程', uninstall)

    def test_paths_do_not_inject_nsis_expressions(self):
        self.assertEqual(nsis_string('a$TEMP"b'), 'a$$TEMP$\\"b')
        with self.assertRaisesRegex(ValueError, "Control character"):
            nsis_string("file\nQuit")

    @unittest.skipIf(sys.platform == "win32", "Symlink staging check requires Unix symlinks")
    def test_linked_payload_outside_staging_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "payload"
            self.payload(root)
            (root / "examples/external.json").symlink_to(root.parent / "outside.json")
            (root.parent / "outside.json").write_text("outside")
            try:
                with self.assertRaisesRegex(ValueError, "escapes staging"):
                    installer_files(root)
            finally:
                (root.parent / "outside.json").unlink()


if __name__ == "__main__":
    unittest.main()
