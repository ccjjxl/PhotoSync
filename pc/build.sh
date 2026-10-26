#!/usr/bin/env bash
# Cross-compile the desktop client for Linux and Windows (amd64 + arm64).
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-1.0.0}"
OUT="${OUT:-dist}"
mkdir -p "$OUT"

targets=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

for t in "${targets[@]}"; do
  GOOS="${t%%/*}"; GOARCH="${t##*/}"
  name="sync-client"
  ext=""
  if [ "$GOOS" = "windows" ]; then ext=".exe"; fi
  echo "→ ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/${name}-${GOOS}-${GOARCH}${ext}" .
done

echo
echo "构建完成，输出在 ${OUT}/"
ls -lh "$OUT"
