import SwiftUI

// Placeholder — shown only when the paydown add-on is enabled.
struct PaydownView: View {
    var body: some View {
        NavigationStack {
            ComingSoon(title: "Paydown", systemImage: "chart.line.downtrend.xyaxis")
                .navigationTitle("Paydown")
        }
    }
}
