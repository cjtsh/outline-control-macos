import AppKit

/// The server id being dragged, as a private pasteboard type so a drag from
/// another app can never be mistaken for a card move.
extension NSPasteboard.PasteboardType {
    static let outlineCard = NSPasteboard.PasteboardType("sh.cjt.outline.card")
}

/// The grip you grab. It only reports a drag once the pointer has moved a few
/// points, so a click still does nothing and the card's buttons stay clickable.
final class DragHandle: NSView {
    var serverID: String = ""
    var onBeginDrag: ((NSEvent) -> Void)?
    private var start: NSPoint?

    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        toolTip = "Drag to reorder"
    }

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: .openHand)
    }

    override func mouseDown(with event: NSEvent) {
        start = event.locationInWindow
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start else { return }
        let now = event.locationInWindow
        guard abs(now.x - start.x) > 3 || abs(now.y - start.y) > 3 else { return }
        self.start = nil
        onBeginDrag?(event)
    }

    override func draw(_ dirtyRect: NSRect) {
        let dot = NSColor(calibratedRed: 0.94, green: 0.83, blue: 0.25, alpha: 1.0)
        dot.setFill()
        for column in 0..<2 {
            for row in 0..<3 {
                let rect = NSRect(x: 3.5 + CGFloat(column) * 6.4, y: 4 + CGFloat(row) * 6.4,
                                  width: 3.8, height: 3.8)
                NSBezierPath(ovalIn: rect).fill()
            }
        }
    }
}

extension NSView {
    /// A drag image of this view at the display's backing scale.
    ///
    /// `cacheDisplay` is used rather than `lockFocus` because the latter renders
    /// at 1x and produces a blurry, wrongly sized image on a Retina screen.
    func dragImage() -> NSImage {
        guard let rep = bitmapImageRepForCachingDisplay(in: bounds) else {
            return NSImage(size: bounds.size)
        }
        cacheDisplay(in: bounds, to: rep)
        let image = NSImage(size: bounds.size)
        image.addRepresentation(rep)
        return image
    }
}
