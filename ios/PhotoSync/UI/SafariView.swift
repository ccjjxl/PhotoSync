import SafariServices
import SwiftUI
import UIKit

/// In-app browser for the gallery.
///
/// Why not `UIApplication.shared.open(url)`?
///   iOS applies App Transport Security to URLs handed to Safari: opening a
///   plain `http://192.168.x.x:8737` shows
///   "Safari 打不开该网页，因为无法连接服务器" even though the phone itself can
///   reach it. `SFSafariViewController` loads the page inside the app, where the
///   `NSAllowsLocalNetworking` exception in Info.plist applies, so plain HTTP on
///   the LAN works.
struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = false
        let controller = SFSafariViewController(url: url, configuration: configuration)
        controller.preferredControlTintColor = UIColor.systemBlue
        controller.dismissButtonStyle = .close
        return controller
    }

    func updateUIViewController(_ uiViewController: SFSafariViewController, context: Context) {}
}

/// Small helper so views can present [SafariView] without duplicating state.
struct SafariPresenter: ViewModifier {
    @Binding var url: URL?

    func body(content: Content) -> some View {
        content.sheet(item: Binding(
            get: { url.map(IdentifiableURL.init) },
            set: { url = $0?.value }
        )) { wrapper in
            SafariView(url: wrapper.value)
                .ignoresSafeArea()
        }
    }
}

/// `URL` is not `Identifiable`; wrap it for `.sheet(item:)`.
struct IdentifiableURL: Identifiable {
    let value: URL
    var id: String { value.absoluteString }
    init(_ value: URL) { self.value = value }
}

extension View {
    /// Presents the given URL in an in-app Safari sheet.
    func safariSheet(url: Binding<URL?>) -> some View {
        modifier(SafariPresenter(url: url))
    }
}
