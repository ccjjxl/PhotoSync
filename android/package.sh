#!/usr/bin/env bash
# 一键打包签名 APK。
#
#   ./package.sh                    构建 release（正式签名）
#   ./package.sh --bump minor       自动提升版本号后再构建
#   ./package.sh --debug            同时输出 debug APK
#   ./package.sh --aab              同时输出 Play 商店用的 AAB
#   ./package.sh --install          构建完直接安装到已连接的设备
#
# 前置：先运行 ./scripts/gen-keystore.sh 生成签名密钥。
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT="$SCRIPT_DIR"
cd "$ROOT"

DO_DEBUG=0
DO_AAB=0
DO_INSTALL=0
BUMP=""
VERSION_NAME=""
VERSION_CODE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --debug) DO_DEBUG=1; shift ;;
    --aab) DO_AAB=1; shift ;;
    --install) DO_INSTALL=1; shift ;;
    --version-name) VERSION_NAME="$2"; shift 2 ;;
    --version-code) VERSION_CODE="$2"; shift 2 ;;
    --bump)
      BUMP="$2"; shift 2 ;;
    -h|--help) sed -n '1,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

# ---------------------------------------------------------------- environment
if [ -f "$ROOT/local.properties" ]; then
  SDK_DIR=$(grep -E '^sdk.dir=' "$ROOT/local.properties" | cut -d'=' -f2-)
fi
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$SDK_DIR}}"
if [ -z "${ANDROID_HOME:-}" ]; then
  echo "✗ 未找到 Android SDK。请在 android/local.properties 中设置 sdk.dir" >&2
  exit 1
fi

# ---------------------------------------------------------------- version bump
PROPS="$ROOT/keystore.properties"
read_prop() { grep -E "^$1=" "$PROPS" 2>/dev/null | cut -d'=' -f2- || true; }
write_prop() {
  local key="$1" value="$2"
  if grep -qE "^$key=" "$PROPS" 2>/dev/null; then
    sed -i.bak "s|^$key=.*|$key=$value|" "$PROPS" && rm -f "$PROPS.bak"
  else
    printf '%s=%s\n' "$key" "$value" >> "$PROPS"
  fi
}

if [ -n "$BUMP" ]; then
  CUR_NAME=$(read_prop versionName); [ -z "$CUR_NAME" ] && CUR_NAME="1.0.0"
  CUR_CODE=$(read_prop versionCode); [ -z "$CUR_CODE" ] && CUR_CODE=1
  IFS='.' read -r MAJOR MINOR PATCH <<< "$CUR_NAME"
  case "$BUMP" in
    major) MAJOR=$((MAJOR+1)); MINOR=0; PATCH=0 ;;
    minor) MINOR=$((MINOR+1)); PATCH=0 ;;
    patch) PATCH=$((PATCH+1)) ;;
    none) ;;
    *) echo "✗ --bump 只支持 major/minor/patch/none" >&2; exit 2 ;;
  esac
  VERSION_NAME="$MAJOR.$MINOR.$PATCH"
  VERSION_CODE=$((CUR_CODE+1))
  if [ -f "$PROPS" ]; then
    write_prop versionName "$VERSION_NAME"
    write_prop versionCode "$VERSION_CODE"
  fi
  echo "→ 版本提升至 $VERSION_NAME (code $VERSION_CODE)"
fi

GRADLE_ARGS=()
[ -n "$VERSION_NAME" ] && GRADLE_ARGS+=("-PversionName=$VERSION_NAME")
[ -n "$VERSION_CODE" ] && GRADLE_ARGS+=("-PversionCode=$VERSION_CODE")

# ---------------------------------------------------------------- signing check
if [ ! -f "$PROPS" ]; then
  echo "⚠ 未找到 keystore.properties —— release 将使用 debug 签名。"
  echo "  正式发布请先运行: ./scripts/gen-keystore.sh"
  echo
fi

# ---------------------------------------------------------------- build
TASKS=(":app:assembleRelease")
[ "$DO_AAB" -eq 1 ] && TASKS+=(":app:bundleRelease")
[ "$DO_DEBUG" -eq 1 ] && TASKS+=(":app:assembleDebug")

echo "→ 执行 ${TASKS[*]}"
./gradlew "${TASKS[@]}" "${GRADLE_ARGS[@]}"

# ---------------------------------------------------------------- report
OUT="app/build/outputs"
APK=$(find "$OUT/apk/release" -name "*.apk" 2>/dev/null | head -1)
if [ -n "$APK" ]; then
  echo
  echo "✓ APK: $APK"
  ls -lh "$APK" | awk '{print "  大小: "$5}'

  APKSIGNER="$ANDROID_HOME/build-tools/"$(ls "$ANDROID_HOME/build-tools" | sort -V | tail -1)"/apksigner"
  if [ -x "$APKSIGNER" ]; then
    echo "  签名信息:"
    "$APKSIGNER" verify --print-certs "$APK" 2>/dev/null \
      | grep -E "Signer #1 (certificate DN|SHA-256)" | sed 's/^/    /' || \
      echo "    （无法读取签名信息）"
    "$APKSIGNER" verify "$APK" >/dev/null 2>&1 \
      && echo "    ✓ 签名校验通过" \
      || echo "    ✗ 签名校验失败"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    echo "  SHA-256: $(sha256sum "$APK" | cut -d' ' -f1)"
  fi
fi

AAB=$(find "$OUT/bundle/release" -name "*.aab" 2>/dev/null | head -1)
[ -n "$AAB" ] && echo "✓ AAB: $AAB"

DBG=$(find "$OUT/apk/debug" -name "*.apk" 2>/dev/null | head -1)
[ -n "$DBG" ] && echo "✓ Debug APK: $DBG"

if [ "$DO_INSTALL" -eq 1 ] && [ -n "$APK" ]; then
  echo
  echo "→ 安装到设备"
  if command -v adb >/dev/null 2>&1; then
    adb install -r "$ROOT/$APK"
  elif [ -x "$ANDROID_HOME/platform-tools/adb" ]; then
    "$ANDROID_HOME/platform-tools/adb" install -r "$ROOT/$APK"
  else
    echo "✗ 找不到 adb，请手动安装: $APK" >&2
    exit 1
  fi
fi
