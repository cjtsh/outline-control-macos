import AppKit

// MARK: - Palette

/// A deliberately cooler palette than Outline's grey-green.
enum Palette {
    static let window      = NSColor(srgbRed: 0.039, green: 0.055, blue: 0.118, alpha: 1) // #0A0E1E
    static let card        = NSColor(srgbRed: 0.078, green: 0.102, blue: 0.184, alpha: 1) // #141A2F
    static let cardEdge    = NSColor(srgbRed: 0.145, green: 0.180, blue: 0.290, alpha: 1)
    static let accent      = NSColor(srgbRed: 0.298, green: 0.490, blue: 1.000, alpha: 1) // #4C7DFF
    static let text        = NSColor(srgbRed: 0.910, green: 0.930, blue: 0.975, alpha: 1)
    static let secondary   = NSColor(srgbRed: 0.580, green: 0.630, blue: 0.750, alpha: 1)
    static let tertiary    = NSColor(srgbRed: 0.420, green: 0.470, blue: 0.600, alpha: 1)
    static let green       = NSColor(srgbRed: 0.230, green: 0.820, blue: 0.520, alpha: 1)
    static let amber       = NSColor(srgbRed: 0.960, green: 0.670, blue: 0.180, alpha: 1)
    static let red         = NSColor(srgbRed: 0.945, green: 0.330, blue: 0.380, alpha: 1)
}

func label(_ text: String, size: CGFloat, weight: NSFont.Weight = .regular,
           color: NSColor = Palette.text) -> NSTextField {
    let field = NSTextField(labelWithString: text)
    field.font = .systemFont(ofSize: size, weight: weight)
    field.textColor = color
    field.lineBreakMode = .byTruncatingTail
    field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
    return field
}

func wrappingLabel(_ text: String, size: CGFloat, color: NSColor) -> NSTextField {
    let field = label(text, size: size, color: color)
    field.lineBreakMode = .byWordWrapping
    field.maximumNumberOfLines = 0
    field.cell?.wraps = true
    field.cell?.isScrollable = false
    return field
}

func monoLabel(_ text: String, size: CGFloat = 11, color: NSColor = Palette.tertiary) -> NSTextField {
    let field = label(text, size: size, color: color)
    field.font = .monospacedDigitSystemFont(ofSize: size, weight: .regular)
    return field
}

func spacer() -> NSView {
    let view = NSView()
    view.setContentHuggingPriority(.init(1), for: .horizontal)
    view.setContentCompressionResistancePriority(.init(1), for: .horizontal)
    view.translatesAutoresizingMaskIntoConstraints = false
    return view
}

// MARK: - Controls

final class DotView: NSView {
    var color: NSColor = Palette.tertiary {
        didSet { layer?.backgroundColor = color.cgColor }
    }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.cornerRadius = 4
        layer?.backgroundColor = color.cgColor
        translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            widthAnchor.constraint(equalToConstant: 8),
            heightAnchor.constraint(equalToConstant: 8),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }
}

enum ButtonStyle: Equatable { case filled, outline, quiet }

final class ActionButton: NSButton {
    private var style: ButtonStyle
    private var widthConstraint: NSLayoutConstraint?
    var onClick: (() -> Void)?

    init(title: String, style: ButtonStyle) {
        self.style = style
        super.init(frame: .zero)
        isBordered = false
        wantsLayer = true
        layer?.cornerRadius = 7
        translatesAutoresizingMaskIntoConstraints = false
        setContentHuggingPriority(.defaultHigh, for: .horizontal)
        heightAnchor.constraint(equalToConstant: 28).isActive = true
        target = self
        action = #selector(tapped)
        styleTitle(title)
        refreshAppearance()
    }

    required init?(coder: NSCoder) { fatalError() }

    func setStyle(_ new: ButtonStyle) {
        guard new != style else { return }
        style = new
        refreshAppearance()
    }

    func styleTitle(_ title: String) {
        let attr = NSAttributedString(string: title, attributes: [
            .foregroundColor: style == .outline ? Palette.accent : NSColor.white,
            .font: NSFont.systemFont(ofSize: 11, weight: .semibold),
            .kern: 0.6,
        ])
        attributedTitle = attr
        widthConstraint?.isActive = false
        let width = ceil(attr.size().width) + 30
        let constraint = widthAnchor.constraint(equalToConstant: max(64, width))
        constraint.isActive = true
        widthConstraint = constraint
    }

    @objc private func tapped() { onClick?() }

    private func refreshAppearance() {
        switch style {
        case .filled:
            layer?.backgroundColor = (isEnabled ? Palette.accent : Palette.accent.withAlphaComponent(0.22)).cgColor
            layer?.borderWidth = 0
        case .outline:
            layer?.backgroundColor = NSColor.clear.cgColor
            layer?.borderWidth = 1
            layer?.borderColor = Palette.accent.withAlphaComponent(isEnabled ? 0.55 : 0.20).cgColor
        case .quiet:
            layer?.backgroundColor = NSColor.white.withAlphaComponent(isEnabled ? 0.06 : 0.03).cgColor
            layer?.borderWidth = 0
        }
        alphaValue = isEnabled ? 1 : 0.5
    }

    override var isEnabled: Bool {
        didSet { refreshAppearance() }
    }
}

// MARK: - Service panel

/// The daemon's own controls: is it running, will it come back at boot, which
/// exit should it use, and a way to restart or switch the whole thing off.
final class ServicePanel: NSView {
    private let dot = DotView()
    private let title = label("Service", size: 12, weight: .semibold)
    private let detail = monoLabel("", size: 10)
    private let restart = ActionButton(title: "Restart", style: .quiet)
    private let power = ActionButton(title: "Stop service", style: .quiet)

    private let bootLabel = label("Start at boot", size: 11, color: Palette.secondary)
    private let bootSwitch = NSSwitch()
    private let defaultLabel = label("Boot exit", size: 11, color: Palette.secondary)
    private let defaultPicker = NSPopUpButton(frame: .zero, pullsDown: false)

    var onRestart: (() -> Void)?
    var onPower: (() -> Void)?
    var onBootToggle: ((Bool) -> Void)?
    var onDefaultChange: ((String?) -> Void)?
    private var servers: [ServerView] = []

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.cornerRadius = 12
        layer?.backgroundColor = Palette.card.cgColor
        layer?.borderWidth = 1
        layer?.borderColor = Palette.cardEdge.cgColor

        bootSwitch.controlSize = .small
        bootSwitch.target = self
        bootSwitch.action = #selector(bootToggled)

        defaultPicker.controlSize = .small
        defaultPicker.font = .systemFont(ofSize: 11)
        defaultPicker.target = self
        defaultPicker.action = #selector(defaultPicked)
        defaultPicker.isEnabled = true

        restart.onClick = { [weak self] in self?.onRestart?() }
        power.onClick = { [weak self] in self?.onPower?() }

        let row1 = NSStackView(views: [dot, title, detail, spacer(), restart, power])
        row1.orientation = .horizontal
        row1.alignment = .centerY
        row1.spacing = 8

        let row2 = NSStackView(views: [bootLabel, bootSwitch, spacer(), defaultLabel, defaultPicker])
        row2.orientation = .horizontal
        row2.alignment = .centerY
        row2.spacing = 8

        let rows = NSStackView(views: [row1, row2])
        rows.orientation = .vertical
        rows.alignment = .leading
        rows.spacing = 10
        rows.translatesAutoresizingMaskIntoConstraints = false
        addSubview(rows)

        NSLayoutConstraint.activate([
            rows.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            rows.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            rows.topAnchor.constraint(equalTo: topAnchor, constant: 12),
            rows.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -12),
            row1.widthAnchor.constraint(equalTo: rows.widthAnchor),
            row2.widthAnchor.constraint(equalTo: rows.widthAnchor),
            defaultPicker.widthAnchor.constraint(greaterThanOrEqualToConstant: 140),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    @objc private func bootToggled() { onBootToggle?(bootSwitch.state == .on) }
    @objc private func defaultPicked() {
        let index = defaultPicker.indexOfSelectedItem
        onDefaultChange?(index <= 0 ? nil : servers[index - 1].id)
    }

    func update(settings: Settings, service: ServiceInfo?, servers: [ServerView]) {
        self.servers = servers
        let suspended = settings.suspended

        title.stringValue = suspended ? "Service stopped" : "Service running"
        title.textColor = suspended ? Palette.secondary : Palette.text
        dot.color = suspended ? Palette.tertiary : Palette.green

        if suspended {
            detail.stringValue = "VPN released · no auto-connect"
        } else if let service {
            let uptime = service.uptime.map { " · up \($0)" } ?? ""
            detail.stringValue = "pid \(service.pid) · \(service.mode) · \(service.engineName)\(uptime)"
        } else {
            detail.stringValue = ""
        }

        power.styleTitle(suspended ? "Start service" : "Stop service")
        restart.isEnabled = !suspended
        bootSwitch.state = settings.autoconnect ? .on : .off
        bootSwitch.isEnabled = true

        let names = ["None"] + servers.map { $0.name }
        if defaultPicker.itemTitles != names {
            defaultPicker.removeAllItems()
            defaultPicker.addItems(withTitles: names)
        }
        if let id = settings.defaultServer,
           let index = servers.firstIndex(where: { $0.id == id }) {
            defaultPicker.selectItem(at: index + 1)
        } else {
            defaultPicker.selectItem(at: 0)
        }
        defaultPicker.isEnabled = !servers.isEmpty
        defaultLabel.textColor = defaultPicker.isEnabled ? Palette.secondary : Palette.tertiary
        bootLabel.textColor = bootSwitch.isEnabled ? Palette.secondary : Palette.tertiary
    }
}

// MARK: - Cards

final class CardView: NSView {
    private let dot = DotView()
    private let nameLabel = label("", size: 15, weight: .semibold)
    private let addressLabel = monoLabel("")
    private let stateLabel = label("", size: 11, weight: .medium, color: Palette.secondary)
    private let primary = ActionButton(title: "CONNECT", style: .filled)
    private let more = ActionButton(title: "⋯", style: .quiet)
    private let defaultTag = label("DEFAULT", size: 9, weight: .semibold, color: Palette.accent)
    private let rows = NSStackView()

    var onPrimary: (() -> Void)?
    var onRename: (() -> Void)?
    var onForget: (() -> Void)?
    var onMakeDefault: (() -> Void)?

    override init(frame: NSRect) {
        super.init(frame: frame)

        wantsLayer = true
        layer?.cornerRadius = 12
        layer?.backgroundColor = Palette.card.cgColor
        layer?.borderWidth = 1
        layer?.borderColor = Palette.cardEdge.cgColor

        primary.onClick = { [weak self] in self?.onPrimary?() }
        more.onClick = { [weak self] in self?.showMenu() }

        let top = NSStackView(views: [dot, nameLabel, spacer(), stateLabel])
        top.orientation = .horizontal
        top.spacing = 8
        top.alignment = .centerY

        let bottom = NSStackView(views: [addressLabel, defaultTag, spacer(), more, primary])
        bottom.orientation = .horizontal
        bottom.spacing = 8
        bottom.alignment = .centerY

        rows.orientation = .vertical
        rows.spacing = 10
        rows.alignment = .leading
        rows.translatesAutoresizingMaskIntoConstraints = false
        rows.addArrangedSubview(top)
        rows.addArrangedSubview(bottom)
        addSubview(rows)

        NSLayoutConstraint.activate([
            rows.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            rows.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            rows.topAnchor.constraint(equalTo: topAnchor, constant: 14),
            rows.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -14),
            top.widthAnchor.constraint(equalTo: rows.widthAnchor),
            bottom.widthAnchor.constraint(equalTo: rows.widthAnchor),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    private func showMenu() {
        let menu = NSMenu()
        let rename = menu.addItem(withTitle: "Rename…", action: #selector(renameTapped), keyEquivalent: "")
        rename.target = self
        let makeDefault = menu.addItem(withTitle: "Use at boot", action: #selector(makeDefaultTapped), keyEquivalent: "")
        makeDefault.target = self
        let forget = menu.addItem(withTitle: "Forget", action: #selector(forgetTapped), keyEquivalent: "")
        forget.target = self
        menu.popUp(positioning: nil, at: NSPoint(x: 0, y: -6), in: more)
    }

    @objc private func renameTapped() { onRename?() }
    @objc private func makeDefaultTapped() { onMakeDefault?() }
    @objc private func forgetTapped() { onForget?() }

    func update(server: ServerView, status: TunnelStatus, health: HealthView?,
                isDefault: Bool, serviceOn: Bool) {
        nameLabel.stringValue = server.name
        addressLabel.stringValue = server.address
        defaultTag.isHidden = !isDefault

        let mine = status.serverId == server.id
        if mine && status.isConnected {
            let since = status.connectedFor.map { " · \($0)" } ?? ""
            stateLabel.stringValue = "Connected\(since)"
            stateLabel.textColor = Palette.green
            dot.color = Palette.green
            primary.setStyle(.outline)
            primary.styleTitle("DISCONNECT")
            primary.isEnabled = true
        } else if mine && status.state == "connecting" {
            stateLabel.stringValue = "Connecting…"
            stateLabel.textColor = Palette.amber
            dot.color = Palette.amber
            primary.setStyle(.filled)
            primary.styleTitle("CONNECTING")
            primary.isEnabled = false
        } else if mine && status.state == "error" {
            stateLabel.stringValue = "Failed"
            stateLabel.textColor = Palette.red
            dot.color = Palette.red
            primary.setStyle(.filled)
            primary.styleTitle("RETRY")
            primary.isEnabled = true
        } else {
            switch health {
            case .none:
                stateLabel.stringValue = "Checking…"
                stateLabel.textColor = Palette.tertiary
                dot.color = Palette.tertiary
            case .some(let h) where h.reachable:
                stateLabel.stringValue = "Ready"
                stateLabel.textColor = Palette.secondary
                dot.color = Palette.accent
            case .some:
                stateLabel.stringValue = "Unreachable"
                stateLabel.textColor = Palette.red
                dot.color = Palette.red
            }
            primary.setStyle(.filled)
            primary.styleTitle("CONNECT")
            primary.isEnabled = serviceOn && status.state != "connecting"
        }
    }
}

final class MessageCard: NSView {
    init(title: String, body: String, command: String?, actionTitle: String, onAction: @escaping () -> Void) {
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 12
        layer?.backgroundColor = Palette.card.cgColor
        layer?.borderWidth = 1
        layer?.borderColor = Palette.cardEdge.cgColor

        let heading = label(title, size: 14, weight: .semibold)
        let text = wrappingLabel(body, size: 12, color: Palette.secondary)

        let stack = NSStackView()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.addArrangedSubview(heading)
        stack.addArrangedSubview(text)
        if let command {
            let code = monoLabel(command, size: 11, color: Palette.accent)
            code.isSelectable = true
            stack.addArrangedSubview(code)
        }
        let button = ActionButton(title: actionTitle, style: .filled)
        button.onClick = onAction
        stack.addArrangedSubview(button)
        addSubview(stack)

        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            stack.topAnchor.constraint(equalTo: topAnchor, constant: 14),
            stack.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -14),
            text.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }
}
