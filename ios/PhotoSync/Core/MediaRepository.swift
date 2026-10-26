import Foundation
import Photos
import UIKit

/// PhotoKit-backed media index.
///
/// iOS has no path-based filesystem access to the photo library, so "folders"
/// are modelled as **albums** (user albums + smart albums). The relative path
/// exposed to the client mirrors the Android layout:
///
///     DCIM/<album name>/<original filename>
///
/// which keeps the desktop client identical across platforms.
final class MediaRepository: @unchecked Sendable {

    static let shared = MediaRepository()

    private let imageManager = PHCachingImageManager()

    /// PhotoKit is not thread-safe and this type is a shared singleton, so every
    /// entry point serialises through this lock.
    private let lock = NSRecursiveLock()

    // MARK: - Authorization

    enum AuthState {
        case notDetermined
        case denied
        case limited
        case authorized
    }

    func authState() -> AuthState {
        switch PHPhotoLibrary.authorizationStatus(for: .readWrite) {
        case .notDetermined: return .notDetermined
        case .restricted, .denied: return .denied
        case .limited: return .limited
        case .authorized: return .authorized
        @unknown default: return .denied
        }
    }

    func requestAuthorization() async -> AuthState {
        let status = await PHPhotoLibrary.requestAuthorization(for: .readWrite)
        switch status {
        case .authorized: return .authorized
        case .limited: return .limited
        case .notDetermined: return .notDetermined
        default: return .denied
        }
    }

    // MARK: - Albums

    /// Every album with at least one asset, newest content first.
    func albums() -> [AlbumInfo] {
        lock.lock()
        defer { lock.unlock() }
        var result: [AlbumInfo] = []

        // User albums (including "Recents" if present).
        let userAlbums = PHAssetCollection.fetchAssetCollections(
            with: .album, subtype: .any, options: nil
        )
        userAlbums.enumerateObjects { collection, _, _ in
            let count = PHAsset.fetchAssets(in: collection, options: nil).count
            if count > 0 {
                result.append(AlbumInfo(
                    title: collection.localizedTitle ?? "未命名相簿",
                    count: count,
                    identifier: collection.localIdentifier,
                    isSmart: false
                ))
            }
        }

        // Camera roll / smart albums are useful too, but excluding the "All
        // Photos" pseudo-album avoids double-counting.
        let smart = PHAssetCollection.fetchAssetCollections(
            with: .smartAlbum, subtype: .any, options: nil
        )
        smart.enumerateObjects { collection, _, _ in
            // Skip "All Photos" / "Recents": their contents duplicate user albums.
            if collection.assetCollectionSubtype == .smartAlbumUserLibrary ||
                collection.assetCollectionSubtype == .smartAlbumGeneric { return }
            let count = PHAsset.fetchAssets(in: collection, options: nil).count
            if count > 0 {
                result.append(AlbumInfo(
                    title: collection.localizedTitle ?? "智能相簿",
                    count: count,
                    identifier: collection.localIdentifier,
                    isSmart: true
                ))
            }
        }

        return result.sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
    }

    // MARK: - Query

    /// Fetches assets, newest first, honouring the album whitelist.
    ///
    /// - Parameters:
    ///   - since: unix seconds; only assets modified at/after this are returned.
    ///   - albums: album titles to include; empty means "all".
    ///   - includeVideos: whether videos participate.
    func query(since: Int = 0, albums: Set<String>, includeVideos: Bool) -> [MediaItem] {
        lock.lock()
        defer { lock.unlock() }
        let options = PHFetchOptions()
        options.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: false)]

        var predicates: [NSPredicate] = []
        var mediaTypes: [PHAssetMediaType] = [.image]
        if includeVideos { mediaTypes.append(.video) }
        predicates.append(NSPredicate(format: "mediaType IN %@", mediaTypes.map { $0.rawValue }))
        if since > 0 {
            predicates.append(NSPredicate(
                format: "modificationDate >= %@",
                Date(timeIntervalSince1970: TimeInterval(since))
            ))
        }
        options.predicate = NSCompoundPredicate(andPredicateWithSubpredicates: predicates)

        var items: [MediaItem] = []
        var seen = Set<String>()

        let collections = collectionsToScan(albums: albums)
        for collection in collections {
            let assets = PHAsset.fetchAssets(in: collection, options: options)
            let albumTitle = collection.localizedTitle ?? "Photos"
            assets.enumerateObjects { asset, _, _ in
                // An asset can live in several albums; emit it once, attributed
                // to the first album we meet it in.
                guard !seen.contains(asset.localIdentifier) else { return }
                seen.insert(asset.localIdentifier)
                items.append(self.makeItem(asset: asset, album: albumTitle))
            }
        }
        return items
    }

    /// Counts matching the same filters as `query`, without building items.
    func stats(albums: Set<String>, includeVideos: Bool) -> (photos: Int, videos: Int, bytes: Int64) {
        let all = query(albums: albums, includeVideos: includeVideos)
        var photos = 0
        var videos = 0
        var bytes: Int64 = 0
        for item in all {
            if item.isVideo { videos += 1 } else { photos += 1 }
            bytes += item.size
        }
        return (photos, videos, bytes)
    }

    /// Resolves the album whitelist into PHAssetCollections. Empty = user albums
    /// plus the camera roll.
    private func collectionsToScan(albums: Set<String>) -> [PHAssetCollection] {
        var result: [PHAssetCollection] = []
        let userAlbums = PHAssetCollection.fetchAssetCollections(
            with: .album, subtype: .any, options: nil
        )
        userAlbums.enumerateObjects { collection, _, _ in
            result.append(collection)
        }
        let smart = PHAssetCollection.fetchAssetCollections(
            with: .smartAlbum, subtype: .any, options: nil
        )
        smart.enumerateObjects { collection, _, _ in
            if collection.assetCollectionSubtype == .smartAlbumUserLibrary {
                result.append(collection)
            }
        }

        if albums.isEmpty { return result }

        // Filter by localized title, which is what the UI exposes.
        return result.filter { albums.contains($0.localizedTitle ?? "") }
    }

    // MARK: - Item construction

    private func makeItem(asset: PHAsset, album: String) -> MediaItem {
        let resources = PHAssetResource.assetResources(for: asset)
        // Prefer the edited/original full-size resource.
        let resource = resources.first { $0.type == .fullSizePhoto || $0.type == .photo }
            ?? resources.first { $0.type == .fullSizeVideo || $0.type == .video }
            ?? resources.first

        let name = resource?.originalFilename ?? defaultName(for: asset)
        let size = resourceSize(resource) ?? 0
        let mime = Self.mimeType(for: name, isVideo: asset.mediaType == .video)
        let safeAlbum = album.replacingOccurrences(of: "/", with: "_")

        return MediaItem(
            id: asset.localIdentifier,
            name: name,
            album: album,
            relPath: "DCIM/\(safeAlbum)/\(name)",
            size: size,
            modified: Int((asset.modificationDate ?? asset.creationDate ?? Date()).timeIntervalSince1970),
            created: Int((asset.creationDate ?? Date()).timeIntervalSince1970),
            mime: mime,
            width: asset.pixelWidth,
            height: asset.pixelHeight,
            isVideo: asset.mediaType == .video,
            duration: asset.duration
        )
    }

    /// `PHAssetResource` has no public byte-count API; the `fileSize` KVC key is
    /// the documented-in-practice way to read it. Never download the asset just
    /// to learn its size.
    private func resourceSize(_ resource: PHAssetResource?) -> Int64? {
        guard let resource else { return nil }
        if let value = resource.value(forKey: "fileSize") as? Int64 {
            return value
        }
        if let number = resource.value(forKey: "fileSize") as? NSNumber {
            return number.int64Value
        }
        return nil
    }

    private func defaultName(for asset: PHAsset) -> String {
        let ext = asset.mediaType == .video ? "MOV" : "JPG"
        return "\(asset.localIdentifier.prefix(8)).\(ext)"
    }

    static func mimeType(for name: String, isVideo: Bool) -> String {
        let ext = (name as NSString).pathExtension.lowercased()
        switch ext {
        case "heic", "heif": return "image/heic"
        case "jpg", "jpeg": return "image/jpeg"
        case "png": return "image/png"
        case "gif": return "image/gif"
        case "webp": return "image/webp"
        case "dng": return "image/x-adobe-dng"
        case "tiff", "tif": return "image/tiff"
        case "mov": return "video/quicktime"
        case "mp4", "m4v": return "video/mp4"
        default: return isVideo ? "video/quicktime" : "image/jpeg"
        }
    }

    // MARK: - Asset access

    func asset(for id: String) -> PHAsset? {
        lock.lock()
        defer { lock.unlock() }
        return PHAsset.fetchAssets(withLocalIdentifiers: [id], options: nil).firstObject
    }

    /// Streams the original bytes of an asset to a URL (used for HTTP responses).
    func writeOriginal(for asset: PHAsset, to url: URL) throws {
        lock.lock()
        defer { lock.unlock() }
        let resources = PHAssetResource.assetResources(for: asset)
        let resource = resources.first { $0.type == .fullSizePhoto || $0.type == .photo }
            ?? resources.first { $0.type == .fullSizeVideo || $0.type == .video }
            ?? resources.first
        guard let resource else {
            throw NSError(domain: "PhotoSync", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "没有可用的原始文件"])
        }
        let options = PHAssetResourceRequestOptions()
        options.isNetworkAccessAllowed = true
        let semaphore = DispatchSemaphore(value: 0)
        var thrown: Error?
        PHAssetResourceManager.default().writeData(
            for: resource, toFile: url, options: options
        ) { error in
            thrown = error
            semaphore.signal()
        }
        _ = semaphore.wait(timeout: .now() + 120)
        if let thrown { throw thrown }
    }

    /// Decodes a thumbnail for grid display / the `/api/thumb` endpoint.
    func thumbnail(for asset: PHAsset, size: CGSize) -> Data? {
        lock.lock()
        defer { lock.unlock() }
        let options = PHImageRequestOptions()
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .fast
        options.isNetworkAccessAllowed = true
        options.isSynchronous = true

        var data: Data?
        imageManager.requestImage(
            for: asset,
            targetSize: size,
            contentMode: .aspectFill,
            options: options
        ) { image, _ in
            data = image?.jpegData(compressionQuality: 0.82)
        }
        return data
    }
}
