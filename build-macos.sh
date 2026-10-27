#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'macOS and Xcode Command Line Tools are required.' >&2
  exit 1
fi
# Use an explicit SDKROOT, or the SDK belonging to the selected developer tools.
if [[ -z "${SDKROOT:-}" ]]; then
  developer_dir="$(xcode-select -p)"
  if [[ -n "${DEVELOPER_DIR:-}" ]]; then developer_dir="$DEVELOPER_DIR"; fi
  if [[ -d "$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk" ]]; then
    export SDKROOT="$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk"
  else
    export SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"
  fi
fi
mkdir -p verification work
# Keep generated staging files outside the root Go module's package traversal.
printf 'module protocol-simulator/work\n\ngo 1.25\n' > work/go.mod
python3 scripts/delivery.py begin --platform macos
npm ci
npm run check > verification/quality-checks.txt 2>&1
python3 -m unittest discover -s tests -p 'test_*.py' -v > verification/packaging-tests.txt 2>&1
go test ./... -count=1 -v -timeout=90s > verification/brand-go-tests.txt 2>&1
go vet ./... > verification/vet.txt 2>&1
go test -race ./... -count=1 -timeout=120s > verification/macos-race-tests.txt 2>&1
python3 scripts/verify_serial_macos.py > verification/macos-serial-tests.txt 2>&1
node --test tests/ui.test.mjs > verification/brand-ui-tests.txt 2>&1
python3 scripts/build_windows_cross.py > verification/windows-cross-build.txt 2>&1
# Use the exact framework version declared by this repository. A matching local
# CLI avoids network-only module metadata checks on an otherwise cached build.
wails_cli="$(go env GOPATH)/bin/wails"
if [[ -x "$wails_cli" ]] && [[ "$(go version -m "$wails_cli" | awk '$1 == "mod" && $2 == "github.com/wailsapp/wails/v2" {print $3}')" == v2.17.0 ]]; then
  wails_command=("$wails_cli")
else
  wails_command=(go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0)
fi
"${wails_command[@]}" build \
  -platform darwin/universal -s -m -nosyncgomod -skipbindings
app="$PWD/build/bin/protocol-simulator.app"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
GYLAB_NATIVE_SMOKE_BINARY="$app/Contents/MacOS/protocol-simulator" \
  go test . -run '^TestNativeWindowSmoke$' -count=1 -v -timeout=180s \
  > verification/brand-window-smoke.txt 2>&1
python3 scripts/delivery.py record --platform macos
python3 scripts/package_protocol.py
