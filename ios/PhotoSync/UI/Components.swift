import SwiftUI

// MARK: - Design tokens

enum Theme {
    static let accent = Color(red: 0.31, green: 0.55, blue: 1.0)
    static let mint = Color(red: 0.24, green: 0.84, blue: 0.60)
    static let amber = Color(red: 1.0, green: 0.69, blue: 0.13)
    static let rose = Color(red: 1.0, green: 0.37, blue: 0.43)
    static let card = Color(.secondarySystemGroupedBackground)
    static let pageBackground = Color(.systemGroupedBackground)

    static let heroGradient = LinearGradient(
        colors: [Color(red: 0.17, green: 0.42, blue: 1.0),
                 Color(red: 0.31, green: 0.55, blue: 1.0),
                 Color(red: 0.44, green: 0.66, blue: 1.0)],
        startPoint: .topLeading, endPoint: .bottomTrailing
    )
}

// MARK: - Reusable views

/// Rounded card container used across the app.
struct Card<Content: View>: View {
    var padding: CGFloat = 16
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.card)
            .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
    }
}

/// Statistic tile (photos / videos / size).
struct StatTile: View {
    let label: String
    let value: String
    let color: Color

    var body: some View {
        VStack(spacing: 3) {
            Text(value)
                .font(.system(.title3, design: .rounded).weight(.semibold))
                .foregroundStyle(color)
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 13)
        .background(Theme.card)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}

/// Small pill button used inside the hero card.
struct PillButton: View {
    let title: String
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.caption.weight(.medium))
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(.white.opacity(0.2))
                .foregroundStyle(.white)
                .clipShape(Capsule())
        }
        .buttonStyle(.plain)
    }
}

/// Animated status dot.
struct PulseDot: View {
    let color: Color
    let active: Bool

    @State private var pulse = false

    var body: some View {
        ZStack {
            if active {
                Circle()
                    .fill(color.opacity(0.25))
                    .frame(width: 16, height: 16)
                    .scaleEffect(pulse ? 1.35 : 0.9)
                    .opacity(pulse ? 0.2 : 0.75)
            }
            Circle()
                .fill(active ? color : Color.secondary.opacity(0.5))
                .frame(width: 8, height: 8)
        }
        .onAppear {
            withAnimation(.easeInOut(duration: 1.3).repeatForever(autoreverses: true)) {
                pulse = true
            }
        }
    }
}

/// The sync glyph, drawn with shapes so there's no image asset to ship.
/// The sync glyph, drawn exactly like Android's `SyncGlyph` (Compose Canvas).
///
/// Android draws two 130° arcs plus two triangular arrow heads; the previous iOS
/// version mixed partial circles with a SF Symbol, which looked different from
/// the Android app. Shapes here mirror the Compose code 1:1.
struct SyncGlyph: View {
    var color: Color = .white
    var size: CGFloat = 34

    var body: some View {
        Canvas { context, canvasSize in
            let width = canvasSize.width
            let height = canvasSize.height
            let radius = min(width, height) / 2 * 0.62
            let center = CGPoint(x: width / 2, y: height / 2)
            let strokeWidth = min(width, height) * 0.11

            func arc(start: Double, sweep: Double, opacity: Double) {
                var path = Path()
                path.addArc(
                    center: center,
                    radius: radius,
                    startAngle: .degrees(start),
                    endAngle: .degrees(start + sweep),
                    clockwise: false
                )
                context.stroke(
                    path,
                    with: .color(color.opacity(opacity)),
                    style: StrokeStyle(lineWidth: strokeWidth, lineCap: .round)
                )
            }

            func triangle(_ points: [CGPoint], opacity: Double) {
                var path = Path()
                path.move(to: points[0])
                path.addLine(to: points[1])
                path.addLine(to: points[2])
                path.closeSubpath()
                context.fill(path, with: .color(color.opacity(opacity)))
            }

            // Two facing arcs (200°+130° and 20°+130°), as in Compose.
            arc(start: 200, sweep: 130, opacity: 1.0)
            arc(start: 20, sweep: 130, opacity: 0.55)

            triangle([
                CGPoint(x: center.x + radius * 0.98, y: center.y - radius * 0.42),
                CGPoint(x: center.x + radius * 1.32, y: center.y - radius * 0.12),
                CGPoint(x: center.x + radius * 0.78, y: center.y + radius * 0.10),
            ], opacity: 0.9)

            triangle([
                CGPoint(x: center.x - radius * 0.98, y: center.y + radius * 0.42),
                CGPoint(x: center.x - radius * 1.32, y: center.y + radius * 0.12),
                CGPoint(x: center.x - radius * 0.78, y: center.y - radius * 0.10),
            ], opacity: 0.45)
        }
        .frame(width: size, height: size)
    }
}

// MARK: - Formatting

/// Formats a byte count the same way Android does ("0 B", "1.5 MB", "3.07 GB").
///
/// `ByteCountFormatter` renders 0 as "Zero KB" and omits the unit for small
/// values, which looked wrong next to the Android app, so this is hand-rolled.
func humanBytes(_ value: Int64) -> String {
    if value <= 0 { return "0 B" }
    let kb = 1024.0, mb = kb * 1024, gb = mb * 1024
    let v = Double(value)
    if v >= gb { return String(format: "%.2f GB", v / gb) }
    if v >= mb { return String(format: "%.1f MB", v / mb) }
    if v >= kb { return String(format: "%.0f KB", v / kb) }
    return "\(value) B"
}

func humanCount(_ value: Int) -> String {
    if value >= 10_000 {
        return String(format: "%.1f万", Double(value) / 10_000)
    }
    return "\(value)"
}
