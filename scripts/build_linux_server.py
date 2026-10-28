"""Build the headless prs command and Linux archives without desktop libraries."""
from pathlib import Path
import hashlib
import json
import os
import platform
import shutil
import subprocess
import tarfile
import tempfile

from delivery import digest, source_hashes, verify_binary_licenses


def main():
    root = Path(__file__).resolve().parents[1]
    version = json.loads((root / 'wails.json').read_text())['info']['productVersion']
    before = source_hashes(root)
    fingerprint = hashlib.sha256(json.dumps(before, sort_keys=True).encode()).hexdigest()
    work, output, verification = (root / name for name in ('work', 'outputs', 'verification'))
    for directory in (work, output, verification):
        directory.mkdir(exist_ok=True)
    with (verification / 'linux-server-tests.txt').open('w') as log:
        subprocess.run(['go', 'test', '-race', './internal/core', './internal/runner', './internal/observe', './internal/tui', '-count=1', '-v', '-timeout=180s'], cwd=root, stdout=log, stderr=subprocess.STDOUT, check=True)
        subprocess.run(['go', 'vet', './cmd/prs', './internal/core', './internal/runner', './internal/observe', './internal/tui'], cwd=root, stdout=log, stderr=subprocess.STDOUT, check=True)
    reports = []
    with tempfile.TemporaryDirectory(prefix='linux-server-build-', dir=work) as directory:
        temporary = Path(directory)
        for architecture in ('amd64', 'arm64'):
            stage = temporary / architecture / 'protocol-simulator-server'
            (stage / 'bin').mkdir(parents=True)
            binary = stage / 'bin/prs'
            environment = {**os.environ, 'GOOS': 'linux', 'GOARCH': architecture, 'CGO_ENABLED': '0'}
            subprocess.run(['go', 'build', '-trimpath', '-ldflags', f'-s -w -X main.version={version}', '-o', str(binary), './cmd/prs'], cwd=root, env=environment, check=True)
            verify_binary_licenses(root, binary)
            modules = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
            if 'wailsapp' in modules:
                raise ValueError('Headless server unexpectedly links desktop libraries')
            for source, name in ((root / 'THIRD-PARTY.txt', 'THIRD-PARTY.txt'), (root / 'README.md', 'README.md')):
                shutil.copy2(source, stage / name)
            shutil.copytree(root / 'examples', stage / 'examples')
            (stage / 'systemd').mkdir()
            shutil.copy2(root / 'build/linux/protocol-simulator@.service', stage / 'systemd/protocol-simulator@.service')
            report = {'version': version, 'architecture': architecture, 'platform': 'linux', 'command': 'prs',
                      'sourceFingerprint': fingerprint, 'binarySHA256': digest(binary),
                      'buildHost': platform.system(), 'desktopLibrariesLinked': False,
                      'linuxRuntimeVerification': 'See verification/linux-server-runtime-*.json; cross-build alone is not runtime acceptance'}
            (stage / 'build.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
            archive = temporary / f'protocol-simulator-server-linux-{architecture}.tar.gz'
            with tarfile.open(archive, 'w:gz') as tar:
                tar.add(stage, arcname=stage.name)
            with tarfile.open(archive) as tar:
                extracted = tar.extractfile('protocol-simulator-server/bin/prs').read()
                assert hashlib.sha256(extracted).hexdigest() == report['binarySHA256']
                assert tar.getmember('protocol-simulator-server/bin/prs').mode & 0o111
                assert tar.extractfile('protocol-simulator-server/systemd/protocol-simulator@.service').read() == (root / 'build/linux/protocol-simulator@.service').read_bytes()
            report.update(archive=archive.name, archiveSHA256=digest(archive))
            reports.append(report)
        if before != source_hashes(root):
            raise ValueError('Source changed during Linux server build')
        for report in reports:
            os.replace(temporary / report['archive'], output / report['archive'])
    (verification / 'linux-server-build.json').write_text(json.dumps({'passed': True, 'sourceSHA256': before, 'builds': reports}, ensure_ascii=False, indent=2))
    print(json.dumps({'passed': True, 'archives': [row['archive'] for row in reports]}))


if __name__ == '__main__':
    main()
