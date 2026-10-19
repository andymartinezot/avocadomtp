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
    /// True while the app installs or updates the service from its helper.
    @Published private(set) var settingUp = false
    /// A developer build of the service is installed; the app offers its own.
    @Published private(set) var devServiceInstalled = false
    @Published private(set) var volumes = ServiceControl.Status.Volumes.unknown
    @Published private(set) var cliOnPath = true

    var installed: Bool { ServiceControl.isInstalled }
    var canInstallService: Bool { ServiceControl.helper != nil }

    var isTransferring: Bool {
        state?.transfers.contains { $0.state == "active" } ?? false
    }

    init() {
        startWatching()
        Task {
            await setUpService()
            registerLoginItemByDefault()
        }
    }

    /// Runs once at launch: installs the service from the bundled helper if
    /// it's missing or older, then reads the status the menu needs.
    private func setUpService() async {
        let setup = await Task.detached {
            ServiceControl.setupNeeded(
                installed: ServiceControl.isInstalled,
                installedVersion: ServiceControl.isInstalled ? ServiceControl.version(of: ServiceControl.binary) : nil,
                helperVersion: ServiceControl.helper.flatMap { ServiceControl.version(of: $0) })
        }.value
        devServiceInstalled = setup == .devInstalled
        if setup == .install || setup == .update {
            await installService(firstTime: setup == .install)
        }
        await refreshStatus()
    }

    /// Installs (or reinstalls) the service from the helper, keeping a stopped
    /// service stopped.
    private func installService(firstTime: Bool) async {
        settingUp = true
        let failure = await Task.detached { () -> String? in
            let wasRunning = firstTime || ServiceControl.status().running
            do {
                try ServiceControl.installFromHelper(start: wasRunning)
                return nil
            } catch {
                return "Couldn't set up the service: " + error.localizedDescription
            }
        }.value
        settingUp = false
        message = failure
    }

    func refreshStatus() async {
        let st = await Task.detached { ServiceControl.status() }.value
        autostart = st.autostart
        volumes = st.volumes
        cliOnPath = ServiceControl.cliOnPath
    }

    /// On first launch, open the menu at login too when the service is set to
    /// start at login (the default). Afterwards the user's toggle decides.
    private func registerLoginItemByDefault() {
        let key = "didApplyDefaultLoginItem"
        guard !UserDefaults.standard.bool(forKey: key) else { return }
        UserDefaults.standard.set(true, forKey: key)
        if autostart, SMAppService.mainApp.status != .enabled {
            do {
                try Self.registerLoginItem()
            } catch {
                message = error.localizedDescription
            }
        }
    }

    /// Adds this menu to the login items. macOS may refuse or want approval
    /// (more likely for ad-hoc signed builds); the service's own autostart
    /// works either way.
    nonisolated private static func registerLoginItem() throws {
        let app = SMAppService.mainApp
        do {
            try app.register()
        } catch {
            throw DaemonError.daemon("The service will start at login, but macOS didn't add this menu: " +
                "add AvocadoMTP in System Settings → General → Login Items.")
        }
        if app.status == .requiresApproval {
            SMAppService.openSystemSettingsLoginItems()
            throw DaemonError.daemon("Allow AvocadoMTP in Login Items to open this menu at login.")
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
                try Self.registerLoginItem()
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

    /// Shows the volume in Finder. Never `open` it: a storage named like a
    /// bundle ("X.app") would be launched instead of shown.
    func reveal(_ mount: ActivityState.Mount) {
        NSWorkspace.shared.selectFile(nil, inFileViewerRootedAtPath: mount.path)
    }

    /// Installs this app's service, or replaces an installed developer build
    /// with it (a stopped one stays stopped).
    func useBundledService() {
        Task {
            await installService(firstTime: !installed)
            devServiceInstalled = false
            await refreshStatus()
        }
    }

    func installCLI() {
        perform({ try ServiceControl.installCLI() }) { [weak self] in
            self?.cliOnPath = ServiceControl.cliOnPath
        }
    }

    /// Called when the menu opens, so the checklist follows changes made
    /// elsewhere (FUSE-T installed, extension enabled) without any polling.
    func menuOpened() {
        guard !settingUp else { return }
        Task { await refreshStatus() }
    }

    func openFUSETDownload() {
        NSWorkspace.shared.open(URL(string: "https://www.fuse-t.org")!)
    }

    func openExtensionsSettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.LoginItems-Settings.extension")!)
    }

    func openLog() {
        let home = FileManager.default.homeDirectoryForCurrentUser
        NSWorkspace.shared.open(home.appendingPathComponent("Library/Logs/avocadomtp.log"))
    }

    /// Runs blocking work off the main thread and reports errors in the menu.
    private func perform(_ work: @escaping @Sendable () throws -> Void, then done: (() -> Void)? = nil) {
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
            done?()
        }
    }
}
