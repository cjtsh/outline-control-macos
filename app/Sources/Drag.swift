import AppKit

/// The server id being dragged, as a private pasteboard type so a drag from
/// another app can never be mistaken for a card move.
extension NSPasteboard.PasteboardType {
    static let outlineCard = NSPasteboard.PasteboardType("sh.cjt.outline.card")
}

/// The grip you grab. It only starts a drag once the pointer has moved a few
/// points, so a click still does nothing and the card's buttons stay clickable.
final class DragHandle: NSView {
    var serverID: String = ""
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

        let item = NSPasteboardItem()
        item.setString(serverID, forType: .outlineCard)
        let dragging = NSDraggingItem(pasteboardWriter: item)
        dragging.setDraggingFrame(bounds.insetBy(dx: -6, dy: -6), contents: snapshot())
        beginDraggingSession(with: [dragging], event: event, source: self)
    }

    private func snapshot() -> NSImage {
        let size = NSSize(width: max(bounds.width, 22), height: max(bounds.height, 28))
        let image = NSImage(size: size)
        image.lockFocus()
        Palette.card.setFill()
        NSBezierPath(roundedRect: NSRect(origin: .zero, size: size), xRadius: 6, yRadius: 6).fill()
        draw(NSRect(origin: .zero, size: size))
        image.unlockFocus()
        return image
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

extension DragHandle: NSDraggingSource {
    func draggingSession(_ session: NSDraggingSession,
                         sourceOperationMaskFor context: NSDraggingContext) -> NSDragOperation {
        .move
    }
}
