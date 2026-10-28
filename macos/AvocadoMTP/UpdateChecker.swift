import AppKit
import Foundation

/// Looks up the latest release on GitHub and starts updates.
enum UpdateChecker {
    struct Release: Equatable {
        let version: String // "0.3.1"
        let page: URL
    }

    static let repo = "andymartinezot/avocadomtp"

    /// This app's version. The `UpdateCheckVersion` default overrides it, to
    /// test the update notice (`defaults write <bundle id> UpdateCheckVersion 0.0.1`).
    static var currentVersion: String {
        if let v = UserDefaults.standard.string(forKey: "UpdateCheckVersion"), !v.isEmpty { return v }
        return Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
    }

    /// The latest published release, or nil when GitHub can't be reached.
    static func latest() async -> Release? {
        var request = URLRequest(url: URL(string: "https://api.github.com/repos/\(repo)/releases/latest")!)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("AvocadoMTP/\(currentVersion)", forHTTPHeaderField: "User-Agent")
        request.timeoutInterval = 15
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              (response as? HTTPURLResponse)?.statusCode == 200,
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let tag = json["tag_name"] as? String
        else { return nil }
        return parse(tag: tag, page: json["html_url"] as? String)
    }

    /// "v1.2.3" → 1.2.3 with its release page; anything else is ignored. The
    /// page must be on github.com: it is opened in the browser.
    static func parse(tag: String, page: String?) -> Release? {
        let version = tag.hasPrefix("v") ? String(tag.dropFirst()) : tag
        let parts = version.split(separator: ".", omittingEmptySubsequences: false)
        guard (2...3).contains(parts.count),
              parts.allSatisfy({ !$0.isEmpty && $0.allSatisfy { $0.isASCII && $0.isNumber } })
        else { return nil }
        let fallback = URL(string: "https://github.com/\(repo)/releases/tag/v\(version)")!
        guard let page, let url = URL(string: page), url.scheme == "https", url.host == "github.com"
        else { return Release(version: version, page: fallback) }
        return Release(version: version, page: url)
    }

    /// True when Homebrew installed the app (`brew install --cask avocadomtp`).
    static var installedByHomebrew: Bool {
        ["/opt/homebrew/Caskroom/avocadomtp", "/usr/local/Caskroom/avocadomtp"].contains {
            FileManager.default.fileExists(atPath: $0)
        }
    }

    /// The commands the update runs in Terminal: the installer from the
    /// release's own tag, so what runs matches what the notice announced.
    static func updateScript(version: String) -> String {
        let update = installedByHomebrew
            ? """
              for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [ -x "$b" ] && eval "$("$b" shellenv)"; done
              brew update && brew upgrade --cask avocadomtp
              """
            : "curl -fsSL https://raw.githubusercontent.com/\(repo)/v\(version)/install.sh | bash"
        return """
            #!/bin/bash
            # Written by AvocadoMTP's "Update" button.
            clear
            \(update)
            echo
            echo "You can close this window."
            """
    }

    /// Writes the update script to the app's cache folder and opens it, which
    /// runs it in Terminal (no extra permission needed, unlike scripting Terminal).
    static func openUpdateInTerminal(version: String) throws {
        let dir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Caches/AvocadoMTP", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let file = dir.appendingPathComponent("update.command")
        try updateScript(version: version).write(to: file, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: file.path)
        guard NSWorkspace.shared.open(file) else { throw DaemonError.io("Terminal didn't open the update") }
    }
}
