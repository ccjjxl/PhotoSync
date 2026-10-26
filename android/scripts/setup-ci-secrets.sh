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

REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || echo "")

# ---------------------------------------------------------------------------
# Pre-flight: writing Actions secrets needs an explicit permission that many
# fine-grained PATs lack. Detect it up front instead of failing on a 403 midway.
# ---------------------------------------------------------------------------
if [ "$DRY" -eq 0 ] && [ -n "$REPO" ]; then
  if ! gh api "repos/$REPO/actions/secrets/public-key" >/dev/null 2>&1; then
    cat >&2 <<MSG
✗ 当前 GitHub token 没有 Secrets 权限，无法写入 Actions Secrets。

细粒度 PAT（github_pat_…）需要显式勾选：
  仓库 Settings → 你正在使用的 token → Repository permissions
    → Secrets : Read and write

或者改用带有 repo scope 的经典 token / 浏览器登录：
  gh auth login --web          # 重新授权（推荐，最省事）
  gh auth refresh -s repo      # 或给现有登录补权限

若不想改动 token，也可以在网页手动添加这 4 个 secret：
  https://github.com/$REPO/settings/secrets/actions

    ANDROID_KEYSTORE_BASE64 = <base64 编码的 jks，见下方命令>
    ANDROID_STORE_PASSWORD  = <keystore.properties 里的 storePassword>
    ANDROID_KEY_ALIAS       = $KEY_ALIAS
    ANDROID_KEY_PASSWORD    = <keystore.properties 里的 keyPassword>

生成 base64：base64 -w0 $KEYSTORE
MSG
    exit 1
  fi
fi

B64=$(base64 -w0 "$KEYSTORE" 2>/dev/null || base64 -i "$KEYSTORE")
B64_FILE=$(mktemp -t photosync-keystore-XXXXXX.b64)
printf '%s' "$B64" > "$B64_FILE"
chmod 600 "$B64_FILE"

run() {
  if [ "$DRY" -eq 1 ]; then
    echo "  [dry-run] gh $*"
  else
    gh "$@"
  fi
}

echo "→ 上传 4 个 secret 到 ${REPO:-<repo>}"

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
echo "  base64 副本: $B64_FILE（已写入，确认后请删除）"
