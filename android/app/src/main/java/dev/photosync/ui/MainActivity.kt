package dev.photosync.ui

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle

class MainActivity : ComponentActivity() {

    private val vm: MainViewModel by viewModels()

    private val permissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { vm.onPermissionResult() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            PhotoSyncTheme {
                val state by vm.state.collectAsStateWithLifecycle()
                var sheet by remember { mutableStateOf<Sheet?>(null) }
                val context = LocalContext.current

                LaunchedEffect(state.permission) {
                    if (state.permission == dev.photosync.core.MediaRepository.PermissionState.Denied) {
                        requestMediaPermissions()
                    }
                }

                LaunchedEffect(Unit) {
                    dev.photosync.service.SyncService.start(applicationContext)
                }

                Box(Modifier.fillMaxSize()) {
                    HomeScreen(
                        state = state,
                        onToggleServer = vm::toggleServer,
                        onCopy = vm::copyToClipboard,
                        onRefreshLibrary = {
                            vm.loadLibrary()
                            vm.consumeToast()
                        },
                        onGrantPermission = { requestMediaPermissions() },
                        onOpenSettings = vm::openPermissionSettings,
                        onOpenSheet = { sheet = it },
                        onOpenUrl = { url ->
                            runCatching {
                                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                            }
                        },
                    )
                }

                sheet?.let {
                    SettingsSheet(
                        sheet = it,
                        state = state,
                        onDismiss = { sheet = null },
                        vm = vm,
                    )
                }

                state.toast?.let { msg ->
                    ToastHost(msg) { vm.consumeToast() }
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        vm.refreshPermission()
    }

    private fun requestMediaPermissions() {
        val perms = Build.VERSION.SDK_INT.let { sdk ->
            when {
                sdk >= 34 -> arrayOf(
                    Manifest.permission.READ_MEDIA_IMAGES,
                    Manifest.permission.READ_MEDIA_VIDEO,
                    "android.permission.READ_MEDIA_VISUAL_USER_SELECTED",
                )
                sdk >= 33 -> arrayOf(
                    Manifest.permission.READ_MEDIA_IMAGES,
                    Manifest.permission.READ_MEDIA_VIDEO,
                )
                else -> arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE)
            }
        }
        val missing = perms.filter {
            ContextCompat.checkSelfPermission(this, it) != android.content.pm.PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) permissionLauncher.launch(missing.toTypedArray())
        if (Build.VERSION.SDK_INT >= 33) {
            val notif = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
            if (notif != android.content.pm.PackageManager.PERMISSION_GRANTED) {
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 42)
            }
        }
    }
}

sealed interface Sheet {
    data object Help : Sheet
    data object Settings : Sheet
    data object Library : Sheet
}
