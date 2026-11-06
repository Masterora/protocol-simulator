"""Native multi-process development with disk assets and atomic backend rebuilds."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import signal
import subprocess
import sys
import time


def backend_identity(root):
    files = [p for p in root.rglob('*.go') if not p.name.endswith('_test.go') and
             not any(part in {'work', 'outputs', 'verification', 'node_modules', '.git', 'references'} for part in p.relative_to(root).parts)]
    files += [root / n for n in ('go.mod', 'go.sum', 'wails.json', 'scripts/dev_reload.js')]
    h = hashlib.sha256()
    for path in sorted(files):
        h.update(str(path.relative_to(root)).encode())
        h.update(path.read_bytes())
    return h.hexdigest()


def build(root, work):
    binary = work / ('protocol-simulator.exe' if sys.platform == 'win32' else 'protocol-simulator')
    if sys.platform == 'darwin':
        binary = work / 'protocol-simulator.app/Contents/MacOS/protocol-simulator'
        contents = binary.parent.parent
        contents.mkdir(parents=True, exist_ok=True)
        metadata = json.loads((root / 'wails.json').read_text(encoding='utf-8'))
        info = {'CFBundleExecutable': 'protocol-simulator', 'CFBundleName': 'protocol-simulator-dev',
                'CFBundleIdentifier': 'com.protocolsimulator.development', 'CFBundlePackageType': 'APPL',
                'CFBundleVersion': metadata['info']['productVersion'], 'NSHighResolutionCapable': True}
        if not (contents / 'Info.plist').exists():
            (contents / 'Info.plist').write_bytes(plistlib.dumps(info))
    binary.parent.mkdir(parents=True, exist_ok=True)
    tags = 'desktop,production,devassets' + (',webkit2_41' if sys.platform.startswith('linux') else '')
    candidate = work / (binary.name + '.next')
    build_env = dict(os.environ)
    if sys.platform == 'darwin':
        build_env['CGO_LDFLAGS'] = build_env.get('CGO_LDFLAGS', '') + ' -framework UniformTypeIdentifiers'
        build_env.setdefault('MACOSX_DEPLOYMENT_TARGET', '12.3')
    subprocess.run(['go', 'build', '-tags', tags, '-o', str(candidate), '.'], cwd=root, env=build_env, check=True)
    return binary, candidate


def stop(process):
    if process is None:
        return
    if sys.platform == 'win32':
        if process.poll() is None:
            subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    else:
        # Includes this development project's independent windows, never other apps.
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        if sys.platform != 'win32':
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        process.wait(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build-only', action='store_true')
    parser.add_argument('--assets-dir', type=Path)
    parser.add_argument('--work-dir', type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    work = (args.work_dir or root / 'work/dev').resolve()
    assets = (args.assets_dir or root / 'frontend').resolve()
    if not (assets / 'index.html').is_file():
        parser.error('assets directory must contain index.html')
    work.mkdir(parents=True, exist_ok=True)
    process = None
    last = None
    try:
        while True:
            try:
                identity = backend_identity(root)
            except OSError:
                time.sleep(.5)
                continue
            if identity != last:
                last = identity
                print('正在构建开发程序…', flush=True)
                try:
                    binary, candidate = build(root, work)
                except (subprocess.CalledProcessError, OSError, json.JSONDecodeError) as error:
                    print(str(error), flush=True)
                    if args.build_only:
                        return 1
                    print('构建失败，保留当前运行程序；修复代码后自动重试。', flush=True)
                else:
                    stop(process)
                    candidate.replace(binary)
                    if args.build_only:
                        print(binary, flush=True)
                        return 0
                    process = subprocess.Popen([str(binary)], cwd=root,
                        env={**os.environ, 'PRS_DEV_ASSETS': str(assets)},
                        start_new_session=sys.platform != 'win32')
                    print(f'开发程序 PID={process.pid}；界面自动刷新，Go修改会重启并中断连接；Ctrl+C结束。', flush=True)
            if process is not None and process.poll() is not None:
                if sys.platform == 'win32':
                    return process.returncode
                try:
                    os.killpg(process.pid, 0)
                except ProcessLookupError:
                    return process.returncode
                # Independent development projects still share this watch group.
            time.sleep(.5)
    except KeyboardInterrupt:
        return 0
    finally:
        stop(process)


if __name__ == '__main__':
    raise SystemExit(main())
