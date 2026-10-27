"""Cross-build Windows PE/resources; does not claim Windows runtime QA."""
from pathlib import Path
import json
import os
import subprocess

from delivery import digest, verify_frontend
from windows_installer import verify_application_resources


def main():
    base = Path(__file__).resolve().parents[1]
    binary = base / 'build/bin/protocol-simulator.exe'
    binary.parent.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, 'GOOS': 'windows', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
    subprocess.run(['go', 'build', '-tags', 'desktop,production', '-ldflags', '-H windowsgui',
                    '-o', str(binary), '.'], cwd=base, env=env, check=True)
    result = subprocess.check_output(['go', 'run', '.', str(base), str(binary)],
                                     cwd=base / 'scripts/windows-resources', text=True)
    verify_application_resources(base, binary)
    verify_frontend(base, binary)
    report = {'product': 'protocol-simulator', 'platform': 'windows/amd64', 'cross_compiled': True,
              'native_windows_runtime_verified': False,
              'resource_writer': 'scripts/windows-resources (shared with Windows native builds)',
              'resource_checks': result.strip(), 'frontend_matches_binary': True,
              'binary_sha256': digest(binary)}
    (base / 'verification/windows-cross-build.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
