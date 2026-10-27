"""Publish the verified current app and remove superseded generated releases."""
from pathlib import Path
import copy, hashlib, json, os, plistlib, shutil, struct, subprocess, tempfile, zipfile, time
import sys

from delivery import digest, verify_current_build, verify_frontend, copy_user_payload, preserve_user_files

def safe_remove(path, parent):
    path, parent = path.resolve(), parent.resolve()
    if path == parent or not path.is_relative_to(parent):
        raise ValueError(f'Unsafe generated path: {path}')
    if path.is_dir():
        shutil.rmtree(path)
    elif path.exists():
        path.unlink()


def package_macos():
    base = Path(__file__).resolve().parents[1]
    app = base / 'build/bin/protocol-simulator.app'
    binary = app / 'Contents/MacOS/protocol-simulator'
    executable = binary.read_bytes()
    proof, checks = verify_current_build(base, binary, "macos")
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

    out = base / 'outputs'
    out.mkdir(exist_ok=True)
    work = base / 'work'
    work.mkdir(exist_ok=True)
    stage = work / 'protocol-simulator-macos-stage'
    release = out / 'protocol-simulator'
    archive = out / 'protocol-simulator.zip'
    if stage.exists():
        safe_remove(stage, work)
    stage.mkdir()
    shutil.copytree(app, stage / app.name)
    windows=base / 'build/bin/protocol-simulator.exe'
    if windows.exists():
        windows_report=json.loads((base / 'verification/windows-cross-build.json').read_text())
        assert windows_report['binary_sha256']==digest(windows) and windows_report['frontend_matches_binary']
        assert proof['additionalBinaries']['windows']['sha256']==digest(windows)
        verify_frontend(base, windows)
        shutil.copy2(windows,stage / windows.name)
    copy_user_payload(base, stage, proof, checks)
    preserved = preserve_user_files(release, stage)
    report = {'product': 'protocol-simulator', 'platform': 'macOS', 'architectures': architectures,
              'frontend_matches_binary': True, 'signature': 'ad-hoc', 'notarized': False,
              'go_tests': checks['goTests'], 'ui_tests': checks['uiTests'], 'native_window_types': checks['nativeLayouts'],
              'serial_pty_verified': True, 'race_tests_passed': True,
              'native_layouts_verified': checks['nativeLayouts'], 'zip_extraction_verified': True,
              'minimum_macos': info['LSMinimumSystemVersion'],
              'binary_sha256': digest(binary), 'preserved_files': preserved}
    (stage / 'verification/macos-package.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    manifest = {p.relative_to(stage).as_posix(): {'size': p.stat().st_size, 'sha256': digest(p)}
                for p in stage.rglob('*') if p.is_file()}
    (stage / 'file-manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    zipstage = work / 'protocol-simulator.zip'
    if zipstage.exists():
        safe_remove(zipstage, work)
    # Rename the staging folder before archiving so extraction uses the canonical name.
    # The temporary parent also confines recursive cleanup to generated work files.
    ziproot = work / 'macos-archive'
    if ziproot.exists():
        safe_remove(ziproot, work)
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
        safe_remove(release, out)
    staged_release.rename(release)
    os.replace(zipstage, archive)
    safe_remove(ziproot, work)
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(release / app.name)], check=True)
    print(json.dumps({'app': str(release / app.name), 'archive': str(archive), **report}, ensure_ascii=False))


if sys.platform == 'darwin':
    package_macos()
    raise SystemExit(0)

import pefile

base=Path(__file__).resolve().parents[1]
out=(base/'outputs').resolve()
out.mkdir(exist_ok=True)
source=base
release=out/'protocol-simulator'
archive=out/'protocol-simulator.zip'
stage=base/'work/protocol-simulator-stage'
binary=source/'build/bin/protocol-simulator.exe'


exe=binary.read_bytes()
for p in (source/'frontend').iterdir():
    if p.is_file():assert p.read_bytes() in exe, 'Stale frontend: '+p.name
pe=pefile.PE(str(binary))
resources={e.id:e.directory.entries[0].directory.entries[0].data.struct for e in pe.DIRECTORY_ENTRY_RESOURCE.entries}
assert {3,14,16,24}<=resources.keys()
icon=(source/'build/windows/icon.ico').read_bytes()
group=resources[14];group=pe.get_data(group.OffsetToData,group.Size)
groups=next(e for e in pe.DIRECTORY_ENTRY_RESOURCE.entries if e.id==14).directory.entries
assert {1,3}<={e.id for e in groups},'Missing Wails native window icon group'
count=struct.unpack_from('<H',icon,4)[0]
assert count==struct.unpack_from('<H',group,4)[0]==8
images={e.id:e.directory.entries[0].data.struct for e in next(e for e in pe.DIRECTORY_ENTRY_RESOURCE.entries if e.id==3).directory.entries}
for i in range(count):
    n,offset=struct.unpack_from('<II',icon,6+16*i+8)
    resource=images[i+1]
    assert icon[offset:offset+n]==pe.get_data(resource.OffsetToData,resource.Size)
fields={k.decode():v.decode() for info in pe.FileInfo for item in info if hasattr(item,'StringTable') for table in item.StringTable for k,v in table.entries.items()}
assert fields['ProductName']=='protocol-simulator' and fields['OriginalFilename']=='protocol-simulator.exe'
pe.close()

proof,checks=verify_current_build(base,binary,'windows')

if stage.exists():safe_remove(stage,base/'work')
stage.mkdir()
shutil.copy2(binary,stage/'protocol-simulator.exe')
copy_user_payload(source,stage,proof,checks)
preserved=preserve_user_files(release,stage)
(stage/'verification/windows-package.json').write_text(json.dumps({'product':'protocol-simulator','binary_sha256':proof['binarySHA256'],'checks':checks,'preserved_files':preserved},ensure_ascii=False,indent=2),encoding='utf-8')
manifest={p.relative_to(stage).as_posix():{'size':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in stage.rglob('*') if p.is_file()}
(stage/'file-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2),encoding='utf-8')
zipstage=base/'work/protocol-simulator.zip'
with zipfile.ZipFile(zipstage,'w',zipfile.ZIP_DEFLATED,compresslevel=6) as z:
    for p in stage.rglob('*'):
        if p.is_file():z.write(p,'protocol-simulator/'+p.relative_to(stage).as_posix())
with zipfile.ZipFile(zipstage) as z:
    assert z.testzip() is None
    assert z.read('protocol-simulator/protocol-simulator.exe')==exe
pending_release=[]
if release.exists():
    previous=release/'protocol-simulator.exe'
    if previous.exists():
        try:safe_remove(previous,release)
        except PermissionError:
            # A running Windows image can be renamed while its file is locked.
            # Keep the running session intact and delete its old image on exit.
            retired=base/'work/superseded-protocol-simulator'
            retired.mkdir(exist_ok=True)
            target=retired/('protocol-simulator-'+str(time.time_ns())+'.exe')
            assert previous.resolve().is_relative_to(out) and target.resolve().is_relative_to((base/'work').resolve())
            previous.rename(target);pending_release.append(str(target))
    for p in list(release.iterdir()):safe_remove(p,release)
    shutil.copytree(stage,release,dirs_exist_ok=True)
    safe_remove(stage,base/'work')
else:stage.rename(release)
if archive.exists():safe_remove(archive,out)
zipstage.rename(archive)
# Retired images are retried on the next package operation, never by a long-lived worker.
retired=base/'work/superseded-protocol-simulator'
if retired.exists():
    for image in retired.glob('protocol-simulator-*.exe'):
        try:safe_remove(image,retired)
        except PermissionError:
            if str(image) not in pending_release:pending_release.append(str(image))
    if not any(retired.iterdir()):retired.rmdir()
print(json.dumps({'app':str(release/'protocol-simulator.exe'),'preserved_files':len(preserved),'pending_locked_builds':pending_release,'exe_sha256':hashlib.sha256(exe).hexdigest()},ensure_ascii=False))
