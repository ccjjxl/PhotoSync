import SwiftUI

/// Usage instructions + API reference, mirroring Android's help sheet.
struct HelpView: View {

    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    step("1", "开启同步服务",
                         "点击主页的圆形按钮。iOS 会在应用进入后台后暂停服务，"
                         + "传输期间请保持 App 在前台。")
                    step("2", "确保同一 Wi-Fi",
                         "手机与电脑需要在同一局域网。首次开启会请求「本地网络」权限，必须允许。")
                    step("3", "电脑端运行客户端",
                         "命令：sync-client sync\n首次运行输入配对码，之后自动记住令牌。")
                    step("4", "增量同步",
                         "客户端记录本地已同步清单，仅传输新增或修改的照片，支持断点续传与多线程加速。")

                    Card {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("当前地址")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            if model.addresses.isEmpty {
                                Text("未连接 Wi-Fi")
                                    .font(.subheadline)
                            } else {
                                ForEach(model.addresses, id: \.self) { address in
                                    Text(address)
                                        .font(.subheadline.monospaced())
                                }
                            }
                            if !model.openAccess {
                                Text("配对码：\(model.pairingCode)")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .padding(.top, 4)
                            }
                        }
                    }

                    Card {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("API（供自建脚本使用）")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            ForEach(apiRows) { row in
                                HStack(alignment: .top) {
                                    Text(row.endpoint)
                                        .font(.caption2.monospaced())
                                        .foregroundStyle(Theme.accent)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                    Text(row.detail)
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }

                    Text("iOS 说明")
                        .font(.subheadline.weight(.medium))
                    Text("· iOS 不允许应用长期在后台运行，切到后台后同步会暂停；回到前台自动恢复。\n"
                         + "· 首次使用需允许「本地网络」权限，否则电脑无法发现设备。\n"
                         + "· 若只授权了「部分照片」，只有这些照片能被同步。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(20)
            }
            .background(Theme.pageBackground)
            .navigationTitle("如何使用")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }
                }
            }
        }
    }

    /// One row in the API reference table.
    private struct ApiRow: Identifiable {
        let id = UUID()
        let endpoint: String
        let detail: String
    }

    private var apiRows: [ApiRow] {
        [
            ApiRow(endpoint: "GET /api/info", detail: "设备与库概况"),
            ApiRow(endpoint: "POST /api/pair {code}", detail: "换取 token"),
            ApiRow(endpoint: "GET /api/list?since=ts", detail: "增量列表"),
            ApiRow(endpoint: "GET /api/file?id=…", detail: "原始文件"),
            ApiRow(endpoint: "GET /api/thumb?id=…", detail: "缩略图"),
        ]
    }

    private func step(_ index: String, _ title: String, _ detail: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text(index)
                .font(.caption.weight(.bold))
                .foregroundStyle(Theme.accent)
                .frame(width: 26, height: 26)
                .background(Theme.accent.opacity(0.14))
                .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.subheadline.weight(.medium))
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}
