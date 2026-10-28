"""Shared release checks and user-file preservation for both desktop platforms."""
from pathlib import Path
import hashlib
import argparse
import time
import json
import re
import shutil
import subprocess
import sys

NATIVE_SCENARIOS = 25
SOURCE_DIRS = {"frontend", "internal", "cmd", "tests", "scripts", "build", "examples", ".github"}
ROOT_FILES = {"README.md", "CONTRIBUTING.md", "THIRD-PARTY.txt", "AGENTS.md", "go.mod", "go.sum", "package.json", "package-lock.json", "wails.json", "build-macos.sh", "build.ps1", ".gitignore", ".gitattributes", ".editorconfig", ".prettierrc.json", ".prettierignore"}
EXCLUDED_DIRS = {".git", "node_modules", "work", "outputs", "verification", "references", "__pycache__", "wailsjs", ".venv"}


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def source_files(base):
    base = Path(base)
    files = []
    for path in base.iterdir():
        if path.is_file() and (path.name in ROOT_FILES or path.suffix == ".go"):
            files.append(path)
        elif path.is_dir() and path.name in SOURCE_DIRS:
            for child in path.rglob("*"):
                rel = child.relative_to(base)
                if not child.is_file() or child.is_symlink():
                    continue
                if any(part in EXCLUDED_DIRS for part in rel.parts) or rel.parts[:2] == ("build", "bin"):
                    continue
                if child.name == ".DS_Store" or child.suffix in {".pyc", ".syso", ".exe"}:
                    continue
                files.append(child)
    return sorted(files)


def source_hashes(base):
    base = Path(base)
    return {p.relative_to(base).as_posix(): digest(p) for p in source_files(base)}


def frontend_files(base):
    return sorted(p for p in (Path(base) / "frontend").iterdir()
                  if p.is_file() and p.suffix in {".html", ".css", ".js", ".svg"})


def verify_frontend(base, binary):
    data = Path(binary).read_bytes()
    for asset in frontend_files(base):
        if asset.read_bytes() not in data:
            raise ValueError(f"Executable has stale frontend: {asset.name}")


def license_sections(base):
    text = (Path(base) / "THIRD-PARTY.txt").read_text(encoding="utf-8")
    return {name: body.strip() for name, body in
            re.findall(r"^=== Module: (.+?) ===\n(.*?)(?=^=== Module: |\Z)", text, re.M | re.S)}


def verify_licenses(base, modules):
    sections = license_sections(base)
    for module, version in modules:
        if not sections.get(module + " " + version):
            raise ValueError(f"Linked dependency has no license body: {module} {version}")


def verify_binary_licenses(base, binary):
    metadata = subprocess.check_output(["go", "version", "-m", str(binary)], text=True)
    modules = re.findall(r"^\s+dep\s+(\S+)\s+(\S+)", metadata, re.M)
    if not modules:
        raise ValueError(f"Cannot inventory binary dependencies: {binary}")
    verify_licenses(base, modules)


def read_log(path):
    data = Path(path).read_bytes()
    return data.decode("utf-16" if data[:2] in (b"\xff\xfe", b"\xfe\xff") else "utf-8-sig")


def check_logs(base, binary, platform):
    base = Path(base)
    names = ["brand-go-tests.txt", "brand-ui-tests.txt", "brand-window-smoke.txt", "vet.txt", "packaging-tests.txt", "quality-checks.txt"]
    if platform == "macos":
        names += ["macos-race-tests.txt", "macos-serial-tests.txt"]
    logs = {name: read_log(base / "verification" / name) for name in names}
    go, ui, native = (logs[n] for n in names[:3])
    go_count = len(re.findall(r"^--- PASS: Test", go, re.M))
    ui_match = re.search(r"\bpass (\d+)\b", ui)
    if not go_count or "FAIL" in go or "gytest-go/internal/core" not in go:
        raise ValueError("Go checks are missing or failed")
    if not ui_match or not int(ui_match[1]) or not re.search(r"\bfail 0\b", ui):
        raise ValueError("Frontend checks are missing or failed")
    if native.count("native process and frontend bridge ready") != NATIVE_SCENARIOS or native.count("visible layout verified") != NATIVE_SCENARIOS or "--- PASS: TestNativeWindowSmoke" not in native:
        raise ValueError("Complete native window/layout checks are required")
    if "verified_binary_sha256=" + digest(binary) not in native:
        raise ValueError("Native checks used a different executable")
    if "OK" not in logs["packaging-tests.txt"] or "FAILED" in logs["packaging-tests.txt"]:
        raise ValueError("Packaging regression checks are missing or failed")
    if "All matched files use Prettier code style" not in logs["quality-checks.txt"] or "[error]" in logs["quality-checks.txt"]:
        raise ValueError("Source quality checks are missing or failed")
    if logs["vet.txt"].strip():
        raise ValueError("go vet reported problems")
    if platform == "macos":
        if "gytest-go/internal/core" not in logs["macos-race-tests.txt"] or "FAIL" in logs["macos-race-tests.txt"] or "DATA RACE" in logs["macos-race-tests.txt"]:
            raise ValueError("Race checks are missing or failed")
        if "--- PASS: TestSerialTransportLoopback" not in logs["macos-serial-tests.txt"] or "FAIL" in logs["macos-serial-tests.txt"]:
            raise ValueError("Serial PTY checks are missing or failed")
    return {"goTests": go_count, "uiTests": int(ui_match[1]), "nativeLayouts": NATIVE_SCENARIOS,
            "raceVerified": platform == "macos", "serialPTYVerified": platform == "macos"}, names


def verify_current_build(base, binary, platform):
    base = Path(base)
    proof = json.loads((base / "verification/build-evidence.json").read_text(encoding="utf-8"))
    if proof["platform"] != platform or proof["binarySHA256"] != digest(binary):
        raise ValueError("Build evidence does not identify this executable/platform")
    if proof["sourceSHA256"] != source_hashes(base):
        raise ValueError("Source changed since the verified build; rebuild before packaging")
    checks, names = check_logs(base, binary, platform)
    if proof["logSHA256"] != {name: digest(base / "verification" / name) for name in names}:
        raise ValueError("Verification logs changed after the build was recorded")
    verify_frontend(base, binary)
    return proof, checks


def copy_user_payload(base, stage, proof, checks):
    base, stage = Path(base), Path(stage)
    for name in ["README.md", "THIRD-PARTY.txt"]:
        shutil.copy2(base / name, stage / name)
    sections = license_sections(base)
    if not sections.get("Go runtime") or any(not body for body in sections.values()):
        raise ValueError("Third-party notice has a missing license body")
    # Only small, maintained examples belong in the ordinary application package.
    shutil.copytree(base / "examples", stage / "examples")
    summary = {"product": "protocol-simulator", "platform": proof["platform"],
               "binarySHA256": proof["binarySHA256"], "sourceFingerprint": proof["sourceFingerprint"],
               "checks": checks, "windowsNativeRuntimeVerified": proof.get("nativeRuntimeVerified", proof["platform"] == "windows"),
               "additionalBinaries": proof.get("additionalBinaries", {})}
    (stage / "verification").mkdir()
    (stage / "verification/build.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")


def preserve_user_files(previous, stage):
    previous, stage = Path(previous), Path(stage)
    if not previous.exists():
        return []
    manifest_path = previous / "file-manifest.json"
    if not manifest_path.is_file():
        raise ValueError(f"Cannot safely inventory existing delivery: {previous}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8-sig"))
    if isinstance(manifest, list):
        manifest = {row["path"]: row for row in manifest}
    preserved = []
    for path in previous.rglob("*"):
        if not path.is_file() or path == manifest_path:
            continue
        rel = path.relative_to(previous)
        known = manifest.get(rel.as_posix())
        if rel.parts[0] != "保留文件" and known and digest(path) == known["sha256"]:
            continue
        destination = rel if rel.parts[0] == "保留文件" else Path("保留文件") / rel
        target = stage / destination
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)
        preserved.append(destination.as_posix())
    return preserved


def remove_legacy_delivery(out):
    """Remove retired generated files while leaving user edits at their paths."""
    out = Path(out).resolve()
    archive = out / "protocol-simulator.zip"
    if archive.is_symlink():
        raise ValueError("Refusing to remove linked legacy archive")
    if archive.is_file():
        archive.unlink()
    release = out / "protocol-simulator"
    manifest = release / "file-manifest.json"
    if not release.exists():
        return []
    if release.is_symlink():
        raise ValueError("Refusing to clean a linked legacy delivery")
    if sys.platform == "darwin":
        commands = subprocess.check_output(["ps", "-axo", "comm="], text=True).splitlines()
        if any(command.strip().startswith(str(release) + "/") for command in commands):
            return [str(release)]
    if not manifest.is_file():
        return [str(release)]
    entries = json.loads(manifest.read_text(encoding="utf-8-sig"))
    if isinstance(entries, list):
        entries = {entry["path"]: entry for entry in entries}
    # Validate every path before deleting anything in this directory.
    for key in entries:
        path = release / key
        if not path.resolve().is_relative_to(release.resolve()):
            raise ValueError("Unsafe legacy delivery manifest path: " + key)
    for key, entry in entries.items():
        path = release / key
        if path.is_file() and not path.is_symlink() and digest(path) == entry["sha256"]:
            try:
                path.unlink()
            except PermissionError:
                pass  # A running Windows image remains in place until closed.
    for path in sorted(release.rglob("*"), key=lambda p: len(p.parts), reverse=True):
        if path.is_dir() and not path.is_symlink():
            try:
                path.rmdir()
            except OSError:
                pass  # Unknown user files keep their directories and paths.
    remaining = [p for p in release.rglob("*") if p != manifest]
    if not remaining:
        manifest.unlink()
        release.rmdir()
    return [str(p) for p in remaining if p.is_file() or p.is_symlink()]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["begin", "record"])
    parser.add_argument("--platform", required=True, choices=["macos", "windows"])
    args = parser.parse_args()
    base = Path(__file__).resolve().parents[1]
    directory = base / "verification"
    directory.mkdir(exist_ok=True)
    marker = directory / "build-start.json"
    if args.action == "begin":
        marker.write_text(json.dumps({"platform": args.platform, "started": time.time(), "sourceSHA256": source_hashes(base)}), encoding="utf-8")
        return
    start = json.loads(marker.read_text(encoding="utf-8"))
    if start["platform"] != args.platform or start["sourceSHA256"] != source_hashes(base):
        raise ValueError("Source/platform changed during the build")
    binary = base / "build/bin" / ("protocol-simulator.app/Contents/MacOS/protocol-simulator" if args.platform == "macos" else "protocol-simulator.exe")
    checks, names = check_logs(base, binary, args.platform)
    for name in names:
        if (directory / name).stat().st_mtime < start["started"] - 1:
            raise ValueError(f"Verification log predates this build: {name}")
    verify_frontend(base, binary)
    verify_binary_licenses(base, binary)
    sources = start["sourceSHA256"]
    proof = {"platform": args.platform, "binarySHA256": digest(binary), "sourceSHA256": sources,
             "sourceFingerprint": hashlib.sha256(json.dumps(sources, sort_keys=True).encode()).hexdigest(),
             "checks": checks, "logSHA256": {name: digest(directory / name) for name in names}}
    cross = directory / "windows-cross-build.json"
    if args.platform == "macos":
        report = json.loads(cross.read_text(encoding="utf-8"))
        windows = base / "build/bin/protocol-simulator.exe"
        verify_binary_licenses(base, windows)
        if report["binary_sha256"] != digest(windows):
            raise ValueError("Windows cross-build report identifies a different binary")
        proof["additionalBinaries"] = {"windows": {"sha256": digest(windows), "nativeRuntimeVerified": False}}
    (directory / "build-evidence.json").write_text(json.dumps(proof, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"passed": True, "platform": args.platform, **checks}))


if __name__ == "__main__":
    main()
