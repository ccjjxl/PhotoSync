import SwiftUI

/// Manual album picker: the iOS equivalent of Android's folder selection.
///
/// The whitelist is enforced on the phone, so the desktop client cannot read
/// excluded albums even with `--folder`.
struct AlbumPickerView: View {

    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var search = ""

    private var filtering: Bool { !model.selectedAlbums.isEmpty }

    private var visibleAlbums: [AlbumInfo] {
        guard !search.isEmpty else { return model.albums }
        return model.albums.filter { $0.title.localizedCaseInsensitiveContains(search) }
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                header
                actionRow
                list
            }
            .background(Theme.pageBackground)
            .navigationTitle("选择同步相簿")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $search, placement: .navigationBarDrawer(displayMode: .always),
                        prompt: "搜索相簿")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }
                }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(filtering
                 ? "只同步勾选的 \(model.selectedAlbums.count) 个相簿"
                 : "当前同步全部 \(model.albums.count) 个相簿")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(filtering ? Theme.accent : Color.secondary)
            Text("被排除的相簿不会出现在电脑端和网页相册里，也不会被传输。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 20)
        .padding(.top, 12)
        .padding(.bottom, 10)
    }

    private var actionRow: some View {
        HStack(spacing: 10) {
            Button {
                model.selectAllAlbums()
            } label: {
                Text("全选（同步全部）")
                    .font(.subheadline.weight(.medium))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 12)
                    .background(Theme.accent)
                    .foregroundStyle(.white)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .buttonStyle(.plain)

            Button {
                model.selectAllAlbums()
            } label: {
                Text("清空选择")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 12)
                    .background(Color(.tertiarySystemFill))
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 14)
    }

    private var list: some View {
        ScrollView {
            VStack(spacing: 8) {
                if visibleAlbums.isEmpty {
                    Text(model.albums.isEmpty
                         ? "还没有读取到相簿，请先授予权限。"
                         : "没有匹配「\(search)」的相簿")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.vertical, 30)
                }
                ForEach(visibleAlbums, id: \.identifier) { album in
                    let checked = !filtering || model.selectedAlbums.contains(album.title)
                    row(album: album, checked: checked)
                }
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 30)
        }
    }

    private func row(album: AlbumInfo, checked: Bool) -> some View {
        HStack(spacing: 12) {
            checkMark(checked: checked)
            VStack(alignment: .leading, spacing: 2) {
                Text(album.title)
                    .font(.subheadline)
                    .lineLimit(1)
                Text("\(album.count) 个\(album.isSmart ? " · 智能相簿" : "")")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                model.onlyAlbum(album.title)
            } label: {
                Text("仅此项")
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(Color(.tertiarySystemFill))
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 11)
        .background(checked ? Theme.accent.opacity(0.10) : Theme.card)
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(checked ? Theme.accent.opacity(0.45) : Color(.separator).opacity(0.4),
                        lineWidth: 1)
        )
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .contentShape(Rectangle())
        .onTapGesture {
            model.toggleAlbum(album.title)
        }
    }

    private func checkMark(checked: Bool) -> some View {
        ZStack {
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .fill(checked ? Theme.accent : Color.clear)
                .frame(width: 22, height: 22)
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .stroke(checked ? Theme.accent : Color(.separator), lineWidth: 1.5)
                .frame(width: 22, height: 22)
            if checked {
                Image(systemName: "checkmark")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(.white)
            }
        }
    }
}
