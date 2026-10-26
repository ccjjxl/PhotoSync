package dev.photosync.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

/** Lightweight toast — avoids the platform Toast so it matches the app theme. */
@Composable
fun ToastHost(message: String, onDone: () -> Unit) {
    var visible by remember(message) { mutableStateOf(false) }
    LaunchedEffect(message) {
        visible = true
        delay(2200)
        visible = false
        delay(220)
        onDone()
    }
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.BottomCenter) {
        AnimatedVisibility(
            visible = visible,
            enter = fadeIn() + slideInVertically { it / 2 },
            exit = fadeOut() + slideOutVertically { it / 2 },
            modifier = Modifier,
        ) {
            Box(
                Modifier
                    .padding(bottom = 54.dp)
                    .clip(RoundedCornerShape(14.dp))
                    .background(Color(0xFF1B2130).copy(alpha = 0.96f))
                    .padding(horizontal = 18.dp, vertical = 12.dp),
            ) {
                Text(
                    message,
                    style = MaterialTheme.typography.bodySmall,
                    color = Color(0xFFE8ECF5),
                )
            }
        }
    }
}
