import Foundation
import UIKit

/// Human-readable device identifiers.
///
/// `UIDevice.current.model` only ever returns "iPhone" / "iPad", which is useless
/// when several devices are paired to one computer. The marketing name comes
/// from mapping the hardware identifier (`utsname.machine`, e.g. "iPhone14,2")
/// through a table — the same approach apps use to show "iPhone 13 Pro".
enum DeviceInfo {

    /// Raw hardware identifier, e.g. "iPhone14,2" or "iPad13,18".
    static var machineIdentifier: String {
        var systemInfo = utsname()
        uname(&systemInfo)
        let mirror = Mirror(reflecting: systemInfo.machine)
        var identifier = ""
        for child in mirror.children {
            guard let value = child.value as? Int8, value != 0 else { break }
            identifier.append(Character(UnicodeScalar(UInt8(value))))
        }
        return identifier
    }

    /// Marketing name, e.g. "iPhone 13 Pro"; falls back to the raw identifier.
    static var marketingName: String {
        let identifier = machineIdentifier
        if let name = table[identifier] { return name }

        // Simulator reports the host arch; describe it as the simulator instead
        // of a bogus model.
        if identifier.hasPrefix("x86_64") || identifier.hasPrefix("arm64") {
            return "\(UIDevice.current.model) 模拟器"
        }

        // Unknown future hardware: reduce "iPhone15,3" to "iPhone 15".
        if let comma = identifier.firstIndex(of: ","),
           let major = identifier[identifier.index(identifier.startIndex, offsetBy: 6)..<comma].compactMap({ $0.wholeNumberValue }).first {
            let family = identifier.hasPrefix("iPad") ? "iPad" : "iPhone"
            return "\(family) \(major)"
        }
        return identifier.isEmpty ? UIDevice.current.model : identifier
    }

    /// "iOS 15.8.4"
    static var systemDescription: String {
        "iOS \(UIDevice.current.systemVersion)"
    }

    // https://everyiphone.com / Apple's device identifier list.
    private static let table: [String: String] = [
        // iPhone
        "iPhone8,1": "iPhone 6s", "iPhone8,2": "iPhone 6s Plus", "iPhone8,4": "iPhone SE (1st)",
        "iPhone9,1": "iPhone 7", "iPhone9,3": "iPhone 7", "iPhone9,2": "iPhone 7 Plus",
        "iPhone9,4": "iPhone 7 Plus", "iPhone10,1": "iPhone 8", "iPhone10,4": "iPhone 8",
        "iPhone10,2": "iPhone 8 Plus", "iPhone10,5": "iPhone 8 Plus",
        "iPhone10,3": "iPhone X", "iPhone10,6": "iPhone X",
        "iPhone11,2": "iPhone XS", "iPhone11,4": "iPhone XS Max", "iPhone11,6": "iPhone XS Max",
        "iPhone11,8": "iPhone XR", "iPhone12,1": "iPhone 11", "iPhone12,3": "iPhone 11 Pro",
        "iPhone12,5": "iPhone 11 Pro Max", "iPhone12,8": "iPhone SE (2nd)",
        "iPhone13,1": "iPhone 12 mini", "iPhone13,2": "iPhone 12", "iPhone13,3": "iPhone 12 Pro",
        "iPhone13,4": "iPhone 12 Pro Max", "iPhone14,4": "iPhone 13 mini",
        "iPhone14,5": "iPhone 13", "iPhone14,2": "iPhone 13 Pro", "iPhone14,3": "iPhone 13 Pro Max",
        "iPhone14,6": "iPhone SE (3rd)", "iPhone14,7": "iPhone 14", "iPhone14,8": "iPhone 14 Plus",
        "iPhone15,2": "iPhone 14 Pro", "iPhone15,3": "iPhone 14 Pro Max",
        "iPhone15,4": "iPhone 15", "iPhone15,5": "iPhone 15 Plus",
        "iPhone16,1": "iPhone 15 Pro", "iPhone16,2": "iPhone 15 Pro Max",
        "iPhone17,1": "iPhone 16 Pro", "iPhone17,2": "iPhone 16 Pro Max",
        "iPhone17,3": "iPhone 16", "iPhone17,4": "iPhone 16 Plus",
        // iPad
        "iPad13,18": "iPad (10th)", "iPad13,19": "iPad (10th)",
        "iPad12,1": "iPad (9th)", "iPad12,2": "iPad (9th)",
        "iPad11,6": "iPad (8th)", "iPad11,7": "iPad (8th)",
        "iPad14,8": "iPad Air (11-inch, M2)", "iPad14,9": "iPad Air (11-inch, M2)",
        "iPad13,1": "iPad Air (4th)", "iPad13,2": "iPad Air (4th)",
        "iPad14,1": "iPad mini (6th)", "iPad14,2": "iPad mini (6th)",
    ]
}
