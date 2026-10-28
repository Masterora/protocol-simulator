"""Exercise a built prs binary on Linux, optionally including a real systemd unit."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import time
import uuid


def address():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()


def project(path, endpoint, protocol='modbus-tcp'):
    rule = {'enabled': True, 'kind': 'holding', 'points': '0', 'start': 0, 'end': 0,
            'intervalMs': 200, 'mode': 'increment', 'min': 100, 'max': 200, 'step': 1}
    channel = {'id': 'server', 'name': 'Linux acceptance', 'protocol': protocol, 'role': 'server',
               'transport': 'tcp-server', 'address': f'{endpoint[0]}:{endpoint[1]}', 'unit': 1,
               'devices': []}
    if protocol != 'raw':
        channel['devices'] = [{'id': 'device', 'name': 'device', 'unit': 1, 'commonAddress': 1,
                               'points': [{'kind': 'holding', 'address': 0, 'name': 'value', 'value': 42, 'quality': 0}],
                               'dataTests': {'telemetry': {'rules': [rule], 'stopOnDisconnect': True}}}]
    path.write_text(json.dumps({'version': 1, 'name': 'Linux acceptance', 'channels': [channel]}))


def connect(endpoint):
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        try:
            return socket.create_connection(endpoint, timeout=1)
        except OSError:
            time.sleep(.05)
    raise AssertionError('listener did not start')


def recv_all(sock, length):
    result = b''
    while len(result) < length:
        data = sock.recv(length - len(result))
        if not data:
            raise AssertionError('connection closed early')
        result += data
    return result


def holding(sock):
    sock.sendall(bytes.fromhex('002a00000006010300000001'))
    header = recv_all(sock, 7)
    body = recv_all(sock, struct.unpack('>H', header[4:6])[0] - 1)
    assert body[:2] == b'\x03\x02', body
    return struct.unpack('>H', body[2:])[0]


def terminate(process):
    if process.poll() is None:
        process.send_signal(signal.SIGTERM)
        try:
            assert process.wait(timeout=8) == 0
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise


def runtime(binary, directory):
    checks = []
    for enabled in (False, True):
        endpoint = address()
        path = directory / ('data-on.json' if enabled else 'data-off.json')
        project(path, endpoint)
        original = path.read_bytes()
        validate = subprocess.run([binary, 'validate', str(path), '--json'], capture_output=True, text=True)
        assert validate.returncode == 0 and json.loads(validate.stdout)['valid']
        bad = subprocess.run([binary, 'run', str(path), '--channels', 'missing'], capture_output=True)
        assert bad.returncode == 2
        log = directory / f'run-{enabled}.jsonl'
        with log.open('wb') as output:
            args = [binary, 'run', str(path), '--log-format', 'json']
            if enabled:
                args += ['--start-data-rules']
            p = subprocess.Popen(args, stdout=output, stderr=subprocess.STDOUT, env={**os.environ, 'DISPLAY': ''})
            try:
                with connect(endpoint) as sock:
                    deadline = time.monotonic() + 4
                    value = holding(sock)
                    if enabled:
                        while value == 42 and time.monotonic() < deadline:
                            time.sleep(.1)
                            value = holding(sock)
                        assert value != 42
                    else:
                        time.sleep(.6)
                        assert holding(sock) == 42
            finally:
                terminate(p)
        rows = [json.loads(line) for line in log.read_text().splitlines()]
        assert any(row.get('msg') == 'stopped' for row in rows)
        assert path.read_bytes() == original
        with socket.socket() as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(endpoint)
        checks.append('explicit-data-on' if enabled else 'no-implicit-data')
    endpoint = address()
    path = directory / 'frames-project.json'
    project(path, endpoint, 'raw')
    frames = directory / 'frames.jsonl'
    with (directory / 'pressure.jsonl').open('wb') as output:
        p = subprocess.Popen([binary, 'run', str(path), '--log-format', 'json', '--frames', str(frames), '--frames-max-mb', '1', '--frames-files', '3'], stdout=output, stderr=subprocess.STDOUT)
        try:
            with connect(endpoint) as sock:
                sock.sendall(b'frame-pressure-' * 300000)
            deadline = time.monotonic() + 10
            while not frames.with_name(frames.name + '.2').exists() and time.monotonic() < deadline:
                time.sleep(.1)
        finally:
            terminate(p)
    logs = list(directory.glob('frames.jsonl*'))
    assert len(logs) == 3, logs
    for path in logs:
        assert path.stat().st_size <= 1024 * 1024
        for line in path.read_text().splitlines():
            row = json.loads(line)
            assert row['direction'] in ('RX', 'TX') and 'T' in row['time']
    checks += ['signal-port-release', 'read-only-projects', 'bounded-frame-rotation', 'no-display-required']
    return checks


def hot_reload(binary, directory):
    endpoints = [address(), address(), address()]
    paths = [directory / 'hot.json', directory / 'independent.json']
    project(paths[0], endpoints[0])
    project(paths[1], endpoints[2])
    config = json.loads(paths[0].read_text())
    survivor = json.loads(json.dumps(config['channels'][0]))
    survivor.update(id='survivor', address=f'{endpoints[1][0]}:{endpoints[1][1]}')
    config['channels'].append(survivor)
    paths[0].write_text(json.dumps(config))
    independent_bytes = paths[1].read_bytes()
    processes, streams, sockets = [], [], []
    log = directory / 'hot-reload.jsonl'
    def await_log(message, count=1):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            rows = [json.loads(line) for line in log.read_text().splitlines()]
            if sum(row.get('msg') == message for row in rows) >= count:
                return
            assert processes[0].poll() is None, 'hot reload runner exited'
            time.sleep(.05)
        raise AssertionError('hot reload missing ' + message + ': ' + log.read_text())
    def replace():
        pending = paths[0].with_suffix('.pending')
        pending.write_text(json.dumps(config))
        pending.replace(paths[0])
    try:
        for i, path in enumerate(paths):
            stream = (log if i == 0 else directory / 'independent.jsonl').open('wb')
            streams.append(stream)
            processes.append(subprocess.Popen([binary, 'run', str(path), '--log-format', 'json'], stdout=stream, stderr=subprocess.STDOUT))
        sockets = [connect(endpoint) for endpoint in endpoints]
        await_log('reloaded')
        paths[0].write_text('{broken')
        await_log('reload_rejected')
        assert [holding(sock) for sock in sockets] == [42, 42, 42]
        with socket.socket() as occupied:
            occupied.bind(('127.0.0.1', 0)); occupied.listen()
            host, port = occupied.getsockname()
            config['channels'][0]['address'] = f'{host}:{port}'
            replace(); await_log('reload_rolled_back')
            sockets[0].close(); sockets[0] = connect(endpoints[0])
            assert [holding(sock) for sock in sockets] == [42, 42, 42]
        config['channels'][0]['address'] = f'{endpoints[0][0]}:{endpoints[0][1]}'
        config['channels'][0]['devices'][0]['points'][0]['value'] = 77
        replace(); await_log('reloaded', 2)
        sockets[0].close(); sockets[0] = connect(endpoints[0])
        assert [holding(sock) for sock in sockets] == [77, 42, 42]
        assert all(p.poll() is None for p in processes)
        assert paths[1].read_bytes() == independent_bytes
        assert json.loads(paths[0].read_text()) == config
    finally:
        for sock in sockets: sock.close()
        for process_ in processes: terminate(process_)
        for stream in streams: stream.close()
    return ['hot-reload-atomic-save', 'invalid-reload-preserves-service', 'reload-bind-failure-rollback',
            'unchanged-channel-stays-connected', 'independent-project-stays-connected', 'hot-reload-read-only']


def systemd(binary, root, directory):
    assert os.geteuid() == 0, 'systemd acceptance needs an isolated root test environment'
    installed = Path('/opt/protocol-simulator/bin/prs')
    config_dir = Path('/etc/protocol-simulator')
    unit_path = Path('/etc/systemd/system/protocol-simulator@.service')
    assert not installed.exists() and not unit_path.exists(), 'existing installation must not be changed'
    instance = 'acceptance-' + uuid.uuid4().hex[:12]
    name = 'protocol-simulator@' + instance + '.service'
    path = config_dir / (instance + '.json')
    endpoint = address()
    user_created = subprocess.run(['id', '-u', 'protocol-simulator'], capture_output=True).returncode != 0
    try:
        if user_created:
            subprocess.run(['useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'protocol-simulator'], check=True)
        installed.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(binary, installed)
        installed.chmod(0o755)
        config_dir.mkdir(mode=0o755, exist_ok=True)
        project(path, endpoint)
        path.chmod(0o644)
        shutil.copy2(root / 'build/linux/protocol-simulator@.service', unit_path)
        subprocess.run(['systemd-analyze', 'verify', str(unit_path)], check=True)
        subprocess.run(['systemctl', 'daemon-reload'], check=True)
        subprocess.run(['systemctl', 'start', name], check=True)
        with connect(endpoint) as sock:
            assert holding(sock) == 42
        state = subprocess.check_output(['systemctl', 'show', name, '-p', 'User', '-p', 'MainPID', '-p', 'ActiveState'], text=True)
        assert 'User=protocol-simulator' in state and 'ActiveState=active' in state
        subprocess.run(['systemctl', 'stop', name], check=True)
        valid_project = path.read_text()
        path.write_text('{invalid project')
        subprocess.run(['systemctl', 'start', name], capture_output=True)
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            status = subprocess.check_output(['systemctl', 'show', name, '-p', 'ExecMainStatus', '-p', 'NRestarts'], text=True)
            if 'ExecMainStatus=2' in status:
                break
            time.sleep(.1)
        assert 'ExecMainStatus=2' in status and 'NRestarts=0' in status, status
        path.write_text(valid_project)
        journal = subprocess.check_output(['journalctl', '-u', name, '-o', 'cat', '--no-pager'], text=True)
        assert '"msg":"stopped"' in journal, journal
        (directory / 'systemd-journal.txt').write_text(journal)
        with socket.socket() as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(endpoint)
        return ['systemd-real-service', 'unprivileged-service-user', 'journald-json', 'systemd-clean-stop', 'invalid-config-does-not-restart']
    finally:
        subprocess.run(['systemctl', 'stop', name], capture_output=True)
        subprocess.run(['systemctl', 'reset-failed', name], capture_output=True)
        for item in (installed, path, unit_path):
            item.unlink(missing_ok=True)
        subprocess.run(['systemctl', 'daemon-reload'], capture_output=True)
        if user_created:
            subprocess.run(['userdel', 'protocol-simulator'], capture_output=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--systemd', action='store_true')
    args = parser.parse_args()
    assert sys.platform == 'linux', 'real Linux execution required'
    root = Path(__file__).resolve().parents[1]
    binary = str(args.binary.resolve())
    args.output.mkdir(parents=True, exist_ok=True)
    system_ps = Path('/usr/bin/ps')
    system_ps_hash = hashlib.sha256(system_ps.read_bytes()).hexdigest()
    environment = {**os.environ, 'PATH': str(Path(binary).parent) + os.pathsep + os.environ.get('PATH', '')}
    assert shutil.which('prs', path=environment['PATH']) == binary
    version = subprocess.check_output(['prs', 'version'], env=environment, text=True).strip()
    expected = json.loads((root / 'wails.json').read_text())['info']['productVersion']
    assert version == 'prs ' + expected, version
    ports = subprocess.run([binary, 'ports', '--json'], capture_output=True, text=True)
    assert ports.returncode == 0 and isinstance(json.loads(ports.stdout), list)
    with tempfile.TemporaryDirectory(prefix='prs-linux-acceptance-') as directory:
        checks = runtime(binary, Path(directory))
        checks += hot_reload(binary, Path(directory))
        if args.systemd:
            checks += systemd(binary, root, args.output)
    assert hashlib.sha256(system_ps.read_bytes()).hexdigest() == system_ps_hash
    checks += ['prs-on-path', 'system-ps-preserved']
    report = {'passed': True, 'platform': sys.platform, 'machine': os.uname().machine,
              'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(), 'version': version,
              'checks': checks, 'systemdVerified': args.systemd,
              'binaryArchitecture': {183: 'arm64', 62: 'amd64'}[struct.unpack_from('<H', Path(binary).read_bytes(), 18)[0]]}
    report['executionMode'] = 'native' if (report['binaryArchitecture'], report['machine']) in {('arm64','aarch64'),('amd64','x86_64')} else 'emulated'
    (args.output / 'runtime.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report))


if __name__ == '__main__':
    main()
