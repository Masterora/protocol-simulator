"""Verify and publish the current macOS app at the shared delivery paths."""
import copy
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import tempfile
import zipfile


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def remove_generated(path, parent):
    path, parent = path.resolve(), parent.resolve()
    if path == parent or not path.is_relative_to(parent):
        raise ValueError(f'Unsafe generated path: {path}')
    if path.is_dir():
        shutil.rmtree(path)
    elif path.exists():
        path.unlink()


def package():
    base = Path(__file__).resolve().parents[1]
    app = base / 'build/bin/protocol-simulator.app'
    binary = app / 'Contents/MacOS/protocol-simulator'
    executable = binary.read_bytes()
    for asset in (base / 'frontend').rglob('*'):
        if asset.is_file() and asset.read_bytes() not in executable:
            raise ValueError(f'Stale frontend: {asset.name}')
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    assert info['CFBundleExecutable'] == 'protocol-simulator'
    assert info['CFBundleName'] == 'protocol-simulator'
    icon = app / 'Contents/Resources' / info['CFBundleIconFile']
    if not icon.suffix:
        icon = icon.with_suffix('.icns')
    assert icon.read_bytes().startswith(b'icns'), 'Missing macOS icon'
    architectures = subprocess.check_output(['lipo', '-archs', str(binary)], text=True).split()
    assert set(architectures) == {'arm64', 'x86_64'}, architectures
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True)
    logs = {name: (base / 'verification' / name).read_text() for name in
            ('brand-go-tests.txt', 'brand-ui-tests.txt', 'brand-window-smoke.txt')}
    go_count = len(re.findall(r'^--- PASS: Test', logs['brand-go-tests.txt'], re.M))
    ui_count = int(re.search(r'pass (\d+)\b', logs['brand-ui-tests.txt'])[1])
    native_count = logs['brand-window-smoke.txt'].count('native process and frontend bridge ready')
    assert go_count >= 74 and 'FAIL' not in logs['brand-go-tests.txt']
    assert ui_count >= 83 and re.search(r'fail 0\b', logs['brand-ui-tests.txt'])
    assert native_count == 11 and '--- PASS: TestNativeWindowSmoke' in logs['brand-window-smoke.txt']
    layout_count = logs['brand-window-smoke.txt'].count('visible layout verified')
    assert layout_count == 11, 'Native layout checks are missing'
    assert 'verified_binary_sha256=' + digest(binary) in logs['brand-window-smoke.txt'], 'Smoke test used a different binary'
    serial_log = (base / 'verification/macos-serial-tests.txt').read_text()
    assert '--- PASS: TestSerialTransportLoopback' in serial_log and 'FAIL' not in serial_log
    race_log = (base / 'verification/macos-race-tests.txt').read_text()
    assert 'gytest-go/internal/core' in race_log and 'FAIL' not in race_log and 'DATA RACE' not in race_log

    out = base / 'outputs'
    out.mkdir(exist_ok=True)
    work = base / 'work'
    work.mkdir(exist_ok=True)
    stage = work / 'protocol-simulator-macos-stage'
    release = out / 'protocol-simulator'
    archive = out / 'protocol-simulator.zip'
    if stage.exists():
        remove_generated(stage, work)
    stage.mkdir()
    shutil.copytree(app, stage / app.name)
    windows=base / 'build/bin/protocol-simulator.exe'
    if windows.exists():
        windows_report=json.loads((base / 'verification/windows-cross-build.json').read_text())
        assert windows_report['binary_sha256']==digest(windows) and windows_report['frontend_matches_binary']
        shutil.copy2(windows,stage / windows.name)
    for name in ('使用说明.txt', 'THIRD-PARTY.txt'):
        shutil.copy2(base / name, stage / name)
    shutil.copy2(base / 'examples/automatic-loopback.json', stage / '自动批量联调.json')
    shutil.copytree(base / 'verification', stage / 'verification')
    # The manifest distinguishes generated files from user projects/settings.
    preserved = []
    previous=[release] if release.exists() else []
    for previous_release in previous:
        manifest_path = previous_release / 'file-manifest.json'
        if not manifest_path.is_file():
            raise ValueError(f'Cannot safely inventory {previous_release}')
        manifest = json.loads(manifest_path.read_text(encoding='utf-8-sig'))
        if isinstance(manifest, list):
            manifest = {item['path']: item for item in manifest}
        for path in previous_release.rglob('*'):
            if not path.is_file() or path == manifest_path:
                continue
            key = path.relative_to(previous_release).as_posix()
            if key in manifest and digest(path) == manifest[key]['sha256']:
                continue
            dest = stage / '保留文件' / previous_release.name / key
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, dest)
            preserved.append(key)
    report = {'product': 'protocol-simulator', 'platform': 'macOS', 'architectures': architectures,
              'frontend_matches_binary': True, 'signature': 'ad-hoc', 'notarized': False,
              'go_tests': go_count, 'ui_tests': ui_count, 'native_window_types': native_count,
              'serial_pty_verified': True, 'race_tests_passed': True,
              'native_layouts_verified': layout_count, 'zip_extraction_verified': True,
              'minimum_macos': info['LSMinimumSystemVersion'],
              'binary_sha256': digest(binary), 'preserved_files': preserved}
    (stage / 'verification/macos-package.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    manifest = {p.relative_to(stage).as_posix(): {'size': p.stat().st_size, 'sha256': digest(p)}
                for p in stage.rglob('*') if p.is_file()}
    (stage / 'file-manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    zipstage = work / 'protocol-simulator.zip'
    if zipstage.exists():
        remove_generated(zipstage, work)
    # Rename the staging folder before archiving so extraction uses the canonical name.
    # The temporary parent also confines recursive cleanup to generated work files.
    ziproot = work / 'macos-archive'
    if ziproot.exists():
        remove_generated(ziproot, work)
    ziproot.mkdir()
    stage.rename(ziproot / release.name)
    staged_release = ziproot / release.name
    # ditto preserves executable permissions and macOS bundle metadata.
    subprocess.run(['ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', str(staged_release), str(zipstage)], check=True)
    # ditto writes UTF-8 filename bytes without setting ZIP's UTF-8 flag.
    # Keep its Apple metadata/permissions, but give other extractors unambiguous names.
    normalized = zipstage.with_suffix('.utf8.zip')
    with zipfile.ZipFile(zipstage) as source_zip, zipfile.ZipFile(normalized, 'w') as target_zip:
        for entry in source_zip.infolist():
            info = copy.copy(entry)
            if not entry.flag_bits & 0x800:
                try:
                    info.filename = entry.filename.encode('cp437').decode('utf-8')
                except (UnicodeEncodeError, UnicodeDecodeError):
                    pass
            target_zip.writestr(info, source_zip.read(entry))
    os.replace(normalized, zipstage)
    with zipfile.ZipFile(zipstage) as z:
        assert z.testzip() is None
        archived_binary = 'protocol-simulator/' + app.name + '/Contents/MacOS/protocol-simulator'
        assert z.read(archived_binary) == executable
        assert z.getinfo(archived_binary).external_attr >> 16 & 0o111, 'Executable permission missing from ZIP'
    with tempfile.TemporaryDirectory(prefix='macos-extract-check-', dir=work) as extracted:
        subprocess.run(['ditto', '-x', '-k', str(zipstage), extracted], check=True)
        unpacked = Path(extracted) / release.name
        for key, entry in manifest.items():
            assert digest(unpacked / key) == entry['sha256'], 'ZIP content mismatch: ' + key
        unpacked_app = unpacked / app.name
        assert os.access(unpacked_app / 'Contents/MacOS/protocol-simulator', os.X_OK)
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(unpacked_app)], check=True)
    if release.exists():
        remove_generated(release, out)
    staged_release.rename(release)
    os.replace(zipstage, archive)
    remove_generated(ziproot, work)
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(release / app.name)], check=True)
    print(json.dumps({'app': str(release / app.name), 'archive': str(archive), **report}, ensure_ascii=False))
