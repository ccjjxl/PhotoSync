import SwiftUI
import UIKit

@main
struct PhotoSyncApp: App {

    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            HomeView()
                .environmentObject(model)
                .preferredColorScheme(nil)
                .onAppear { model.bootstrap() }
        }
    }
}

/// Observable app state shared by all views.
@MainActor
final class AppModel: ObservableObject {

    @Published var authorized = false
    @Published var limitedAccess = false
    @Published var running = false
    @Published var lastError: String?
    @Published var port: Int = Settings.defaultPort
    @Published var deviceName: String = ""
    @Published var deviceId: String = ""
    @Published var folderName: String = ""
    @Published var pairingCode: String = ""
    @Published var openAccess = false
    @Published var includeVideos = true
    @Published var addresses: [String] = []
    @Published var photos = 0
    @Published var videos = 0
    @Published var bytes: Int64 = 0
    @Published var albums: [AlbumInfo] = []
    @Published var selectedAlbums: Set<String> = []
    @Published var recentThumbnails: [RecentThumb] = []

    struct RecentThumb: Identifiable {
        let id: String
        let image: UIImage
        let isVideo: Bool
    }

    private let settings = Settings.shared
    private let repository = MediaRepository.shared
    private let engine = ServerEngine.shared
    private var refreshTask: Task<Void, Never>?

    // MARK: - Lifecycle

    func bootstrap() {
        engine.onStateChange = { [weak self] in
            Task { @MainActor in self?.pullState() }
        }
        refreshAuthorization()
        pullState()
        startRefreshing()
    }

    func refreshAuthorization() {
        switch repository.authState() {
        case .authorized:
            authorized = true
            limitedAccess = false
        case .limited:
            authorized = true
            limitedAccess = true
        case .denied, .notDetermined:
            authorized = false
        }
        reloadLibrary()
    }

    func requestAccess() async {
        let state = await repository.requestAuthorization()
        switch state {
        case .authorized: authorized = true; limitedAccess = false
        case .limited: authorized = true; limitedAccess = true
        default: authorized = false
        }
        reloadLibrary()
        pullState()
    }

    // MARK: - Server

    func startServer() {
        guard authorized else { return }
        engine.start()
        pullState()
    }

    func stopServer() {
        engine.stop()
        pullState()
    }

    func toggleServer() {
        running ? stopServer() : startServer()
    }

    func setPort(_ newPort: Int) {
        settings.setPort(newPort)
        pullState()
        if running { engine.restart() }
    }

    func setIncludeVideos(_ value: Bool) {
        settings.setIncludeVideos(value)
        reloadLibrary()
        pullState()
    }

    func setOpenAccess(_ value: Bool) {
        settings.setOpenAccess(value)
        pullState()
    }

    func setDeviceName(_ name: String) {
        settings.setDeviceName(name)
        pullState()
        if running { engine.restart() }
    }

    func rotatePairingCode() {
        settings.rotatePairingCode()
        pullState()
    }

    func rotateToken() {
        settings.rotateToken()
        pullState()
    }

    // MARK: - Albums

    func toggleAlbum(_ title: String) {
        var current = settings.currentAlbums()
        // Empty set means "all": materialise before toggling so the first
        // de-selection pins the rest explicitly.
        if current.isEmpty {
            current = Set(albums.map { $0.title })
        }
        if current.contains(title) { current.remove(title) } else { current.insert(title) }
        settings.setSelectedAlbums(current)
        selectedAlbums = settings.currentAlbums()
        reloadLibrary()
        pullState()
    }

    func onlyAlbum(_ title: String) {
        settings.setSelectedAlbums([title])
        selectedAlbums = settings.currentAlbums()
        reloadLibrary()
        pullState()
    }

    func selectAllAlbums() {
        settings.selectAllAlbums()
        selectedAlbums = []
        reloadLibrary()
        pullState()
    }

    // MARK: - Data refresh

    private func startRefreshing() {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_500_000_000)
                self?.pullState()
            }
        }
    }

    func reloadLibrary() {
        guard authorized else { return }
        let snapshot = settings.snapshot()
        let selected = snapshot.selectedAlbums

        Task.detached(priority: .utility) { [weak self] in
            guard let self else { return }
            let albumList = MediaRepository.shared.albums()
            let all = MediaRepository.shared.query(
                albums: selected, includeVideos: snapshot.includeVideos
            )
            let photos = all.filter { !$0.isVideo }.count
            let videos = all.filter { $0.isVideo }.count
            let bytes = all.reduce(Int64(0)) { $0 + $1.size }

            // Build a handful of thumbnails for the "recent" strip.
            var thumbs: [RecentThumb] = []
            for item in all.prefix(12) {
                guard let asset = MediaRepository.shared.asset(for: item.id),
                      let data = MediaRepository.shared.thumbnail(
                        for: asset, size: CGSize(width: 240, height: 240)),
                      let image = UIImage(data: data) else { continue }
                thumbs.append(RecentThumb(id: item.id, image: image, isVideo: item.isVideo))
            }

            await MainActor.run {
                self.albums = albumList
                self.photos = photos
                self.videos = videos
                self.bytes = bytes
                self.recentThumbnails = thumbs
            }
        }
    }

    func pullState() {
        let snapshot = settings.snapshot()
        port = snapshot.port
        deviceName = settings.effectiveDeviceName()
        deviceId = snapshot.deviceId
        folderName = settings.folderName()
        pairingCode = snapshot.pairingCode
        openAccess = snapshot.openAccess
        includeVideos = snapshot.includeVideos
        selectedAlbums = snapshot.selectedAlbums
        running = engine.running
        lastError = engine.lastError
        addresses = NetworkInfo.addresses().map { "http://\($0.ip):\(snapshot.port)" }
    }

    /// Copy helper used by the UI.
    func copy(_ text: String) {
        UIPasteboard.general.string = text
    }
}
