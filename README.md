# PhotoSync · 安卓照片快速同步到电脑

把手机相册**快速、增量**地同步到 Linux / Windows 电脑的完整方案。

* **手机端** — Kotlin + Jetpack Compose 应用，内嵌一个零依赖的高性能 HTTP 服务，界面简洁美观，支持全量 / 增量、缩略图浏览、前台服务。
* **电脑端** — Go 编写的单文件命令行客户端（`sync-client`），支持自动发现、多线程并发、断点续传、增量清单、Web UI，Linux / Windows / macOS 通用。

传输全程走局域网直连，不经过任何服务器。

```
┌────────────────┐        UDP 8738  (自动发现)        ┌──────────────────┐
│  安卓手机       │ ◀──────────────────────────────── │  电脑 sync-client │
│  PhotoSync App │        HTTP 8737 (同步)            │  Linux / Windows  │
│                │ ─────────────────────────────────▶ │                  │
└────────────────┘   GET /api/list · /api/file (Range) └──────────────────┘
```

---

## 快速开始

### 1. 安装手机端

**正式签名发布版（推荐）：**

```bash
cd android
./scripts/gen-keystore.sh          # 首次：生成签名密钥（只需一次）
./package.sh                       # 打包签名 APK
./package.sh --install             # 打包并安装到已连接的手机
```

产物：`android/app/build/outputs/apk/release/PhotoSync-1.0.1.apk`

**开发调试版**（包名带 `.debug` 后缀，可与正式版共存）：

```bash
cd android && ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/*.apk
```

或用 Android Studio 打开 `android/` 目录直接运行。

打开 App → 授予照片权限 → 点主页右上角的圆形按钮开启同步服务。首页会显示：

* 局域网地址，例如 `192.168.1.23:8737`
* **6 位配对码**（每次重装/重置后会更新）

> 手机与电脑必须在**同一 Wi-Fi**。若路由器开启了「AP 隔离」则无法互访。

### 2. 安装电脑端

```bash
cd pc
./build.sh                 # 生成 dist/sync-client-{linux,windows}-{amd64,arm64}
```

Windows 也可以在 `pc/` 目录下执行 `go build -o sync-client.exe .` 或双击 `build.ps1`。

### 3. 同步

最简单的方式——**交互式向导**：

```bash
./sync-client
```

它会自动发现手机、询问保存目录和同步模式，然后开始传输。

也可以直接命令行：

```bash
./sync-client sync                      # 同步所有已配对手机（增量）
./sync-client sync --out /data/photos   # 指定相册根目录
./sync-client sync --host 192.168.1.23  # 指定手机 IP（跳过发现）
./sync-client sync --device Pixel       # 只同步名字含 Pixel 的那台
./sync-client sync --full               # 全量重新校验
./sync-client sync --watch 60           # 每 60 秒检查所有手机的新照片
./sync-client devices                   # 查看已配对手机列表
```

首次连接需要输入手机上的 6 位配对码，之后每台手机的令牌独立保存在
`~/.config/photosync/config.json`（Windows：`%APPDATA%\PhotoSync\config.json`）。

### 多台手机

**一条命令即可同步所有手机**，每台自动存到独立子目录，互不覆盖：

```
~/Pictures/Android/
├── Pixel-7-Pro-3a04a9cd/          ← 手机 A（设备名-设备ID）
│   ├── DCIM/Camera/IMG_0001.jpg
│   └── .photosync-manifest.json   ← A 的增量清单
└── Xiaomi-14-m0ck0002/            ← 手机 B
    ├── DCIM/Camera/IMG_0001.jpg   ← 同名文件也不冲突
    └── .photosync-manifest.json   ← B 的增量清单
```

每台手机在 App 里可以自定义**设备名称**（设置 → 设备名称），
电脑端据此生成目录名。设备 ID 是安装时生成的 8 位随机串，**卸载重装才会变**，
保证同名手机也能区分。

```bash
./sync-client sync                   # 全部手机
./sync-client sync --device Mock     # 名称/ID/目录片段匹配
./sync-client sync --device 3a04a9cd # 按设备 ID
./sync-client sync --device A,B      # 多选（逗号分隔）
./sync-client sync --parallel-devices 3   # 同时同步 3 台
./sync-client devices                # 列表 + 在线状态 + 已同步数量
./sync-client forget Pixel           # 移除某台的配对记录（照片保留）
```

> 旧版本（单设备）的配置会自动迁移：原有相册目录和清单原地保留，
> 被识别为「第一台设备」，新手机才会放到子目录里。

---

## 电脑端功能

| 命令 | 说明 |
| --- | --- |
| `sync-client` | 交互式向导（推荐首次使用，支持多选手机） |
| `sync-client sync [参数]` | 同步所有（或指定的）手机 |
| `sync-client devices` | 列出已配对手机、在线状态、已同步数量 |
| `sync-client discover` | 扫描局域网内的手机（显示设备 ID 与配对状态） |
| `sync-client pair` | 配对一台或多台，保存各自令牌 |
| `sync-client forget <设备>` | 移除配对记录（不删照片） |
| `sync-client status` | 每台手机的同步统计 |
| `sync-client serve` | 启动本地 Web UI（多设备表格 + 逐个/批量同步） |

### 同步模式

| 模式 | 行为 |
| --- | --- |
| **增量**（默认） | 只传输新增或修改过的照片，按 `路径 + 大小 + 修改时间` 判断 |
| **全量** `--full` | 忽略清单，重新下载并校验全部文件 |
| **修复** `--repair` | 只补齐本地缺失的文件，不动已有文件 |

### 常用参数

```
--out DIR            相册根目录（默认 ~/Pictures/Android），每台手机一个子目录
--device NAME|ID     只同步匹配的手机，可重复或逗号分隔
--all                同步所有已配对手机（多设备时默认）
--parallel-devices N 同时同步几台手机（默认 2）
--flat-dir           所有手机平铺到同一目录（不推荐）
--workers N          每台手机的并发下载数（默认 CPU 核数，4~12）
--since 24h          只同步最近 24 小时修改的文件（支持 7d、2024-05-01、unix 时间戳）
--no-videos          不同步视频
--flat               不保留相簿目录结构，全部平铺
--overwrite          忽略清单强制重下
--dry-run            只列出计划，不传输
--watch 60           循环增量同步
--quiet              只输出最终统计
--code 123456        非交互提供配对码
--verbose            详细日志
```

### 为什么快

* **手机端**用 MediaStore 索引而不是遍历目录，几万张照片的列表也是毫秒级。
* **HTTP 服务**基于 NIO `ServerSocketChannel` + `FileChannel.transferTo`，
  内核态 sendfile 直接搬运数据，不做用户态拷贝。
* **电脑端**默认并发 8~12 个连接，`DisableCompression` 避免对已压缩的
  JPEG/MP4 做无用解压。
* **增量判断**在传输前完成——第二次同步通常 1 秒内结束。
* **断点续传**用 `.part` 临时文件 + `Range` 请求实现，中断后重跑即可续传。

实测（本机回环 + 模拟器，324 个文件 / 31 MB）：并行传输 **~48 MB/s**，
二次增量收敛 **23 ms**。真实局域网通常是 Wi-Fi 带宽上限。

---

## 手机端功能

* 一键开启 / 关闭同步服务（前台服务，退出界面也不中断）
* **可自定义设备名称**（多手机时电脑端据此区分目录）
* 显示设备 ID、局域网地址、配对码、已传输流量、缩略图缓存占用
* 「最近拍摄」横向预览、相簿列表、照片 / 视频 / 占用统计
* 可调端口、是否同步视频、是否免配对访问
* 更换配对码 / 重置访问令牌 / 清理缩略图缓存
* 内置网页相册：任何浏览器打开 `http://手机IP:8737` 即可浏览、预览、下载

### 安全模型

* 首次连接需输入 6 位配对码，换取 24 字节随机令牌
* 令牌保存在 App 私有存储，每次请求通过 `X-PhotoSync-Token` 头校验
* 令牌比较使用常量时间比较，避免时序侧信道
* 「免配对访问」默认关闭，仅在可信网络下建议开启

---

## HTTP API

自建脚本 / 其他客户端可直接调用。

| 端点 | 说明 |
| --- | --- |
| `GET /api/ping` | 存活检查，返回 `device` / `deviceId` / `folder`（无需令牌） |
| `POST /api/pair` `{"code":"123456"}` | 用配对码换取令牌，同时返回 `deviceId` / `folder` |
| `GET /api/info` | 设备信息（含 `deviceId`、`folder`）+ 照片/视频数量与总大小 |
| `GET /api/stats` | 统计与缓存信息 |
| `GET /api/folders` | 相簿列表及数量 |
| `GET /api/list?since=&offset=&limit=&images=&videos=&asc=` | 分页媒体列表 |
| `GET /api/thumb?id=N&size=320` | JPEG 缩略图（磁盘+内存两级缓存） |
| `GET /api/file?id=N` | 原始文件，支持 `Range` / `ETag` |
| `GET /dl/DCIM/Camera/x.jpg` | 按路径下载 |
| `GET /?token=...` | 网页相册 |

示例：

```bash
TOKEN=$(curl -s -X POST -d '{"code":"123456"}' http://192.168.1.23:8737/api/pair | jq -r .token)
curl -H "X-PhotoSync-Token: $TOKEN" 'http://192.168.1.23:8737/api/list?limit=5' | jq
curl -H "X-PhotoSync-Token: $TOKEN" -o photo.jpg 'http://192.168.1.23:8737/api/file?id=42'
```

### 自动发现协议

```
UDP 广播到 8738：
  "PHOTOSYNC/1 DISCOVER"
手机回复（同一端口，单播回发送方）：
  {"magic":"PHOTOSYNC/1","type":"OFFER",
   "device":"Pixel-7","deviceId":"3a04a9cd",
   "folder":"Pixel-7-3a04a9cd","port":8737,"api":1,"openAccess":false,
   "addresses":[{"ip":"192.168.1.23","wifi":true,"iface":"wlan0"}]}
```

只使用广播，不依赖 mDNS 或组播，兼容性最好。

`deviceId` 是手机端安装时生成的 8 位随机串，`folder` 是建议的保存子目录名；
客户端用前者做身份识别（决定复用哪个令牌、写到哪个目录），
用后者作为目录名（并做去重与路径安全处理）。

### 多设备寻址

客户端解析目标设备时按以下优先级：

1. `--host` 显式指定 → 直接用
2. UDP 广播发现
3. 已配对设备的 **stored endpoint**（保存的是「上次实际连通的地址」，
   因此在 NAT / adb 端口转发 / 路由器端口映射下依然可用）

发现到的地址若不可达会被跳过并告警，不会中断其他手机的同步。

---

## 目录结构

```
photo_sync/
├── android/                      安卓应用（Kotlin + Compose）
│   ├── app/src/main/java/dev/photosync/
│   │   ├── core/                 MediaStore 索引、设置存储
│   │   ├── server/               HTTP 服务、缩略图、发现协议、网页相册
│   │   ├── service/              前台服务
│   │   ├── ui/                   Compose 界面
│   │   └── util/                 网络工具
│   └── build.gradle.kts
└── pc/                           电脑客户端（Go，单文件二进制）
    ├── api.go                    HTTP 客户端 + UDP 发现
    ├── config.go                 多设备配置（每台独立 token / 目录 / 时间）
    ├── sync.go                   同步引擎（并发、续传、模式）
    ├── manifest.go               增量清单（每台手机一份）
    ├── progress.go               进度条与统计
    ├── serve.go / webui.go       本地 Web UI
    ├── wizard.go                 交互式向导
    └── build.sh                  跨平台构建
```

## 常见问题

**找不到设备？**
确认手机 App 中服务已开启、同一 Wi-Fi、路由器未开 AP 隔离。也可 `--host` 直接指定 IP。

**多台手机怎么区分？**
每台手机在 App 设置里可自定义「设备名称」，电脑端存成
`<相册根目录>/<设备名>-<设备ID>/`。用 `sync-client devices` 可查看全部设备及目录。

**同一台手机重装 App 后，会不会生成新目录？**
会。设备 ID 存在 App 私有存储里，卸载重装会重新生成。
若想沿用旧目录，把旧目录改名成新的 `<设备名>-<设备ID>` 即可（清单里记的是相对路径，继续有效）。

**端口 8737 被占用？**
App 设置里改端口（例如 8738+），电脑端加 `--port`。

**同步中断了怎么办？**
重新运行同样的命令，未完成的 `.part` 文件会从断点继续。

**想重新全量同步？**
`sync-client sync --full`（或 `--overwrite`）。

**保存目录里的 `.photosync-manifest.json` 可以删吗？**
可以，但删掉后下次会重新比对全部文件（已存在且大小一致的文件仍会被跳过）。

**Windows 上防火墙提示？**
客户端只发起出站连接、并在本地回环监听 Web UI，允许即可。

## 开发

```bash
# 手机端
cd android && ./gradlew :app:assembleDebug

# 电脑端
cd pc && go build -o sync-client . && go vet ./...
```

---

## 安卓签名打包

发布版必须用**固定密钥**签名，否则无法覆盖安装升级（用户需先卸载，本地数据与配对令牌全部丢失）。

### 首次：生成签名密钥

```bash
cd android
./scripts/gen-keystore.sh
# 可选：--alias myalias --cn "Your Name" --org "Your Org" --country CN --validity 10000
```

会创建两个 **git-ignored** 文件：

| 文件 | 说明 |
| --- | --- |
| `keystore/photosync-release.jks` | PKCS12 密钥库，RSA 4096 位，有效期约 27 年 |
| `keystore.properties` | 密码、别名、默认版本号 |

脚本会自动生成 24 位随机强密码，并打印证书 SHA-1 / SHA-256 指纹。

> ⚠ **务必立即备份这两个文件**（放到密码管理器或离线存储）。密钥丢失后无法再发布更新，
> 只能更换包名重来。本仓库已把它们加入 `.gitignore`。

### 打包

```bash
cd android
./package.sh                    # 正式签名 APK
./package.sh --bump patch       # 自动 1.0.1 → 1.0.2（code +1）后打包
./package.sh --bump minor       # 1.0.1 → 1.1.0
./package.sh --aab              # 同时输出 Play 商店 AAB
./package.sh --debug            # 同时输出 debug APK
./package.sh --install          # 打包后直接装到设备
```

打包结束会打印 APK 路径、体积、**签名证书指纹**、`apksigner verify` 结果和 SHA-256：

```
✓ APK: app/build/outputs/apk/release/PhotoSync-1.0.1.apk
  大小: 1.3M
  签名信息:
    Signer #1 certificate DN: CN=PhotoSync Release, OU=Mobile, O=PhotoSync, L=Unknown, ST=Unknown, C=CN
    Signer #1 certificate SHA-256 digest: 8874a3a2…3783776
    ✓ 签名校验通过
  SHA-256: c069360c…94db4a00
```

### 签名方案

`app/build.gradle.kts` 中启用 **v2 + v3**（v1 关闭，minSdk 26 已不需要）：

| 方案 | 用途 |
| --- | --- |
| v2 | Android 7.0+ 全文件签名，验证更快 |
| v3 | Android 9+ 支持密钥轮换 |
| v4 | 关闭（仅增量安装需要，徒增体积） |

### 版本号管理

优先级：命令行 `-PversionName/-PversionCode` > `keystore.properties` > 默认 `1.0.0 / 1`。

```bash
./gradlew :app:assembleRelease -PversionName=2.0.0 -PversionCode=20
```

### 没有密钥时

若 `keystore.properties` 不存在（CI、新克隆仓库），`assembleRelease` 会**回退到 debug 签名**
并打印警告，保证流水线仍能产出可安装包。但这样的包无法覆盖正式签名的旧版本——
系统会拒绝：

```
Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE:
         Existing package dev.photosync signatures do not match newer version]
```

### CI 中打包

把密钥做成 Secrets，构建前还原成文件即可：

```yaml
- name: Restore keystore
  run: |
    mkdir -p android/keystore
    echo "${{ secrets.KEYSTORE_BASE64 }}" | base64 -d > android/keystore/photosync-release.jks
    cat > android/keystore.properties <<EOF
    storeFile=keystore/photosync-release.jks
    storePassword=${{ secrets.STORE_PASSWORD }}
    keyAlias=${{ secrets.KEY_ALIAS }}
    keyPassword=${{ secrets.KEY_PASSWORD }}
    EOF
- name: Build signed APK
  run: cd android && ./package.sh
```

### 签名相关速查

```bash
# 查看 APK 签名
$ANDROID_HOME/build-tools/36.0.0/apksigner verify --print-certs PhotoSync-1.0.1.apk

# 查看本地密钥库指纹
keytool -list -v -keystore android/keystore/photosync-release.jks -alias photosync

# 校验已安装应用的签名是否与 APK 一致
adb shell dumpsys package dev.photosync | grep -A2 signatures

# 对齐检查
$ANDROID_HOME/build-tools/36.0.0/zipalign -c -v 4 PhotoSync-1.0.1.apk
```

### Release 版做了哪些优化

* R8 代码混淆 + 资源压缩：12 MB → **1.3 MB**
* 只保留 `zh` / `en` 资源，剔除其他语言
* 关闭 v4 签名、剔除无用 META-INF
* ProGuard 规则保留 `dev.photosync.**`（反射调用的 Compose / 自定义组件安全）
* Debug 版包名加 `.debug` 后缀，可与正式版同时安装在一台手机上

---

## 交付清单

| 文件 | 说明 |
| --- | --- |
| `android/app/build/outputs/apk/release/PhotoSync-1.0.2.apk` | **正式签名**发布版（1.3 MB，v2+v3 签名） |
| `android/scripts/gen-keystore.sh` | 生成签名密钥库 |
| `android/package.sh` | 一键签名打包脚本 |
| `pc/dist/sync-client-linux-amd64` | Linux x86_64 客户端 |
| `pc/dist/sync-client-linux-arm64` | Linux ARM64 客户端 |
| `pc/dist/sync-client-windows-amd64.exe` | Windows x86_64 客户端 |
| `pc/dist/sync-client-windows-arm64.exe` | Windows ARM64 客户端 |

首次使用：手机装 APK → 打开 App 授权并开启服务 → 电脑运行 `sync-client` 按向导输入配对码。
**多台手机**：每台都装一次 App 并配对一遍，之后 `sync-client sync` 一条命令全部同步。

> 发布前请先执行 `android/scripts/gen-keystore.sh` 生成自己的签名密钥，
> 并妥善备份 `android/keystore/` 与 `android/keystore.properties`。
