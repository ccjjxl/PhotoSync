import Darwin
import Foundation
import Network
import UIKit

/// Local IPv4 addresses, used for the address shown in the UI and the API.
enum NetworkInfo {

    struct Address {
        let ip: String
        let iface: String
        let isWifi: Bool
    }

    static func addresses() -> [Address] {
        var results: [Address] = []
        var ifaddrPointer: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&ifaddrPointer) == 0, let first = ifaddrPointer else { return results }

        defer { freeifaddrs(ifaddrPointer) }

        var pointer: UnsafeMutablePointer<ifaddrs>? = first
        while let current = pointer {
            let flags = Int32(current.pointee.ifa_flags)
            if let addr = current.pointee.ifa_addr, addr.pointee.sa_family == UInt8(AF_INET) {
                let isUp = (flags & IFF_UP) == IFF_UP
                let isLoopback = (flags & IFF_LOOPBACK) == IFF_LOOPBACK
                if isUp && !isLoopback {
                    var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
                    let length = socklen_t(addr.pointee.sa_len)
                    if getnameinfo(addr, length, &host, socklen_t(host.count),
                                   nil, 0, NI_NUMERICHOST) == 0 {
                        let name = String(cString: current.pointee.ifa_name)
                        let ip = String(cString: host)

                        // Skip cellular data interfaces (pdp_ip*) and link-local
                        // addresses (169.254.x.x). iOS creates a self-assigned
                        // link-local address on the Wi-Fi interface whenever the
                        // DHCP lease is missing or stale — it is useless for the
                        // desktop client, and it used to sort first, so the app
                        // displayed an address that could never work.
                        let ignored = ["tun", "tap", "ppp", "ipsec", "utun", "awdl", "llw", "bridge"]
                        guard !name.hasPrefix("pdp_ip"),
                              !ignored.contains(where: { name.hasPrefix($0) }),
                              !ip.hasPrefix("169.254.") else {
                            pointer = current.pointee.ifa_next
                            continue
                        }

                        let isWifi = name == "en0" || name.hasPrefix("en")
                        results.append(Address(ip: ip, iface: name, isWifi: isWifi))
                    }
                }
            }
            pointer = current.pointee.ifa_next
        }

        // Prefer real LAN addresses: private Wi-Fi ranges first, then other Wi-Fi,
        // then everything else.
        func rank(_ address: Address) -> Int {
            if address.isWifi && Self.isPrivate(address.ip) { return 0 }
            if address.isWifi { return 1 }
            return Self.isPrivate(address.ip) ? 2 : 3
        }
        return results.sorted { lhs, rhs in
            let l = rank(lhs), r = rank(rhs)
            if l != r { return l < r }
            return lhs.ip < rhs.ip
        }
    }

    static func primaryAddress() -> String? { addresses().first?.ip }

    /// RFC 1918 private ranges — the ones a home LAN actually uses.
    static func isPrivate(_ ip: String) -> Bool {
        let parts = ip.split(separator: ".").compactMap { Int($0) }
        guard parts.count == 4 else { return false }
        switch (parts[0], parts[1]) {
        case (10, _): return true
        case (192, 168): return true
        case (172, 16...31): return true
        default: return false
        }
    }
}

/// Answers the desktop client's UDP discovery probe.
///
/// The PC broadcasts `PHOTOSYNC/1 DISCOVER` to port 8738 and expects an OFFER
/// back on the same socket. iOS requires local-network permission before
/// broadcast sockets work, which the first request triggers.
final class DiscoveryResponder: @unchecked Sendable {

    static let discoveryPort: UInt16 = 8738
    static let magic = "PHOTOSYNC/1"

    private var listener: NWListener?
    private let queue = DispatchQueue(label: "dev.photosync.discovery")
    private var advertisedPort: UInt16 = UInt16(Settings.defaultPort)

    func start(port: UInt16) {
        advertisedPort = port
        guard listener == nil else { return }

        let parameters = NWParameters.udp
        parameters.allowLocalEndpointReuse = true
        // Accept packets arriving on the broadcast address.
        parameters.requiredInterfaceType = .wifi

        guard let listener = try? NWListener(using: parameters,
                                             on: NWEndpoint.Port(rawValue: Self.discoveryPort)!) else {
            return
        }
        listener.newConnectionHandler = { [weak self] connection in
            self?.handle(connection)
        }
        listener.start(queue: queue)
        self.listener = listener
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    private func handle(_ connection: NWConnection) {
        connection.start(queue: queue)
        receive(on: connection)
    }

    private func receive(on connection: NWConnection) {
        connection.receiveMessage { [weak self] data, _, _, error in
            guard let self else { return }
            if let data, let text = String(data: data, encoding: .utf8),
               text.hasPrefix(Self.magic),
               text.uppercased().contains("DISCOVER"),
               let reply = self.offerData() {
                connection.send(content: reply, completion: .contentProcessed { _ in
                    connection.cancel()
                })
                return
            }
            if error == nil {
                self.receive(on: connection)
            } else {
                connection.cancel()
            }
        }
    }

    private func offerData() -> Data? {
        let settings = Settings.shared
        let snapshot = settings.snapshot()
        let addresses: [[String: Any]] = NetworkInfo.addresses().map {
            ["ip": $0.ip, "iface": $0.iface, "wifi": $0.isWifi]
        }

        let offer: [String: Any] = [
            "magic": Self.magic,
            "type": "OFFER",
            "app": "PhotoSync",
            "version": ServerEngine.version,
            "api": ServerEngine.apiVersion,
            "device": settings.effectiveDeviceName(),
            "deviceId": snapshot.deviceId,
            "folder": settings.folderName(),
            "model": DeviceInfo.marketingName,
            "port": Int(advertisedPort),
            "openAccess": snapshot.openAccess,
            "folderFilter": !snapshot.selectedAlbums.isEmpty,
            "host": "ios",
            "addresses": addresses,
        ]
        return try? JSONSerialization.data(withJSONObject: offer, options: [.sortedKeys])
    }

}
