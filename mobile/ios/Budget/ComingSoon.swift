import SwiftUI

// Simple placeholder used by the not-yet-built tabs.
struct ComingSoon: View {
    let title: String
    let systemImage: String

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: systemImage)
                .font(.system(size: 44))
                .foregroundStyle(.tint)
            Text(title)
                .font(.title2.bold())
            Text("Coming soon")
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
