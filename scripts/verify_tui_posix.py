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


holding_locks = {}


def holding(sock, address=0):
    # Background traffic and foreground assertions share protocol connections.
    # Keep each request/response together so one reader cannot consume another's reply.
    with holding_locks.setdefault(sock, threading.Lock()):
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
                points += [{'address': 1, 'kind': 'input', 'name': 'input-only', 'value': 99}, {'address': 2, 'kind': 'coil', 'name': 'coil-only', 'value': 1}, {'address': 3, 'kind': 'discrete', 'name': 'binding-feedback', 'value': 0}]
                project = {'version': 2, 'name': f'Acceptance project {index}', 'channels': [{'id': f'channel-{index}', 'name': f'Channel {index}', 'protocol': 'modbus-tcp', 'role': 'server', 'transport': 'tcp-server', 'address': f'{address[0]}:{address[1]}', 'unit': 1, 'devices': [{'id': 'device-1', 'name': 'Device', 'unit': 1, 'commonAddress': 1, 'points': points, 'dataTests': {'telemetry': {'stopOnDisconnect': False, 'rules': [{'enabled': True, 'kind': 'holding', 'points': '0', 'intervalMs': 200, 'mode': 'increment', 'min': 220, 'max': 230, 'step': 1}]}}}]}]}
                rules = project['channels'][0]['devices'][0]['dataTests']['telemetry']['rules']
                rules.append({**rules[0], 'points': '2'})
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
                    frame = re.sub(rb'(?:\x1b\[\d+;\d+H)?\x1b\[\?25[hl]$', b'', frame)
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
            # Numbered tabs resolve project/channel/device without Enter.
            for number, title, content in [('2','通道','Channel '),('3','装置','Device'),('4','点位','voltage-0'),('5','规则','递增'),('6','报文','接收'),('7','日志','自动上报已启动')]:
                key('1'); key(number); wait_screen(title+' '); wait_screen(content)
            key('1'); key(':fl\x1bOP\x1b[3~'); wait_screen('工程 ')
            assert '命令 :' not in screen() and '确认永久删除' not in screen() and '配置速查' not in screen()
            key('X'); wait_screen('确认永久删除'); wait_screen('› 取消')
            key(' \t\x1b[A'); assert '› 取消' in screen()
            key('\x1b[C'); wait_screen('› 永久删除')
            key('\x1b'); pump(.4); assert '确认永久删除' not in screen()
            key('1'); key('/Acceptance project 1\r'); key('4'); wait_screen('点位 '); wait_screen('221'); capture('project-1-points')
            key('1'); key('/Acceptance project 0\r'); key('2')
            wait_screen('通道 '); wait_screen('Channel 0'); wait_screen('角色'); wait_screen('子站'); capture('channels')
            key('e'); wait_screen('编辑通道')
            key('\t'*4+'\x7f')
            assert re.search(r'› 监听地址 +127\.0 +\.0 +\.1 +:'+str(endpoints[0][1])[:-1]+r' +│',screen()),screen()
            assert '波特率' not in screen() and '本地绑定' not in screen(),screen()
            key('\x15'); wait_screen('   .   .   .   :')
            blink_start=len(raw); pump(1.2)
            assert b'\x1b[?25h' in raw[blink_start:] and b'\x1b[?25l' in raw[blink_start:], 'input caret did not blink'
            key('1921681001001000'); wait_screen('192.168.100.100:1000')
            key('\x15'+'1.1.1.1:1'); wait_screen('1  .1  .1  .1  :1')
            key('\x1b[1;2Z'); wait_screen('› 连接方式')
            key('\x1b'); pump(.4)
            assert paths[0].read_bytes()==originals[0]
            key('\r'); wait_screen('装置 '); wait_screen('Device'); key('\r')
            wait_screen('点位 '); wait_screen('voltage-0'); capture('points')
            lines=screen().splitlines(); top=next(i for i,l in enumerate(lines)if '┌'in l)
            assert next(i for i,l in enumerate(lines)if '[q]'in l)<top
            assert next(i for i,l in enumerate(lines)if re.match(r'^\s*\d+d \d{2}:\d{2}:\d{2}\s',l))>top
            assert '保持寄存器'in screen()and '输入寄存器'in screen()
            assert '保持寄存器 205'not in screen()and '输入寄存器 1'not in screen()
            assert lines[4].find('[Enter]')>=0 and re.match(r'^\s*\d+d \d{2}:\d{2}:\d{2}\s',lines[-1])
            assert 'PRS' not in lines[0]
            assert '缺口'not in screen() and '实时刷新' not in screen() and '界面更新' not in screen()
            assert lines[-1].endswith('Produced by Der_Orange 🍊')
            def visible_uptime():
                match=re.match(r'^\s*(\d+)d (\d{2}):(\d{2}):(\d{2})\s',screen().splitlines()[-1])
                assert match,'project uptime missing from footer: '+screen()
                days,hours,minutes,seconds=map(int,match.groups())
                assert hours<24 and minutes<60 and seconds<60
                return days*86400+hours*3600+minutes*60+seconds
            initial_uptime=visible_uptime();pump(1.2);assert visible_uptime()>initial_uptime,'project uptime stopped advancing'

            assert '['not in lines[3]and lines[5].startswith('┌ 点位 ')
            assert '保持寄存器'in lines[5]and '输入寄存器'in lines[5]
            assert '\x1b[1;38;5;231;48;5;60m 4:点位 'in raw.decode(errors='replace')
            assert '[d]'not in screen()and 'j/k'not in screen()and '回车'not in screen()
            key('djk');assert '点位详情'not in screen()
            first=next(l for l in screen().splitlines()if 'voltage-0'in l);assert '›'in first
            wait_screen('上报时间')
            def point_communication_clock():
                line=next((l for l in screen().splitlines() if 'voltage-0' in l),'')
                match=re.search(r'\d{2}:\d{2}:\d{2}(?![.\d])',line)
                return match.group(0) if match else None
            header=next(l for l in screen().splitlines()if '点位'in l and '品质'in l);assert header.index('点位')==re.search(r'\s0\s+0x',first).start()+1
            addresses=[int(m.group(1))for l in lines if(m:=re.search(r'[│]..\s*\d+\s+(\d+)\s+0x',l))]
            assert len(addresses)>4 and addresses==sorted(addresses)and addresses[0]==0,addresses
            key('\x1b[C');wait_screen('input-only');assert 'voltage-0'not in screen();capture('input-points')
            key('\x1b[C');wait_screen('coil-only');assert 'input-only'not in screen();capture('coil-points')
            # The binding selector is real terminal input; saving it must not
            # stop the existing rules or the independent project.
            wait_screen('[e]编辑');wait_screen('未绑定');assert '[b]' not in screen()
            key('B');assert '编辑点位' not in screen()
            key('E');wait_screen('编辑点位')
            for _ in range(7):key('\t')
            wait_screen('绑定遥信点位');assert 'binding-feedback' not in screen()
            key('3');key('\r');wait_screen('配置已保存并生效')
            wait_screen('3 binding-feedback');capture('binding-selected')
            binding=json.loads(paths[0].read_text())['channels'][0]['devices'][0]['configuration']['controls']['yk']['mappings']
            assert binding==[{'source':{'kind':'coil','address':2},'target':{'kind':'discrete','address':3},'invert':False}]
            key('7');wait_screen('日志 ');key('/配置已保存\r');wait_screen('编辑点位 ·')
            assert 'configuration_saved' not in screen(),screen()
            key('\r');wait_screen('日志详情');wait_screen('事件标识：configuration_saved')
            capture('configuration-saved-log');key('\x1b');pump(.4)
            key('4');wait_screen('3 binding-feedback')
            key('\r');wait_screen('点位详情');wait_screen('绑定点位：3 binding-feedback')
            ordered=['点位：2', '名称：coil-only', '类型：线圈', '数值：', '品质：', '绑定点位：3 binding-feedback', '上报时间：']
            positions=[screen().index(field) for field in ordered]
            assert positions==sorted(positions),screen()
            capture('binding-detail');key('\x1b');pump(.4)
            with socket.create_connection(endpoints[0],timeout=2) as binding_socket:
                def binding_exchange(data):
                    binding_socket.sendall(bytes.fromhex(data))
                    header=recv_all(binding_socket,7)
                    return recv_all(binding_socket,int.from_bytes(header[4:6],'big')-1)
                assert binding_exchange('00010000000601050002ff00')==bytes.fromhex('050002ff00')
                assert binding_exchange('000200000006010200030001')==bytes.fromhex('020101')
            key('e');wait_screen('编辑点位')
            for _ in range(7):key('\t')
            key('\x15');key('\r');wait_screen('未绑定')
            assert json.loads(paths[0].read_text())['channels'][0]['devices'][0]['configuration']['controls']['yk']['mappings']==[]
            key('\r');wait_screen('绑定点位：未绑定');key('\x1b');pump(.4)
            assert paths[1].read_bytes()==originals[1]
            key('\x1b[D');wait_screen('input-only');key('\x1b[D');wait_screen('voltage-0')
            assert '装置总计'not in screen()and '点位 1-'not in screen()and '匹配'not in screen()
            # Edit a point outside the automatic rule and verify the actual Modbus value.
            key('\x1b[B'); key('v');wait_screen('修改运行点值')
            key('333\r');wait_screen('已修改运行值')
            with socket.create_connection(endpoints[0],timeout=2)as manual_socket:assert holding(manual_socket,1)==333
            wait_screen('333');capture('manual-value')
            key('v');wait_screen('修改运行点值');key('\t128\r');wait_screen('已修改运行值')
            key('\r');wait_screen('点位详情');wait_screen('品质：128');wait_screen('数值：333');capture('manual-detail')
            key('\x1b');pump(.4);key('s')
            assert '[s]' not in screen() and 'Modbus 由主站读取' not in screen()
            key('\x1b[H')
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
            previous=point_communication_clock();assert previous is not None,'actual poll time missing'
            assert not re.search(r'\d{2}:\d{2}:\d{2}\.\d+', next(l for l in screen().splitlines() if 'voltage-0' in l))
            wait_changed(previous,point_communication_clock)
            capture('point-communication-time')
            initial_value = visible_value(); assert initial_value is not None
            wait_changed(initial_value)
            for unused_key in ('p', 'P', ' '):
                before = visible_value(); reads_before = counts[:]
                key(unused_key); wait_changed(before)
                assert '显示已暂停' not in screen() and '[p]' not in screen()
                assert all(counts[i] > reads_before[i] for i in range(2))

            # Detail values must continue to follow the selected point, not a saved copy.
            key('\r'); wait_screen('点位详情'); wait_screen('数值：')
            assert re.search(r'上报时间：\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?=\s)', screen()), screen()
            assert '更新时间' not in screen()
            def detail_value():
                value = re.search(r'数值：\s*(\d+)', screen())
                return int(value.group(1)) if value else None
            previous = detail_value(); assert previous is not None
            wait_changed(previous, detail_value)
            for unused_key in ('p', 'P', ' '):
                before = detail_value(); key(unused_key); wait_changed(before, detail_value)
                assert '显示已暂停' not in screen()
            capture('point-detail')
            key('\x1b'); pump(.5); wait_screen('点位 ')

            key('/电压-204\r'); wait_screen('0xCC'); assert 'voltage-203' not in screen(); capture('search')
            key('\x1b'); pump(.5); wait_screen('voltage-0')
            key('\x1b[6~'); wait_screen('voltage-21'); assert re.search(r'│› 22\s+21\s+0x',screen()), screen()
            for target in (42,63,84,105):
                key('\x1b[6~'); wait_screen(f'voltage-{target}')
                assert re.search(fr'│› {target+1}\s+{target}\s+0x',screen()), screen()
            capture('point-page')
            key('\x1b[5~'); wait_screen('voltage-99'); assert re.search(r'│› 85\s+84\s+0x',screen()), screen()
            key('\x1b[F'); wait_screen('电压-204'); assert re.search(r'│› 205\s+204\s+0x',screen()), screen()
            key('\x1b[H'); wait_screen('voltage-0'); assert re.search(r'│› 1\s+0\s+0x',screen()), screen()
            key('5'); wait_screen('规则 '); wait_screen('递增'); wait_screen('状态'); assert any('递增' in line and '运行中' in line for line in screen().splitlines()); capture('rules')
            key('\r'); wait_screen('规则详情'); wait_screen('执行次数：')
            def rule_cycles():
                value = re.search(r'执行次数：\s*(\d+)', screen())
                return int(value.group(1)) if value else None
            previous = rule_cycles(); assert previous is not None
            wait_changed(previous, rule_cycles); capture('rule-detail'); key('\x1b'); pump(.5)

            # Direct task shortcuts need no menu or Enter and leave the other
            # project's wire traffic and project configuration unchanged.
            key('R'); wait_screen('所选规则已停止'); wait_screen('[r]启动规则'); assert any('递增' in line and '已停止' in line for line in screen().splitlines()); assert '规则 ·' not in screen()
            assert '当前装置规则任务' not in screen()
            value0=holding(sockets[0]); sibling0=holding(sockets[0],2); other0=holding(sockets[1]); pump(.45)
            assert holding(sockets[0])==value0 and holding(sockets[1])!=other0
            assert holding(sockets[0],2)!=sibling0
            assert any('递增' in line and '运行中' in line for line in screen().splitlines())
            capture('individual-rule-stopped')
            key('s'); assert '[r]启动规则' in screen(); assert holding(sockets[0]) == value0
            key('R'); wait_screen('所选规则已启动'); wait_screen('[r]停止规则'); assert any('递增' in line and '运行中' in line for line in screen().splitlines()); assert '规则 ·' not in screen()
            wait_changed(value0, lambda: holding(sockets[0]))
            key('r'); wait_screen('所选规则已停止'); wait_screen('[r]启动规则'); assert '规则 ·' not in screen()
            stopped = holding(sockets[0]); pump(.45); assert holding(sockets[0]) == stopped
            key('r'); wait_screen('所选规则已启动'); wait_screen('[r]停止规则'); assert '规则 ·' not in screen()
            assert '当前装置规则任务' not in screen()
            key('p'); assert '显示已暂停' not in screen()
            key('6'); wait_screen('报文 '); wait_screen('接收'); wait_screen('发送'); capture('frames')
            key('\r'); pump(.2); capture('frame-detail'); key('\x1b'); pump(.5)
            key('p'); before = counts[:]; pump(.7)
            assert all(counts[i] > before[i] for i in range(2)); assert '显示已暂停' not in screen()
            key('7'); wait_screen('日志 '); wait_screen('自动上报已启动'); capture('logs')
            key('\x1b[A'); wait_screen('[g]跟随')
            key('\x1b[F'); wait_screen('日志 · 浏览')
            key('G'); wait_screen('日志 · 跟随'); assert '[g]' not in screen()
            key('?'); wait_screen('配置速查'); wait_screen('prs validate FILE --json')
            wait_screen('浏览快捷键'); wait_screen('工程继续运行')
            key('\x1b[6~'); wait_screen('配置快捷键')
            key('\x1bp'); wait_screen('Produced by Der_Orange 🍊'); assert '配置速查' not in screen()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 10, 40, 0, 0)); wait_screen('终端太小')
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 150, 0, 0)); wait_screen('日志 ')
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
            original_terminal = launch()
            wait_screen('Acceptance project 0')
            key('/Acceptance project 0\r')
            key('2')
            wait_screen('Channel 0')
            selected_socket = list(runtime.glob(f'{processes[0].pid}-*.sock'))
            assert len(selected_socket) == 1
            selected_socket[0].unlink()
            wait_screen('连接错误：')
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
            original_terminal=launch(('--runtime-dir',str(iec_runtime)));wait_screen('Manual IEC104');key('4');wait_screen('IEC voltage')
            key('s');wait_screen('等待主站连接')
            iec_socket=connect(iec_address);sockets_extra=[iec_socket]
            key('2');wait_screen('待激活');key('4');wait_screen('IEC voltage')
            key('s');wait_screen('等待主站发送 STARTDT')
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
            key('v');wait_screen('修改运行点值');key('\t\x1b[9;2u221.5\t128\r');wait_screen('已上报')
            assert iec_value(221.5)==(221.5,128)
            key('s');wait_screen('已上报');assert iec_value()[1]==128
            capture('manual-iec')
            # Automatic generation is still active and takes effect after the manual change.
            assert iec_value(222)==(222.0,128)
            key('\r');wait_screen('点位详情');wait_screen('数值：222');wait_screen('报文类型：短浮点遥测（13）');capture('manual-iec-detail');key('\x1b');pump(.4)
            key('e');wait_screen('编辑点位')
            for _ in range(5):key('\t')
            key(' ');wait_screen('选择报文类型');wait_screen('短浮点遥测（13）');wait_screen('标度化遥测（11）')
            assert '单点遥信' not in screen() and '单点遥控' not in screen(),screen()
            key('\x1b');pump(.4);key('\x1b');pump(.4)
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
            # Only explicitly edited fields are persisted; automatic values remain runtime-only.
            for i,path in enumerate(paths):
                stored=json.loads(path.read_text());original=json.loads(originals[i])
                actual=stored['channels'][0]['devices'][0];prior=original['channels'][0]['devices'][0]
                old_points={(p['kind'],p['address']):p for p in prior['points']}
                for point in actual['points']:
                    assert 'communicationAt' not in point,'runtime communication history leaked into configuration'
                    point_key=(point['kind'],point['address']);expected=old_points[point_key]
                    if i==0 and point_key==('holding',1):
                        assert point['value']==333 and point['quality']==128
                    elif i==2 and point_key==('yc',16385):
                        assert point['value']==223.5 and point['quality']==0
                    else:assert point.get('value',0)==expected.get('value',0) and point.get('quality',0)==expected.get('quality',0)
                for scope,config in prior.get('dataTests',{}).items():
                    got=actual['dataTests'][scope]
                    assert got['stopOnDisconnect']==config['stopOnDisconnect'] and len(got['rules'])==len(config['rules'])
                    for old,new in zip(config['rules'],got['rules']):
                        assert all(new.get(k)==v for k,v in old.items())
                        assert all(k in old or v in (0,False,'',None) for k,v in new.items())
            assert paths[1].read_bytes()==originals[1], 'independent project configuration changed'
            # File transfers use an isolated project; the original
            # protocol peers stay connected during these operations.
            tool_runtime = directory / 'tool-sockets'
            tool_path = directory / 'tools.json'
            modbus_address = endpoint()
            tool_project = {'version':2,'name':'TUI tool acceptance','channels':[
                {'id':'table','name':'Table','protocol':'modbus-tcp','role':'server','transport':'tcp-server',
                 'address':f'{modbus_address[0]}:{modbus_address[1]}','devices':[{'id':'table-device','name':'Table device',
                 'unit':1,'commonAddress':1,'points':[{'kind':'holding','address':0,'value':42,'name':'Before'}]}]}]}

            tool_path.write_text(json.dumps(tool_project))
            original_tools = tool_path.read_bytes()
            tool_process = subprocess.Popen([str(binary),'run',str(tool_path),'--runtime-dir',str(tool_runtime)],
                                            stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            processes.append(tool_process)
            modbus_peer = connect(modbus_address)
            sockets_extra = [modbus_peer]
            assert holding(modbus_peer) == 42
            point_source = directory/'导入.csv'
            point_source.write_text('kind,address,name,value\nholding,0,Imported,75\nholding,1,Added,82\n')
            original_terminal = launch(('--runtime-dir',str(tool_runtime)))
            wait_screen('TUI tool acceptance')
            key('4'); wait_screen('Before'); wait_screen('[m]导入')
            key('i'); wait_screen('点位详情'); key('\x1b'); pump(.4)
            key('M'); wait_screen('导入点表'); key(str(point_source)+'\r')
            wait_screen('新增 1 · 覆盖 1 · 删除 0')
            assert tool_path.read_bytes() == original_tools and holding(modbus_peer) == 42
            key('\x1b'); pump(.4); assert tool_path.read_bytes() == original_tools
            key('m'); wait_screen('导入点表'); key(str(point_source)+'\r')
            wait_screen('确认导入点表'); key('\r'); wait_screen('点表已保存并生效')
            assert holding(modbus_peer) == 75 and holding(modbus_peer,1) == 82
            stored = json.loads(tool_path.read_text())
            assert stored['channels'][0]['devices'][0]['points'][0]['name'] == 'Imported'
            destination = directory/'导出.json'
            key('o'); wait_screen('导出点表'); key(str(destination)+'\t\x1b[C\x1b[C\r')
            wait_screen('点表已导出')
            exported = destination.read_bytes()
            assert [(p['address'],p['value']) for p in json.loads(exported)] == [(0,75),(1,82)]
            key('o'); wait_screen('导出点表'); key(str(destination)+'\t\x1b[C\x1b[C\r')
            wait_screen('文件已存在'); assert destination.read_bytes() == exported
            key('\x1b'); pump(.4); key('6'); wait_screen('报文 ')
            key('a')
            assert '[a]' not in screen() and '报文自动化' not in screen()
            assert holding(modbus_peer) == 75 and all(p.poll() is None for p in processes)
            key('\x03'); finish_viewer()
            assert same_terminal_settings(termios.tcgetattr(slave),original_terminal)
            assert paths[1].read_bytes() == originals[1] and not failures
            # The read-only API never stops an engine on client disconnect.
            report = {'passed': True, 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'actualPTY': True, 'pointTableImportExportVerified': True, 'removedRawShortcutVerified': True,
                      'twoIndependentProjects': True, 'numberedTabsVerified': True, 'highlightedProjectScopeVerified': True, 'searchVerified': True, 'pointPaginationVerified': True,
                      'framesAndRuntimeLogsVerified': True, 'terminalRestoredOnQuitAndCtrlC': True,
                      'quitCtrlCAndViewerCrashPreserveProjects': True, 'formerPauseKeysPreserveLiveRefresh': True,
                      'unicodeSearchVerified': True, 'unlistedKeysIgnored': True, 'deleteConfirmationKeysVerified': True, 'logFollowShortcutVerified': True, 'pointKindTabsAndNumericOrderVerified': True, 'controlsAtTopAndStatusAtBottomVerified': True, 'enterAndArrowKeysVerified': True, 'removedDJKShortcutsVerified': True, 'tableHeaderAlignmentVerified': True, 'manualValueQualityAndModbusReadVerified': True, 'compactPointSummaryVerified': True, 'manualIEC104WireAndCLIReportVerified': True, 'automaticRulesContinueAfterManualEditVerified': True, 'coloredNavigationWithoutBracketsVerified': True, 'livePointDetailAlwaysRefreshesVerified': True, 'liveRuleDetailCyclesVerified': True, 'livePointValuesRefreshVerified': True, 'pointCommunicationTimeVerified': True, 'pointClockSecondsOnlyVerified': True, 'displayPauseRemovedVerified': True, 'fastEscapeThenUnusedKeyVerified': True, 'resizeVerified': True, 'observerConnectionLossPreservesProjects': True, 'explicitEditsPersistedWithoutRuntimeSnapshot': True, 'independentProjectFileUnchanged': True, 'protocolReadCounts': counts,
                      'singleRuleToggleKeyWithoutMenuVerified': True, 'liveRuleStatusColumnVerified': True, 'individualRuleIsolationVerified': True, 'pointBindingSelectorAndWireFeedbackVerified': True, 'pointDetailOrderAndBindingsVerified': True, 'localizedConfigurationSaveLogVerified': True, 'platform': sys.platform}
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
