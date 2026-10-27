"""Run real serial transport tests through two disposable macOS pseudo terminals."""
import errno
import os
from pathlib import Path
import pty
import select
import subprocess
import sys
import threading
import tty


def main():
    if sys.platform != 'darwin':
        raise SystemExit('This serial verification runner requires macOS.')
    pairs = [pty.openpty(), pty.openpty()]
    stop = threading.Event()
    for _, slave in pairs:
        tty.setraw(slave)

    def relay():
        masters = [pair[0] for pair in pairs]
        while not stop.is_set():
            readable, _, _ = select.select(masters, [], [], 0.1)
            for source in readable:
                try:
                    data = os.read(source, 65536)
                    target = masters[1] if source == masters[0] else masters[0]
                    while data:
                        data = data[os.write(target, data):]
                except OSError as exc:
                    if exc.errno not in (errno.EIO, errno.EINTR):
                        raise

    thread = threading.Thread(target=relay, daemon=True)
    thread.start()
    env = dict(os.environ, GYLAB_SERIAL_A=os.ttyname(pairs[0][1]), GYLAB_SERIAL_B=os.ttyname(pairs[1][1]))
    try:
        result = subprocess.run(['go', 'test', './internal/core', '-run', '^(TestSerialTransportLoopback|TestScaleSerialFullTablesAndMappings)$',
                                 '-v', '-count=1', '-race', '-timeout=90s'],
                                cwd=Path(__file__).resolve().parents[1], env=env, timeout=110)
        return result.returncode
    finally:
        stop.set()
        thread.join(timeout=2)
        for pair in pairs:
            for fd in pair:
                os.close(fd)


if __name__ == '__main__':
    raise SystemExit(main())
