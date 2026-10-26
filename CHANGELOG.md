# 更新日志

本文件由发布流程读取：`## ` 标题下的第一块内容会作为 GitHub Release 的说明。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 1.2.0 — 2026-10-26

### 新增
- iOS 客户端（SwiftUI + PhotoKit），与安卓共用同一套 API，电脑端零改动
  - 相簿即"文件夹"，落盘路径 `DCIM/<相簿>/<原文件名>` 与安卓一致
  - 令牌存钥匙串；支持"部分照片"受限授权
  - UDP 自动发现、网页相册、断点续传（Range）
- 手动选择要同步的文件夹 / 相簿
  - 安卓按相簿勾选，iOS 同名功能
  - 白名单在手机端强制执行，`--folder` 无法越权
  - 被排除的内容不传输、不出现在电脑端与网页相册
- 电脑端 `sync-client folders` 命令，列出相簿及是否同步
- 电脑端 `--folder` 参数，只同步指定文件夹

### 修复
- 路由器/AP 丢弃 UDP 广播时无法自动发现设备
  - 广播与全网段单播扫描并行，并合并结果
  - 扫描用独立 socket 并发（253 个地址约 10ms 发出）
  - HTTP dial 超时 10s → 2.5s，陈旧地址不再拖住发现
  - 发现耗时 11~13s → 2~3s
- `SharedPreferences.putStringSet` 的引用语义导致写入后读回旧值、UI 不刷新
- Swift `hashValue` 每进程随机，用作 ETag 会导致缓存永久失效（改用 FNV-1a）

### 改进
- 版本号统一由仓库根的 `VERSION` 文件驱动，Android versionCode 自动推导
- `gradlew` 改为官方 wrapper 实现，可在无 Gradle 缓存的 CI 机器上运行

## 1.1.0 — 2026-10-26

### 新增
- 多设备支持：一台电脑同步多台手机，每台独立子目录 / 令牌 / 增量清单
  - 配置升级到 v2，自动迁移旧版单设备配置
  - 一条命令同步所有设备，`--device` 按名称或 ID 筛选
  - `sync-client devices` / `forget` 命令
  - 手机端可自定义设备名称

### 修复
- 已配对设备保存的是手机自报端口，在 NAT / adb 转发场景下不可达

## 1.0.0 — 2026-10-26

### 新增
- 安卓客户端（Kotlin + Jetpack Compose）
  - 内嵌零依赖 HTTP 服务：NIO + `FileChannel.transferTo`（内核 sendfile）
  - MediaStore 索引，支持 Range / ETag / gzip / keep-alive
  - 缩略图两级缓存（内存 LRU + 磁盘 JPEG），视频自动抽帧
  - UDP 广播自动发现，前台服务保证后台同步
  - 内置网页相册
- 电脑端客户端（Go，单文件二进制，6 个平台）
  - 增量 / 全量 / 修复三种模式
  - 多线程并发、断点续传、失败退避重试
  - 本地 Web UI
- 正式签名打包流程（RSA-4096，v2+v3 签名，R8 混淆）
