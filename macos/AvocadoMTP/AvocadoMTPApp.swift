import AppKit
import ServiceManagement
import SwiftUI

@main
struct AvocadoMTPApp: App {
    @StateObject private var model = AppModel()

    init() {
        // `AvocadoMTP --uninstall` (run by `avocadomtp uninstall`): undo what only
        // the app itself can — its login item and its preferences — then quit.
        if CommandLine.arguments.contains("--uninstall") {
            try? SMAppService.mainApp.unregister()
            if let id = Bundle.main.bundleIdentifier {
                UserDefaults.standard.removePersistentDomain(forName: id)
            }
            exit(0)
        }
    }

    var body: some Scene {
        MenuBarExtra {
            MenuView(model: model)
        } label: {
            MenuBarLabel(model: model)
        }
        .menuBarExtraStyle(.window)
    }
}

/// The icon in the menu bar, plus the total speed while files are moving.
struct MenuBarLabel: View {
    @ObservedObject var model: AppModel

    var body: some View {
        HStack(spacing: 4) {
            Image(nsImage: AvocadoIcon.template(active: model.state?.connected == true))
            if model.isTransferring, let st = model.state {
                Text(Format.rate(st.rateIn + st.rateOut))
                    .monospacedDigit()
            }
        }
    }
}

/// The menu bar avocado (macos/Design/menubar, concept A): a template image,
/// so macOS tints it for light and dark menu bars. The USB-C pit is empty when
/// idle and filled while a phone is connected.
enum AvocadoIcon {
    static func template(active: Bool) -> NSImage {
        let image = NSImage(named: active ? "MenuBarConnected" : "MenuBarIdle") ?? NSImage()
        image.isTemplate = true
        image.size = NSSize(width: 18, height: 18)
        return image
    }
}

enum Format {
    static func bytes(_ n: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: n, countStyle: .binary)
    }

    static func rate(_ n: Int64) -> String { bytes(n) + "/s" }
}
