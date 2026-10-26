# iOS 支持

PhotoSync 的 iOS 客户端，与安卓版共用同一套 HTTP API，因此**电脑端客户端无需改动**即可识别 iPhone。

> **只能在 macOS 上编译。** 本仓库的 iOS 源码在 Linux 上编写与静态检查，
> 由 `.github/workflows/ios.yml` 在 GitHub Actions 的 macOS runner 上真实编译验证。

## 快速开始

```bash
cd ios
./build.sh --open          # 生成工程并用 Xcode 打开
```

或在 Mac 上：

```bash
brew install xcodegen
cd ios && ./build.sh                    # 构建模拟器版
./build.sh --device                     # 构建真机版（需配置签名）
./build.sh --bump patch                 # 提升版本号后构建
```

真机安装需要在 Xcode 里配置你的开发者 Team：

1. 打开 `PhotoSync.xcodeproj`
2. 选中 PhotoSync target → Signing & Capabilities
3. 勾选 *Automatically manage signing*，选择你的 Team
4. 如 Bundle ID 冲突，改成 `dev.yourname.photosync`

## 目录结构

```
ios/
├── project.yml                      XcodeGen 工程定义（不提交 .xcodeproj）
├── build.sh                         构建脚本
└── PhotoSync/
    ├── Info.plist                   权限声明
    ├── Core/
    │   ├── Models.swift             MediaItem / AlbumInfo
    │   ├── Settings.swift           偏好 + 钥匙串令牌 + 设备 ID
    │   └── MediaRepository.swift    PhotoKit 索引、相簿、缩略图、原始文件
    ├── Server/
    │   ├── HttpServer.swift         Network.framework HTTP 服务（Range/ETag）
    │   ├── ServerEngine.swift       API 路由（与安卓完全一致）
    │   ├── DiscoveryResponder.swift UDP 自动发现
    │   └── WebGallery.swift         浏览器相册
    └── UI/
        ├── AppModel.swift           全局状态
        ├── HomeView.swift           主界面
        ├── AlbumPickerView.swift    相簿多选
        ├── SettingsView.swift       设置
        ├── HelpView.swift           说明
        └── Components.swift         设计元素
```

## 远程 Mac 打包（无外网环境）

这台 Mac（`chucun@192.168.5.41`）**无法访问 GitHub / Homebrew**，所以装不了
XcodeGen。工程用纯 Python 生成，不依赖任何第三方工具。

```bash
# 在开发机上（本仓库根目录）
./ios/remote-build.sh                      # 构建模拟器版
./ios/remote-build.sh --download           # 构建并拉回 dist/
./ios/remote-build.sh --device             # 构建真机版（需签名）
./ios/remote-build.sh --host user@host     # 指定其他 Mac
```

流程：`gen_xcodeproj.py` 生成工程 → `rsync` 推源码 → 远端 `xcodebuild` → 可选拉回产物。

### 为什么不用 XcodeGen

Mac 无外网，`brew install xcodegen` 不可行。`gen_xcodeproj.py` 只用标准库，
生成 objectVersion 56 的 pbxproj（Xcode 14+ 可读），并带上共享 scheme，
所以 `xcodebuild -scheme PhotoSync` 直接可用。

改动 Swift 文件后无需维护工程文件——源文件按目录自动发现。

## 与安卓版的差异

| 项目 | 安卓 | iOS |
| --- | --- | --- |
| 媒体索引 | MediaStore（文件路径） | PhotoKit（相簿） |
| "文件夹" | `DCIM/Camera` 等真实路径 | **相簿名称**（映射为 `DCIM/<相簿>/`） |
| 后台运行 | 前台服务，可长期后台 | **系统会挂起**，传输时需保持前台 |
| 本地文件 | 直接读取 fd | PhotoKit 导出到临时目录再流式发送 |
| 令牌存储 | SharedPreferences | **钥匙串（Keychain）** |

### 相同的部分

- API 端点、JSON 字段、认证方式完全一致
- 配对码 + 令牌机制相同
- UDP 发现协议相同（`PHOTOSYNC/1 DISCOVER` → `OFFER`）
- 相簿白名单在手机端强制执行，电脑端 `--folder` 无法越权
- 目录结构 `DCIM/<相簿名>/<原文件名>`，电脑端落盘路径一致

## iOS 平台限制

### 1. 后台限制

iOS 不允许第三方应用长期在后台运行网络服务。实际行为：

- App 在前台 → 服务正常
- 切到后台 → 系统在约 30 秒内挂起 App，服务中断
- 回到前台 → 服务自动恢复（`NWListener` 会重新绑定端口）

因此 **首次全量同步请保持 App 在前台**。增量同步很快（几秒），影响不大。

> 不要试图通过播放静音音频来保活——那是 App Store 的拒绝理由。
> 本项目没有这样做。

### 2. 本地网络权限

iOS 14+ 首次访问局域网会弹窗请求「本地网络」权限。**必须允许**，否则：

- 电脑端无法通过 UDP 发现设备
- 即使手动输入 IP，连接也会被系统拦截

如果误点了拒绝，去「设置 → 隐私与安全 → 本地网络」重新打开。

### 3. 相册权限

- **所有照片**：可读取整个相册（推荐）
- **部分照片**：只能同步你勾选的那些，UI 会提示
- 权限被拒 → 主界面显示引导页

### 4. HEIC 格式

iPhone 默认拍照存为 HEIC。电脑端会原样保存，含义：

- Linux 上查看需要 `heif-gdk-pixbuf`（GNOME）或 `libheif` 支持
- 如需 JPG，可在 iPhone 设置 → 相机 → 格式 → 选「兼容性最佳」

## 自动发现的工作原理

iOS 上 UDP 广播需要本地网络权限，且 `NWListener` 默认不接收广播包。
`DiscoveryResponder` 的做法：

- 绑定 UDP 8738，`requiredInterfaceType = .wifi`
- 收到 `PHOTOSYNC/1 DISCOVER` 后回一个 JSON `OFFER`
- `OFFER` 里包含 `deviceId` / `folder` / `port` / 局域网地址列表

如果广播不通（部分路由器/AP 隔离），电脑端会自动退化为逐台单播扫描。

## CI 验证

`.github/workflows/ios.yml` 在每次推送 `ios/**` 时：

1. 在 `macos-14` 上安装 XcodeGen
2. `xcodegen generate`
3. `xcodebuild -sdk iphonesimulator` 编译
4. SwiftLint（仅提示，不阻断）

因为 iOS 无法在 Linux 交叉编译，这是保证代码可编译的唯一可靠方式。

## 已知限制 / 后续可做

- [ ] 后台传输（需要 `BGProcessingTask` + 断点续传客户端配合，收益有限）
- [ ] Live Photo 的配对视频单独导出
- [ ] 支持按日期/人物筛选（PhotoKit 已能提供，只是缺 UI）
- [ ] iPad 分栏布局优化
- [ ] App Store 打包（当前只有 ad-hoc / 开发签名）
