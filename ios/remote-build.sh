#!/usr/bin/env bash
# 在远程 Mac 上构建 iOS App（本地无网络时也能用）。
#
# 用法（在本仓库根目录执行）：
#   ./ios/remote-build.sh                       构建模拟器版（默认）
#   ./ios/remote-build.sh --device              构建真机版（需签名，通常只在 Xcode 里做）
#   ./ios/remote-build.sh --download            构建后把 .app 拉回本地 dist/
#   ./ios/remote-build.sh --host chucun@192.168.5.41
#
# 原理：Mac 没有外网，无法 brew install xcodegen，因此用 ios/gen_xcodeproj.py
# 生成 .xcodeproj；源码通过 rsync 推到 Mac 上构建，产物可再拉回来。
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$REPO_ROOT"

REMOTE="${PHOTOSYNC_MAC:-chucun@192.168.5.41}"
REMOTE_DIR="${PHOTOSYNC_MAC_DIR:-~/photosync-build}"
CONFIG="Debug"
DEST="generic/platform=iOS Simulator"
DOWNLOAD=0
VERSION=$(tr -d '[:space:]' < VERSION 2>/dev/null || echo "1.2.0")
# Team ID can come from --team, $PHOTOSYNC_TEAM, or ios/.team (git-ignored).
if [ -z "${PHOTOSYNC_TEAM:-}" ] && [ -f "$REPO_ROOT/ios/.team" ]; then
  PHOTOSYNC_TEAM=$(tr -d '[:space:]' < "$REPO_ROOT/ios/.team")
fi
TEAM="${PHOTOSYNC_TEAM:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --host) REMOTE="$2"; shift 2 ;;
    --dir) REMOTE_DIR="$2"; shift 2 ;;
    --device) CONFIG="Release"; DEST="generic/platform=iOS"; shift ;;
    --release) CONFIG="Release"; DEST="generic/platform=iOS Simulator"; shift ;;
    --download) DOWNLOAD=1; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    --team) TEAM="$2"; shift 2 ;;
    -h|--help) sed -n '1,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

step() { printf '\n\033[1m→ %s\033[0m\n' "$*"; }
fail() { printf '\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- connectivity
step "检查到 $REMOTE 的连接"
if ! ssh -o BatchMode=yes -o ConnectTimeout=8 "$REMOTE" true 2>/dev/null; then
  fail "无法通过 SSH 连接 $REMOTE。请确认已配置免密登录：
    ssh-copy-id $REMOTE
  或用 --host 指定其他主机。"
fi

step "检查远端工具链"
REMOTE_INFO=$(ssh -o BatchMode=yes "$REMOTE" '
  echo "macos=$(sw_vers -productVersion)"
  echo "xcode=$(xcodebuild -version 2>/dev/null | head -1 | awk "{print \$2}")"
  echo "arch=$(uname -m)"
' 2>/dev/null) || fail "远端环境检查失败"

MACOS=$(sed -n 's/^macos=//p' <<< "$REMOTE_INFO")
XCODE=$(sed -n 's/^xcode=//p' <<< "$REMOTE_INFO")
ARCH=$(sed -n 's/^arch=//p' <<< "$REMOTE_INFO")
echo "   macOS $MACOS · Xcode $XCODE · $ARCH"

[ -n "$XCODE" ] || fail "远端没有可用的 xcodebuild（需要安装 Xcode 并执行 xcode-select）"

# ---------------------------------------------------------------- generate project
step "生成 App 图标"
( cd "$REPO_ROOT/ios" && python3 gen_appicon.py )

step "生成 Xcode 工程（无需 XcodeGen）"
( cd ios && python3 gen_xcodeproj.py --version "$VERSION" --build-number "${BUILD_NUMBER:-1}" --team "$TEAM" )

# ---------------------------------------------------------------- sync sources
step "同步源码到 $REMOTE:$REMOTE_DIR"
ssh -o BatchMode=yes "$REMOTE" "mkdir -p $REMOTE_DIR"
rsync -az --delete \
  --exclude 'build/' \
  --exclude 'DerivedData/' \
  --exclude '*.xcresult' \
  --exclude 'PhotoSync.xcodeproj/project.xcworkspace/' \
  "$REPO_ROOT/ios/" "$REMOTE:$REMOTE_DIR/ios/"
echo "   已同步 $(find ios -name '*.swift' | wc -l | tr -d ' ') 个 Swift 文件"

# ---------------------------------------------------------------- build
step "在远端构建（$CONFIG · $DEST）"
# Simulator builds skip signing; device builds must be signed.
if [ "$CONFIG" = "Release" ] && [ "$DEST" = "generic/platform=iOS" ]; then
  SIGNING_FLAG="DEVELOPMENT_TEAM=$TEAM"
  [ -n "$TEAM" ] || fail "真机构建需要 Team ID：--team DQUV87K656"
else
  SIGNING_FLAG="CODE_SIGNING_ALLOWED=NO"
fi
export SIGNING_FLAG

set +e
ssh -o BatchMode=yes "$REMOTE" "cd $REMOTE_DIR/ios && \
  xcodebuild \
    -project PhotoSync.xcodeproj \
    -scheme PhotoSync \
    -configuration $CONFIG \
    -destination '$DEST' \
    -derivedDataPath build \
    MARKETING_VERSION=$VERSION \
    $SIGNING_FLAG \
    build 2>&1 | tail -60" 
BUILD_STATUS=$?
set -e

if [ $BUILD_STATUS -ne 0 ]; then
  echo
  fail "远端构建失败。查看完整日志：
    ssh $REMOTE 'cd $REMOTE_DIR/ios && xcodebuild -project PhotoSync.xcodeproj -scheme PhotoSync -configuration $CONFIG -destination \"$DEST\" build'
或定位错误：
    ssh $REMOTE 'cd $REMOTE_DIR/ios && grep -E \"error:\" build.log 2>/dev/null | head -20'"
fi

# ---------------------------------------------------------------- report
step "产物"
ssh -o BatchMode=yes "$REMOTE" "
  APP=\$(find $REMOTE_DIR/ios/build/Build/Products -maxdepth 1 -name 'PhotoSync.app' | head -1)
  if [ -n \"\$APP\" ]; then
    echo \"   \$APP\"
    du -sh \"\$APP\" | awk '{print \"   大小: \"\$1}'
    /usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' \"\$APP/Info.plist\" 2>/dev/null | sed 's/^/   版本: /'
  else
    echo '   未找到 PhotoSync.app'
  fi
"

if [ "$DOWNLOAD" -eq 1 ]; then
  step "拉取到本地 dist/"
  mkdir -p dist
  rsync -az "$REMOTE:$REMOTE_DIR/ios/build/Build/Products/" /tmp/photosync-ios-products/ 2>/dev/null || true
  ssh -o BatchMode=yes "$REMOTE" "cd $REMOTE_DIR/ios/build/Build/Products && ditto -c -k --keepParent $CONFIG-iphonesimulator/PhotoSync.app /tmp/PhotoSync-ios-v$VERSION.zip" 2>/dev/null || \
    ssh -o BatchMode=yes "$REMOTE" "cd $REMOTE_DIR/ios/build/Build/Products && ditto -c -k --keepParent $CONFIG-iphoneos/PhotoSync.app /tmp/PhotoSync-ios-v$VERSION.zip"
  rsync -az "$REMOTE:/tmp/PhotoSync-ios-v$VERSION.zip" "dist/" || fail "拉取失败"
  ls -lh "dist/PhotoSync-ios-v$VERSION.zip"
fi

step "完成"
echo "  在 Mac 上直接安装到模拟器："
echo "    ssh $REMOTE 'xcrun simctl boot \"iPhone 15\" 2>/dev/null; \\"
echo "      xcrun simctl install booted $REMOTE_DIR/ios/build/Build/Products/$CONFIG-iphonesimulator/PhotoSync.app; \\"
echo "      xcrun simctl launch booted dev.photosync'"
