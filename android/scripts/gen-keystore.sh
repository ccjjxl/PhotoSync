# 生成正式签名密钥库（默认写入 android/keystore/ 与 android/keystore.properties）
#
#   ./scripts/gen-keystore.sh                     # 交互式，自动生成强密码
#   ./scripts/gen-keystore.sh --alias myalias --cn "My Name"
#
# 生成后可用 ./package.sh 打包签名 APK。
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

KEYSTORE_DIR="$ROOT/keystore"
KEYSTORE="$KEYSTORE_DIR/photosync-release.jks"
PROPS="$ROOT/keystore.properties"

ALIAS="photosync"
CN="PhotoSync"
ORG="PhotoSync"
COUNTRY="CN"
VALIDITY=10000
FORCE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --alias) ALIAS="$2"; shift 2 ;;
    --cn) CN="$2"; shift 2 ;;
    --org) ORG="$2"; shift 2 ;;
    --country) COUNTRY="$2"; shift 2 ;;
    --validity) VALIDITY="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help)
      sed -n '1,12p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

# ---------------------------------------------------------------- locate tooling
if [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/keytool" ]; then
  KEYTOOL="$JAVA_HOME/bin/keytool"
elif command -v keytool >/dev/null 2>&1; then
  KEYTOOL=$(command -v keytool)
else
  echo "✗ 找不到 keytool，请设置 JAVA_HOME 或安装 JDK 17+" >&2
  exit 1
fi

mkdir -p "$KEYSTORE_DIR"

if [ -f "$KEYSTORE" ] && [ "$FORCE" -ne 1 ]; then
  echo "✗ 密钥库已存在: $KEYSTORE"
  echo "  若要重新生成（会破坏升级覆盖安装），加 --force"
  exit 1
fi

# ---------------------------------------------------------------- passwords
# 16 字节 base64，去掉 shell/属性文件里需要转义的字符
gen_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | cut -c1-24
  else
    LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 24
  fi
}

STORE_PASS=$(gen_password)
KEY_PASS="$STORE_PASS"

echo "→ 生成密钥库 $KEYSTORE"
"$KEYTOOL" -genkeypair \
  -keystore "$KEYSTORE" \
  -storetype PKCS12 \
  -storepass "$STORE_PASS" \
  -keypass "$KEY_PASS" \
  -alias "$ALIAS" \
  -keyalg RSA \
  -keysize 4096 \
  -validity "$VALIDITY" \
  -dname "CN=$CN, OU=Mobile, O=$ORG, L=Unknown, ST=Unknown, C=$COUNTRY" \
  -noprompt 2>&1 | grep -v "^$" || true

# ---------------------------------------------------------------- properties
cat > "$PROPS" <<EOF
# 由 scripts/gen-keystore.sh 自动生成 —— 请务必备份本文件与 keystore/ 目录！
# 丢失密钥将无法对已安装用户发布更新（只能换包名重新上架）。
storeFile=keystore/photosync-release.jks
storePassword=$STORE_PASS
keyAlias=$ALIAS
keyPassword=$KEY_PASS

# 打包时使用的默认版本号，可用 -PversionName/-PversionCode 覆盖
versionName=1.0.0
versionCode=1
EOF
chmod 600 "$PROPS"
chmod 600 "$KEYSTORE"

echo
echo "✓ 完成"
echo "  密钥库: $KEYSTORE"
echo "  配置:   $PROPS"
echo "  别名:   $ALIAS"
echo
echo "  证书指纹:"
"$KEYTOOL" -list -v -keystore "$KEYSTORE" -storepass "$STORE_PASS" -alias "$ALIAS" \
  | grep -E "SHA1:|SHA256:|Owner:|Valid from" | sed 's/^/    /'
echo
echo "⚠ 请立即备份 keystore/ 目录与 keystore.properties（两者均已 git-ignore）。"
