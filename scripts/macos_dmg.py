"""Create and verify a read-only drag-to-install macOS disk image."""
from contextlib import contextmanager
from pathlib import Path
import json
import os
import plistlib
import shutil
import subprocess
import tempfile

from delivery import digest

APP_NAME = "protocol-simulator.app"


def application_icon(app):
    app = Path(app)
    info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
    icon = app / "Contents/Resources" / info["CFBundleIconFile"]
    if not icon.suffix:
        icon = icon.with_suffix(".icns")
    if not icon.read_bytes().startswith(b"icns"):
        raise ValueError("Application icon is missing or invalid")
    return icon


def custom_icon_enabled(path):
    info = bytes.fromhex(subprocess.check_output(["xattr", "-px", "com.apple.FinderInfo", str(path)], text=True))
    return bool(int.from_bytes(info[8:10], "big") & 0x0400)


def set_file_icon(path, icon):
    # A DMG file's Finder icon is macOS metadata. The mounted volume's icon is
    # embedded separately, so it also survives downloads that strip xattrs.
    script = '''function run(argv) {
        ObjC.import('AppKit');
        const image = $.NSImage.alloc.initWithContentsOfFile(argv[1]);
        if (!$.NSWorkspace.sharedWorkspace.setIconForFileOptions(image, argv[0], 0))
            throw Error('Could not set Finder icon');
        return 'ok';
    }'''
    subprocess.run(["osascript", "-l", "JavaScript", "-e", script, str(path), str(icon)],
                   check=True, capture_output=True, text=True)
    fork = bytes.fromhex(subprocess.check_output(["xattr", "-px", "com.apple.ResourceFork", str(path)], text=True))
    if not custom_icon_enabled(path) or b"icns" not in fork:
        raise ValueError("DMG Finder icon metadata is missing")


def prepare_payload(release, destination):
    release, destination = Path(release), Path(destination)
    shutil.copytree(release / APP_NAME, destination / APP_NAME, symlinks=True)
    (destination / "Applications").symlink_to("/Applications", target_is_directory=True)
    resources = destination / "资料"
    resources.mkdir()
    for path in release.iterdir():
        if path.name in {APP_NAME, "protocol-simulator.exe", "file-manifest.json"}:
            continue
        target = resources / path.name
        if path.is_dir():
            shutil.copytree(path, target, symlinks=True)
        else:
            shutil.copy2(path, target)
    (destination / "安装说明.txt").write_text(
        "protocol-simulator 安装与更新\n\n"
        "1. 将 protocol-simulator 拖到 Applications（应用程序）。\n"
        "2. 安装完成后，推出此磁盘镜像，从“应用程序”启动。\n\n"
        "更新：先保存工程并退出旧版，再拖入新版，选择替换。\n"
        "工程 JSON 请保存在文稿等可写目录；更新后手动打开工程、连接和启动任务。\n"
        "使用指南、示例、许可和构建摘要见“资料”文件夹。\n"
        "当前应用使用本地签名；如系统阻止打开，请在“系统设置 → 隐私与安全性”中允许打开。\n",
        encoding="utf-8",
    )


def inventory(directory):
    entries = {}
    for path in Path(directory).rglob("*"):
        key = path.relative_to(directory).as_posix()
        if path.is_symlink():
            entries[key] = {"symlink": os.readlink(path)}
        elif path.is_file():
            entries[key] = {"sha256": digest(path), "executable": bool(path.stat().st_mode & 0o111)}
    return entries


def verify_inventory(directory, entries):
    for key, entry in entries.items():
        path = Path(directory) / key
        if "symlink" in entry:
            if not path.is_symlink() or os.readlink(path) != entry["symlink"]:
                raise ValueError("Disk image symlink mismatch: " + key)
        elif path.is_symlink() or not path.is_file() or digest(path) != entry["sha256"]:
            raise ValueError("Disk image content mismatch: " + key)
        elif bool(path.stat().st_mode & 0o111) != entry["executable"]:
            raise ValueError("Disk image executable permission mismatch: " + key)


@contextmanager
def mount_image(image, work, read_only=True):
    mount = Path(tempfile.mkdtemp(prefix="dmg-mount-check-", dir=work))
    attached = False
    try:
        subprocess.run(["hdiutil", "attach", "-readonly" if read_only else "-readwrite", "-nobrowse", "-noautoopen",
                        "-mountpoint", str(mount), str(image)], check=True)
        attached = True
        if read_only and not os.statvfs(mount).f_flag & os.ST_RDONLY:
            raise ValueError("Disk image did not mount read-only")
        yield mount
    finally:
        if attached:
            subprocess.run(["hdiutil", "detach", str(mount)], check=True)
        # Never recursively delete a mount point, including on detach failure.
        mount.rmdir()


def mount_readonly(image, work):
    return mount_image(image, work)


def create_disk_image(release, image, work):
    icon = application_icon(Path(release) / APP_NAME)
    with tempfile.TemporaryDirectory(prefix="dmg-build-", dir=work) as directory:
        source = Path(directory) / "payload"
        source.mkdir()
        prepare_payload(release, source)
        shutil.copy2(icon, source / ".VolumeIcon.icns")
        entries = inventory(source)
        (source / "资料/dmg-manifest.json").write_text(
            json.dumps(entries, ensure_ascii=False, indent=2), encoding="utf-8")
        writable = Path(directory) / "writable.dmg"
        subprocess.run(["hdiutil", "create", "-srcfolder", str(source), "-volname", "protocol-simulator",
                        "-fs", "HFS+", "-format", "UDRW", str(writable)], check=True)
        with mount_image(writable, work, read_only=False) as mounted:
            subprocess.run(["SetFile", "-a", "C", str(mounted)], check=True)
            if not custom_icon_enabled(mounted):
                raise ValueError("Cannot enable the mounted volume icon")
        subprocess.run(["hdiutil", "convert", str(writable), "-format", "UDZO", "-ov", "-o", str(image)], check=True)
    subprocess.run(["hdiutil", "verify", str(image)], check=True)
    set_file_icon(image, icon)
    with mount_readonly(image, work) as mounted:
        verify_inventory(mounted, entries)
        if not custom_icon_enabled(mounted):
            raise ValueError("Mounted volume custom icon is not enabled")
        if json.loads((mounted / "资料/dmg-manifest.json").read_text(encoding="utf-8")) != entries:
            raise ValueError("Disk image manifest mismatch")
        if (mounted / "protocol-simulator.exe").exists():
            raise ValueError("Windows executable leaked into the Mac installer")
        mounted_app = mounted / APP_NAME
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(mounted_app)], check=True)
        with tempfile.TemporaryDirectory(prefix="dmg-install-check-", dir=work) as install:
            installed_app = Path(install) / "Applications" / APP_NAME
            installed_app.parent.mkdir()
            subprocess.run(["ditto", str(mounted_app), str(installed_app)], check=True)
            verify_inventory(installed_app, inventory(mounted_app))
            subprocess.run(["codesign", "--verify", "--deep", "--strict", str(installed_app)], check=True)
