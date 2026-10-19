import Foundation

/// Mirrors activity.State in internal/activity (the daemon's "watch" snapshots).
struct ActivityState: Decodable, Equatable {
    struct Device: Decodable, Equatable {
        let manufacturer: String
        let model: String
        let serial: String
    }
    struct Mount: Decodable, Equatable, Identifiable {
        let storageId: UInt32
        let name: String
        let path: String
        let backend: String
        var id: UInt32 { storageId }
    }
    struct Transfer: Decodable, Equatable, Identifiable {
        let id: UInt64
        let path: String
        let direction: String // "toPhone" or "fromPhone"
        let bytes: Int64
        let total: Int64
        let rate: Int64
        let state: String // "active", "done", "failed"
        let error: String?
        var toPhone: Bool { direction == "toPhone" }
        var progress: Double? { total > 0 ? min(1, Double(bytes) / Double(total)) : nil }
    }
    let seq: UInt64
    let connected: Bool
    let device: Device?
    let error: String?
    let mounts: [Mount]
    let transfers: [Transfer]
    let rateIn: Int64
    let rateOut: Int64
}

enum DaemonError: Error, LocalizedError {
    case notRunning
    case io(String)
    case daemon(String)

    var errorDescription: String? {
        switch self {
        case .notRunning: return "The AvocadoMTP service is not running."
        case .io(let s): return s
        case .daemon(let s): return s
        }
    }
}

/// A connection to the daemon's Unix socket speaking its framed protocol:
/// every message is a 4-byte big-endian length followed by JSON.
final class DaemonConnection {
    static var socketPath: String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return home + "/Library/Application Support/AvocadoMTP/avocadomtp.sock"
    }

    private let fd: Int32

    init(path: String = DaemonConnection.socketPath) throws {
        fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw DaemonError.io("socket() failed") }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else {
            close(fd)
            throw DaemonError.io("socket path too long")
        }
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
            raw[bytes.count] = 0
        }
        let ok = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        if ok != 0 {
            close(fd)
            throw DaemonError.notRunning
        }
    }

    deinit { close(fd) }

    /// Unblocks a reader stuck in readFrame (used to stop a watch).
    func shutdown() { Darwin.shutdown(fd, SHUT_RDWR) }

    func send(_ object: [String: Any]) throws {
        let body = try JSONSerialization.data(withJSONObject: object)
        var len = UInt32(body.count).bigEndian
        var frame = Data(bytes: &len, count: 4)
        frame.append(body)
        try frame.withUnsafeBytes { raw in
            var off = 0
            while off < raw.count {
                let n = write(fd, raw.baseAddress! + off, raw.count - off)
                if n <= 0 { throw DaemonError.io("connection to the service lost") }
                off += n
            }
        }
    }

    func readFrame() throws -> Data {
        let header = try readExact(4)
        let len = header.withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        return try readExact(Int(len))
    }

    private func readExact(_ n: Int) throws -> Data {
        var data = Data(count: n)
        var off = 0
        try data.withUnsafeMutableBytes { raw in
            while off < n {
                let r = read(fd, raw.baseAddress! + off, n - off)
                if r <= 0 { throw DaemonError.io("connection to the service lost") }
                off += r
            }
        }
        return data
    }

    /// Sends one request and checks the daemon's {"ok": …} response.
    func call(_ request: [String: Any]) throws {
        try send(request)
        let resp = try JSONSerialization.jsonObject(with: try readFrame()) as? [String: Any] ?? [:]
        if resp["ok"] as? Bool != true {
            throw DaemonError.daemon(resp["error"] as? String ?? "the service refused the request")
        }
    }
}

/// Runs the installed avocadomtp CLI (service control stays in one place: Go).
enum ServiceControl {
    static var binary: String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return home + "/Library/Application Support/AvocadoMTP/bin/avocadomtp"
    }

    /// The avocadomtp bundled in the app by scripts/build-app.sh. Xcode builds
    /// without it rely on a separately installed service, as before.
    static var helper: String? {
        let p = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/avocadomtp").path
        return FileManager.default.isExecutableFile(atPath: p) ? p : nil
    }

    /// Where "Install command-line tool…" links the CLI (daemon.CLILink).
    static let cliLink = "/usr/local/bin/avocadomtp"

    static var isInstalled: Bool { FileManager.default.isExecutableFile(atPath: binary) }

    @discardableResult
    static func run(_ args: [String], executable: String = binary) throws -> String {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: executable)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = out
        try p.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        let text = String(decoding: data, as: UTF8.self)
        if p.terminationStatus != 0 {
            throw DaemonError.daemon(text.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        return text
    }

    /// The version an avocadomtp binary reports: "1.2.3", "dev" for a plain
    /// go build, nil for builds from before the version command.
    static func version(of path: String) -> String? {
        guard let out = try? run(["version"], executable: path) else { return nil }
        return out.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// What the app should do with the installed service on launch.
    enum Setup: Equatable {
        case none           // up to date, or no helper to install from
        case install        // no service yet
        case update         // the helper is newer than the installed service
        case devInstalled   // a developer build is installed: never replaced on its own
    }

    static func setupNeeded(installed: Bool, installedVersion: String?, helperVersion: String?) -> Setup {
        guard let helperVersion, helperVersion != "dev" else { return .none }
        guard installed else { return .install }
        guard let installedVersion else { return .update } // predates `version`
        if installedVersion == "dev" { return .devInstalled }
        return isOlder(installedVersion, than: helperVersion) ? .update : .none
    }

    /// Compares dotted versions numerically ("0.10.0" > "0.9.1"); anything after
    /// the digits of a part ("1-rc1") is ignored.
    static func isOlder(_ a: String, than b: String) -> Bool {
        func parts(_ v: String) -> [Int] {
            v.split(separator: ".").map { Int($0.prefix { $0.isNumber }) ?? 0 }
        }
        let x = parts(a), y = parts(b)
        for i in 0..<max(x.count, y.count) {
            let l = i < x.count ? x[i] : 0, r = i < y.count ? y[i] : 0
            if l != r { return l < r }
        }
        return false
    }

    /// Installs or updates the service from the bundled helper. A stopped
    /// service stays stopped; autostart is launchd's switch and survives.
    static func installFromHelper(start: Bool) throws {
        guard let helper else { return }
        try run(["daemon", "install"] + (start ? [] : ["--no-start"]), executable: helper)
    }

    /// The parts of `avocadomtp daemon status` the menu uses.
    struct Status: Equatable {
        enum Volumes: Equatable { case fskit, nfs, unavailable, unknown }
        var running = false
        var autostart = true
        var volumes = Volumes.unknown
    }

    static func status() -> Status {
        guard let out = try? run(["daemon", "status"]) else { return Status() }
        return parseStatus(out)
    }

    static func parseStatus(_ out: String) -> Status {
        var st = Status()
        for line in out.split(separator: "\n") {
            if line.hasPrefix("daemon:") {
                st.running = line.contains("running")
            } else if line.hasPrefix("autostart:") {
                st.autostart = !line.contains("off")
            } else if line.hasPrefix("volumes:") {
                st.volumes = line.contains("fskit") ? .fskit
                    : line.contains("nfs") ? .nfs
                    : line.contains("unavailable") ? .unavailable : .unknown
            }
        }
        return st
    }

    /// True when some avocadomtp is already on the usual PATH (ours or not).
    static var cliOnPath: Bool {
        [cliLink, "/opt/homebrew/bin/avocadomtp"].contains {
            FileManager.default.fileExists(atPath: $0)
        }
    }

    /// Links /usr/local/bin/avocadomtp to the service's binary, asking for an
    /// administrator password (the folder is root-owned).
    static func installCLI() throws {
        func sh(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'" }
        let command = "mkdir -p /usr/local/bin && ln -sf \(sh(binary)) \(sh(cliLink))"
        let escaped = command.replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        do {
            try run(["-e", "do shell script \"\(escaped)\" with administrator privileges"],
                    executable: "/usr/bin/osascript")
        } catch DaemonError.daemon(let text) where text.contains("-128") {
            return // the user cancelled the password prompt
        }
    }
}
