import AppKit

/// Legal surface for the app.
///
/// The terms themselves are not invented here: `LICENSE`, `DISCLAIMER.md` and
/// `FIRST-RUN-NOTICE.txt` are shipped inside the bundle, copied from
/// <https://github.com/cjtsh/software-disclaimer> at a pinned release. This file
/// only presents them and records that the user accepted.
enum Legal {
    /// Bumped when the upstream terms change, so the gate is shown again.
    static let termsVersion = "1.2.2"

    static let canonicalNotice = "https://github.com/cjtsh/software-disclaimer"
    static let canonicalTerms = "https://github.com/cjtsh/software-disclaimer/blob/main/DISCLAIMER.md"

    private static let acceptedKey = "legalAcceptedTermsVersion"
    private static let acceptedAtKey = "legalAcceptedAt"

    static var isAccepted: Bool {
        UserDefaults.standard.string(forKey: acceptedKey) == termsVersion
    }

    static var acceptedAt: Date? {
        let stamp = UserDefaults.standard.double(forKey: acceptedAtKey)
        return stamp > 0 ? Date(timeIntervalSince1970: stamp) : nil
    }

    static func recordAcceptance() {
        UserDefaults.standard.set(termsVersion, forKey: acceptedKey)
        UserDefaults.standard.set(Date().timeIntervalSince1970, forKey: acceptedAtKey)
    }

    /// The bundle's copy of a legal file, falling back to a short inline notice
    /// if the resources were not packaged (for example when the binary is run
    /// straight out of the build directory).
    static func text(named name: String) -> String? {
        let bundle = Bundle.main
        if let url = bundle.url(forResource: name, withExtension: nil),
           let text = try? String(contentsOf: url, encoding: .utf8) {
            return text
        }
        // Development fallback: app/Resources next to the executable.
        let sibling = bundle.bundleURL.appendingPathComponent("Resources/\(name)")
        if let text = try? String(contentsOf: sibling, encoding: .utf8) {
            return text
        }
        return nil
    }

    static var firstRunNotice: String {
        text(named: "FIRST-RUN-NOTICE.txt") ?? """
        THIS SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.
        It is experimental, free and open source. Use it at your own risk.
        Full terms: \(canonicalTerms)
        """
    }

    static var fullTerms: String {
        let disclaimer = text(named: "DISCLAIMER.md") ?? "See \(canonicalTerms)"
        let license = text(named: "LICENSE") ?? "MIT License. See \(canonicalNotice)/blob/main/LICENSE"
        return disclaimer + "\n\n" + String(repeating: "=", count: 78) + "\n\n" + license
    }
}

/// First-run gate. The main window is only built after `then(true)`, so the
/// terms genuinely stand between a new user and the daemon.
enum LegalGate {
    private static var retained: TermsWindowController?

    static func ensureAccepted(then: @escaping (Bool) -> Void) {
        if Legal.isAccepted {
            then(true)
            return
        }
        let controller = TermsWindowController(
            title: "Before you use Outline Control",
            preamble: """
            Outline Control is free and open-source software, published by Bitseeker LLC.

            It is experimental. It reconfigures this Mac's network while it is
            connected, it needs administrator rights to run, and it comes with no
            warranty. Please read the notice below before you continue.
            """,
            body: Legal.firstRunNotice,
            acceptTitle: "I accept — continue",
            declineTitle: "Quit"
        )
        controller.onDecision = { accepted in
            retained = nil
            if accepted {
                Legal.recordAcceptance()
            }
            then(accepted)
        }
        retained = controller
        controller.showWindow(nil)
        controller.window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}

/// A plain window that shows legal text, either as a gate or as a reader.
final class TermsWindowController: NSWindowController {
    var onDecision: ((Bool) -> Void)?

    init(title: String, preamble: String?, body: String, acceptTitle: String, declineTitle: String, closable: Bool = false) {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 640, height: 580),
            styleMask: closable ? [.titled, .closable] : [.titled],
            backing: .buffered,
            defer: false
        )
        window.title = title
        window.appearance = NSAppearance(named: .darkAqua)
        window.isReleasedWhenClosed = false
        super.init(window: window)
        build(preamble: preamble, body: body, acceptTitle: acceptTitle, declineTitle: declineTitle)
        window.center()
    }

    required init?(coder: NSCoder) { fatalError("not used") }

    private func build(preamble: String?, body: String, acceptTitle: String, declineTitle: String) {
        guard let window = window, let content = window.contentView else { return }

        var views: [NSView] = []

        if let preamble {
            let intro = NSTextField(wrappingLabelWithString: preamble)
            intro.font = .systemFont(ofSize: 12)
            intro.textColor = .secondaryLabelColor
            intro.maximumNumberOfLines = 0
            views.append(intro)
        }

        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = true
        scroll.backgroundColor = NSColor(calibratedWhite: 0.08, alpha: 1)
        scroll.borderType = .lineBorder
        scroll.wantsLayer = true

        let text = NSTextView()
        text.isEditable = false
        text.isSelectable = true
        text.drawsBackground = false
        text.textColor = .labelColor
        text.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        text.textContainerInset = NSSize(width: 14, height: 14)
        text.string = body
        text.autoresizingMask = [.width]
        text.textContainer?.widthTracksTextView = true
        text.isHorizontallyResizable = false
        text.minSize = NSSize(width: 0, height: 0)
        text.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        scroll.documentView = text
        views.append(scroll)

        let link = NSButton(title: "Read the full disclaimer online", target: self, action: #selector(openDisclaimer))
        link.isBordered = false
        link.contentTintColor = NSColor(calibratedRed: 0.30, green: 0.49, blue: 1.0, alpha: 1)
        link.font = .systemFont(ofSize: 11)
        link.alignment = .left
        views.append(link)

        let decline = NSButton(title: declineTitle, target: self, action: #selector(declineAction))
        decline.bezelStyle = .rounded
        decline.keyEquivalent = "\u{1b}"
        let accept = NSButton(title: acceptTitle, target: self, action: #selector(acceptAction))
        accept.bezelStyle = .rounded
        accept.keyEquivalent = "\r"
        accept.contentTintColor = NSColor(calibratedRed: 0.30, green: 0.49, blue: 1.0, alpha: 1)

        let buttons = NSStackView(views: [NSView(), decline, accept])
        buttons.orientation = .horizontal
        buttons.spacing = 10
        views.append(buttons)

        let stack = NSStackView(views: views)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 20),
            stack.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -20),
            stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 18),
            stack.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -18),
            scroll.widthAnchor.constraint(equalTo: stack.widthAnchor),
            buttons.widthAnchor.constraint(equalTo: stack.widthAnchor),
            link.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor),
        ])
    }

    @objc private func acceptAction() {
        onDecision?(true)
    }

    @objc private func declineAction() {
        onDecision?(false)
    }

    func runModal() -> NSApplication.ModalResponse {
        // Kept for the read-only terms reader, which has nothing to gate.
        guard let window = window else { return .cancel }
        NSApp.runModal(for: window)
        return .OK
    }

    @objc private func openDisclaimer() {
        if let url = URL(string: Legal.canonicalTerms) {
            NSWorkspace.shared.open(url)
        }
    }
}
