#!/bin/bash
# Builds "Outline Control.app": universal binary, generated icon, ad-hoc signed.
# No Xcode required — the SwiftUI-free sources compile with the command line
# tools alone.
set -euo pipefail

cd "$(dirname "$0")"

NAME="Outline Control"
BUNDLE="build/$NAME.app"
MODCACHE="${TMPDIR:-/tmp}/outline-control-swiftcache"
mkdir -p "$MODCACHE" build

echo "==> building provider"
( cd ../provider && GOCACHE="${GOCACHE:-/tmp/oc-gocache}" GOMODCACHE="${GOMODCACHE:-/tmp/oc-modcache}" \
  go build -trimpath -ldflags "-s -w" -o ../app/build/outline-controld . )

echo "==> building app binary"
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos13.0" \
    -module-cache-path "$MODCACHE" \
    -o "build/OutlineControl-$arch" \
    Sources/main.swift Sources/API.swift Sources/Views.swift Sources/Legal.swift Sources/Drag.swift
done
lipo -create -output build/OutlineControl build/OutlineControl-arm64 build/OutlineControl-x86_64 >/dev/null
rm -f build/OutlineControl-arm64 build/OutlineControl-x86_64

echo "==> drawing icon"
swiftc -O -target "$(uname -m)-apple-macos13.0" -module-cache-path "$MODCACHE" \
  -o build/icongen Sources/IconGen.swift
rm -rf build/AppIcon.iconset
./build/icongen build/AppIcon.iconset
iconutil -c icns build/AppIcon.iconset -o build/AppIcon.icns

echo "==> assembling bundle"
rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp Info.plist "$BUNDLE/Contents/Info.plist"
cp build/OutlineControl "$BUNDLE/Contents/MacOS/OutlineControl"
cp build/AppIcon.icns "$BUNDLE/Contents/Resources/AppIcon.icns"
cp build/outline-controld "$BUNDLE/Contents/MacOS/outline-controld"

# Legal notice travels with the binary, not just with the repository.
cp Resources/FIRST-RUN-NOTICE.txt "$BUNDLE/Contents/Resources/FIRST-RUN-NOTICE.txt"
cp ../DISCLAIMER.md "$BUNDLE/Contents/Resources/DISCLAIMER.md"
cp ../LICENSE "$BUNDLE/Contents/Resources/LICENSE"
printf 'APPL????' > "$BUNDLE/Contents/PkgInfo"

codesign --force --deep --sign - "$BUNDLE" >/dev/null 2>&1 || true

echo "built: $BUNDLE"
