import SwiftUI
import UIKit

// Organic palette, ported from the web theme (internal/web/tailwind/theme.css).
// Each token pairs the web's light value with its published dark-mode role swap,
// so views never need their own dark variants.
extension Color {
    /// Page ground (--background): cream / warm near-black.
    static let appBackground = dynamic(light: 0xF5EAD8, dark: 0x1C1A17)
    /// Raised surface (--card): darker cream / warm dark grey.
    static let appCard = dynamic(light: 0xEBDDC5, dark: 0x2E2B25)
    /// Brand accent (--primary): terracotta, one ramp step lighter in dark.
    static let appPrimary = dynamic(light: 0xC67139, dark: 0xD67F48)
    /// Errors and destructive actions (--destructive): terracotta-700 / accent-400.
    /// The web palette has no red.
    static let appDestructive = dynamic(light: 0x8C491A, dark: 0xF6A06B)
    /// Signed money (--money-positive / --money-negative): sage / terracotta.
    static let moneyPositive = dynamic(light: 0x56633F, dark: 0xAEBF92)
    static let moneyNegative = dynamic(light: 0x8C491A, dark: 0xF6A06B)

    private static func dynamic(light: UInt32, dark: UInt32) -> Color {
        Color(UIColor { traits in
            traits.userInterfaceStyle == .dark ? UIColor(rgb: dark) : UIColor(rgb: light)
        })
    }
}

private extension UIColor {
    convenience init(rgb: UInt32) {
        self.init(
            red: CGFloat((rgb >> 16) & 0xFF) / 255,
            green: CGFloat((rgb >> 8) & 0xFF) / 255,
            blue: CGFloat(rgb & 0xFF) / 255,
            alpha: 1
        )
    }
}
