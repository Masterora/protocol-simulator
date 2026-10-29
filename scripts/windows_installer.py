"""Build a real NSIS installer and verify its embedded Windows payload."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import urllib.parse
import urllib.request

from delivery import digest

BOOTSTRAPPER_URL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
SETUP_NAME = "protocol-simulator-Setup.exe"


def tool_path(variable, names, windows_relative):
    configured = os.environ.get(variable)
    if configured:
        return configured
    for name in names:
        found = shutil.which(name)
        if found:
            return found
    for key in ("ProgramFiles(x86)", "ProgramFiles"):
        candidate = Path(os.environ.get(key, "C:/Program Files")) / windows_relative
        if candidate.is_file():
            return str(candidate)
    raise RuntimeError(f"Install {names[0]} or set {variable} before packaging Windows Setup")


def installer_files(payload):
    payload = Path(payload)
    required = [payload / name for name in ("protocol-simulator.exe", "README.md", "THIRD-PARTY.txt")]
    for path in required:
        if not path.is_file() or path.is_symlink():
            raise ValueError("Missing installer payload: " + str(path))
    files = required + sorted(p for name in ("examples", "verification")
                              for p in (payload / name).rglob("*") if p.is_file())
    for path in files:
        if path.is_symlink() or not path.resolve().is_relative_to(payload.resolve()):
            raise ValueError("Installer payload escapes staging: " + str(path))
    return files


def nsis_string(value):
    # Paths can contain spaces and Unicode; do not allow preprocessor interpolation.
    if any(ord(c) < 32 for c in str(value)):
        raise ValueError("Control character in installer path")
    return str(value).replace("$", "$$").replace('"', '$\\"')


def pe_resources(data):
    pe = struct.unpack_from("<I", data, 60)[0]
    optional = pe + 24
    directory = optional + (112 if struct.unpack_from("<H", data, optional)[0] == 0x20b else 96)
    resource_rva = struct.unpack_from("<I", data, directory + 16)[0]
    section_count = struct.unpack_from("<H", data, pe + 6)[0]
    section_table = optional + struct.unpack_from("<H", data, pe + 20)[0]
    sections = [struct.unpack_from("<IIII", data, section_table + i * 40 + 8) for i in range(section_count)]

    def offset(rva):
        for virtual_size, address, raw_size, raw in sections:
            if address <= rva < address + max(virtual_size, raw_size):
                return raw + rva - address
        raise ValueError("Invalid PE resource RVA")

    root = offset(resource_rva)
    resources = {}

    def walk(relative, path):
        start = root + relative
        named, numbered = struct.unpack_from("<HH", data, start + 12)
        for i in range(named + numbered):
            key, value = struct.unpack_from("<II", data, start + 16 + i * 8)
            if key & 0x80000000:
                continue
            if value & 0x80000000:
                walk(value & 0x7fffffff, path + (key,))
            else:
                rva, size = struct.unpack_from("<II", data, root + value)
                resources[path + (key,)] = data[offset(rva):offset(rva) + size]
    walk(0, ())
    return resources


def verify_setup_resources(data, icon, version):
    resources = pe_resources(data)
    actual_icons = {digest_bytes(value) for key, value in resources.items() if key[0] == 3}
    count = struct.unpack_from("<H", icon, 4)[0]
    for i in range(count):
        size, start = struct.unpack_from("<II", icon, 6 + i * 16 + 8)
        if digest_bytes(icon[start:start + size]) not in actual_icons:
            raise ValueError("Setup icon differs from canonical application icon")
    version_resources = b"".join(value for key, value in resources.items() if key[0] == 16)
    for text in ("protocol-simulator Setup", SETUP_NAME, version):
        if text.encode("utf-16-le") not in version_resources:
            raise ValueError("Setup version metadata mismatch: " + text)


def digest_bytes(data):
    return hashlib.sha256(data).hexdigest()


def windows_resource_spec(base):
    base = Path(base)
    info = json.loads((base / "wails.json").read_text(encoding="utf-8"))["info"]
    version = info["productVersion"]
    if not re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", version):
        raise ValueError("Windows version requires three numeric components")
    parts = tuple(map(int, version.split(".")))
    if any(n > 65535 for n in parts):
        raise ValueError("Windows version components must not exceed 65535")
    template = (base / "build/windows/app.manifest").read_bytes()
    if template.count(b"{{.Version}}") != 1:
        raise ValueError("Application manifest requires one version placeholder")
    manifest = template.replace(b"{{.Version}}", (version + ".0").encode())
    fields = {"FileDescription": info["productName"], "ProductName": info["productName"],
              "InternalName": "protocol-simulator", "OriginalFilename": "protocol-simulator.exe",
              "FileVersion": version + ".0", "ProductVersion": version + ".0"}
    return parts, fields, manifest


def verify_application_resources(base, binary):
    base = Path(base)
    data = Path(binary).read_bytes()
    pe = struct.unpack_from("<I", data, 60)[0]
    if data[pe:pe + 4] != b"PE\0\0" or struct.unpack_from("<H", data, pe + 4)[0] != 0x8664:
        raise ValueError("Application is not a Windows x64 executable")
    resources = pe_resources(data)
    icon = (base / "build/windows/icon.ico").read_bytes()
    count = struct.unpack_from("<H", icon, 4)[0]
    for group_id in (1, 3):
        group = resources.get((14, group_id, 1033), b"")
        if len(group) < 6 or struct.unpack_from("<H", group, 4)[0] != count:
            raise ValueError("Missing application/native window icon group")
        for i in range(count):
            size, start = struct.unpack_from("<II", icon, 6 + i * 16 + 8)
            image_id = struct.unpack_from("<H", group, 6 + i * 14 + 12)[0]
            if resources.get((3, image_id, 1033)) != icon[start:start + size]:
                raise ValueError("Application icon resource mismatch")
    parts, fields, manifest = windows_resource_spec(base)
    if resources.get((24, 1, 1033)) != manifest:
        raise ValueError("Application manifest resource mismatch")
    version_data = resources.get((16, 1, 1033), b"")
    fixed = version_data.find(struct.pack("<I", 0xFEEF04BD))
    numeric = (parts[0] << 16 | parts[1], parts[2] << 16) * 2
    if fixed < 0 or struct.unpack_from("<4I", version_data, fixed + 8) != numeric:
        raise ValueError("Application fixed version resource mismatch")
    for key, value in fields.items():
        name = (key + "\0").encode("utf-16-le")
        position = version_data.find(name)
        start = (position + len(name) + 3) & ~3
        expected = (value + "\0").encode("utf-16-le")
        if position < 0 or version_data[start:start + len(expected)] != expected:
            raise ValueError("Application version resource mismatch: " + key)


def generate_file_lists(payload, directory):
    payload, directory = Path(payload), Path(directory)
    install, uninstall, folders = [], [], set()
    for path in installer_files(payload):
        relative = path.relative_to(payload)
        parent = relative.parent.as_posix().replace("/", "\\")
        target = relative.as_posix().replace("/", "\\")
        example = relative.parts[0] == "examples"
        install += ["SetOverwrite off" if example else "SetOverwrite on",
                    'SetOutPath "$INSTDIR' + ("\\" + nsis_string(parent) if parent != "." else "") + '"',
                    'File "' + nsis_string(path.resolve()) + '"']
        # Examples are editable JSON projects: do not overwrite them on upgrade
        # or delete them on uninstall. Unknown user files are never enumerated.
        if not example:
            uninstall.append('Delete "$INSTDIR\\' + nsis_string(target) + '"')
        folders.update(p for p in relative.parents if p != Path("."))
    for folder in sorted(folders, key=lambda p: len(p.parts), reverse=True):
        uninstall.append('RMDir "$INSTDIR\\' + nsis_string(str(folder).replace("/", "\\")) + '"')
    (directory / "install-files.nsh").write_text("\n".join(install) + "\n", encoding="utf-8-sig")
    (directory / "uninstall-files.nsh").write_text("\n".join(uninstall) + "\n", encoding="utf-8-sig")


def webview_bootstrapper(base):
    cached = Path(base) / "work/installer-runtime/MicrosoftEdgeWebview2Setup.exe"
    metadata = cached.with_suffix(".json")
    cached.parent.mkdir(parents=True, exist_ok=True)
    if not cached.exists() or not metadata.exists():
        with urllib.request.urlopen(BOOTSTRAPPER_URL, timeout=90) as response:
            host = urllib.parse.urlparse(response.url).hostname or ""
            if not (host.endswith(".microsoft.com") or host.endswith(".msedge.net")):
                raise ValueError("WebView2 bootstrapper redirected outside Microsoft")
            data = response.read(8 * 1024 * 1024 + 1)
        if len(data) > 8 * 1024 * 1024 or not data.startswith(b"MZ"):
            raise ValueError("Invalid WebView2 bootstrapper download")
        cached.with_suffix(".download").write_bytes(data)
        os.replace(cached.with_suffix(".download"), cached)
        metadata.write_text(json.dumps({"source": BOOTSTRAPPER_URL, "sha256": digest(cached)}), encoding="utf-8")
    cached_info = json.loads(metadata.read_text(encoding="utf-8"))
    if cached_info["source"] != BOOTSTRAPPER_URL or cached_info["sha256"] != digest(cached):
        raise ValueError("Cached WebView2 bootstrapper integrity mismatch; remove the cache and rebuild")
    if not cached.read_bytes().startswith(b"MZ"):
        raise ValueError("Invalid cached WebView2 bootstrapper")
    if sys.platform == "win32":
        powershell = shutil.which("pwsh") or shutil.which("powershell")
        if not powershell:
            raise RuntimeError("PowerShell is required to verify the Microsoft bootstrapper signature")
        subprocess.run([powershell, "-NoProfile", "-NonInteractive", "-File",
                        str(Path(base) / "build/windows/verify-bootstrapper.ps1"), "-Path", str(cached)], check=True)
    return cached


def build_installer(base, payload, output):
    base, payload, output = Path(base).resolve(), Path(payload).resolve(), Path(output).resolve()
    verify_application_resources(base, payload / "protocol-simulator.exe")
    version = json.loads((base / "wails.json").read_text(encoding="utf-8"))["info"]["productVersion"]
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("Windows installer requires a three-component numeric version")
    makensis = tool_path("MAKENSIS", ("makensis", "makensis.exe"), "NSIS/makensis.exe")
    sevenzip = tool_path("SEVENZIP", ("7zz", "7z", "7z.exe"), "7-Zip/7z.exe")
    bootstrapper = webview_bootstrapper(base)
    work = base / "work"
    work.mkdir(exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="windows-setup-", dir=work) as directory:
        directory = Path(directory)
        generate_file_lists(payload, directory)
        definitions = {"SETUP_OUTPUT": output, "APP_VERSION": version,
                       "APP_ICON": base / "build/windows/icon.ico", "WEBVIEW_BOOTSTRAPPER": bootstrapper,
                       "INSTALL_FILES": directory / "install-files.nsh", "UNINSTALL_FILES": directory / "uninstall-files.nsh"}
        prefix = "/" if sys.platform == "win32" else "-"
        subprocess.run([makensis, prefix + "V3", prefix + "WX"] +
                       [prefix + "D" + key + "=" + str(value) for key, value in definitions.items()] +
                       [str(base / "build/windows/installer.nsi")], check=True)
        data = output.read_bytes()
        if data[:2] != b"MZ" or data[struct.unpack_from("<I", data, 60)[0]:][:4] != b"PE\0\0":
            raise ValueError("Installer is not a Windows PE executable")
        verify_setup_resources(data, (base / "build/windows/icon.ico").read_bytes(), version)
        extracted = directory / "extracted"
        subprocess.run([sevenzip, "x", "-y", "-o" + str(extracted), str(output)], check=True, stdout=subprocess.DEVNULL)
        for path in installer_files(payload):
            relative = path.relative_to(payload)
            candidates = [p for p in extracted.rglob(path.name) if p.is_file() and digest(p) == digest(path)]
            if not candidates:
                raise ValueError("Installer is missing or changed payload: " + relative.as_posix())
        bootstrap = list(extracted.rglob("MicrosoftEdgeWebview2Setup.exe"))
        if len(bootstrap) != 1 or digest(bootstrap[0]) != digest(bootstrapper):
            raise ValueError("Installer WebView2 bootstrapper mismatch")
        if any(p.name.endswith(".app") for p in extracted.rglob("*")):
            raise ValueError("Mac application leaked into Windows Setup")
    report = {"product": "protocol-simulator", "format": "NSIS Unicode EXE", "version": version,
              "installerSHA256": digest(output), "applicationSHA256": digest(payload / "protocol-simulator.exe"),
              "payloadExtractionVerified": True, "perUserInstall": True,
              "projectPreservationPolicy": "No recursive uninstall; examples are not overwritten or removed",
              "iconAndVersionResourcesVerified": True,
              "webView2BootstrapperURL": BOOTSTRAPPER_URL, "webView2BootstrapperSHA256": digest(bootstrapper),
              "windowsNativeInstallVerified": False, "signature": "unsigned"}
    (base / "verification/windows-installer.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--payload", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(build_installer(Path(__file__).resolve().parents[1], args.payload, args.output)))
