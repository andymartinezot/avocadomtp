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

    static var isInstalled: Bool { FileManager.default.isExecutableFile(atPath: binary) }

    @discardableResult
    static func run(_ args: [String]) throws -> String {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: binary)
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

    /// Reads "autostart: on|off" from `avocadomtp daemon status`.
    static func autostart() -> Bool {
        guard let out = try? run(["daemon", "status"]) else { return true }
        return !out.contains("autostart: off")
    }
}
