# Homebrew cask for AvocadoMTP. This repository is its own tap:
#   brew tap andymartinezot/avocadomtp https://github.com/andymartinezot/avocadomtp
#   brew trust --cask andymartinezot/avocadomtp/avocadomtp
#   brew install --cask avocadomtp
# scripts/update-cask.sh X.Y.Z sets version and sha256 after each release.
cask "avocadomtp" do
  version "0.3.0"
  sha256 "7a7094f3fcee1bfafb8ef898ad5d12d5c735f6f84ee0868ee86d1aba5f9fd03b"

  url "https://github.com/andymartinezot/avocadomtp/releases/download/v#{version}/AvocadoMTP.zip"
  name "AvocadoMTP"
  desc "Android file transfer over USB: Finder volumes, menu bar app and CLI"
  homepage "https://github.com/andymartinezot/avocadomtp"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on cask: "fuse-t"
  depends_on macos: :sonoma

  app "AvocadoMTP.app"
  binary "#{appdir}/AvocadoMTP.app/Contents/Helpers/avocadomtp"

  # The app is ad-hoc signed, not notarized. Like install.sh (whose download
  # never gets the quarantine flag), skip the "Open Anyway" step; Homebrew has
  # already checked the sha256 above.
  postflight_steps do
    run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{appdir}}/AvocadoMTP.app"]
  end

  # These run on upgrades too: the service stops and its LaunchAgent goes, so
  # it can't come back at login after an uninstall. Homebrew reopens the app
  # after an upgrade (and it opens at login), and the app then installs and
  # starts the new service.
  uninstall launchctl:  "com.andymartinezot.avocadomtp",
            quit:       "com.andymartinezot.avocadomtp.menubar",
            login_item: "AvocadoMTP",
            trash:      "~/Library/LaunchAgents/com.andymartinezot.avocadomtp.plist"

  zap trash: [
    "~/Library/Application Support/AvocadoMTP",
    "~/Library/Caches/AvocadoMTP",
    "~/Library/Logs/avocadomtp.log",
    "~/Library/Preferences/com.andymartinezot.avocadomtp.menubar.plist",
  ]

  caveats <<~EOS
    Open AvocadoMTP once (Applications or Spotlight): it sets up the background
    service and adds the menu bar icon. `brew uninstall --zap --cask avocadomtp`
    removes everything, settings included.
  EOS
end
