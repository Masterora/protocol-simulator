"""Install a DEB, verify real WebViews and independent X11 main windows, uninstall."""
from pathlib import Path
import argparse
import base64
import ctypes as C
import hashlib
import io
import tarfile
import json
import os
import pwd
import shutil
import subprocess
import sys
import tempfile
import time


def x11():
    lib = C.CDLL('libX11.so.6')
    lib.XOpenDisplay.argtypes = [C.c_char_p]
    lib.XOpenDisplay.restype = C.c_void_p
    lib.XInternAtom.argtypes = [C.c_void_p, C.c_char_p, C.c_int]
    lib.XInternAtom.restype = C.c_ulong
    display = lib.XOpenDisplay(None)
    assert display, 'X11 display is required for this acceptance procedure'
    return lib, display


def close_window(window):
    class Data(C.Union):
        _fields_ = [('b', C.c_char * 20), ('s', C.c_short * 10), ('l', C.c_long * 5)]
    class Client(C.Structure):
        _fields_ = [('type', C.c_int), ('serial', C.c_ulong), ('send_event', C.c_int),
                    ('display', C.c_void_p), ('window', C.c_ulong), ('message_type', C.c_ulong),
                    ('format', C.c_int), ('data', Data)]
    class Event(C.Union):
        _fields_ = [('client', Client), ('pad', C.c_long * 24)]
    lib, display = x11()
    event = Event()
    event.client.type, event.client.display, event.client.window = 33, display, window
    event.client.message_type = lib.XInternAtom(display, b'WM_PROTOCOLS', 0)
    event.client.format = 32
    event.client.data.l[0] = lib.XInternAtom(display, b'WM_DELETE_WINDOW', 0)
    lib.XSendEvent.argtypes = [C.c_void_p, C.c_ulong, C.c_int, C.c_long, C.POINTER(Event)]
    lib.XFlush.argtypes = [C.c_void_p]
    assert lib.XSendEvent(display, window, 0, 0, C.byref(event))
    lib.XFlush(display)


def screenshot(window, destination):
    subprocess.run(['xdotool', 'windowraise', str(window)], check=True)
    subprocess.run(['xdotool', 'windowfocus', '--sync', str(window)], check=True)
    time.sleep(.3)
    gdk = C.CDLL('libgdk-3.so.0')
    pixbuf = C.CDLL('libgdk_pixbuf-2.0.so.0')
    gdk.gdk_init.argtypes = [C.c_void_p, C.c_void_p]
    gdk.gdk_init(None, None)
    gdk.gdk_display_get_default.restype = C.c_void_p
    display = gdk.gdk_display_get_default()
    gdk.gdk_x11_window_foreign_new_for_display.argtypes = [C.c_void_p, C.c_ulong]
    gdk.gdk_x11_window_foreign_new_for_display.restype = C.c_void_p
    foreign = gdk.gdk_x11_window_foreign_new_for_display(display, window)
    gdk.gdk_window_get_width.argtypes = gdk.gdk_window_get_height.argtypes = [C.c_void_p]
    width, height = gdk.gdk_window_get_width(foreign), gdk.gdk_window_get_height(foreign)
    gdk.gdk_pixbuf_get_from_window.argtypes = [C.c_void_p, C.c_int, C.c_int, C.c_int, C.c_int]
    gdk.gdk_pixbuf_get_from_window.restype = C.c_void_p
    image = gdk.gdk_pixbuf_get_from_window(foreign, 0, 0, width, height)
    pixbuf.gdk_pixbuf_savev.argtypes = [C.c_void_p, C.c_char_p, C.c_char_p, C.c_void_p, C.c_void_p, C.c_void_p]
    assert image and pixbuf.gdk_pixbuf_savev(image, os.fsencode(destination), b'png', None, None, None)


def window_named(name, process):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        assert process.poll() is None, 'desktop exited during startup'
        result = subprocess.run(['xdotool', 'search', '--onlyvisible', '--name', name], capture_output=True, text=True)
        if result.returncode == 0:
            return int(result.stdout.splitlines()[0])
        time.sleep(.1)
    raise AssertionError('main frontend did not publish its project title: ' + name)


def session(directory, smoke):
    binary = Path('/usr/bin/protocol-simulator')
    env = {**os.environ, 'GYLAB_NATIVE_SMOKE_BINARY': str(binary)}
    native = subprocess.run([str(smoke), '-test.run=^TestNativeWindowSmoke$', '-test.v', '-test.timeout=180s'], env=env, capture_output=True, text=True, timeout=200)
    text = native.stdout + native.stderr
    (directory / 'native-windows.txt').write_text(text)
    assert native.returncode == 0, text[-12000:]
    assert text.count('native process and frontend bridge ready') == 25
    assert 'verified_binary_sha256=' + hashlib.sha256(binary.read_bytes()).hexdigest() in text
    processes = []
    try:
        windows = []
        for index, role in enumerate(('client', 'server')):
            name = f'Linux acceptance {index}'
            payload = base64.urlsafe_b64encode(json.dumps({'name': name, 'role': role}).encode()).decode().rstrip('=')
            log = (directory / f'main-{index}.txt').open('w')
            process = subprocess.Popen([str(binary), '--protocol-project=' + payload], stdout=log, stderr=subprocess.STDOUT)
            processes.append(process)
            windows.append(window_named(name, process))
        classes = subprocess.check_output(['xprop', '-id', str(windows[0]), 'WM_CLASS'], text=True)
        assert 'protocol-simulator' in classes, classes
        (directory / 'window-class.txt').write_text(classes)
        subprocess.run(['xdotool','windowfocus','--sync',str(windows[0])],check=True)
        subprocess.run(['xdotool','key','ctrl+s'],check=True)
        deadline=time.monotonic()+10
        dialog=None
        while time.monotonic()<deadline:
            result=subprocess.run(['xdotool','search','--onlyvisible','--name','保存'],capture_output=True,text=True)
            if result.returncode==0:
                dialog=int(result.stdout.splitlines()[0]);break
            time.sleep(.1)
        assert dialog,'native Save dialog did not open'
        saved=directory/'saved-project.json'
        subprocess.run(['xdotool','windowfocus','--sync',str(dialog)],check=True)
        subprocess.run(['xdotool','key','ctrl+l'],check=True)
        subprocess.run(['xdotool','key','ctrl+a'],check=True)
        subprocess.run(['xdotool','type','--clearmodifiers',str(saved)],check=True)
        subprocess.run(['xdotool','key','Return'],check=True)
        time.sleep(.5)
        if not saved.exists():subprocess.run(['xdotool','key','Return'],check=True)
        deadline=time.monotonic()+10
        while not saved.exists() and time.monotonic()<deadline:time.sleep(.1)
        if not saved.exists():
            (directory/'save-window-tree.txt').write_text(subprocess.check_output(['xwininfo','-root','-tree'],text=True))
            screenshot(windows[0],directory/'save-failure-main.png')
            try:screenshot(dialog,directory/'save-failure-dialog.png')
            except Exception:pass
            raise AssertionError('native Save did not write the project')
        stored=json.loads(saved.read_text())
        assert stored['name']=='saved-project' and len(stored['channels'])==1
        assert processes[1].poll() is None
        time.sleep(.5)
        screenshot(windows[0], directory / 'main-client.png')
        screenshot(windows[1], directory / 'main-server.png')
        close_window(windows[0])
        assert processes[0].wait(timeout=10) == 0
        assert processes[1].poll() is None, 'closing one project interrupted another'
        close_window(windows[1])
        assert processes[1].wait(timeout=10) == 0
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
    (directory / 'session.json').write_text(json.dumps({'passed': True, 'nativeWindowScenarios': 25, 'independentMainWindowsVerified': True, 'windowClassVerified': True, 'nativeProjectSaveVerified': True, 'displayBackend': 'X11/Xvfb'}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--package', type=Path)
    parser.add_argument('--smoke-binary', type=Path, required=True)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--user', default=os.environ.get('SUDO_USER') or 'tester')
    parser.add_argument('--session', type=Path)
    args = parser.parse_args()
    assert sys.platform == 'linux'
    if args.session:
        session(args.session, args.smoke_binary)
        return
    assert os.geteuid() == 0 and args.package and args.output
    check = subprocess.run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'protocol-simulator'], capture_output=True, text=True)
    assert check.stdout.strip() != 'installed', 'existing desktop installation must not be modified'
    account = pwd.getpwnam(args.user)
    assert account.pw_uid != 0, 'desktop acceptance must run as a normal user'
    args.output.mkdir(parents=True, exist_ok=True)
    system_ps = Path('/usr/bin/ps')
    system_ps_hash = hashlib.sha256(system_ps.read_bytes()).hexdigest() if system_ps.exists() else None
    try:
        subprocess.run(['dpkg', '-i', str(args.package)], check=True)
        binary = Path('/usr/bin/protocol-simulator')
        with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['dpkg-deb','--fsys-tarfile',str(args.package)]))) as archive:
            member=next(m for m in archive.getmembers() if m.name.lstrip('./')=='usr/share/doc/protocol-simulator/build.json')
            info=json.loads(archive.extractfile(member).read())
        assert info['binarySHA256'] == hashlib.sha256(binary.read_bytes()).hexdigest()
        with tempfile.TemporaryDirectory(prefix='protocol-desktop-session-') as temporary:
            directory = Path(temporary)
            os.chown(directory, account.pw_uid, account.pw_gid)
            (directory/'runtime').mkdir(mode=0o700)
            os.chown(directory/'runtime',account.pw_uid,account.pw_gid)
            env = ['HOME=' + account.pw_dir, 'XDG_CONFIG_HOME=' + str(directory / 'config'),
                   'XDG_RUNTIME_DIR=' + str(directory/'runtime'), 'XDG_CACHE_HOME=' + str(directory / 'cache'), 'XDG_DATA_HOME=' + str(directory / 'data'),
                   'GDK_BACKEND=x11', 'LIBGL_ALWAYS_SOFTWARE=1', 'WEBKIT_DISABLE_DMABUF_RENDERER=1']
            command = ['runuser', '-u', args.user, '--', 'env', *env, 'dbus-run-session', '--', 'xvfb-run', '-a', '-s', '-screen 0 1600x1200x24',
                       'python3', str(Path(__file__).resolve()), '--session', str(directory), '--smoke-binary', str(args.smoke_binary)]
            result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=300)
            (args.output / 'desktop-session.txt').write_text(result.stdout)
            for name in ('session.json', 'native-windows.txt', 'main-client.png', 'main-server.png', 'main-0.txt', 'main-1.txt', 'window-class.txt', 'saved-project.json', 'save-window-tree.txt', 'save-failure-main.png', 'save-failure-dialog.png'):
                if (directory / name).exists():
                    shutil.copy2(directory / name, args.output / name)
            assert result.returncode == 0, result.stdout[-12000:]
            user_project = directory / 'saved-project.json'
            original_project = user_project.read_bytes()
            subprocess.run(['dpkg', '-r', 'protocol-simulator'], check=True)
            assert user_project.read_bytes() == original_project
            assert not binary.exists()
        if system_ps_hash:
            assert hashlib.sha256(system_ps.read_bytes()).hexdigest() == system_ps_hash
        report = {'passed': True, 'packageSHA256': hashlib.sha256(args.package.read_bytes()).hexdigest(),
                  'binarySHA256': info['binarySHA256'], 'architecture': info['architecture'], 'nativeWindowScenarios': 25,
                  'installationVerified': True, 'nativeProjectSaveVerified': True, 'uninstallPreservesProjects': True, 'systemPsPreserved': True,
                  'displayBackend': 'X11/Xvfb', 'waylandVerified': False}
        (args.output / 'runtime.json').write_text(json.dumps(report, indent=2))
        print(json.dumps(report))
    finally:
        subprocess.run(['dpkg', '-r', 'protocol-simulator'], capture_output=True)


if __name__ == '__main__':
    main()
