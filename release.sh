#!/usr/bin/env bash
# 发布一个新版本：更新 VERSION / CHANGELOG，提交并打 tag。
#
#   ./release.sh 1.3.0           发布 1.3.0
#   ./release.sh 1.3.0 --push    发布并推送到 origin（触发 GitHub Actions）
#   ./release.sh patch --push    基于当前版本递增补丁号
#
# 推送 tag 后，.github/workflows/release.yml 会自动：
#   1. 交叉编译 6 个平台的电脑客户端
#   2. 构建签名 APK
#   3. 在 macOS 上构建 iOS 应用
#   4. 汇总产物并创建 GitHub Release（附带 SHA256SUMS）
set -euo pipefail

cd "$(dirname "$0")"

PUSH=0
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --push) PUSH=1 ;;
    -h|--help) sed -n '1,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) ARGS+=("$arg") ;;
  esac
done

if [ ${#ARGS[@]} -eq 0 ]; then
  echo "用法: ./release.sh <版本号|major|minor|patch> [--push]" >&2
  exit 2
fi

TARGET="${ARGS[0]}"
CURRENT=$(tr -d '[:space:]' < VERSION)
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT"

case "$TARGET" in
  major) NEW="$((MAJOR+1)).0.0" ;;
  minor) NEW="$MAJOR.$((MINOR+1)).0" ;;
  patch) NEW="$MAJOR.$MINOR.$((PATCH+1))" ;;
  *)
    if ! [[ "$TARGET" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "✗ 版本号格式应为 X.Y.Z，或 major/minor/patch" >&2
      exit 2
    fi
    NEW="$TARGET"
    ;;
esac

if [ "$NEW" = "$CURRENT" ]; then
  echo "✗ 版本已经是 $NEW" >&2
  exit 1
fi

# ---------------------------------------------------------------- sanity checks
if [ -n "$(git status --porcelain)" ]; then
  echo "✗ 工作区有未提交的改动，请先提交或暂存" >&2
  git status --short
  exit 1
fi

if ! grep -q "^## $NEW " CHANGELOG.md; then
  echo "✗ CHANGELOG.md 缺少 '## $NEW' 段落，请先补充本版本的更新说明" >&2
  echo "  当前文件中的版本：" >&2
  grep -E '^## ' CHANGELOG.md | sed 's/^/    /'
  exit 1
fi

echo "→ $CURRENT → $NEW"

# ---------------------------------------------------------------- bump
printf '%s\n' "$NEW" > VERSION

# Keep the Go fallback in sync so a plain `go build` reports a sane version.
if [ -f pc/main.go ]; then
  sed -i.bak "s/^var version = \"[^\"]*\"/var version = \"$NEW\"/" pc/main.go
  rm -f pc/main.go.bak
fi

git add VERSION CHANGELOG.md pc/main.go
git commit -q -m "chore(release): v$NEW"
git tag -a "v$NEW" -m "PhotoSync v$NEW"

echo "✓ 已提交并打 tag v$NEW"

if [ "$PUSH" -eq 1 ]; then
  echo "→ 推送到 origin"
  git push origin HEAD
  git push origin "v$NEW"
  echo
  echo "✓ 已推送。Actions 正在构建："
  echo "  https://github.com/$(git remote get-url origin | sed -E 's#.*[:/]([^/]+/[^/]+?)(\.git)?$#\1#')/actions"
else
  echo
  echo "下一步（推送后自动构建并发布）："
  echo "  git push origin HEAD && git push origin v$NEW"
fi
