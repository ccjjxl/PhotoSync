import SwiftUI

/// Settings sheet: device name, port, videos, open access, secrets, folder picker entry.
struct SettingsView: View {

    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var nameText = ""
    @State private var portText = ""
    @State private var showAlbums = false
    @State private var showRotateTokenConfirm = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 22) {
                    deviceIdentity
                    Divider()
                    portSection
                    Divider()
                    albumSection
                    Divider()
                    switches
                    Divider()
                    securityCard
                    systemButton
                }
                .padding(20)
            }
            .background(Theme.pageBackground)
            .navigationTitle("同步设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }
                }
            }
            .onAppear {
                nameText = model.deviceName
                portText = "\(model.port)"
            }
            .sheet(isPresented: $showAlbums) {
                AlbumPickerView().environmentObject(model)
            }
            .confirmationDialog("重置访问令牌？", isPresented: $showRotateTokenConfirm) {
                Button("重置", role: .destructive) {
                    model.rotateToken()
                }
                Button("取消", role: .cancel) {}
            } message: {
                Text("已配对的电脑需要重新输入配对码。")
            }
        }
    }

    // MARK: - Sections

    private var deviceIdentity: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("设备名称")
                .font(.subheadline.weight(.medium))
            Text("电脑端按此名称区分不同设备，每台设备的相册会存到独立文件夹")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack(spacing: 10) {
                TextField("例如 我的 iPhone", text: $nameText)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 12)
                    .background(Theme.card)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                Button {
                    model.setDeviceName(nameText)
                } label: {
                    Text("保存")
                        .font(.subheadline.weight(.medium))
                        .padding(.horizontal, 18)
                        .padding(.vertical, 13)
                        .background(canApplyName ? Theme.accent : Color(.tertiarySystemFill))
                        .foregroundStyle(canApplyName ? .white : .secondary)
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                .buttonStyle(.plain)
                .disabled(!canApplyName)
            }
            Text("设备 ID \(model.deviceId)  ·  电脑端文件夹 \(model.folderName)")
                .font(.caption2.monospaced())
                .foregroundStyle(.secondary)
        }
    }

    private var portSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("端口")
                .font(.subheadline.weight(.medium))
            Text("电脑端通过该端口访问，默认 \(Settings.defaultPort)；修改后服务会自动重启")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack(spacing: 10) {
                TextField("\(Settings.defaultPort)", text: $portText)
                    .keyboardType(.numberPad)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 12)
                    .background(Theme.card)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                Button {
                    model.setPort(parsedPort)
                } label: {
                    Text("应用")
                        .font(.subheadline.weight(.medium))
                        .padding(.horizontal, 18)
                        .padding(.vertical, 13)
                        .background(canApplyPort ? Theme.accent : Color(.tertiarySystemFill))
                        .foregroundStyle(canApplyPort ? .white : .secondary)
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                .buttonStyle(.plain)
                .disabled(!canApplyPort)
            }
        }
    }

    // MARK: - Derived input state

    private var canApplyName: Bool {
        let trimmed = nameText.trimmingCharacters(in: .whitespaces)
        return !trimmed.isEmpty && trimmed != model.deviceName
    }

    private var parsedPort: Int { Int(portText.filter(\.isNumber)) ?? 0 }

    private var canApplyPort: Bool {
        parsedPort >= 1024 && parsedPort <= 65535 && parsedPort != model.port
    }

    private var albumSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("同步相簿")
                .font(.subheadline.weight(.medium))
            Text(model.selectedAlbums.isEmpty
                 ? "当前同步全部 \(model.albums.count) 个相簿"
                 : "只同步已选的 \(model.selectedAlbums.count) 个相簿")
                .font(.caption)
                .foregroundStyle(model.selectedAlbums.isEmpty ? .secondary : Theme.accent)
            Button {
                showAlbums = true
            } label: {
                Text("选择要同步的相簿")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Theme.accent)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 13)
                    .background(Theme.accent.opacity(0.12))
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .buttonStyle(.plain)
            if model.limitedAccess {
                Text("当前只允许访问部分照片。可在系统设置 → 隐私 → 照片中改为“所有照片”。")
                    .font(.caption2)
                    .foregroundStyle(Theme.amber)
            }
        }
    }

    private var switches: some View {
        VStack(spacing: 16) {
            Toggle(isOn: Binding(
                get: { model.includeVideos },
                set: { model.setIncludeVideos($0) }
            )) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("同步视频").font(.subheadline.weight(.medium))
                    Text("关闭后只同步照片，可显著加快首次全量同步")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            Toggle(isOn: Binding(
                get: { model.openAccess },
                set: { model.setOpenAccess($0) }
            )) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("免配对访问").font(.subheadline.weight(.medium))
                    Text("开启后任意设备无需配对码即可读取数据（仅建议在可信网络中开启）")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private var securityCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("配对码").font(.subheadline)
                        Text(model.pairingCode)
                            .font(.system(.title2, design: .monospaced).weight(.semibold))
                            .tracking(5)
                            .foregroundStyle(Theme.accent)
                    }
                    Spacer()
                    Button {
                        model.rotatePairingCode()
                    } label: {
                        Text("更换")
                            .font(.caption.weight(.medium))
                            .padding(.horizontal, 13)
                            .padding(.vertical, 9)
                            .background(Color(.tertiarySystemFill))
                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    .buttonStyle(.plain)
                }
                Text("在电脑端输入配对码即完成连接；令牌保存在系统钥匙串中。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button {
                    showRotateTokenConfirm = true
                } label: {
                    Text("重置访问令牌")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(Theme.rose)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 11)
                        .overlay(
                            RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .stroke(Color(.separator), lineWidth: 1)
                        )
                }
                .buttonStyle(.plain)
            }
        }
    }

    private var systemButton: some View {
        Button {
            if let url = URL(string: UIApplication.openSettingsURLString) {
                UIApplication.shared.open(url)
            }
        } label: {
            Text("系统权限设置")
                .font(.subheadline.weight(.medium))
                .frame(maxWidth: .infinity)
                .padding(.vertical, 14)
                .background(Color(.tertiarySystemFill))
                .clipShape(RoundedRectangle(cornerRadius: 13, style: .continuous))
        }
        .buttonStyle(.plain)
        .padding(.bottom, 24)
    }
}
