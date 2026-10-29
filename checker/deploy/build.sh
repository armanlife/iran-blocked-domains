#!/bin/bash
# Builds ready-to-copy packages in dist/:
#   dist/domain-checker-linux-amd64.tar.gz
#   dist/domain-checker-linux-arm64.tar.gz
#   dist/domain-checker-windows-amd64.exe
# Needs Go. Run it from anywhere.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIST=$ROOT/dist
rm -rf "$DIST"

for arch in amd64 arm64; do
    pkg=$DIST/domain-checker
    mkdir -p "$pkg"
    (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w" -o "$pkg/checker" ./cmd/checker)
    cp "$ROOT/deploy/service.sh" "$ROOT/deploy/README.md" "$pkg/"
    chmod +x "$pkg/checker" "$pkg/service.sh"
    # Owned by root, not by the user who builds
    tar --owner=0 --group=0 -czf "$DIST/domain-checker-linux-$arch.tar.gz" -C "$DIST" domain-checker
    rm -rf "$pkg"
    echo "built $DIST/domain-checker-linux-$arch.tar.gz"
done

(cd "$ROOT" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$DIST/domain-checker-windows-amd64.exe" ./cmd/checker)
echo "built $DIST/domain-checker-windows-amd64.exe"
