"""Clean unchanged historical build/test output only after successful publication."""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import time


CURRENT_REPORTS = {
    'build-start.json', 'build-evidence.json', 'cleanup-artifacts.json',
    'brand-go-tests.txt', 'brand-ui-tests.txt', 'brand-window-smoke.txt',
    'vet.txt', 'packaging-tests.txt', 'quality-checks.txt',
    'macos-race-tests.txt', 'macos-serial-tests.txt', 'macos-audit.json',
    'windows-resource-tests.txt', 'windows-cross-build.txt',
    'windows-cross-build.json', 'windows-installer.json',
    'windows-installer-runtime.json', 'linux-server-tests.txt',
    'linux-server-build.json',
}
for architecture in ('amd64', 'arm64'):
    for kind in ('server-runtime', 'desktop-runtime', 'desktop-build'):
        CURRENT_REPORTS.add(f'linux-{kind}-{architecture}.json')
        CURRENT_REPORTS.add(f'{kind}-{architecture}.json')

GENERATED_SUFFIXES = {
    '.txt', '.log', '.out', '.png', '.jpg', '.exe', '.deb', '.dmg',
    '.gz', '.zip', '.pyc', '.test', '.bin', '.icns', '.ico',
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def generated(path):
    # Unknown files and configuration formats are retained, even inside work/.
    name = path.name.lower()
    if any(word in name for word in ('project', 'config', 'settings', '.env')):
        return False
    if path.suffix == '.json':
        try:
            data = json.loads(path.read_text(encoding='utf-8-sig'))
        except (ValueError, UnicodeError):
            return False
        if not isinstance(data, dict) or any(k in data for k in ('channels', 'devices', 'role', 'project', 'projects', 'rules', 'name')):
            return False
        return any(k in data for k in ('passed', 'checks', 'sha256', 'binarySHA256', 'sourceSHA256', 'sourceFingerprint', 'movedFiles'))
    return path.suffix.lower() in GENERATED_SUFFIXES


def process_commands():
    if os.name == 'nt':
        return subprocess.check_output([
            'powershell', '-NoProfile', '-NonInteractive', '-Command',
            'Get-CimInstance Win32_Process | ForEach-Object { $_.CommandLine }',
        ], text=True, timeout=15)
    return subprocess.check_output(['ps', '-axo', 'command='], text=True, timeout=15)


class ArtifactCleanup:
    def __init__(self, root, started=None, preserve=()):
        self.root = Path(root).resolve()
        self.started = time.time() if started is None else started
        self.preserve = [Path(p).resolve() for p in preserve]
        self.entries = []
        for directory in ('work', 'verification'):
            base = self.root / directory
            if base.is_symlink() or not base.is_dir():
                continue
            # os.walk never follows directory symlinks.
            for parent, dirs, files in os.walk(base, followlinks=False):
                dirs[:] = [n for n in dirs if not (Path(parent) / n).is_symlink()
                           and n not in {'.git', 'golang', 'installer-runtime', 'node_modules', '.venv', 'venv'}]
                for name in files:
                    path = Path(parent) / name
                    if self.safe(path) and generated(path) and not self.current(path):
                        stat = path.stat()
                        if stat.st_mtime < self.started:
                            self.entries.append((path.relative_to(self.root).as_posix(),
                                                 digest(path), stat.st_mtime_ns))

    def safe(self, path):
        relative = path.relative_to(self.root)
        if relative.parts[0] not in ('work', 'verification') or '..' in relative.parts:
            return False
        return (not any(path == p or p in path.parents for p in self.preserve)
                and not any(p.is_symlink() for p in (path, *path.parents) if p != self.root)
                and path.is_file())

    def current(self, path):
        relative = path.relative_to(self.root)
        return relative.parts[0] == 'verification' and len(relative.parts) == 2 and path.name in CURRENT_REPORTS

    def finish(self):
        report = {'removed': [], 'retained': [], 'removedBytes': 0}
        if any((self.root / name).is_symlink() for name in ('work', 'verification')):
            report['skipped'] = 'Refusing linked artifact directories'
            return report
        try:
            commands = process_commands().replace('\\', '/').lower()
        except (OSError, subprocess.SubprocessError) as error:
            report['skipped'] = 'Cannot inspect active tasks: ' + str(error)
            return report
        empty_parents = set()
        for relative, expected, modified in self.entries:
            path = self.root / relative
            # Protect the whole top-level task directory if a process uses it.
            parts = Path(relative).parts
            task = '/'.join(parts[:2])
            active = relative.lower() in commands or str(path).replace('\\', '/').lower() in commands
            if len(parts) > 2:
                active = active or task.lower() + '/' in commands or str(self.root / task).replace('\\', '/').lower() in commands
            if (active or not self.safe(path) or self.current(path)
                    or path.stat().st_mtime_ns != modified or digest(path) != expected
                    or not generated(path)):
                report['retained'].append(relative)
                continue
            size = path.stat().st_size
            try:
                path.unlink()
            except OSError:
                report['retained'].append(relative)
                continue
            report['removed'].append({'path': relative, 'sha256': expected})
            report['removedBytes'] += size
            empty_parents.update(p for p in path.parents if p != self.root and self.root in p.parents
                                 and p not in (self.root / 'work', self.root / 'verification'))
        for path in sorted(empty_parents, key=lambda p: len(p.parts), reverse=True):
            if not path.is_symlink():
                try:
                    path.rmdir()
                except OSError:
                    pass
        evidence = self.root / 'verification'
        if evidence.is_symlink():
            report['skipped'] = 'Evidence directory changed to a link'
            return report
        evidence.mkdir(exist_ok=True)
        (evidence / 'cleanup-artifacts.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        return report
