"""Installer payload boundaries and integrity checks, independent of macOS tools."""
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from macos_dmg import APP_NAME, inventory, prepare_payload, verify_inventory


@unittest.skipIf(sys.platform == "win32", "Mac installer symlinks are verified on macOS")
class MacDiskImageTests(unittest.TestCase):
    def test_platform_payload_and_user_files_survive_drag_installer_preparation(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            release, payload = base / "release", base / "payload"
            binary = release / APP_NAME / "Contents/MacOS/protocol-simulator"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"universal app fixture")
            binary.chmod(0o755)
            (release / "protocol-simulator.exe").write_bytes(b"Windows-only fixture")
            (release / "file-manifest.json").write_text("{}")
            (release / "README.md").write_text("user guide")
            (release / "保留文件").mkdir()
            (release / "保留文件/工程.json").write_text('{"name":"user project"}')
            payload.mkdir()
            prepare_payload(release, payload)
            self.assertEqual((payload / "Applications").readlink(), Path("/Applications"))
            self.assertEqual((payload / APP_NAME / "Contents/MacOS/protocol-simulator").read_bytes(), binary.read_bytes())
            self.assertFalse((payload / "protocol-simulator.exe").exists())
            self.assertFalse((payload / "资料/protocol-simulator.exe").exists())
            self.assertFalse((payload / "资料/file-manifest.json").exists())
            self.assertEqual((payload / "资料/保留文件/工程.json").read_text(), '{"name":"user project"}')
            self.assertTrue((payload / "安装说明.txt").is_file())
            self.assertTrue(binary.exists())
            verify_inventory(payload, inventory(payload))

    def test_modified_content_permissions_and_shortcut_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            binary = base / "app"
            binary.write_bytes(b"original")
            binary.chmod(0o755)
            shortcut = base / "Applications"
            shortcut.symlink_to("/Applications", target_is_directory=True)
            expected = inventory(base)
            binary.write_bytes(b"changed")
            with self.assertRaisesRegex(ValueError, "content mismatch"):
                verify_inventory(base, expected)
            binary.write_bytes(b"original")
            binary.chmod(0o644)
            with self.assertRaisesRegex(ValueError, "permission mismatch"):
                verify_inventory(base, expected)
            binary.chmod(0o755)
            shortcut.unlink()
            shortcut.symlink_to("/tmp", target_is_directory=True)
            with self.assertRaisesRegex(ValueError, "symlink mismatch"):
                verify_inventory(base, expected)


if __name__ == "__main__":
    unittest.main()
