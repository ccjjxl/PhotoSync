import SwiftUI

/// Main screen: server toggle, live addresses, stats, recent thumbnails,
/// album selection summary. Mirrors the Android home screen.
struct HomeView: View {

    @EnvironmentObject private var model: AppModel
    @State private var showSettings = false
    @State private var showAlbums = false
    @State private var showHelp = false
    @State private var browserURL: URL?
    /// Mirrors Android's animated `glow` for the stop square.
    @State private var stopGlow: CGFloat = 1

    var body: some View {
        NavigationView {
            ScrollView {
                VStack(spacing: 18) {
                    if !model.authorized {
                        permissionGate
                    } else {
                        header
                        heroCard
                        statsRow
                        if !model.recentThumbnails.isEmpty { recentStrip }
                        albumSection
                        if model.running { sessionCard }
                        if let error = model.lastError {
                            errorBanner(error)
                        }
                        footer
                    }
                }
                .padding(.horizontal, 18)
                .padding(.top, 8)
                .padding(.bottom, 40)
            }
            .background(Theme.pageBackground)
            .navigationBarHidden(true)
            .sheet(isPresented: $showSettings) {
                SettingsView().environmentObject(model)
            }
            .sheet(isPresented: $showAlbums) {
                AlbumPickerView().environmentObject(model)
            }
            .sheet(isPresented: $showHelp) {
                HelpView().environmentObject(model)
            }
            .safariSheet(url: $browserURL)
            .onAppear {
                withAnimation(.easeInOut(duration: 1.4).repeatForever(autoreverses: true)) {
                    stopGlow = 0
                }
            }
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 2) {
                Text("PhotoSync")
                    .font(.system(.title2, design: .rounded).weight(.bold))
                Text("\(model.deviceName) · \(model.deviceId)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            HStack(spacing: 6) {
                PulseDot(color: Theme.mint, active: model.running)
                Text(model.running ? "运行中" : "已停止")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(model.running ? Theme.mint : Color.secondary)
            }
            Button {
                showSettings = true
            } label: {
                Image(systemName: "gearshape.fill")
                    .font(.system(size: 16))
                    .foregroundStyle(.secondary)
                    .frame(width: 36, height: 36)
                    .background(Theme.card)
                    .clipShape(Circle())
            }
            .buttonStyle(.plain)
            .padding(.leading, 6)
        }
    }

    // MARK: - Hero

    private var heroCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(model.running ? "同步服务已开启" : "同步服务已关闭")
                        .font(.system(.title3, design: .rounded).weight(.semibold))
                        .foregroundStyle(.white)
                    Text(model.running
                         ? "等待电脑端连接 · 端口 \(model.port)"
                         : "点击右侧按钮开启局域网共享")
                        .font(.caption)
                        .foregroundStyle(.white.opacity(0.82))
                }
                Spacer()
                Button {
                    model.toggleServer()
                } label: {
                    ZStack {
                        Circle()
                            .fill(model.running ? Color.white.opacity(0.2) : Color.white)
                            .frame(width: 64, height: 64)
                        if model.running {
                            RoundedRectangle(cornerRadius: 4)
                                .fill(Color.white.opacity(0.55 + 0.45 * stopGlow))
                                .frame(width: 20, height: 20)
                        } else {
                            SyncGlyph(color: Color(red: 0.17, green: 0.42, blue: 1.0), size: 34)
                        }
                    }
                }
                .buttonStyle(.plain)
            }

            if model.running, let primary = model.addresses.first {
                Button {
                    model.copy(primary)
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(primary.replacingOccurrences(of: "http://", with: ""))
                                .font(.system(.subheadline, design: .monospaced).weight(.medium))
                                .foregroundStyle(.white)
                            Text("点击复制地址")
                                .font(.caption2)
                                .foregroundStyle(.white.opacity(0.7))
                        }
                        Spacer()
                        PillButton(title: "浏览器打开") {
                            // Include the token: the gallery shell is public but
                            // its data endpoints require authentication.
                            let token = Settings.shared.snapshot().token
                            let target = token.isEmpty ? primary : "\(primary)/?token=\(token)"
                            browserURL = URL(string: target)
                        }
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 12)
                    .background(.white.opacity(0.16))
                    .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                }
                .buttonStyle(.plain)
                .padding(.top, 18)

                if model.addresses.count > 1 {
                    Text("其他网卡 \(model.addresses.dropFirst().joined(separator: "  "))")
                        .font(.caption2)
                        .foregroundStyle(.white.opacity(0.62))
                        .padding(.top, 6)
                }

                HStack(spacing: 8) {
                    Text("配对码")
                        .font(.caption)
                        .foregroundStyle(.white.opacity(0.72))
                    Text(model.pairingCode)
                        .font(.system(.subheadline, design: .monospaced).weight(.semibold))
                        .tracking(3)
                        .foregroundStyle(.white)
                    if model.openAccess {
                        Text("免配对")
                            .font(.caption2)
                            .foregroundStyle(Color(red: 1.0, green: 0.88, blue: 0.54))
                    }
                    Spacer()
                    PillButton(title: "说明") { showHelp = true }
                }
                .padding(.top, 14)
            } else if !model.running {
                Text("开启后，同一 Wi-Fi 下的 Linux / Windows 电脑即可发现设备并同步照片，"
                     + "支持全量与增量传输、断点续传。")
                    .font(.caption)
                    .foregroundStyle(.white.opacity(0.78))
                    .padding(.top, 16)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.heroGradient)
        .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))
    }

    // MARK: - Stats

    private var statsRow: some View {
        HStack(spacing: 10) {
            StatTile(label: "照片", value: humanCount(model.photos), color: Theme.accent)
            StatTile(label: "视频", value: humanCount(model.videos), color: Theme.amber)
            StatTile(label: "占用", value: humanBytes(model.bytes), color: Theme.mint)
        }
    }

    // MARK: - Recent

    private var recentStrip: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("最近拍摄")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    model.reloadLibrary()
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(model.recentThumbnails) { thumb in
                        ZStack(alignment: .bottomTrailing) {
                            Image(uiImage: thumb.image)
                                .resizable()
                                .aspectRatio(contentMode: .fill)
                                .frame(width: 84, height: 84)
                                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                            if thumb.isVideo {
                                Text("视频")
                                    .font(.system(size: 9))
                                    .foregroundStyle(.white)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1)
                                    .background(.black.opacity(0.55))
                                    .clipShape(RoundedRectangle(cornerRadius: 5))
                                    .padding(4)
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Albums

    /// Albums to display: everything, or only the enabled ones.
    private var visibleAlbums: [AlbumInfo] {
        model.selectedAlbums.isEmpty
            ? model.albums
            : model.albums.filter { model.selectedAlbums.contains($0.title) }
    }

    private var albumSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("同步相册")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                Spacer()
                Text(model.selectedAlbums.isEmpty
                     ? "全部 \(model.albums.count) 个"
                     : "已选 \(model.selectedAlbums.count)/\(model.albums.count)")
                    .font(.caption)
                    .foregroundStyle(model.selectedAlbums.isEmpty ? .secondary : Theme.accent)
                Button {
                    showAlbums = true
                } label: {
                    Text("选择")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(Theme.accent)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(Theme.card)
                        .clipShape(Capsule())
                }
                .buttonStyle(.plain)
            }

            VStack(spacing: 8) {
                ForEach(visibleAlbums.prefix(6), id: \.identifier) { album in
                    HStack(spacing: 12) {
                        Text(String(album.title.prefix(1)))
                            .font(.caption.weight(.bold))
                            .foregroundStyle(Theme.accent)
                            .frame(width: 34, height: 34)
                            .background(Theme.accent.opacity(0.14))
                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                        Text(album.title)
                            .font(.subheadline)
                            .lineLimit(1)
                        Spacer()
                        Text("\(album.count)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 11)
                    .background(Theme.card)
                    .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                }
            }

            if visibleAlbums.count > 6 {
                Button {
                    showAlbums = true
                } label: {
                    Text(model.selectedAlbums.isEmpty
                         ? "另有 \(visibleAlbums.count - 6) 个相册…"
                         : "另有 \(visibleAlbums.count - 6) 个已选相册…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
    }

    // MARK: - Session / error / footer

    private var sessionCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("本次会话")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)
            // Mirrors the Android layout: bytes served + file count on the left,
            // cache size on the right.
            Card {
                HStack(alignment: .center) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(humanBytes(model.servedBytes))
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(Theme.mint)
                        Text("已传输 \(model.servedFiles) 个文件")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 2) {
                        Text("缩略图缓存")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        Text(humanBytes(model.cacheBytes))
                            .font(.subheadline.weight(.medium))
                    }
                }
            }
        }
    }

    private func errorBanner(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(Theme.rose)
            Text(message)
                .font(.caption)
                .fixedSize(horizontal: false, vertical: true)
            Spacer()
            Button("改端口") { showSettings = true }
                .font(.caption.weight(.medium))
        }
        .padding(14)
        .background(Theme.rose.opacity(0.14))
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private var footer: some View {
        Text("在电脑上运行 sync-client，它会自动发现本机并增量传输照片。\n"
             + "也可用浏览器直接打开上方地址浏览、下载。")
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 4)
    }

    // MARK: - Permission gate

    private var permissionGate: some View {
        VStack(spacing: 22) {
            Spacer(minLength: 80)
            SyncGlyph(color: Theme.accent, size: 64)
            VStack(spacing: 10) {
                Text("需要相册访问权限")
                    .font(.title2.weight(.bold))
                Text("PhotoSync 只读取你的照片与视频，用于同步到自己的电脑；"
                     + "不会上传到任何服务器。")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            Button {
                Task { await model.requestAccess() }
            } label: {
                Text("授予权限")
                    .font(.subheadline.weight(.semibold))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 15)
                    .background(Theme.accent)
                    .foregroundStyle(.white)
                    .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            }
            .buttonStyle(.plain)

            Button("前往系统设置") {
                if let url = URL(string: UIApplication.openSettingsURLString) {
                    UIApplication.shared.open(url)
                }
            }
            .font(.subheadline)
            .foregroundStyle(Theme.accent)
            Spacer()
        }
        .padding(.horizontal, 8)
    }
}
