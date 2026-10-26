package dev.photosync.ui

import android.app.Activity
import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowCompat

private val Blue = Color(0xFF4F8CFF)
private val BlueDeep = Color(0xFF2E5FD0)
private val Mint = Color(0xFF3ED598)
private val Amber = Color(0xFFFFB020)
private val Rose = Color(0xFFFF5F6D)

private val DarkColors = darkColorScheme(
    primary = Blue,
    onPrimary = Color.White,
    primaryContainer = Color(0xFF1B2A47),
    onPrimaryContainer = Color(0xFFD6E3FF),
    secondary = Mint,
    onSecondary = Color(0xFF04231A),
    secondaryContainer = Color(0xFF12362A),
    onSecondaryContainer = Color(0xFFB6F2D8),
    tertiary = Amber,
    background = Color(0xFF0B0D12),
    onBackground = Color(0xFFE8ECF5),
    surface = Color(0xFF11141B),
    onSurface = Color(0xFFE8ECF5),
    surfaceVariant = Color(0xFF1A1F2B),
    onSurfaceVariant = Color(0xFF9AA3B5),
    surfaceContainer = Color(0xFF161A23),
    surfaceContainerHigh = Color(0xFF1C2230),
    outline = Color(0xFF2A3243),
    outlineVariant = Color(0xFF1F2635),
    error = Rose,
)

private val LightColors = lightColorScheme(
    primary = BlueDeep,
    onPrimary = Color.White,
    primaryContainer = Color(0xFFDCE7FF),
    onPrimaryContainer = Color(0xFF001A41),
    secondary = Color(0xFF12855F),
    secondaryContainer = Color(0xFFC5F2DF),
    tertiary = Color(0xFFB06A00),
    background = Color(0xFFF6F8FC),
    onBackground = Color(0xFF11141B),
    surface = Color.White,
    onSurface = Color(0xFF11141B),
    surfaceVariant = Color(0xFFEDF1F8),
    onSurfaceVariant = Color(0xFF5A6478),
    surfaceContainer = Color(0xFFF1F4FA),
    surfaceContainerHigh = Color(0xFFE7ECF5),
    outline = Color(0xFFD3DAE6),
    outlineVariant = Color(0xFFE3E9F2),
    error = Color(0xFFD64550),
)

private val AppTypography = Typography(
    displaySmall = TextStyle(fontSize = 30.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.5).sp),
    headlineMedium = TextStyle(fontSize = 22.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.3).sp),
    titleLarge = TextStyle(fontSize = 18.sp, fontWeight = FontWeight.SemiBold),
    titleMedium = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.Medium),
    bodyLarge = TextStyle(fontSize = 15.sp),
    bodyMedium = TextStyle(fontSize = 13.sp),
    bodySmall = TextStyle(fontSize = 12.sp),
    labelLarge = TextStyle(fontSize = 13.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.2.sp),
    labelMedium = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.3.sp),
    labelSmall = TextStyle(fontSize = 10.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.4.sp),
)

@Composable
fun PhotoSyncTheme(darkTheme: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val colors = if (darkTheme) DarkColors else LightColors
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as? Activity)?.window ?: return@SideEffect
            window.statusBarColor = Color.Transparent.toArgb()
            window.navigationBarColor = Color.Transparent.toArgb()
            WindowCompat.getInsetsController(window, view).apply {
                isAppearanceLightStatusBars = !darkTheme
                isAppearanceLightNavigationBars = !darkTheme
            }
        }
    }
    MaterialTheme(colorScheme = colors, typography = AppTypography, content = content)
}
