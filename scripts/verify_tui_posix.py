"""Exercise prs tui in a real PTY while two independent Modbus projects run.

Uses only the Python standard library. Captures actual terminal output for QA.
"""
from pathlib import Path
import argparse
import base64
import fcntl
import hashlib
import json
import os
import pty
import re
import select
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time


def recv_all(sock, size):
    result = b''
    while len(result) < size:
        part = sock.recv(size - len(result))
        assert part, 'protocol connection was closed'
        result += part
    return result


def holding(sock, address=0):
    sock.sendall(bytes.fromhex('002a000000060103') + struct.pack('>HH', address, 1))
    header = recv_all(sock, 7)
    data = recv_all(sock, int.from_bytes(header[4:6], 'big') - 1)
    assert data[:2] == b'\x03\x02'
    return int.from_bytes(data[2:], 'big')


def endpoint():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()


def connect(address):
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        try:
            return socket.create_connection(address, timeout=1)
        except OSError:
            time.sleep(.05)
    raise AssertionError('project listener not ready')


def same_terminal_settings(actual, expected):
    # Darwin's tty layer can set PENDIN after TIOCSETA to retype pending input.
    # It is kernel bookkeeping, not an input/echo/raw-mode setting we changed.
    actual, expected = actual[:], expected[:]
    if sys.platform == 'darwin':
        actual[3] &= ~termios.PENDIN
        expected[3] &= ~termios.PENDIN
    return actual == expected


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    assert sys.platform in ('darwin', 'linux'), 'PTY acceptance needs a POSIX host'
    binary = args.binary.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    sockets, processes, streams = [], [], []
    stop = threading.Event()
    failures = []
    counts = [0, 0]
    master, slave, viewer = None, None, None
    with tempfile.TemporaryDirectory(prefix='prs-tui-') as temporary:
        directory = Path(temporary)
        runtime = directory / 'sockets'
        paths, endpoints = [], []
        originals = []
        try:
            for index in range(2):
                address = endpoint(); endpoints.append(address)
                path = directory / f'project-{index}.json'; paths.append(path)
                points = [{'address': i, 'kind': 'holding', 'name': (f'电压-{i}' if i == 204 else f'voltage-{i}'), 'value': 220 + index, 'quality': 0} for i in range(205)]
                points.reverse()
                points += [{'address': 1, 'kind': 'input', 'name': 'input-only', 'value': 99}, {'address': 2, 'kind': 'coil', 'name': 'coil-only', 'value': 1}]
                project = {'version': 2, 'name': f'Acceptance project {index}', 'channels': [{'id': f'channel-{index}', 'name': f'Channel {index}', 'protocol': 'modbus-tcp', 'role': 'server', 'transport': 'tcp-server', 'address': f'{address[0]}:{address[1]}', 'unit': 1, 'devices': [{'id': 'device-1', 'name': 'Device', 'unit': 1, 'commonAddress': 1, 'points': points, 'dataTests': {'telemetry': {'stopOnDisconnect': False, 'rules': [{'enabled': True, 'kind': 'holding', 'points': '0', 'intervalMs': 200, 'mode': 'increment', 'min': 220, 'max': 230, 'step': 1}]}}}]}]}
                path.write_text(json.dumps(project)); originals.append(path.read_bytes())
                stream = (args.output / f'project-{index}.jsonl').open('wb'); streams.append(stream)
                processes.append(subprocess.Popen([str(binary), 'run', str(path), '--runtime-dir', str(runtime), '--start-data-rules', '--log-format', 'json'], stdout=stream, stderr=subprocess.STDOUT))
                sockets.append(connect(address))
            def traffic():
                try:
                    while not stop.is_set():
                        for index, sock in enumerate(sockets):
                            value = holding(sock)
                            assert 220 <= value <= 230, value
                            counts[index] += 1
                        stop.wait(.05)
                except Exception as error:
                    failures.append(str(error))
            worker = threading.Thread(target=traffic, daemon=True); worker.start()
            captures = {}
            raw = bytearray()
            def pump(seconds=.15):
                deadline = time.monotonic() + seconds
                while time.monotonic() < deadline:
                    ready, _, _ = select.select([master], [], [], min(.05, max(0, deadline-time.monotonic())))
                    if ready:
                        try:
                            data = os.read(master, 65536)
                        except OSError:
                            break
                        if not data: break
                        raw.extend(data)
            def screen():
                frames=bytes(raw).split(b'\x1b[H')
                for frame in reversed(frames):
                    if frame.count(b'\r\n')==29 and frame.endswith(b'\x1b[0m\x1b[K') or frame.startswith('\x1b[2J终端太小'.encode())and frame.endswith(b'\r\n'):
                        return re.sub(r'\x1b\[[0-9;?]*[A-Za-z~]','',frame.decode(errors='replace'))
                return '' 
            def wait_screen(fragment):
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    pump()
                    assert viewer.poll() is None, 'viewer exited unexpectedly'
                    if fragment in screen(): return
                raise AssertionError('TUI missing ' + fragment + ': ' + screen())
            def key(value):
                os.write(master, value.encode()); pump(.2)
            def capture(name):
                pump(.2); captures[name] = bytes(raw)
                (args.output / (name+'.ansi')).write_bytes(raw)
                (args.output / (name+'.txt')).write_text(screen())
            def finish_viewer():
                deadline=time.monotonic()+4
                while viewer.poll()is None and time.monotonic()<deadline:pump(.05)
                assert viewer.poll()==0,'viewer did not exit'
            def launch(extra=()):
                nonlocal master, slave, viewer, raw
                master, slave = pty.openpty()
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 150, 0, 0))
                original = termios.tcgetattr(slave)
                viewer = subprocess.Popen([str(binary), 'tui', '--runtime-dir', str(runtime), *extra], stdin=slave, stdout=slave, stderr=slave, env={**os.environ, 'TERM':'xterm-256color'}, start_new_session=True)
                raw = bytearray()
                return original
            original_terminal = launch()
            wait_screen('Acceptance project 0'); wait_screen('Acceptance project 1'); capture('projects')
            # Direct numeric/alias jumps must resolve project/channel/device without Enter.
            for number, title, content in [('2','通道','Channel '),('3','装置','Device'),('4','点位','voltage-0'),('5','规则','递增'),('6','报文','接收'),('7','日志','自动上报已启动')]:
                key('1'); key(number); wait_screen(title+' ['); wait_screen(content)
            key('1'); key(':pt\r'); wait_screen('点位 ['); wait_screen('voltage-0')
            key('1'); key(':点位\r'); wait_screen('点位 ['); wait_screen('voltage-0')
            key('1'); key('/Acceptance project 1\r'); key('4'); wait_screen('点位 ['); wait_screen('221'); capture('project-1-points')
            key('1'); key('/Acceptance project 0\r'); key('2')
            wait_screen('通道 ['); wait_screen('Channel 0'); capture('channels')
            key('\r'); wait_screen('装置 ['); wait_screen('Device'); key('\r')
            wait_screen('点位 ['); wait_screen('voltage-0'); capture('points')
            lines=screen().splitlines(); top=next(i for i,l in enumerate(lines)if '┌'in l)
            assert next(i for i,l in enumerate(lines)if '[q]'in l)>top
            assert next(i for i,l in enumerate(lines)if '报文缺口'in l)>top
            assert '保持寄存器 205'in screen()and '输入寄存器 1'in screen()
            assert lines[-2].find('[Enter]')>=0 and '报文缺口'in lines[-1]
            assert '['not in lines[3]and '['not in lines[4]
            assert '\x1b[1;38;5;231;48;5;60m 4:点位 'in raw.decode(errors='replace')
            assert '[d]'not in screen()and 'j/k'not in screen()and '回车'not in screen()
            key('djk');assert '点位详情'not in screen()
            first=next(l for l in screen().splitlines()if 'voltage-0'in l);assert '›'in first
            header=next(l for l in screen().splitlines()if '地址'in l and '品质'in l);assert header.index('地址')==first.index('0')==3
            addresses=[int(m.group(1))for l in lines if(m:=re.search(r'[│]..\s*(\d+)\s+0x',l))]
            assert len(addresses)>4 and addresses==sorted(addresses)and addresses[0]==0,addresses
            key('\x1b[C');wait_screen('input-only');assert 'voltage-0'not in screen();capture('input-points')
            key('\x1b[C');wait_screen('coil-only');assert 'input-only'not in screen();capture('coil-points')
            key('\x1b[D');wait_screen('input-only');key('\x1b[D');wait_screen('voltage-0')
            assert '装置总计'not in screen()and '点位 1-'not in screen()and '匹配'not in screen()
            # Edit a point outside the automatic rule and verify the actual Modbus value.
            key('\x1b[B'); key('e');wait_screen('修改运行点值')
            key('333\r');wait_screen('已修改运行值')
            with socket.create_connection(endpoints[0],timeout=2)as manual_socket:assert holding(manual_socket,1)==333
            wait_screen('333');capture('manual-value')
            key('e');wait_screen('修改运行点值');key('\t128\r');wait_screen('已修改运行值')
            key('\r');wait_screen('点位详情');wait_screen('"品质": 128');wait_screen('"数值": 333');capture('manual-detail')
            key('\x1b');pump(.4);key('s');wait_screen('Modbus 由主站读取');key('g')
            def visible_value():
                for line in screen().splitlines():
                    if 'voltage-0' in line:
                        value = re.search(r'保持寄存器\s+(\d+)', line)
                        if value: return int(value.group(1))
                return None
            def wait_changed(previous, read_value=visible_value):
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline:
                    pump(.1)
                    current = read_value()
                    if current is not None and current != previous: return current
                raise AssertionError('Displayed point did not refresh: '+screen())
            initial_value = visible_value(); assert initial_value is not None
            wait_changed(initial_value)
            key('p'); wait_screen('显示已暂停'); frozen = visible_value(); assert frozen is not None
            reads_before = counts[:]; pump(.7)
            assert visible_value() == frozen, 'Paused display changed values'
            assert all(counts[i] > reads_before[i] for i in range(2)), 'Pause stopped protocol traffic'
            key('p'); wait_screen('实时刷新'); wait_changed(frozen)

            # Detail values must continue to follow the selected point, not a saved copy.
            key('\r'); wait_screen('点位详情'); wait_screen('"数值":')
            def detail_value():
                value = re.search(r'"数值":\s*(\d+)', screen())
                return int(value.group(1)) if value else None
            previous = detail_value(); assert previous is not None
            wait_changed(previous, detail_value)
            key('p'); wait_screen('显示已暂停'); frozen_detail = detail_value(); reads_before = counts[:]
            pump(.7); assert detail_value() == frozen_detail
            assert all(counts[i] > reads_before[i] for i in range(2)), 'Detail pause stopped protocol traffic'
            key('p'); wait_screen('实时刷新'); wait_changed(frozen_detail, detail_value); capture('point-detail')
            key('\x1b'); pump(.5); wait_screen('点位 [')

            key('/电压-204\r'); wait_screen('0xCC'); assert 'voltage-203' not in screen(); capture('search')
            key('\x1b'); pump(.5); wait_screen('voltage-0')
            key(']'); wait_screen('101-200 / 205'); capture('point-page')
            key('5'); wait_screen('规则 ['); wait_screen('递增'); capture('rules')
            key('\r'); wait_screen('规则详情'); wait_screen('"执行次数":')
            def rule_cycles():
                value = re.search(r'"执行次数":\s*(\d+)', screen())
                return int(value.group(1)) if value else None
            previous = rule_cycles(); assert previous is not None
            wait_changed(previous, rule_cycles); capture('rule-detail'); key('\x1b'); pump(.5)

            key('6'); wait_screen('报文 ['); wait_screen('接收'); wait_screen('发送'); capture('frames')
            key('\r'); pump(.2); capture('frame-detail'); key('\x1b'); pump(.5)
            key('p'); wait_screen('显示已暂停'); before_pause = counts[:]; pump(.7)
            assert all(counts[i] > before_pause[i] for i in range(2)); key('p')
            key('7'); wait_screen('日志 ['); wait_screen('自动上报已启动'); capture('logs')
            key('?'); wait_screen('工程继续运行'); key('\x1bp'); wait_screen('显示已暂停'); key('p'); wait_screen('实时刷新')
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 10, 40, 0, 0)); wait_screen('终端太小')
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 150, 0, 0)); wait_screen('日志 [')
            key('q'); finish_viewer(); pump()
            assert same_terminal_settings(termios.tcgetattr(slave), original_terminal), ('q did not restore terminal', original_terminal, termios.tcgetattr(slave))
            assert b'\x1b[?1049l' in raw and b'\x1b[?25h' in raw
            os.close(master); os.close(slave); master = slave = None
            count_before = counts[:]; time.sleep(.4); assert all(counts[i] > count_before[i] for i in range(2))
            original_terminal = launch(); wait_screen('Acceptance project 0'); key('\x03'); finish_viewer()
            assert same_terminal_settings(termios.tcgetattr(slave), original_terminal), 'Ctrl+C did not restore terminal'
            os.close(master); os.close(slave); master = slave = None
            launch(); wait_screen('Acceptance project 0'); viewer.kill(); viewer.wait(timeout=4)
            os.close(master); os.close(slave); master = slave = None
            count_before=counts[:];time.sleep(.4);assert all(counts[i]>count_before[i] for i in range(2))
            original_terminal = launch();wait_screen('Acceptance project 0');key('\r');wait_screen('通道 [')
            selected_socket = list(runtime.glob(f'{processes[0].pid}-*.sock'))
            assert len(selected_socket) == 1
            selected_socket[0].unlink()
            wait_screen('连接中断');wait_screen('连接错误：')
            count_before=counts[:];time.sleep(.4);assert all(counts[i]>count_before[i] for i in range(2))
            key('q');finish_viewer()
            assert same_terminal_settings(termios.tcgetattr(slave), original_terminal)
            os.close(master);os.close(slave);master=slave=None
            # A separate loopback IEC104 project exercises manual edits and explicit
            # reports against real wire frames, while both Modbus projects keep running.
            iec_address=endpoint();iec_runtime=directory/'iec-sockets';iec_path=directory/'iec.json'
            iec_project={'version':2,'name':'Manual IEC104','channels':[{'id':'iec-manual','name':'IEC manual','protocol':'iec104','role':'server','transport':'tcp-server','address':f'{iec_address[0]}:{iec_address[1]}','unit':1,'devices':[{'id':'device-1','name':'IEC device','unit':1,'commonAddress':1,'points':[{'kind':'yc','address':16385,'wireType':13,'value':220.5,'name':'IEC voltage','quality':0},{'kind':'yk','address':100,'value':0,'name':'control-rejected'}],'dataTests':{'telemetry':{'stopOnDisconnect':False,'rules':[{'enabled':True,'kind':'yc','points':'16385','intervalMs':2000,'mode':'fixed','min':222,'max':222,'step':1}]}}}]}]}
            iec_path.write_text(json.dumps(iec_project));paths.append(iec_path);originals.append(iec_path.read_bytes())
            iec_process=subprocess.Popen([str(binary),'run',str(iec_path),'--runtime-dir',str(iec_runtime),'--start-data-rules','--log-format','json'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);processes.append(iec_process)
            iec_socket=connect(iec_address);sockets_extra=[iec_socket]
            iec_socket.sendall(bytes.fromhex('680407000000'));iec_socket.settimeout(4)
            observed_types=[]
            def iec_value(expected=None):
                deadline=time.monotonic()+4
                while time.monotonic()<deadline:
                    frame=recv_all(iec_socket,2);assert frame[0]==104;frame+=recv_all(iec_socket,frame[1])
                    if frame[2]&1:
                        if frame[2]==0x43:iec_socket.sendall(bytes.fromhex('680483000000'))
                        continue
                    number=int.from_bytes(frame[2:4],'little')//2
                    iec_socket.sendall(bytes.fromhex('68040100')+struct.pack('<H',(number+1)%32768*2))
                    asdu=frame[6:];observed_types.append(asdu[0])
                    if len(asdu)>=14 and asdu[0]==13 and int.from_bytes(asdu[6:9],'little')==16385:
                        value=struct.unpack('<f',asdu[9:13])[0]
                        if expected is None or abs(value-expected)<.001:return value,asdu[13]
                raise AssertionError('Expected IEC104 point report missing')
            assert iec_value(222)[0]==222
            original_terminal=launch(('--runtime-dir',str(iec_runtime)));wait_screen('Manual IEC104');key('4');wait_screen('IEC voltage')
            key('e');wait_screen('修改运行点值');key('221.5\t128\r');wait_screen('已上报 1 个连接')
            assert iec_value(221.5)==(221.5,128)
            key('s');wait_screen('已上报 1 个连接');assert iec_value()[1]==128
            capture('manual-iec')
            # Automatic generation is still active and takes effect after the manual change.
            assert iec_value(222)==(222.0,128)
            key('\r');wait_screen('点位详情');wait_screen('"数值": 222');capture('manual-iec-detail');key('\x1b');pump(.4)
            base_args=['--pid',str(iec_process.pid),'--runtime-dir',str(iec_runtime),'--channel','iec-manual','--device','device-1','--kind','yc','--address','16385']
            set_result=subprocess.run([str(binary),'point','set',*base_args,'--value','223.5','--quality','0','--json'],capture_output=True,text=True,timeout=5)
            assert set_result.returncode==0,set_result.stderr;assert json.loads(set_result.stdout)['applied'];assert iec_value(223.5)==(223.5,0)
            report_result=subprocess.run([str(binary),'point','report',*base_args,'--json'],capture_output=True,text=True,timeout=5)
            assert report_result.returncode==0,report_result.stderr;assert not json.loads(report_result.stdout)['applied'];assert iec_value()[1]==0
            invalid=subprocess.run([str(binary),'point','set',*base_args,'--value','NaN'],capture_output=True,text=True,timeout=5);assert invalid.returncode==2
            forbidden=subprocess.run([str(binary),'point','set','--pid',str(iec_process.pid),'--runtime-dir',str(iec_runtime),'--channel','iec-manual','--device','device-1','--kind','yk','--address','100','--value','1'],capture_output=True,text=True,timeout=5);assert forbidden.returncode==1
            assert not any(t in (45,46,48,49,50)for t in observed_types)
            key('q');finish_viewer();assert same_terminal_settings(termios.tcgetattr(slave),original_terminal)
            os.close(master);os.close(slave);master=slave=None;iec_socket.close();sockets_extra=[]
            assert all(p.poll() is None for p in processes), 'viewer stopped project process' 
            assert not failures, failures
            assert all(path.read_bytes()==originals[i] for i,path in enumerate(paths)), 'viewer modified configuration'
            # The read-only API never stops an engine on client disconnect.
            report = {'passed': True, 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'actualPTY': True,
                      'twoIndependentProjects': True, 'directNumericAndAliasJumpsVerified': True, 'highlightedProjectScopeVerified': True, 'searchVerified': True, 'pointPaginationVerified': True,
                      'framesAndRuntimeLogsVerified': True, 'terminalRestoredOnQuitAndCtrlC': True,
                      'quitCtrlCAndViewerCrashPreserveProjects': True, 'pausePreservesCommunication': True,
                      'unicodeCommandsAndSearchVerified': True, 'pointKindTabsAndNumericOrderVerified': True, 'controlsAndLiveStatusAtBottomVerified': True, 'enterAndArrowKeysVerified': True, 'removedDJKShortcutsVerified': True, 'tableHeaderAlignmentVerified': True, 'manualValueQualityAndModbusReadVerified': True, 'compactPointSummaryVerified': True, 'manualIEC104WireAndCLIReportVerified': True, 'automaticRulesContinueAfterManualEditVerified': True, 'coloredNavigationWithoutBracketsVerified': True, 'livePointDetailRefreshPauseResumeVerified': True, 'liveRuleDetailCyclesVerified': True, 'livePointValuesRefreshVerified': True, 'pausedValuesFreezeAndResumeVerified': True, 'fastEscapeThenPauseVerified': True, 'resizeVerified': True, 'observerConnectionLossPreservesProjects': True, 'projectFilesUnchanged': True, 'protocolReadCounts': counts,
                      'platform': sys.platform}
            (args.output/'runtime.json').write_text(json.dumps(report, indent=2));print(json.dumps(report))
        finally:
            stop.set()
            if 'worker' in locals(): worker.join(timeout=3)
            if viewer and viewer.poll() is None:
                viewer.terminate()
                try: viewer.wait(timeout=3)
                except subprocess.TimeoutExpired: viewer.kill();viewer.wait()
            if 'raw' in locals(): (args.output/'last-terminal.ansi').write_bytes(raw)
            for fd in (master,slave):
                if fd is not None: os.close(fd)
            for sock in sockets: sock.close()
            for sock in locals().get('sockets_extra',[]):sock.close()
            for process in processes:
                if process.poll() is None:
                    process.send_signal(signal.SIGTERM)
                    try: assert process.wait(timeout=5)==0
                    except subprocess.TimeoutExpired: process.kill();process.wait();raise
            for stream in streams: stream.close()


if __name__ == '__main__':
    main()
