#!/usr/bin/env bash
# 把本地签名密钥上传为 GitHub Actions Secrets，让 Release 产出正式签名 APK。
#
#   ./scripts/setup-ci-secrets.sh            # 读取 android/keystore.properties
#   ./scripts/setup-ci-secrets.sh --dry-run  # 只打印将要执行的命令
#
# 需要：已安装 gh 并完成 `gh auth login`，且 token 具备 repo 权限。
set -euo pipefail

cd "$(dirname "$0")/.."

PROPS="keystore.properties"
KEYSTORE="keystore/photosync-release.jks"
DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

if [ ! -f "$PROPS" ]; then
  echo "✗ 未找到 $PROPS，请先运行 ./scripts/gen-keystore.sh" >&2
  exit 1
fi

read_prop() { grep -E "^$1=" "$PROPS" | head -1 | cut -d'=' -f2-; }

STORE_FILE=$(read_prop storeFile)
STORE_PASS=$(read_prop storePassword)
KEY_ALIAS=$(read_prop keyAlias)
KEY_PASS=$(read_prop keyPassword)

[ -n "$STORE_FILE" ] && KEYSTORE="$STORE_FILE"

if [ ! -f "$KEYSTORE" ]; then
  echo "✗ 未找到密钥库 $KEYSTORE" >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "✗ 未安装 gh CLI: https://cli.github.com" >&2
  exit 1
fi

B64=$(base64 -w0 "$KEYSTORE" 2>/dev/null || base64 -i "$KEYSTORE")

run() {
  if [ "$DRY" -eq 1 ]; then
    echo "  [dry-run] gh $*"
  else
    gh "$@"
  fi
}

echo "→ 上传 4 个 secret 到 $(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || echo '<repo>')"

if [ "$DRY" -eq 0 ]; then
  printf '%s' "$B64" | gh secret set ANDROID_KEYSTORE_BASE64
  printf '%s' "$STORE_PASS" | gh secret set ANDROID_STORE_PASSWORD
  printf '%s' "$KEY_ALIAS" | gh secret set ANDROID_KEY_ALIAS
  printf '%s' "$KEY_PASS" | gh secret set ANDROID_KEY_PASSWORD
else
  run secret set ANDROID_KEYSTORE_BASE64
  run secret set ANDROID_STORE_PASSWORD
  run secret set ANDROID_KEY_ALIAS
  run secret set ANDROID_KEY_PASSWORD
fi

echo "✓ 完成。下次打 tag 时会产出正式签名的 APK。"
echo "  验证: gh secret list"
