"""Exercise cleanup boundaries with real files, mutations and packaging failures."""
from pathlib import Path
import json
import os
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
from cleanup_artifacts import ArtifactCleanup
import package_protocol


class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.addCleanup(patch.stopall)
        patch('cleanup_artifacts.process_commands', return_value='').start()

    def file(self, relative, content='generated'):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding='utf-8')
        os.utime(path, (time.time() - 100, time.time() - 100))
        return path

    def test_only_unchanged_historical_output_is_removed(self):
        old = self.file('work/old/smoke.txt')
        receipt = self.file('verification/old/result.json', '{"passed":true}')
        edited = self.file('work/old/edited.log')
        touched = self.file('work/old/touched.log')
        cleanup = ArtifactCleanup(self.root)
        edited.write_text('user edit')
        os.utime(touched, None)
        new = self.file('work/current/new.txt')
        report = cleanup.finish()
        self.assertFalse(old.exists())
        self.assertFalse(receipt.exists())
        self.assertEqual(edited.read_text(), 'user edit')
        self.assertTrue(touched.exists())
        self.assertTrue(new.exists())
        self.assertEqual(len(report['removed']), 2)
        self.assertTrue((self.root / 'verification/cleanup-artifacts.json').exists())

    def test_projects_configs_sources_outputs_and_current_evidence_survive(self):
        keep = [self.file(n, text) for n, text in [
            ('work/old/工程.json', '{"channels":[],"name":"工程"}'),
            ('verification/old/device.json', '{"devices":[],"passed":true}'),
            ('work/old/unknown.json', '{"important":"value"}'),
            ('work/old/broken.json', '{not json'),
            ('work/old/settings.txt', 'user settings'),
            ('work/old/.env', 'PRIVATE=value'),
            ('work/old/source.go', 'package main'),
            ('work/old/notes.md', 'manual evidence'),
            ('work/go.mod', 'module work'),
            ('work/golang/cache/test.bin', 'cached toolchain'),
            ('work/installer-runtime/bootstrap.exe', 'cached runtime'),
            ('outputs/application.dmg', 'installer'),
            ('examples/project.json', '{"channels":[]}'),
            ('tests/source_test.go', 'package test'),
            ('verification/build-evidence.json', '{"passed":true}'),
            ('verification/brand-go-tests.txt', 'current evidence'),
            ('verification/linux-server-tests.txt', 'current server evidence'),
        ]]
        self.assertEqual(ArtifactCleanup(self.root).finish()['removed'], [])
        self.assertTrue(all(p.exists() for p in keep))

    def test_current_build_evidence_and_running_tasks_survive(self):
        started = time.time() - 10
        current = self.file('verification/current/smoke.txt')
        os.utime(current, None)
        active = self.file('work/active/result.log')
        patch('cleanup_artifacts.process_commands', return_value=f'python {self.root}/work/active/script.py').start()
        ArtifactCleanup(self.root, started).finish()
        self.assertTrue(current.exists())
        self.assertTrue(active.exists())

    def test_running_task_root_log_survives(self):
        log = self.file('verification/live-run.txt')
        patch('cleanup_artifacts.process_commands', return_value='bash worker.sh > verification/live-run.txt').start()
        ArtifactCleanup(self.root).finish()
        self.assertTrue(log.exists())

    @unittest.skipIf(os.name == 'nt', 'Symlink creation needs extra privileges on Windows')
    def test_symlinked_root_and_replaced_ancestor_cannot_escape(self):
        outside = self.file('outside/important.txt', 'user data')
        directory = self.root / 'work'
        directory.symlink_to(outside.parent, target_is_directory=True)
        self.assertEqual(ArtifactCleanup(self.root).finish()['removed'], [])
        directory.unlink()
        original = self.file('work/old/important.txt')
        cleanup = ArtifactCleanup(self.root)
        original.unlink()
        original.parent.rmdir()
        original.parent.symlink_to(outside.parent, target_is_directory=True)
        cleanup.finish()
        self.assertEqual(outside.read_text(), 'user data')
        self.assertTrue(original.parent.is_symlink())

    def test_process_inventory_failure_preserves_everything(self):
        artifact = self.file('work/old/output.log')
        patch('cleanup_artifacts.process_commands', side_effect=OSError('ps unavailable')).start()
        self.assertIn('skipped', ArtifactCleanup(self.root).finish())
        self.assertTrue(artifact.exists())

    @unittest.skipIf(os.name == 'nt', 'Symlink creation needs extra privileges on Windows')
    def test_linked_evidence_root_is_never_written(self):
        outside = self.root / 'outside'
        outside.mkdir()
        (self.root / 'verification').symlink_to(outside, target_is_directory=True)
        self.assertIn('skipped', ArtifactCleanup(self.root).finish())
        self.assertEqual(list(outside.iterdir()), [])

    def test_second_cleanup_does_not_accumulate_reports(self):
        self.file('work/old/output.log')
        ArtifactCleanup(self.root).finish()
        self.assertEqual(ArtifactCleanup(self.root).finish()['removed'], [])
        self.assertEqual([p.name for p in (self.root / 'verification').iterdir()], ['cleanup-artifacts.json'])

    def test_custom_output_and_empty_current_directories_survive(self):
        package = self.file('work/custom-output/package.deb')
        current = self.root / 'work/current/empty'
        current.mkdir(parents=True)
        old = self.file('work/old/output.log')
        ArtifactCleanup(self.root, preserve=[package.parent]).finish()
        self.assertTrue(package.exists())
        self.assertTrue(current.is_dir())
        self.assertFalse(old.exists())

    def package(self, fail=False):
        self.file('scripts/package_protocol.py')
        self.file('build/bin/protocol-simulator.exe')
        self.file('verification/build-start.json', json.dumps({'started': time.time() - 10}))
        patch.object(package_protocol, '__file__', str(self.root / 'scripts/package_protocol.py')).start()
        patch.object(package_protocol.sys, 'platform', 'win32').start()
        patch.object(package_protocol, 'verify_current_build', return_value=({}, {})).start()
        patch.object(package_protocol, 'copy_user_payload').start()

        def installer(base, payload, target):
            if fail:
                raise RuntimeError('installer verification failed')
            target.write_bytes(b'verified setup')

        patch.object(package_protocol, 'build_installer', side_effect=installer).start()
        package_protocol.package()

    def test_successful_publication_triggers_cleanup(self):
        artifact = self.file('work/old/output.log')
        self.package()
        self.assertFalse(artifact.exists())
        self.assertEqual((self.root / 'outputs/protocol-simulator-Setup.exe').read_bytes(), b'verified setup')

    def test_failed_packaging_keeps_existing_delivery_and_diagnostics(self):
        artifact = self.file('work/old/output.log')
        setup = self.file('outputs/protocol-simulator-Setup.exe', 'last good installer')
        with self.assertRaisesRegex(RuntimeError, 'verification failed'):
            self.package(fail=True)
        self.assertTrue(artifact.exists())
        self.assertEqual(setup.read_text(), 'last good installer')
        self.assertFalse((self.root / 'verification/cleanup-artifacts.json').exists())


if __name__ == '__main__':
    unittest.main()
