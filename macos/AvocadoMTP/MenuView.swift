import SwiftUI

/// The window that opens from the menu bar icon.
struct MenuView: View {
    @ObservedObject var model: AppModel
    /// The volume being renamed inline, and the name typed so far.
    @State private var renaming: UInt32?
    @State private var draft = ""
    @FocusState private var nameFieldFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            Divider()
            if model.settingUp {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Setting up the AvocadoMTP service…")
                }
            } else if !model.installed {
                notInstalled
            } else if !model.serviceRunning {
                Label("The service is stopped. Turn it on to see your phone.", systemImage: "pause.circle")
                    .foregroundStyle(.secondary)
            } else if let st = model.state {
                phone(st)
                if !st.mounts.isEmpty || st.connected { volumes(st) }
                if !st.transfers.isEmpty { transfers(st) }
            }
            if !model.settingUp && model.installed { checklist }
            if let msg = model.message {
                Label(msg, systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
                    .font(.callout)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Divider()
            footer
        }
        .padding(14)
        .frame(width: 340)
        .onAppear { model.menuOpened() }
    }

    // MARK: Sections

    private var header: some View {
        HStack(alignment: .center) {
            Image(nsImage: AvocadoIcon.template(active: model.state?.connected == true))
            Text("AvocadoMTP").font(.headline)
            Spacer()
            Toggle("", isOn: Binding(
                get: { model.serviceRunning },
                set: { model.setService(running: $0) }
            ))
            .toggleStyle(.switch)
            .controlSize(.small)
            .disabled(model.busy || !model.installed)
            .help(model.serviceRunning ? "Stop the service (volumes unmount)" : "Start the service")
        }
    }

    private var notInstalled: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("The service isn't installed yet.", systemImage: "wrench.and.screwdriver")
            if model.canInstallService {
                Button("Set up the service") { model.useBundledService() }
            } else {
                Text("Run `avocadomtp daemon install` once in Terminal.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
    }

    /// Shown only when something is missing; empty when all is well.
    @ViewBuilder private var checklist: some View {
        if let update = model.update {
            VStack(alignment: .leading, spacing: 4) {
                Label("AvocadoMTP \(update.version) is available.", systemImage: "arrow.down.app")
                    .font(.callout)
                HStack {
                    Button("Update", action: model.installUpdate)
                        .help(UpdateChecker.installedByHomebrew
                              ? "Runs brew upgrade in Terminal"
                              : "Runs the installer in Terminal")
                    Button("Release notes", action: model.openReleaseNotes)
                        .buttonStyle(.link)
                }
                .controlSize(.small)
            }
        }
        if model.volumes == .unavailable {
            setupRow("Finder volumes need FUSE-T (free).", icon: "externaldrive.badge.xmark",
                     action: "Get FUSE-T", model.openFUSETDownload)
        } else if model.volumes == .nfs {
            setupRow("Turn on fuse-t in File System Extensions for faster volumes.", icon: "bolt",
                     action: "Open Settings", model.openExtensionsSettings)
        }
        if model.devServiceInstalled && model.canInstallService {
            setupRow("A developer build of the service is installed.", icon: "hammer",
                     action: "Use this app's version", model.useBundledService)
        }
    }

    private func setupRow(_ text: String, icon: String, action: String, _ run: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Label(text, systemImage: icon)
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
            Button(action, action: run)
                .controlSize(.small)
        }
    }

    private func phone(_ st: ActivityState) -> some View {
        HStack(spacing: 8) {
            Image(systemName: st.connected ? "iphone.gen3" : st.asleep == true ? "moon.zzz" : "iphone.gen3.slash")
                .font(.title2)
                .foregroundStyle(st.connected ? Color.accentColor : .secondary)
            VStack(alignment: .leading, spacing: 1) {
                if st.connected, let d = st.device {
                    Text(d.model).font(.body.weight(.semibold))
                    Text(d.manufacturer).font(.caption).foregroundStyle(.secondary)
                } else if st.asleep == true {
                    Text("Your phone isn't answering").font(.body.weight(.semibold))
                    Text("Unlock it. If it stays like this, unplug the cable and plug it in again.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    Button("Try again") { model.mountAll() }
                        .buttonStyle(.link)
                        .font(.caption)
                        .disabled(model.busy)
                } else {
                    Text("No phone connected").font(.body.weight(.semibold))
                    Text("Unlock it and choose “File transfer” in its USB notification.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer()
            if st.connected, st.rateIn + st.rateOut > 0 {
                VStack(alignment: .trailing, spacing: 1) {
                    if st.rateIn > 0 { Text("↓ " + Format.rate(st.rateIn)) }
                    if st.rateOut > 0 { Text("↑ " + Format.rate(st.rateOut)) }
                }
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
            }
        }
    }

    private func volumes(_ st: ActivityState) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Volumes").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                Spacer()
                if st.connected {
                    if !st.mounts.isEmpty {
                        Button("Eject all") { model.ejectAll(st.mounts) }
                            .buttonStyle(.link)
                            .font(.caption)
                            .disabled(model.busy)
                            .help("Unmount every volume (safe to unplug afterwards)")
                    }
                    Button("Mount all") { model.mountAll() }
                        .buttonStyle(.link)
                        .font(.caption)
                        .disabled(model.busy)
                }
            }
            if st.mounts.isEmpty {
                Text("Nothing mounted").font(.callout).foregroundStyle(.secondary)
            }
            ForEach(st.mounts) { m in
                HStack(spacing: 8) {
                    Image(systemName: "externaldrive.fill").foregroundStyle(.secondary)
                    if renaming == m.storageId {
                        TextField("Name", text: $draft)
                            .textFieldStyle(.roundedBorder)
                            .focused($nameFieldFocused)
                            .onSubmit {
                                model.rename(m, to: draft)
                                renaming = nil
                            }
                            .onExitCommand { renaming = nil }
                            .help("Return saves, Esc cancels. Empty goes back to the phone's name.")
                    } else {
                        Button {
                            model.reveal(m)
                        } label: {
                            VStack(alignment: .leading, spacing: 1) {
                                Text(m.name).foregroundStyle(.primary)
                                Text(m.path.replacingOccurrences(of: NSHomeDirectory(), with: "~"))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                        }
                        .buttonStyle(.plain)
                        .help("Open in Finder · right-click to rename")
                    }
                    Spacer()
                    Button {
                        model.eject(m)
                    } label: {
                        Image(systemName: "eject.fill")
                    }
                    .buttonStyle(.borderless)
                    .disabled(model.busy)
                    .help("Eject \(m.name)")
                }
                .contextMenu {
                    Button("Rename…") {
                        draft = m.name
                        renaming = m.storageId
                        DispatchQueue.main.async { nameFieldFocused = true } // once the field exists
                    }
                    Button("Use the Phone's Name") { model.rename(m, to: "") }
                    Divider()
                    Button("Show in Finder") { model.reveal(m) }
                    Button("Eject") { model.eject(m) }
                }
            }
        }
    }

    private func transfers(_ st: ActivityState) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Transfers").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            ForEach(st.transfers) { t in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Image(systemName: t.toPhone ? "arrow.up.circle" : "arrow.down.circle")
                            .foregroundStyle(.secondary)
                            .help(t.toPhone ? "To the phone" : "From the phone")
                        Text((t.path as NSString).lastPathComponent)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Spacer()
                        Text(status(t))
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(t.state == "failed" ? .red : .secondary)
                    }
                    if let p = t.progress, t.state == "active" {
                        ProgressView(value: p).controlSize(.small)
                    }
                }
            }
        }
    }

    private func status(_ t: ActivityState.Transfer) -> String {
        switch t.state {
        case "done": return "Done · " + Format.bytes(t.bytes)
        case "failed": return "Failed"
        default:
            if t.total > 0 {
                return "\(Format.bytes(t.bytes)) of \(Format.bytes(t.total)) · \(Format.rate(t.rate))"
            }
            return Format.bytes(t.bytes) + " · " + Format.rate(t.rate)
        }
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: 8) {
            Toggle("Start at login", isOn: Binding(
                get: { model.autostart },
                set: { model.setAutostart($0) }
            ))
            .disabled(model.busy || !model.installed)
            .help("Start the service and this menu when you log in")
            HStack {
                Button("Show log") { model.openLog() }
                if !model.cliOnPath && model.installed {
                    Button("Install command-line tool…") { model.installCLI() }
                        .help("Adds avocadomtp to /usr/local/bin (asks for your password)")
                        .disabled(model.busy)
                }
                Spacer()
                Button("Quit menu") { NSApp.terminate(nil) }
                    .help("Quit this menu; the service keeps running")
            }
            .buttonStyle(.link)
            .font(.callout)
        }
    }
}
