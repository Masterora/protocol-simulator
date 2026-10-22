"""Publish the verified current app and remove superseded generated releases."""
from pathlib import Path
import hashlib, json, re, shutil, struct, zipfile, time
import sys

if sys.platform == 'darwin':
    from package_macos import package
    package()
    raise SystemExit(0)

import pefile

base=Path(__file__).resolve().parents[1]
out=(base/'outputs').resolve()
source=base
release=out/'protocol-simulator'
archive=out/'protocol-simulator.zip'
stage=base/'work/protocol-simulator-stage'
binary=source/'build/bin/protocol-simulator.exe'

def safe_remove(path, parent):
    path=path.resolve();parent=parent.resolve()
    assert path!=parent and path.is_relative_to(parent),str(path)
    if path.is_dir():shutil.rmtree(path)
    elif path.exists():path.unlink()

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

def readlog(name):
    b=(source/'verification'/name).read_bytes()
    return b.decode('utf-16' if b[:2] in (b'\xff\xfe',b'\xfe\xff') else 'utf-8-sig')
go=readlog('brand-go-tests.txt');ui=readlog('brand-ui-tests.txt');smoke=readlog('brand-window-smoke.txt')
go_count=len(re.findall(r'^--- PASS: Test',go,re.M))
ui_count=int(re.search(r'pass (\d+)\b',ui)[1])
assert go_count>=74 and 'FAIL' not in go
assert ui_count>=80 and re.search(r'fail 0\b',ui)
native_count=smoke.count('native process and frontend bridge ready')
assert '--- PASS: TestNativeWindowSmoke' in smoke and native_count>=10

old=[p for p in out.iterdir() if re.fullmatch(r'GYLab(?:-[\w-]+)?(?:\.zip)?',p.name)]
# Preserve user-added/edited files from previous packages before deleting their app versions.
preserved=[]
for folder in [p for p in old if p.is_dir()]+([release] if release.exists() else []):
    mf=folder/'file-manifest.json'
    assert mf.exists(),'Cannot safely inventory '+str(folder)
    manifest=json.loads(mf.read_text(encoding='utf-8-sig'))
    if isinstance(manifest,list):manifest={item['path']:item for item in manifest}
    for p in folder.rglob('*'):
        if not p.is_file() or p==mf:continue
        key=p.relative_to(folder).as_posix()
        known=manifest.get(key)
        if known and hashlib.sha256(p.read_bytes()).hexdigest()==known['sha256']:continue
        preserved.append((p,Path('保留文件')/folder.name/key))

if stage.exists():safe_remove(stage,base/'work')
stage.mkdir()
shutil.copy2(binary,stage/'protocol-simulator.exe')
shutil.copy2(source/'使用说明.txt',stage/'使用说明.txt')
shutil.copy2(source/'examples/automatic-loopback.json',stage/'自动批量联调.json')
for name,rel in [('appicon.png','icon.png'),('icon.svg','icon.svg'),('windows/icon.ico','icon.ico')]:
    shutil.copy2(source/'build'/name,stage/rel)
shutil.copytree(source,stage/'source',ignore=lambda d,n:[x for x in n if x in {'node_modules','.git','verification','file-manifest.json','outputs','work','references','__pycache__'} or x.endswith(('.exe','.syso'))])
shutil.copytree(source/'verification',stage/'verification')
for p,rel in preserved:
    dest=stage/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(p,dest)
(stage/'verification/品牌与图标检查.json').write_text(json.dumps({'product':'protocol-simulator','icon_sizes':[16,20,24,32,48,64,128,256],'exe_icon_bytes_match':True,'frontend_matches_exe':True,'go_tests':go_count,'ui_tests':ui_count,'native_window_types':native_count,'preserved_files':[str(r) for _,r in preserved]},ensure_ascii=False,indent=2),encoding='utf-8')
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
for p in old:safe_remove(p,out)
pending=[]
for p in (source/'build/bin').glob('*.exe'):
    if p.name!='protocol-simulator.exe':
        try:safe_remove(p,source/'build/bin')
        except PermissionError:pending.append(str(p))
print(json.dumps({'app':str(release/'protocol-simulator.exe'),'old_releases_deleted':[p.name for p in old],'preserved_files':len(preserved),'pending_locked_builds':pending+pending_release,'exe_sha256':hashlib.sha256(exe).hexdigest()},ensure_ascii=False))
