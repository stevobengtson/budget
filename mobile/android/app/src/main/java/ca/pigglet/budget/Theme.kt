package ca.pigglet.budget

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// Organic palette, ported from the web theme (internal/web/tailwind/theme.css).
// Terracotta primary on a warm cream ground; sage as the secondary. The web
// palette has no red — errors use terracotta-700, like the web's --destructive.
private val LightColors = lightColorScheme(
    primary = Color(0xFFC67139),            // --primary (terracotta)
    onPrimary = Color(0xFF201E1D),          // --primary-foreground (ink, for AA contrast)
    primaryContainer = Color(0xFFFFE1D0),   // accent-200
    onPrimaryContainer = Color(0xFF643312), // accent-800
    secondary = Color(0xFF728157),          // accent2-600 (sage)
    onSecondary = Color(0xFFF5EAD8),
    secondaryContainer = Color(0xFFE1EECC), // accent2-200
    onSecondaryContainer = Color(0xFF3D472B),
    background = Color(0xFFF5EAD8),         // --background
    onBackground = Color(0xFF201E1D),
    surface = Color(0xFFF5EAD8),
    onSurface = Color(0xFF201E1D),
    surfaceVariant = Color(0xFFEEE7DB),     // --muted
    onSurfaceVariant = Color(0xFF645C50),   // --muted-foreground
    surfaceContainer = Color(0xFFEBDDC5),   // --card (nav bar, dialogs)
    surfaceContainerHigh = Color(0xFFEBDDC5),
    surfaceContainerHighest = Color(0xFFEBDDC5),
    error = Color(0xFF8C491A),              // --destructive
    onError = Color(0xFFF5EAD8),
    outline = Color(0xFFDCD3C4),            // --border
    outlineVariant = Color(0xFFDCD3C4),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFFD67F48),            // steps down to accent-500, dark label
    onPrimary = Color(0xFF1C1A17),
    primaryContainer = Color(0xFF643312),
    onPrimaryContainer = Color(0xFFFFC6A5),
    secondary = Color(0xFF8FA073),          // accent2-500
    onSecondary = Color(0xFF1C1A17),
    secondaryContainer = Color(0xFF3D472B),
    onSecondaryContainer = Color(0xFFCCDBB2),
    background = Color(0xFF1C1A17),
    onBackground = Color(0xFFF5EAD8),
    surface = Color(0xFF1C1A17),
    onSurface = Color(0xFFF5EAD8),
    surfaceVariant = Color(0xFF474238),
    onSurfaceVariant = Color(0xFFC0B6A5),
    surfaceContainer = Color(0xFF2E2B25),   // --card
    surfaceContainerHigh = Color(0xFF2E2B25),
    surfaceContainerHighest = Color(0xFF2E2B25),
    error = Color(0xFFF6A06B),
    onError = Color(0xFF1C1A17),
    outline = Color(0x24F5EAD8),            // rgba(245,234,216,0.14)
    outlineVariant = Color(0x24F5EAD8),
)

@Composable
fun BudgetTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = if (isSystemInDarkTheme()) DarkColors else LightColors,
        content = content,
    )
}

// Signed-money colors (web --money-positive / --money-negative / --money-zero).
val moneyPositive: Color @Composable get() =
    if (isSystemInDarkTheme()) Color(0xFFAEBF92) else Color(0xFF56633F)
val moneyNegative: Color @Composable get() =
    if (isSystemInDarkTheme()) Color(0xFFF6A06B) else Color(0xFF8C491A)
val moneyZero: Color @Composable get() =
    if (isSystemInDarkTheme()) Color(0xFFC0B6A5) else Color(0xFF645C50)
