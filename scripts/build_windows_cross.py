"""Build and verify Windows PE/resources on macOS; does not claim Windows runtime QA."""
from pathlib import Path
import hashlib
import json
import os
import subprocess

base=Path(__file__).resolve().parents[1]
work=base/'work'
work.mkdir(exist_ok=True)
modfile=work/'windows-resources.mod'
modfile.write_text('module resource-builder\n\ngo 1.25\n\nrequire github.com/tc-hib/winres v0.3.1\n')
exe=base/'build/bin/protocol-simulator.exe'
exe.parent.mkdir(parents=True,exist_ok=True)
env={**os.environ,'GOOS':'windows','GOARCH':'amd64','CGO_ENABLED':'0'}
subprocess.run(['go','build','-tags','desktop,production','-ldflags','-H windowsgui','-o',str(exe),'.'],cwd=base,env=env,check=True)
result=subprocess.check_output(['go','run','-mod=mod','-modfile',str(modfile),'scripts/windows_resources.go','apply',str(base)],cwd=base,text=True)
raw=exe.read_bytes()
for asset in (base/'frontend').rglob('*'):
    if asset.is_file():assert asset.read_bytes() in raw, 'Stale frontend: '+asset.name
report={'product':'protocol-simulator','platform':'windows/amd64','cross_compiled':True,'native_windows_runtime_verified':False,'resources_spec':'build/windows/resources.ps1','resource_writer':'winres v0.3.1 (cross-platform equivalent)','resource_checks':result.strip(),'frontend_matches_binary':True,'binary_sha256':hashlib.sha256(raw).hexdigest()}
(base/'verification/windows-cross-build.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
print(json.dumps(report,ensure_ascii=False))
