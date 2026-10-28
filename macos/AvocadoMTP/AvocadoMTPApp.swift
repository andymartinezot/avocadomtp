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
            Image(nsImage: AvocadoIcon.menuBar(active: model.state?.connected == true, light: model.light))
                .accessibilityLabel("AvocadoMTP: " + model.light.meaning)
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

    /// The status dot's colours: green ready, purple copying, amber needs you
    /// (phone asleep), red something is wrong; no dot without a phone.
    enum Light: Equatable {
        case none, ready, transferring, asleep, error

        var color: NSColor? {
            switch self {
            case .none: return nil
            case .ready: return .systemGreen
            case .transferring: return .systemPurple
            case .asleep: return .systemOrange
            case .error: return .systemRed
            }
        }

        var meaning: String {
            switch self {
            case .none: return "no phone"
            case .ready: return "phone connected"
            case .transferring: return "copying files"
            case .asleep: return "phone not answering"
            case .error: return "phone can't be used"
            }
        }
    }

    /// The menu bar icon: the avocado in the menu bar's text colour, with a
    /// coloured dot at the top right. Only the dot has colour, so the icon
    /// still matches light and dark menu bars; with no dot it stays a template.
    static func menuBar(active: Bool, light: Light) -> NSImage {
        let base = template(active: active)
        guard let color = light.color else { return base }
        let image = NSImage(size: base.size, flipped: false) { rect in
            base.draw(in: rect)
            // Tint the avocado like a template image (drawn at display time,
            // so it follows the menu bar's appearance).
            NSColor.labelColor.set()
            rect.fill(using: .sourceAtop)
            let d: CGFloat = 7
            // Top right: the avocado is narrow there, so the USB-C pit stays visible.
            let dot = NSRect(x: rect.maxX - d, y: rect.maxY - d, width: d, height: d)
            // A clear ring keeps the dot apart from the avocado's outline.
            NSGraphicsContext.current?.compositingOperation = .clear
            NSBezierPath(ovalIn: dot.insetBy(dx: -1.5, dy: -1.5)).fill()
            NSGraphicsContext.current?.compositingOperation = .sourceOver
            color.setFill()
            NSBezierPath(ovalIn: dot).fill()
            return true
        }
        image.isTemplate = false
        return image
    }
}

enum Format {
    static func bytes(_ n: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: n, countStyle: .binary)
    }

    static func rate(_ n: Int64) -> String { bytes(n) + "/s" }
}
