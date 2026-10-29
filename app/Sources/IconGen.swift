import AppKit
import CoreGraphics
import Foundation

// Draws the app icon with Core Graphics: no binary art in the repo, the .icns
// is produced at build time and ignored by git.

let canvas: CGFloat = 1024

func color(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 0xff) / 255,
            green: CGFloat((hex >> 8) & 0xff) / 255,
            blue: CGFloat(hex & 0xff) / 255,
            alpha: alpha)
}

func renderMaster() -> CGImage? {
    let space = CGColorSpaceCreateDeviceRGB()
    guard let ctx = CGContext(data: nil, width: Int(canvas), height: Int(canvas),
                              bitsPerComponent: 8, bytesPerRow: 0, space: space,
                              bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
    ctx.setAllowsAntialiasing(true)

    // Squircle body, Big Sur proportions.
    let body = CGRect(x: 100, y: 100, width: 824, height: 824)
    let bodyPath = CGPath(roundedRect: body, cornerWidth: 230, cornerHeight: 230, transform: nil)

    ctx.saveGState()
    ctx.addPath(bodyPath)
    ctx.clip()
    let bg = CGGradient(colorsSpace: space,
                        colors: [color(0x1E2C63), color(0x0A0F26), color(0x05070F)] as CFArray,
                        locations: [0, 0.55, 1])!
    ctx.drawLinearGradient(bg, start: CGPoint(x: 0, y: canvas), end: CGPoint(x: 0, y: 0), options: [])

    // Blue glow behind the glyph.
    let glow = CGGradient(colorsSpace: space,
                          colors: [color(0x3F6BFF, 0.55), color(0x3F6BFF, 0.0)] as CFArray,
                          locations: [0, 1])!
    ctx.drawRadialGradient(glow,
                           startCenter: CGPoint(x: 512, y: 512), startRadius: 0,
                           endCenter: CGPoint(x: 512, y: 512), endRadius: 420,
                           options: [])
    ctx.restoreGState()

    // Thin rim light.
    ctx.saveGState()
    ctx.addPath(bodyPath)
    ctx.setStrokeColor(color(0xFFFFFF, 0.10))
    ctx.setLineWidth(3)
    ctx.strokePath()
    ctx.restoreGState()

    // The exit: a ring with a gap on the right, and an arrow leaving through it.
    let center = CGPoint(x: 470, y: 512)
    let radius: CGFloat = 132
    let stroke: CGFloat = 40

    ctx.setLineCap(.round)
    ctx.setLineWidth(stroke)
    ctx.setStrokeColor(color(0xEAF1FF, 0.95))
    let ring = CGMutablePath()
    ring.addArc(center: center, radius: radius, startAngle: 0, endAngle: 2 * .pi, clockwise: false)
    ctx.addPath(ring)
    ctx.strokePath()

    // Arrow shaft + head, exiting to the right.
    let shaft = CGRect(x: center.x, y: center.y - 20, width: 300, height: 40)
    ctx.setFillColor(color(0xEAF1FF, 0.97))
    ctx.addRect(shaft)
    ctx.fillPath()

    let head = CGMutablePath()
    head.move(to: CGPoint(x: center.x + 300, y: center.y + 92))
    head.addLine(to: CGPoint(x: center.x + 300, y: center.y - 92))
    head.addLine(to: CGPoint(x: center.x + 450, y: center.y))
    head.closeSubpath()
    ctx.addPath(head)
    ctx.fillPath()

    return ctx.makeImage()
}

func scaled(_ master: CGImage, _ px: Int) -> CGImage? {
    let space = CGColorSpaceCreateDeviceRGB()
    guard let ctx = CGContext(data: nil, width: px, height: px, bitsPerComponent: 8,
                              bytesPerRow: 0, space: space,
                              bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
    ctx.interpolationQuality = .high
    ctx.draw(master, in: CGRect(x: 0, y: 0, width: px, height: px))
    return ctx.makeImage()
}

func writePNG(_ image: CGImage, to path: String) {
    let url = URL(fileURLWithPath: path) as CFURL
    guard let dest = CGImageDestinationCreateWithURL(url, "public.png" as CFString, 1, nil) else { return }
    CGImageDestinationAddImage(dest, image, nil)
    CGImageDestinationFinalize(dest)
}

let outDir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "build/AppIcon.iconset"
try? FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)

guard let master = renderMaster() else {
    FileHandle.standardError.write(Data("cannot render icon\n".utf8))
    exit(1)
}

// iconutil's expected naming: 1x and 2x for 16, 32, 128, 256, 512.
let plan: [(name: String, px: Int)] = [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
]
for item in plan {
    guard let image = scaled(master, item.px) else { continue }
    writePNG(image, to: outDir + "/" + item.name)
}
print("wrote \(plan.count) icon images to \(outDir)")
