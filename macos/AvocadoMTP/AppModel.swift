import AppKit
import Foundation
import ServiceManagement

/// Everything the menu shows. Snapshots arrive from the daemon's watch stream;
/// nothing is polled.
@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var state: ActivityState?
    @Published private(set) var serviceRunning = false
    @Published private(set) var autostart = true
    @Published private(set) var busy = false
    @Published var message: String?

    var installed: Bool { ServiceControl.isInstalled }

    var isTransferring: Bool {
        state?.transfers.contains { $0.state == "active" } ?? false
    }

    init() {
        startWatching()
        Task {
            autostart = await Task.detached { ServiceControl.autostart() }.value
            registerLoginItemByDefault()
        }
    }

    /// On first launch, open the menu at login too when the service is set to
    /// start at login (the default). Afterwards the user's toggle decides.
    private func registerLoginItemByDefault() {
        let key = "didApplyDefaultLoginItem"
        guard !UserDefaults.standard.bool(forKey: key) else { return }
        UserDefaults.standard.set(true, forKey: key)
        if autostart, SMAppService.mainApp.status != .enabled {
            try? SMAppService.mainApp.register()
        }
    }

    // MARK: Watch stream

    private func startWatching() {
        let thread = Thread { [weak self] in
            while true {
                do {
                    let conn = try DaemonConnection()
                    try conn.send(["op": "watch"])
                    _ = try conn.readFrame() // {"ok": true}
                    while true {
                        let frame = try conn.readFrame()
                        let st = try JSONDecoder().decode(ActivityState.self, from: frame)
                        DispatchQueue.main.async {
                            self?.serviceRunning = true
                            if self?.state != st { self?.state = st }
                        }
                    }
                } catch {
                    DispatchQueue.main.async {
                        self?.serviceRunning = false
                        self?.state = nil
                    }
                }
                // The service is stopped or restarting: a local connect every
                // 2 s is cheap and makes the menu follow start/stop quickly.
                Thread.sleep(forTimeInterval: 2)
            }
        }
        thread.name = "avocadomtp.watch"
        thread.qualityOfService = .utility
        thread.start()
    }

    // MARK: Actions

    func setService(running: Bool) {
        perform { try ServiceControl.run(["daemon", running ? "start" : "stop"]) }
    }

    /// One switch for "start at login": the daemon's launchd autostart and the
    /// menu bar app's own login item.
    func setAutostart(_ on: Bool) {
        autostart = on
        perform {
            try ServiceControl.run(["daemon", "autostart", on ? "on" : "off"])
            let app = SMAppService.mainApp
            if on, app.status != .enabled {
                try app.register()
            } else if !on, app.status == .enabled {
                try app.unregister()
            }
        }
    }

    func eject(_ mount: ActivityState.Mount) {
        perform { try DaemonConnection().call(["op": "eject", "storage": mount.storageId]) }
    }

    /// Ejects every volume; they stay unmounted until "Mount all" or a reconnect.
    func ejectAll(_ mounts: [ActivityState.Mount]) {
        let ids = mounts.map(\.storageId)
        perform {
            for id in ids { try DaemonConnection().call(["op": "eject", "storage": id]) }
        }
    }

    func mountAll() {
        perform { try DaemonConnection().call(["op": "mount"]) }
    }

    func reveal(_ mount: ActivityState.Mount) {
        NSWorkspace.shared.open(URL(fileURLWithPath: mount.path))
    }

    func openLog() {
        let home = FileManager.default.homeDirectoryForCurrentUser
        NSWorkspace.shared.open(home.appendingPathComponent("Library/Logs/avocadomtp.log"))
    }

    /// Runs blocking work off the main thread and reports errors in the menu.
    private func perform(_ work: @escaping @Sendable () throws -> Void) {
        busy = true
        message = nil
        Task {
            let failure = await Task.detached { () -> String? in
                do {
                    try work()
                    return nil
                } catch {
                    return error.localizedDescription
                }
            }.value
            busy = false
            message = failure
        }
    }
}
