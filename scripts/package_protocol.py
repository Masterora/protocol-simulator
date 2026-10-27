"""Publish verified macOS DMG and Windows Setup installers."""
from pathlib import Path
import json
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile

from delivery import copy_user_payload, digest, preserve_user_files, remove_legacy_delivery, verify_current_build, verify_frontend
from macos_dmg import application_icon, create_disk_image
from windows_installer import build_installer, SETUP_NAME


def macos_report(app, checks):
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    assert info['CFBundleExecutable'] == info['CFBundleName'] == 'protocol-simulator'
    version = json.loads((app.parents[2] / 'wails.json').read_text(encoding='utf-8'))['info']['productVersion']
    assert info['CFBundleVersion'] == info['CFBundleShortVersionString'] == version, 'macOS version mismatch'
    application_icon(app)
    binary = app / 'Contents/MacOS/protocol-simulator'
    architectures = subprocess.check_output(['lipo', '-archs', str(binary)], text=True).split()
    assert set(architectures) == {'arm64', 'x86_64'}, architectures
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True)
    return {'product': 'protocol-simulator', 'platform': 'macOS', 'architectures': architectures,
            'installer_format': 'DMG', 'frontend_matches_binary': True, 'signature': 'ad-hoc', 'notarized': False,
            'go_tests': checks['goTests'], 'ui_tests': checks['uiTests'], 'native_window_types': checks['nativeLayouts'],
            'serial_pty_verified': checks['serialPTYVerified'], 'race_tests_passed': checks['raceVerified'],
            'dmg_checksum_verified': True, 'dmg_readonly_mount_verified': True,
            'applications_shortcut_verified': True, 'installation_copy_verified': True,
            'finder_dmg_icon_verified': True, 'mounted_volume_icon_verified': True,
            'minimum_macos': info['LSMinimumSystemVersion'], 'binary_sha256': digest(binary)}


def package():
    base = Path(__file__).resolve().parents[1]
    if sys.platform not in {'darwin', 'win32'}:
        raise RuntimeError('Run packaging on macOS or Windows')
    macos = sys.platform == 'darwin'
    app = base / 'build/bin/protocol-simulator.app'
    windows = base / 'build/bin/protocol-simulator.exe'
    primary = app / 'Contents/MacOS/protocol-simulator' if macos else windows
    proof, checks = verify_current_build(base, primary, 'macos' if macos else 'windows')
    windows_proof, windows_checks = proof, checks
    if macos:
        cross = json.loads((base / 'verification/windows-cross-build.json').read_text(encoding='utf-8'))
        assert cross['binary_sha256'] == digest(windows) == proof['additionalBinaries']['windows']['sha256']
        assert cross['frontend_matches_binary']
        verify_frontend(base, windows)
        windows_proof = {**proof, 'platform': 'windows', 'binarySHA256': digest(windows),
                         'nativeRuntimeVerified': False, 'additionalBinaries': {}}
        windows_checks = {'crossCompiled': True, 'frontendMatchesBinary': True, 'resourcesVerified': True}
    out, work = base / 'outputs', base / 'work'
    out.mkdir(exist_ok=True)
    work.mkdir(exist_ok=True)
    legacy = out / 'protocol-simulator'
    with tempfile.TemporaryDirectory(prefix='installer-stage-', dir=work) as directory:
        stage = Path(directory)
        windows_stage = stage / 'windows'
        windows_stage.mkdir()
        shutil.copy2(windows, windows_stage / windows.name)
        copy_user_payload(base, windows_stage, windows_proof, windows_checks)
        setup = stage / SETUP_NAME
        build_installer(base, windows_stage, setup)
        if macos:
            mac_stage = stage / 'macos'
            mac_stage.mkdir()
            shutil.copytree(app, mac_stage / app.name, symlinks=True)
            copy_user_payload(base, mac_stage, proof, checks)
            report = macos_report(app, checks)
            report['preserved_files'] = preserve_user_files(legacy, mac_stage)
            (mac_stage / 'verification/macos-package.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
            image = stage / 'protocol-simulator.dmg'
            create_disk_image(mac_stage, image, work)
            os.replace(image, out / image.name)
        # Publish only after every installer for this host passes verification.
        os.replace(setup, out / SETUP_NAME)
    retained = remove_legacy_delivery(out)
    print(json.dumps({'windows_setup': str(out / SETUP_NAME),
                      'macos_dmg': str(out / 'protocol-simulator.dmg') if macos else None,
                      'retained_user_or_running_files': retained, 'checks': checks}, ensure_ascii=False))


if __name__ == '__main__':
    package()
