import AppKit
import SwiftUI

@main
struct AvocadoMTPApp: App {
    @StateObject private var model = AppModel()

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

/// A monochrome avocado drawn in code so it's crisp at any scale and follows
/// the menu bar's light/dark appearance (template image). The pit is filled
/// while a phone is connected.
enum AvocadoIcon {
    static func template(active: Bool) -> NSImage {
        let image = NSImage(size: NSSize(width: 16, height: 18), flipped: false) { _ in
            let pear = NSBezierPath()
            pear.move(to: NSPoint(x: 8, y: 1))
            pear.curve(to: NSPoint(x: 14.5, y: 7), controlPoint1: NSPoint(x: 12, y: 1), controlPoint2: NSPoint(x: 14.5, y: 3.5))
            pear.curve(to: NSPoint(x: 11, y: 12), controlPoint1: NSPoint(x: 14.5, y: 9.5), controlPoint2: NSPoint(x: 12, y: 10.5))
            pear.curve(to: NSPoint(x: 8, y: 17), controlPoint1: NSPoint(x: 10, y: 13.5), controlPoint2: NSPoint(x: 10.5, y: 17))
            pear.curve(to: NSPoint(x: 5, y: 12), controlPoint1: NSPoint(x: 5.5, y: 17), controlPoint2: NSPoint(x: 6, y: 13.5))
            pear.curve(to: NSPoint(x: 1.5, y: 7), controlPoint1: NSPoint(x: 4, y: 10.5), controlPoint2: NSPoint(x: 1.5, y: 9.5))
            pear.curve(to: NSPoint(x: 8, y: 1), controlPoint1: NSPoint(x: 1.5, y: 3.5), controlPoint2: NSPoint(x: 4, y: 1))
            pear.close()
            pear.lineWidth = 1.5
            NSColor.black.set()
            pear.stroke()
            let pit = NSBezierPath(ovalIn: NSRect(x: 5, y: 3.5, width: 6, height: 6))
            if active {
                pit.fill()
            } else {
                pit.lineWidth = 1.2
                pit.stroke()
            }
            return true
        }
        image.isTemplate = true
        return image
    }
}

enum Format {
    static func bytes(_ n: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: n, countStyle: .binary)
    }

    static func rate(_ n: Int64) -> String { bytes(n) + "/s" }
}
