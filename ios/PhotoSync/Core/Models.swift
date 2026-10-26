import Foundation
import Photos

/// One photo or video exposed to the desktop client.
///
/// `relPath` mirrors the Android app's layout so the PC client can treat both
/// platforms identically: `DCIM/<album>/<filename>`.
struct MediaItem {
    let id: String            // PHAsset.localIdentifier
    let name: String          // original filename, e.g. IMG_1234.HEIC
    let album: String         // album / folder name shown to the client
    let relPath: String       // "DCIM/<album>/<name>"
    let size: Int64
    let modified: Int         // unix seconds
    let created: Int          // unix seconds
    let mime: String
    let width: Int
    let height: Int
    let isVideo: Bool
    let duration: Double      // seconds, 0 for photos

    /// Stable cache key used for ETag / thumbnail filenames.
    var key: String { "\(Self.stableHash(id))-\(size)-\(modified)" }

    /// Deterministic 32-bit FNV-1a.
    ///
    /// Swift's `hashValue` is randomly seeded per process, so using it for an
    /// ETag would change on every launch and defeat HTTP caching.
    static func stableHash(_ text: String) -> String {
        var hash: UInt32 = 2_166_136_261
        for byte in text.utf8 {
            hash ^= UInt32(byte)
            hash = hash &* 16_777_619
        }
        return String(hash, radix: 16)
    }

    /// JSON representation, matching the Android `/api/list` schema.
    func jsonObject() -> [String: Any] {
        [
            "id": id,
            "name": name,
            "path": relPath,
            "folder": album,
            "size": size,
            "mtime": modified,
            "taken": created,
            "mime": mime,
            "width": width,
            "height": height,
            "video": isVideo,
            "duration": Int(duration),
            "key": key,
        ]
    }
}

/// An album the user can choose to sync (or exclude).
struct AlbumInfo {
    let title: String
    let count: Int
    let identifier: String
    /// Smart albums (Recents, Favorites…) are informational; only the camera
    /// roll is selected by default when nothing is chosen.
    let isSmart: Bool
}
