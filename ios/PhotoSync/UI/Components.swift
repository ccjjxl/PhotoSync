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
struct SyncGlyph: View {
    var color: Color = .white
    var size: CGFloat = 34

    var body: some View {
        ZStack {
            Circle()
                .trim(from: 0.12, to: 0.62)
                .stroke(color, style: StrokeStyle(lineWidth: size * 0.11, lineCap: .round))
                .rotationEffect(.degrees(-30))
            Circle()
                .trim(from: 0.62, to: 0.92)
                .stroke(color.opacity(0.55), style: StrokeStyle(lineWidth: size * 0.11, lineCap: .round))
                .rotationEffect(.degrees(150))
            Image(systemName: "arrow.triangle.2.circlepath")
                .font(.system(size: size * 0.44, weight: .semibold))
                .foregroundStyle(color)
                .opacity(0.9)
        }
        .frame(width: size, height: size)
    }
}

// MARK: - Formatting

func humanBytes(_ value: Int64) -> String {
    let formatter = ByteCountFormatter()
    formatter.countStyle = .file
    formatter.allowedUnits = [.useKB, .useMB, .useGB]
    return formatter.string(fromByteCount: value)
}

func humanCount(_ value: Int) -> String {
    if value >= 10_000 {
        return String(format: "%.1f万", Double(value) / 10_000)
    }
    return "\(value)"
}
