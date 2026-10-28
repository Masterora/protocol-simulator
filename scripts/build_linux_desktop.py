"""Build and verify a Linux desktop DEB on its target Linux architecture."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile

from delivery import digest, source_hashes, verify_binary_licenses, verify_frontend


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output-dir', type=Path)
    parser.add_argument('--work-dir', type=Path)
    parser.add_argument('--evidence-dir', type=Path)
    args = parser.parse_args()
    if sys.platform != 'linux':
        raise SystemExit('Build desktop DEBs inside the target Linux environment')
    root = Path(__file__).resolve().parents[1]
    output = args.output_dir or root / 'outputs'
    work = args.work_dir or root / 'work'
    evidence = args.evidence_dir or root / 'verification'
    for directory in (output, work, evidence):
        directory.mkdir(parents=True, exist_ok=True)
    before = source_hashes(root)
    metadata = json.loads((root / 'wails.json').read_text())
    version = metadata['info']['productVersion']
    architecture = subprocess.check_output(['dpkg', '--print-architecture'], text=True).strip()
    if architecture not in ('amd64', 'arm64'):
        raise ValueError('Unsupported desktop architecture: ' + architecture)
    with tempfile.TemporaryDirectory(prefix='linux-desktop-', dir=work) as temporary:
        temporary = Path(temporary)
        stage = temporary / 'package'
        binary = stage / 'usr/bin/protocol-simulator'
        binary.parent.mkdir(parents=True)
        env = {**os.environ, 'GOOS': 'linux', 'GOARCH': architecture, 'CGO_ENABLED': '1'}
        subprocess.run(['go', 'build', '-trimpath', '-tags', 'desktop,production,webkit2_41',
                        '-ldflags', '-s -w', '-o', str(binary), '.'], cwd=root, env=env, check=True)
        verify_frontend(root, binary)
        verify_binary_licenses(root, binary)
        destinations = {'build/linux/protocol-simulator.desktop': 'usr/share/applications/protocol-simulator.desktop',
                        'frontend/icon.svg': 'usr/share/icons/hicolor/scalable/apps/protocol-simulator.svg',
                        'README.md': 'usr/share/doc/protocol-simulator/README.md',
                        'THIRD-PARTY.txt': 'usr/share/doc/protocol-simulator/copyright'}
        for source, destination in destinations.items():
            target = stage / destination
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / source, target)
            target.chmod(0o644)
        docs = stage / 'usr/share/doc/protocol-simulator'
        shutil.copytree(root / 'examples', docs / 'examples')
        control = stage / 'DEBIAN'
        control.mkdir()
        # Local build contact; an explicitly configured author email takes precedence.
        author = metadata.get('author', {})
        maintainer = f"{author.get('name') or 'protocol-simulator'} <{author.get('email') or 'noreply@localhost'}>"
        base_control = f'Package: protocol-simulator\nVersion: {version}\nArchitecture: {architecture}\nMaintainer: {maintainer}\nSection: net\nPriority: optional\nDescription: IEC104 and Modbus desktop protocol simulator\n'
        (temporary / 'debian').mkdir()
        (temporary / 'debian/control').write_text('Source: protocol-simulator\n\n' + base_control)
        dependency_line = subprocess.check_output(['dpkg-shlibdeps', '-O', '-e' + str(binary)], cwd=temporary, text=True).strip()
        assert dependency_line.startswith('shlibs:Depends='), dependency_line
        dependencies = dependency_line.split('=', 1)[1] + ', librsvg2-common, hicolor-icon-theme'
        (control / 'control').write_text(base_control + 'Depends: ' + dependencies + '\n')
        for script in ('postinst', 'postrm'):
            shutil.copy2(root / 'build/linux' / script, control / script)
            (control / script).chmod(0o755)
        report = {'version': version, 'architecture': architecture, 'format': 'deb', 'binarySHA256': digest(binary),
                  'sourceFingerprint': hashlib.sha256(json.dumps(before, sort_keys=True).encode()).hexdigest(),
                  'frontendMatchesBinary': True, 'iconSHA256': digest(root / 'frontend/icon.svg'),
                  'runtimeDependencies': dependencies,
                  'runtimeAcceptanceReport': f'verification/linux-desktop-runtime-{architecture}.json'}
        (docs / 'build.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        installed_size = (sum(p.stat().st_size for p in (stage / 'usr').rglob('*') if p.is_file()) + 1023) // 1024
        with (control / 'control').open('a') as stream:
            stream.write(f'Installed-Size: {installed_size}\n')
        package = temporary / f'protocol-simulator-desktop-linux-{architecture}.deb'
        subprocess.run(['dpkg-deb', '--build', '--root-owner-group', str(stage), str(package)], check=True)
        extracted = temporary / 'extracted'
        subprocess.run(['dpkg-deb', '--extract', str(package), str(extracted)], check=True)
        assert digest(extracted / 'usr/bin/protocol-simulator') == report['binarySHA256']
        assert (extracted / 'usr/bin/protocol-simulator').stat().st_mode & 0o111
        for source, destination in destinations.items():
            assert (extracted / destination).read_bytes() == (root / source).read_bytes()
        assert not (extracted / 'usr/bin/ps').exists(), 'Do not replace the system ps command'
        if before != source_hashes(root):
            raise ValueError('Source changed during Linux desktop build')
        report.update(package=package.name, packageSHA256=digest(package))
        descriptor, pending = tempfile.mkstemp(prefix='.desktop-package-', dir=output)
        os.close(descriptor)
        try:
            shutil.copy2(package, pending)
            os.replace(pending, output / package.name)
        finally:
            Path(pending).unlink(missing_ok=True)
        (evidence / f'desktop-build-{architecture}.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
