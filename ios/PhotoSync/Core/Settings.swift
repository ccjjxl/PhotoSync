import Foundation
import Security
import UIKit

/// Persisted preferences and generated secrets (port, token, device id…).
///
/// Uses `UserDefaults` for plain values; the pairing token lives in the
/// Keychain so it survives reinstalls less often than UserDefaults and is not
/// included in unencrypted backups.
final class Settings: @unchecked Sendable {

    static let shared = Settings()

    static let defaultPort = 8737

    struct Snapshot {
        var port: Int
        var includeVideos: Bool
        var openAccess: Bool
        var pairingCode: String
        var token: String
        var deviceId: String
        var deviceName: String
        /// Empty set = sync every album (fresh-install behaviour).
        var selectedAlbums: Set<String>
    }

    private let defaults = UserDefaults.standard
    private let lock = NSLock()

    /// In-memory mirror of the album whitelist. Reading straight back from
    /// UserDefaults after writing is racy on device, so the UI reads this.
    private var albumMemo: Set<String>?

    private enum Key {
        static let port = "photosync.port"
        static let includeVideos = "photosync.includeVideos"
        static let openAccess = "photosync.openAccess"
        static let pairingCode = "photosync.pairingCode"
        static let deviceId = "photosync.deviceId"
        static let deviceName = "photosync.deviceName"
        static let selectedAlbums = "photosync.selectedAlbums"
        static let tokenAccount = "dev.photosync.token"
    }

    private init() {
        bootstrap()
    }

    // MARK: - Lifecycle

    private func bootstrap() {
        lock.lock()
        defer { lock.unlock() }

        if loadToken().isEmpty {
            saveToken(Settings.randomHex(24))
        }
        if defaults.string(forKey: Key.pairingCode) == nil {
            defaults.set(Settings.randomCode(), forKey: Key.pairingCode)
        }
        if defaults.string(forKey: Key.deviceId)?.isEmpty ?? true {
            defaults.set(Settings.randomHex(4), forKey: Key.deviceId)
        }
    }

    // MARK: - Reads

    func snapshot() -> Snapshot {
        lock.lock()
        defer { lock.unlock() }
        return Snapshot(
            port: defaults.object(forKey: Key.port) as? Int ?? Settings.defaultPort,
            includeVideos: defaults.object(forKey: Key.includeVideos) as? Bool ?? true,
            openAccess: defaults.object(forKey: Key.openAccess) as? Bool ?? false,
            pairingCode: defaults.string(forKey: Key.pairingCode) ?? "000000",
            token: loadToken(),
            deviceId: defaults.string(forKey: Key.deviceId) ?? "",
            deviceName: defaults.string(forKey: Key.deviceName) ?? "",
            selectedAlbums: currentAlbumsLocked()
        )
    }

    /// Album whitelist, served from memory so reads are consistent immediately
    /// after a write.
    func currentAlbums() -> Set<String> {
        lock.lock()
        defer { lock.unlock() }
        return currentAlbumsLocked()
    }

    private func currentAlbumsLocked() -> Set<String> {
        if let memo = albumMemo { return memo }
        let stored = Set(defaults.stringArray(forKey: Key.selectedAlbums) ?? [])
        albumMemo = stored
        return stored
    }

    func hasAlbumFilter() -> Bool { !currentAlbums().isEmpty }

    // MARK: - Writes

    func setPort(_ port: Int) {
        mutate { $0.port = min(max(port, 1024), 65535) }
    }

    func setIncludeVideos(_ value: Bool) {
        mutate { $0.includeVideos = value }
    }

    func setOpenAccess(_ value: Bool) {
        mutate { $0.openAccess = value }
    }

    func setDeviceName(_ name: String) {
        let trimmed = String(name.trimmingCharacters(in: .whitespacesAndNewlines).prefix(40))
        lock.lock()
        defaults.set(trimmed, forKey: Key.deviceName)
        lock.unlock()
    }

    /// Replaces the album whitelist. An empty set means "sync everything".
    func setSelectedAlbums(_ albums: Set<String>) {
        let cleaned = Set(albums.map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty })
        lock.lock()
        albumMemo = cleaned
        defaults.set(Array(cleaned), forKey: Key.selectedAlbums)
        lock.unlock()
    }

    func selectAllAlbums() { setSelectedAlbums([]) }

    func rotatePairingCode() {
        lock.lock()
        defaults.set(Settings.randomCode(), forKey: Key.pairingCode)
        lock.unlock()
    }

    func rotateToken() {
        lock.lock()
        saveToken(Settings.randomHex(24))
        lock.unlock()
    }

    /// Applies a change to the three simple toggles without re-entering `lock`.
    ///
    /// `snapshot()` already takes the lock, so calling it here would deadlock on
    /// a non-recursive NSLock.
    private func mutate(_ block: (inout Snapshot) -> Void) {
        lock.lock()
        defer { lock.unlock() }

        var copy = Snapshot(
            port: defaults.object(forKey: Key.port) as? Int ?? Settings.defaultPort,
            includeVideos: defaults.object(forKey: Key.includeVideos) as? Bool ?? true,
            openAccess: defaults.object(forKey: Key.openAccess) as? Bool ?? false,
            pairingCode: defaults.string(forKey: Key.pairingCode) ?? "000000",
            token: loadToken(),
            deviceId: defaults.string(forKey: Key.deviceId) ?? "",
            deviceName: defaults.string(forKey: Key.deviceName) ?? "",
            selectedAlbums: currentAlbumsLocked()
        )
        block(&copy)

        defaults.set(copy.port, forKey: Key.port)
        defaults.set(copy.includeVideos, forKey: Key.includeVideos)
        defaults.set(copy.openAccess, forKey: Key.openAccess)
    }

    // MARK: - Derived names

    /// User-set name, falling back to the marketing model ("iPhone 6s").
    ///
    /// `UIDevice.current.model` only says "iPhone", which cannot distinguish
    /// two paired devices, so the hardware identifier is resolved to a real name.
    func effectiveDeviceName() -> String {
        let custom = defaults.string(forKey: Key.deviceName) ?? ""
        if !custom.isEmpty { return custom }
        return DeviceInfo.marketingName
    }

    /// Folder name used on the PC: "iPhone-3a04a9cd".
    func folderName() -> String {
        let base = effectiveDeviceName()
            .replacingOccurrences(of: "[^A-Za-z0-9._-]", with: "-", options: .regularExpression)
            .trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        let safe = base.isEmpty ? "iOS" : base
        return "\(safe)-\(snapshot().deviceId)"
    }

    // MARK: - Helpers

    static func randomCode() -> String {
        String(format: "%06d", Int.random(in: 0...999_999))
    }

    static func randomHex(_ bytes: Int) -> String {
        var data = Data(count: bytes)
        _ = data.withUnsafeMutableBytes { buffer in
            SecRandomCopyBytes(kSecRandomDefault, bytes, buffer.baseAddress!)
        }
        return data.map { String(format: "%02x", $0) }.joined()
    }

    // MARK: - Keychain

    private func loadToken() -> String {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: Key.tokenAccount,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data,
              let token = String(data: data, encoding: .utf8) else {
            return ""
        }
        return token
    }

    private func saveToken(_ token: String) {
        let data = Data(token.utf8)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: Key.tokenAccount,
        ]
        SecItemDelete(query as CFDictionary)
        var attributes = query
        attributes[kSecValueData as String] = data
        attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(attributes as CFDictionary, nil)
    }
}
