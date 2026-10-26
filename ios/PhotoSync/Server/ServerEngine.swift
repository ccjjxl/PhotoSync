import Foundation
import Photos
import UIKit

/// The sync API, compatible with the Android app so one desktop client serves
/// both platforms.
///
///   GET  /api/ping      liveness + device identity (no token)
///   POST /api/pair      {code} -> {token}
///   GET  /api/info      device + library summary
///   GET  /api/stats     counters
///   GET  /api/folders   albums with enabled flags
///   GET  /api/list      paginated media metadata
///   GET  /api/thumb     JPEG thumbnail
///   GET  /api/file      original bytes (Range + ETag)
///   GET  /              web gallery
///
/// JSON is produced with `JSONSerialization` rather than string interpolation:
/// filenames and album titles are user data and must be escaped properly.
final class ServerEngine: @unchecked Sendable {

    static let shared = ServerEngine()

    static let apiVersion = 1

    private let settings = Settings.shared
    private let repository = MediaRepository.shared
    private let discovery = DiscoveryResponder()

    private var server: HttpServer?
    private var thumbCache: [String: Data] = [:]
    private let thumbLock = NSLock()

    private(set) var running = false
    private(set) var lastError: String?

    /// Notifies the UI about state changes (main queue).
    var onStateChange: (() -> Void)?

    // MARK: - Lifecycle

    func start() {
        guard !running else { return }
        let port = UInt16(settings.snapshot().port)

        let engine = HttpServer(port: port) { [weak self] request in
            self?.route(request) ?? .json(500, "{\"error\":\"engine gone\"}")
        }
        engine.onStateChange = { [weak self] running, error in
            self?.running = running
            self?.lastError = error
            self?.onStateChange?()
        }
        server = engine
        engine.start()
        discovery.start(port: port)
    }

    func stop() {
        server?.stop()
        server = nil
        discovery.stop()
        running = false
        onStateChange?()
    }

    func restart() {
        stop()
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
            self?.start()
        }
    }

    // MARK: - Routing

    private func route(_ request: HttpServer.Request) -> HttpServer.Response {
        switch request.path {
        case "/api/ping":
            return ping()
        case "/api/pair":
            return pair(request)
        default:
            break
        }

        guard authorized(request) else {
            return json(401, ["error": "unauthorized"])
        }

        switch request.path {
        case "/api/info": return info()
        case "/api/stats": return stats()
        case "/api/folders": return folders()
        case "/api/list": return list(request)
        case "/api/thumb": return thumb(request)
        case "/api/file": return file(request)
        case "/", "/index.html": return gallery(request)
        default: return json(404, ["error": "not found"])
        }
    }

    // MARK: - Auth

    private func authorized(_ request: HttpServer.Request) -> Bool {
        let snapshot = settings.snapshot()
        if snapshot.openAccess { return true }
        guard let provided = request.headers["x-photosync-token"] ?? request.q("token") else {
            return false
        }
        return constantTimeEquals(provided, snapshot.token)
    }

    /// Constant-time compare so a network attacker cannot learn the token byte
    /// by byte from response timing.
    private func constantTimeEquals(_ a: String, _ b: String) -> Bool {
        let lhs = Array(a.utf8)
        let rhs = Array(b.utf8)
        guard lhs.count == rhs.count else { return false }
        var diff: UInt8 = 0
        for index in lhs.indices { diff |= lhs[index] ^ rhs[index] }
        return diff == 0
    }

    /// Albums the user allowed; empty means everything.
    private func allowedAlbums() -> Set<String> { settings.currentAlbums() }

    // MARK: - Endpoints

    private func ping() -> HttpServer.Response {
        let snapshot = settings.snapshot()
        return json(200, [
            "app": "PhotoSync",
            "version": Self.version,
            "api": Self.apiVersion,
            "device": settings.effectiveDeviceName(),
            "deviceId": snapshot.deviceId,
            "folder": settings.folderName(),
            "platform": "ios",
            "openAccess": snapshot.openAccess,
        ])
    }

    private func pair(_ request: HttpServer.Request) -> HttpServer.Response {
        guard request.method == "POST" else {
            return json(405, ["error": "POST required"])
        }
        var code = request.q("code") ?? ""
        if code.isEmpty, let body = request.body,
           let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
           let value = object["code"] as? String {
            code = value
        }
        let snapshot = settings.snapshot()
        guard constantTimeEquals(code, snapshot.pairingCode) else {
            return json(403, ["error": "wrong code"])
        }
        return json(200, [
            "token": snapshot.token,
            "device": settings.effectiveDeviceName(),
            "deviceId": snapshot.deviceId,
            "folder": settings.folderName(),
            "api": Self.apiVersion,
        ])
    }

    private func info() -> HttpServer.Response {
        let snapshot = settings.snapshot()
        let albums = allowedAlbums()
        let counts = repository.stats(albums: albums, includeVideos: snapshot.includeVideos)

        let addresses: [[String: Any]] = NetworkInfo.addresses().map {
            ["ip": $0.ip, "iface": $0.iface, "wifi": $0.isWifi]
        }

        return json(200, [
            "app": "PhotoSync",
            "version": Self.version,
            "api": Self.apiVersion,
            "device": settings.effectiveDeviceName(),
            "deviceId": snapshot.deviceId,
            "folder": settings.folderName(),
            "model": UIDevice.current.model,
            "manufacturer": "Apple",
            "android": "iOS \(UIDevice.current.systemVersion)",
            "platform": "ios",
            "port": snapshot.port,
            "openAccess": snapshot.openAccess,
            "includeVideos": snapshot.includeVideos,
            "photos": counts.photos,
            "videos": counts.videos,
            "bytes": counts.bytes,
            "folderFilter": !albums.isEmpty,
            "folders": albums.isEmpty ? "all" : albums.sorted().joined(separator: ","),
            "server": "PhotoSync-iOS/1.0",
            "addresses": addresses,
        ])
    }

    private func stats() -> HttpServer.Response {
        let snapshot = settings.snapshot()
        let counts = repository.stats(albums: allowedAlbums(),
                                      includeVideos: snapshot.includeVideos)
        thumbLock.lock()
        let cached = thumbCache.count
        thumbLock.unlock()
        return json(200, [
            "photos": counts.photos,
            "videos": counts.videos,
            "bytes": counts.bytes,
            "thumbCached": cached,
        ])
    }

    private func folders() -> HttpServer.Response {
        let enabled = allowedAlbums()
        let list: [[String: Any]] = repository.albums().map { album in
            [
                "path": album.title,
                "count": album.count,
                "enabled": enabled.isEmpty || enabled.contains(album.title),
            ]
        }
        return json(200, [
            "filterActive": !enabled.isEmpty,
            "folders": list,
        ])
    }

    private func list(_ request: HttpServer.Request) -> HttpServer.Response {
        let snapshot = settings.snapshot()
        let since = request.qInt("since", 0)
        let limit = min(max(request.qInt("limit", 500), 1), 2000)
        let offset = max(request.qInt("offset", 0), 0)
        let includeImages = request.qBool("images", true)
        let includeVideos = request.qBool("videos", snapshot.includeVideos)
        let requestedAlbum = request.q("folder")

        // The phone-side whitelist always wins: a client cannot request an
        // album the user excluded.
        let allowed = allowedAlbums()
        var albums: Set<String>
        if let requestedAlbum {
            if allowed.isEmpty || allowed.contains(requestedAlbum) {
                albums = [requestedAlbum]
            } else {
                return json(200, [
                    "since": since, "offset": offset, "limit": limit,
                    "total": 0, "count": 0, "hasMore": false, "items": [],
                ])
            }
        } else {
            albums = allowed
        }

        var items = repository.query(since: since, albums: albums,
                                     includeVideos: includeVideos)
        if !includeImages {
            items = items.filter { $0.isVideo }
        }

        let total = items.count
        let page = Array(items.dropFirst(offset).prefix(limit))

        return json(200, [
            "since": since,
            "offset": offset,
            "limit": limit,
            "total": total,
            "count": page.count,
            "hasMore": page.count == limit,
            "items": page.map { $0.jsonObject() },
        ])
    }

    private func thumb(_ request: HttpServer.Request) -> HttpServer.Response {
        let id = request.q("id") ?? ""
        guard !id.isEmpty else { return json(400, ["error": "id required"]) }
        let size = min(max(request.qInt("size", 320), 96), 1024)
        let cacheKey = "\(id)-\(size)"

        thumbLock.lock()
        if let cached = thumbCache[cacheKey] {
            thumbLock.unlock()
            return .data(cached, contentType: "image/jpeg", cacheSeconds: 86_400)
        }
        thumbLock.unlock()

        guard let asset = repository.asset(for: id),
              let data = repository.thumbnail(for: asset,
                                              size: CGSize(width: size, height: size)) else {
            return json(404, ["error": "no thumbnail"])
        }

        thumbLock.lock()
        // Thumbnails are small; a few hundred is a few MB. Drop everything
        // rather than build an LRU — this keeps the code trivial.
        if thumbCache.count > 400 { thumbCache.removeAll(keepingCapacity: true) }
        thumbCache[cacheKey] = data
        thumbLock.unlock()

        return .data(data, contentType: "image/jpeg", cacheSeconds: 86_400)
    }

    private func file(_ request: HttpServer.Request) -> HttpServer.Response {
        let id = request.q("id") ?? ""
        guard let asset = repository.asset(for: id) else {
            return json(404, ["error": "not found"])
        }

        let resources = PHAssetResource.assetResources(for: asset)
        let name = resources.first?.originalFilename ?? "\(MediaItem.stableHash(id)).bin"
        let target = Self.spoolURL(for: id, name: name)

        if !FileManager.default.fileExists(atPath: target.path) {
            do {
                try repository.writeOriginal(for: asset, to: target)
            } catch {
                return json(500, ["error": error.localizedDescription])
            }
        }

        let mime = MediaRepository.mimeType(for: name, isVideo: asset.mediaType == .video)
        let modified = Int(asset.modificationDate?.timeIntervalSince1970 ?? 0)
        let etag = "\"\(MediaItem.stableHash(id))-\(modified)\""

        if request.headers["if-none-match"] == etag {
            return .empty(304)
        }
        return .file(target, contentType: mime, etag: etag)
    }

    /// PhotoKit can only hand out an asset by writing it to a file, so we keep
    /// one spool copy per asset and let the HTTP layer stream it.
    private static func spoolURL(for id: String, name: String) -> URL {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("photosync-files", isDirectory: true)
        try? FileManager.default.createDirectory(at: directory,
                                                 withIntermediateDirectories: true)
        // Hash the identifier: localIdentifiers contain slashes.
        let safeName = name.replacingOccurrences(of: "/", with: "_")
        return directory.appendingPathComponent("\(MediaItem.stableHash(id))-\(safeName)")
    }

    private func gallery(_ request: HttpServer.Request) -> HttpServer.Response {
        let token = request.q("token") ?? request.headers["x-photosync-token"] ?? ""
        let escaped = Self.escapeForJavaScript(token)
        let html = WebGallery.html.replacingOccurrences(of: "__TOKEN__", with: escaped)
        return .text(200, html, contentType: "text/html; charset=utf-8")
    }

    // MARK: - JSON helpers

    static let version = (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String)
        ?? "1.0.0"

    private func json(_ status: Int, _ object: [String: Any]) -> HttpServer.Response {
        guard let data = try? JSONSerialization.data(withJSONObject: object,
                                                    options: [.sortedKeys]),
              let text = String(data: data, encoding: .utf8) else {
            return .text(500, "{\"error\":\"encoding failed\"}",
                         contentType: "application/json; charset=utf-8")
        }
        return .json(status, text)
    }

    /// The token is injected into a JS string literal; strip anything that could
    /// break out of it. Tokens are hex, so this is belt-and-braces.
    private static func escapeForJavaScript(_ value: String) -> String {
        var out = ""
        for character in value {
            switch character {
            case "\\": out += "\\\\"
            case "\"": out += "\\\""
            case "'": out += "\\'"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "<": out += "\\u003c"
            case ">": out += "\\u003e"
            default: out.append(character)
            }
        }
        return out
    }
}
