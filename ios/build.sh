#!/usr/bin/env bash
# 构建 iOS App（需在 macOS + Xcode 环境运行）。
#
#   ./build.sh                     构建 Debug（模拟器）
#   ./build.sh --device            构建 Release（真机，需配置签名）
#   ./build.sh --open              生成工程并用 Xcode 打开
#   ./build.sh --bump minor        提升版本号后构建
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"

CONFIG="Debug"
DEST="generic/platform=iOS Simulator"
OPEN_XCODE=0
BUMP=""

while [ $# -gt 0 ]; do
  case "$1" in
    --device) CONFIG="Release"; DEST="generic/platform=iOS"; shift ;;
    --open) OPEN_XCODE=1; shift ;;
    --bump) BUMP="$2"; shift 2 ;;
    -h|--help) sed -n '1,10p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

# ---------------------------------------------------------------- environment
if ! command -v xcodebuild >/dev/null 2>&1; then
  cat >&2 <<'EOF'
✗ 未找到 xcodebuild。

iOS 应用只能在 macOS 上编译。请选择其一：
  1) 在 Mac 上运行本脚本（需安装 Xcode 15+）
  2) 用 XcodeGen 生成工程后用 Xcode 打开
  3) 推送到 GitHub，Actions 里的 "iOS build" 工作流会自动在 macOS 上验证编译

工程源码位于 ios/ 目录，本身与平台无关，可在任意环境编辑。
EOF
  exit 1
fi

if ! command -v xcodegen >/dev/null 2>&1; then
  echo "→ 安装 XcodeGen"
  if command -v brew >/dev/null 2>&1; then
    brew install xcodegen
  else
    echo "✗ 请先安装 Homebrew，或手动安装 XcodeGen: https://github.com/yonaskolb/XcodeGen" >&2
    exit 1
  fi
fi

# ---------------------------------------------------------------- version bump
if [ -n "$BUMP" ]; then
  CUR=$(grep -E 'MARKETING_VERSION:' project.yml | head -1 | sed 's/.*"\(.*\)"/\1/')
  CODE=$(grep -E 'CURRENT_PROJECT_VERSION:' project.yml | head -1 | sed 's/.*"\(.*\)"/\1/')
  IFS='.' read -r MAJOR MINOR PATCH <<< "$CUR"
  case "$BUMP" in
    major) MAJOR=$((MAJOR+1)); MINOR=0; PATCH=0 ;;
    minor) MINOR=$((MINOR+1)); PATCH=0 ;;
    patch) PATCH=$((PATCH+1)) ;;
    *) echo "✗ --bump 只支持 major/minor/patch" >&2; exit 2 ;;
  esac
  NEW="$MAJOR.$MINOR.$PATCH"
  NEWCODE=$((CODE+1))
  sed -i.bak "s/MARKETING_VERSION: \"$CUR\"/MARKETING_VERSION: \"$NEW\"/" project.yml
  sed -i.bak "s/CURRENT_PROJECT_VERSION: \"$CODE\"/CURRENT_PROJECT_VERSION: \"$NEWCODE\"/" project.yml
  rm -f project.yml.bak
  echo "→ 版本提升至 $NEW ($NEWCODE)"
fi

# ---------------------------------------------------------------- generate + build
echo "→ 生成 Xcode 工程"
xcodegen generate --quiet

if [ "$OPEN_XCODE" -eq 1 ]; then
  open PhotoSync.xcodeproj
  exit 0
fi

echo "→ 构建 ($CONFIG, $DEST)"
xcodebuild \
  -project PhotoSync.xcodeproj \
  -scheme PhotoSync \
  -configuration "$CONFIG" \
  -destination "$DEST" \
  CODE_SIGNING_ALLOWED=NO \
  build

APP=$(find ~/Library/Developer/Xcode/DerivedData -name "PhotoSync.app" -path "*/$CONFIG-iphonesimulator/*" 2>/dev/null | head -1)
if [ -n "$APP" ]; then
  echo
  echo "✓ 产物: $APP"
  du -sh "$APP" | awk '{print "  大小: "$1}'
  echo
  echo "在模拟器中运行:"
  echo "  xcrun simctl boot 'iPhone 15' 2>/dev/null || true"
  echo "  xcrun simctl install booted \"$APP\""
  echo "  xcrun simctl launch booted dev.photosync"
fi
