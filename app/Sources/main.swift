import AppKit

/// Top-left origin inside the scroll view, so a long list starts at the top.
final class FlippedDocument: NSView {
    override var isFlipped: Bool { true }
}

final class MainWindowController: NSWindowController {
    private var api: OutlineAPI?
    private var servers: [ServerView] = []
    private var status = TunnelStatus.empty
    private var settings = Settings.empty
    private var service: ServiceInfo?
    private var health: [String: HealthView] = [:]
    private var providerUp = true
    private var providerError = ""
    private var signature = ""
    private var ticks = 0
    private var timer: Timer?

    private let stack = NSStackView()
    private let scroll = NSScrollView()
    private let headerState = label("", size: 12, color: Palette.secondary)
    private let footerText = label("", size: 11, color: Palette.tertiary)
    private let footerDot = DotView()
    private let countLabel = label("", size: 11, color: Palette.tertiary)
    private let panel = ServicePanel()

    convenience init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 580, height: 660),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false)
        window.title = "Outline Control"
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.isMovableByWindowBackground = true
        window.minSize = NSSize(width: 480, height: 420)
        window.backgroundColor = Palette.window
        window.center()
        self.init(window: window)
        build()
        start()
    }

    // MARK: layout

    private func build() {
        guard let window else { return }

        let root = NSView()
        root.wantsLayer = true
        root.layer?.backgroundColor = Palette.window.cgColor
        window.contentView = root

        let title = label("Outline Control", size: 22, weight: .bold)
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        let versionChip = monoLabel("v\(version)", size: 10, color: Palette.tertiary)
        versionChip.font = .monospacedSystemFont(ofSize: 10, weight: .semibold)
        versionChip.setContentCompressionResistancePriority(.required, for: .horizontal)
        versionChip.toolTip = "Outline Control \(version) — the same number as the release on GitHub"
        let titleRow = NSStackView(views: [title, versionChip, spacer()])
        titleRow.orientation = .horizontal
        titleRow.alignment = .firstBaseline
        titleRow.spacing = 8
        let subtitle = label("Exits for this Mac", size: 12, color: Palette.tertiary)
        let titleStack = NSStackView(views: [titleRow, subtitle])
        titleStack.orientation = .vertical
        titleStack.alignment = .leading
        titleStack.spacing = 2

        let add = ActionButton(title: "＋  Add exit", style: .filled)
        add.onClick = { [weak self] in self?.addExit() }

        let headerRow = NSStackView(views: [titleStack, spacer(), add])
        headerRow.orientation = .horizontal
        headerRow.alignment = .centerY
        headerRow.spacing = 12
        headerRow.translatesAutoresizingMaskIntoConstraints = false

        headerState.translatesAutoresizingMaskIntoConstraints = false
        headerState.lineBreakMode = .byTruncatingTail

        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false

        let document = FlippedDocument()
        document.translatesAutoresizingMaskIntoConstraints = false
        document.addSubview(stack)

        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.documentView = document
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.scrollerStyle = .overlay

        footerDot.translatesAutoresizingMaskIntoConstraints = false
        footerText.translatesAutoresizingMaskIntoConstraints = false
        countLabel.translatesAutoresizingMaskIntoConstraints = false
        let footer = NSStackView(views: [footerDot, footerText, spacer(), countLabel])
        footer.orientation = .horizontal
        footer.alignment = .centerY
        footer.spacing = 8
        footer.translatesAutoresizingMaskIntoConstraints = false

        panel.translatesAutoresizingMaskIntoConstraints = false
        panel.onRestart = { [weak self] in self?.restartService() }
        panel.onPower = { [weak self] in self?.toggleService() }
        panel.onBootToggle = { [weak self] in self?.setAutoconnect($0) }
        panel.onDefaultChange = { [weak self] in self?.setDefault($0) }

        root.addSubview(headerRow)
        root.addSubview(headerState)
        root.addSubview(scroll)
        root.addSubview(panel)
        root.addSubview(footer)

        NSLayoutConstraint.activate([
            headerRow.topAnchor.constraint(equalTo: root.topAnchor, constant: 34),
            headerRow.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24),
            headerRow.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24),

            headerState.topAnchor.constraint(equalTo: headerRow.bottomAnchor, constant: 6),
            headerState.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24),
            headerState.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24),

            scroll.topAnchor.constraint(equalTo: headerState.bottomAnchor, constant: 16),
            scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 20),
            scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -20),
            scroll.bottomAnchor.constraint(equalTo: panel.topAnchor, constant: -12),

            panel.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 20),
            panel.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -20),
            panel.bottomAnchor.constraint(equalTo: footer.topAnchor, constant: -12),

            document.topAnchor.constraint(equalTo: scroll.contentView.topAnchor),
            document.leadingAnchor.constraint(equalTo: scroll.contentView.leadingAnchor),
            document.trailingAnchor.constraint(equalTo: scroll.contentView.trailingAnchor),
            document.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor),
            document.heightAnchor.constraint(greaterThanOrEqualTo: scroll.contentView.heightAnchor),

            stack.topAnchor.constraint(equalTo: document.topAnchor, constant: 4),
            stack.leadingAnchor.constraint(equalTo: document.leadingAnchor, constant: 4),
            stack.trailingAnchor.constraint(equalTo: document.trailingAnchor, constant: -4),
            document.bottomAnchor.constraint(greaterThanOrEqualTo: stack.bottomAnchor, constant: 4),

            footer.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24),
            footer.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24),
            footer.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -16),
            footer.heightAnchor.constraint(equalToConstant: 20),
        ])
    }

    // MARK: data

    private func start() {
        if let token = OutlineAPI.loadToken() {
            api = OutlineAPI(token: token)
        }
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1.5, repeats: true) { [weak self] _ in
            self?.refresh()
        }
        timer?.tolerance = 0.4
    }

    private func refresh() {
        ticks += 1
        guard let api else {
            providerUp = false
            providerError = "No token at ~/Library/Application Support/Outline Control/token"
            rebuildIfNeeded()
            return
        }
        api.servers { result in
            switch result {
            case .failure(let message):
                self.providerUp = false
                self.providerError = message.text
                self.rebuildIfNeeded()
            case .success(let list):
                self.servers = list
                self.providerUp = true
                self.providerError = ""
                api.status { statusResult in
                    if case .success(let envelope) = statusResult {
                        self.status = envelope.tunnel
                        self.settings = envelope.settings ?? .empty
                        self.service = envelope.service
                    }
                    if self.ticks % 4 == 1 || self.health.isEmpty {
                        api.health { healthResult in
                            if case .success(let entries) = healthResult {
                                self.health = Dictionary(uniqueKeysWithValues: entries.map { ($0.id, $0) })
                            }
                            self.rebuildIfNeeded()
                        }
                    } else {
                        self.rebuildIfNeeded()
                    }
                }
            }
        }
    }

    private func signatureNow() -> String {
        var parts: [String] = []
        parts.append(providerUp ? "up" : "down:\(providerError)")
        parts.append("\(status.state)|\(status.serverId ?? "-")|\(status.connectedFor ?? "-")|\(status.lastError ?? "-")")
        parts.append("svc:\(settings.autoconnect)|\(settings.defaultServer ?? "-")|\(settings.suspended)|\(service?.pid ?? 0)|\(service?.uptime ?? "-")")
        for server in servers {
            let h = health[server.id].map { $0.reachable ? "ok" : "bad" } ?? "?"
            parts.append("\(server.id)|\(server.name)|\(server.address)|\(h)")
        }
        return parts.joined(separator: "·")
    }

    private func rebuildIfNeeded() {
        let sig = signatureNow()
        guard sig != signature else { return }
        signature = sig
        rebuild()
    }

    private func rebuild() {
        for view in stack.arrangedSubviews { view.removeFromSuperview() }

        guard providerUp else {
            let card = MessageCard(
                title: "The provider is not running",
                body: providerError.isEmpty
                    ? "The background service that owns the tunnel is not answering on 127.0.0.1:9911."
                    : providerError,
                command: "launchctl kickstart gui/\(getuid())/sh.cjt.outline-control",
                actionTitle: "Try again",
                onAction: { [weak self] in self?.refresh() })
            stack.addArrangedSubview(card)
            card.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
            headerState.stringValue = "Waiting for the provider…"
            headerState.textColor = Palette.amber
            footerDot.color = Palette.red
            footerText.stringValue = "provider offline"
            countLabel.stringValue = ""
            return
        }

        if servers.isEmpty {
            let card = MessageCard(
                title: "No exits yet",
                body: "Paste an Outline access key to add your first server. Keys start with ss:// and are stored on this Mac only.",
                command: nil,
                actionTitle: "Add exit",
                onAction: { [weak self] in self?.addExit() })
            stack.addArrangedSubview(card)
            card.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
        }

        panel.update(settings: settings, service: service, servers: servers)

        for server in servers {
            let card = CardView()
            card.update(server: server, status: status, health: health[server.id],
                        isDefault: settings.defaultServer == server.id,
                        serviceOn: !settings.suspended)
            card.onPrimary = { [weak self] in self?.toggle(server) }
            card.onRename = { [weak self] in self?.rename(server) }
            card.onForget = { [weak self] in self?.forget(server) }
            card.onMakeDefault = { [weak self] in self?.setDefault(server.id) }
            stack.addArrangedSubview(card)
            card.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
        }

        if settings.suspended {
            headerState.stringValue = "Service stopped — the VPN is off and will not reconnect by itself"
            headerState.textColor = Palette.secondary
            footerDot.color = Palette.tertiary
            footerText.stringValue = "SERVICE OFF"
        } else if status.isConnected, let name = status.serverName {
            let since = status.connectedFor.map { " · \($0)" } ?? ""
            headerState.stringValue = "Connected to \(name)\(since)"
            headerState.textColor = Palette.green
            footerDot.color = Palette.green
            if status.isFullTunnel {
                footerText.stringValue = "FULL TUNNEL  ·  all traffic  ·  " +
                    (status.backend.map { ($0 as NSString).lastPathComponent } ?? "sing-box")
            } else {
                footerText.stringValue = "SOCKS \(status.socks)" + (status.backend.map { backend in
                    "  ·  " + (backend as NSString).lastPathComponent
                } ?? "")
            }
        } else if status.state == "error" {
            headerState.stringValue = status.lastError ?? "Tunnel failed"
            headerState.textColor = Palette.red
            footerDot.color = Palette.red
            footerText.stringValue = "no tunnel"
        } else {
            headerState.stringValue = status.isFullTunnel
                ? "Not connected — this Mac is using its normal network"
                : "Not connected — your normal network is in use"
            headerState.textColor = Palette.secondary
            footerDot.color = Palette.tertiary
            footerText.stringValue = status.isFullTunnel ? "FULL TUNNEL  ·  ready" : "SOCKS \(status.socks)"
        }
        countLabel.stringValue = servers.count == 1 ? "1 exit" : "\(servers.count) exits"
    }

    // MARK: actions

    private func toggle(_ server: ServerView) {
        guard let api else { return }
        let isMine = status.serverId == server.id && status.isConnected
        if isMine {
            api.disconnect { result in
                if case .failure(let message) = result { self.presentError(message.text) }
                self.refresh()
            }
        } else {
            api.connect(server.id) { result in
                if case .failure(let message) = result { self.presentError(message.text) }
                self.refresh()
            }
        }
    }

    private func setAutoconnect(_ on: Bool) {
        api?.patchSettings(["autoconnect": on]) { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func setDefault(_ id: String?) {
        api?.patchSettings(["defaultServer": id ?? ""]) { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func toggleService() {
        let turningOn = settings.suspended
        api?.setService(on: turningOn) { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func restartService() {
        if service?.supervised == false {
            let alert = NSAlert()
            alert.alertStyle = .warning
            alert.messageText = "Restart the provider?"
            alert.informativeText = "It was not started by launchd, so restarting it will leave it stopped. "
                + "You would have to start it again yourself."
            alert.addButton(withTitle: "Restart anyway")
            alert.addButton(withTitle: "Cancel")
            guard alert.runModal() == .alertFirstButtonReturn else { return }
        }
        api?.restartService { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func addExit() {
        let alert = NSAlert()
        alert.messageText = "Add VPN access key"
        alert.informativeText = "Paste the key from your Outline server. It looks like ss://… and stays on this Mac."
        alert.addButton(withTitle: "Add")
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 400, height: 24))
        field.placeholderString = "ss://…"
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        let key = field.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty else { return }
        api?.add(key: key, name: nil) { result in
            switch result {
            case .success: self.refresh()
            case .failure(let message): self.presentError(message.text)
            }
        }
    }

    private func rename(_ server: ServerView) {
        let alert = NSAlert()
        alert.messageText = "Rename exit"
        alert.informativeText = server.address
        alert.addButton(withTitle: "Save")
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 320, height: 24))
        field.stringValue = server.name
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        let name = field.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, name != server.name else { return }
        api?.rename(server.id, to: name) { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func forget(_ server: ServerView) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Forget \(server.name)?"
        alert.informativeText = "The access key is deleted from this Mac. The server itself is untouched."
        alert.addButton(withTitle: "Forget")
        alert.addButton(withTitle: "Cancel")
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        api?.forget(server.id) { result in
            if case .failure(let message) = result { self.presentError(message.text) }
            self.refresh()
        }
    }

    private func presentError(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "That didn't work"
        alert.informativeText = message
        alert.addButton(withTitle: "OK")
        alert.runModal()
    }
}

// MARK: - App

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var controller: MainWindowController?

    private var legalWindow: TermsWindowController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        buildMenu()

        // Nothing touches the network or the daemon until the terms are accepted.
        LegalGate.ensureAccepted { [weak self] accepted in
            guard let self else { return }
            guard accepted else {
                NSApp.terminate(nil)
                return
            }
            self.controller = MainWindowController()
            self.controller?.showWindow(nil)
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    @objc func showDisclaimer(_ sender: Any?) {
        if legalWindow == nil {
            legalWindow = TermsWindowController(
                title: "Disclaimer and License",
                preamble: "Terms version \(Legal.termsVersion). Published by Bitseeker LLC.",
                body: Legal.fullTerms,
                acceptTitle: "Close",
                declineTitle: "Close",
                closable: true
            )
        }
        legalWindow?.runModal()
        legalWindow?.close()
    }

    private func buildMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About Outline Control", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(withTitle: "Disclaimer and License…", action: #selector(showDisclaimer(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Hide Outline Control", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(withTitle: "Quit Outline Control", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        main.addItem(appItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        main.addItem(editItem)

        let windowItem = NSMenuItem()
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowMenu.addItem(.separator())
        windowMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowItem.submenu = windowMenu
        main.addItem(windowItem)

        NSApp.mainMenu = main
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
