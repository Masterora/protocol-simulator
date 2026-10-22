#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'macOS and Xcode Command Line Tools are required.' >&2
  exit 1
fi
# Select the SDK shipped with the active Xcode, rather than a newer, incompatible
# CommandLineTools SDK that xcrun may discover on upgraded development machines.
developer_dir="$(xcode-select -p)"
if [[ -d "$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk" ]]; then
  export SDKROOT="$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk"
else
  export SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"
fi
mkdir -p verification work
# Keep generated staging files outside the root Go module's package traversal.
printf 'module protocol-simulator/work\n\ngo 1.25\n' > work/go.mod
npm ci
go test ./... -count=1 -v -timeout=90s > verification/brand-go-tests.txt 2>&1
go vet ./...
go test -race ./... -count=1 -timeout=120s > verification/macos-race-tests.txt 2>&1
python3 scripts/verify_serial_macos.py > verification/macos-serial-tests.txt 2>&1
node --test tests/ui.test.mjs > verification/brand-ui-tests.txt 2>&1
python3 scripts/build_windows_cross.py > verification/windows-cross-build.txt 2>&1
# Use the exact framework version declared by this repository.
go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0 build \
  -platform darwin/universal -s -m -nosyncgomod -skipbindings
app="$PWD/build/bin/protocol-simulator.app"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
GYLAB_NATIVE_SMOKE_BINARY="$app/Contents/MacOS/protocol-simulator" \
  go test . -run '^TestNativeWindowSmoke$' -count=1 -v -timeout=180s \
  > verification/brand-window-smoke.txt 2>&1
python3 scripts/package_protocol.py
