package dev.photosync.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import dev.photosync.AppGraph
import dev.photosync.R
import dev.photosync.ui.MainActivity
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Keeps the HTTP server alive while the app is in the background, and keeps the
 * transfer counters fresh in the notification so the user sees live progress.
 */
class SyncService : LifecycleService() {

    private var ticker: Job? = null

    override fun onCreate() {
        super.onCreate()
        isRunning = true
        createChannel()
        startForeground(NOTIFICATION_ID, buildNotification("正在等待电脑连接…"))
        AppGraph.get(this).server.start()

        ticker = lifecycleScope.launch {
            var lastText = ""
            while (isActive) {
                val engine = AppGraph.get(this@SyncService).server
                val st = engine.status.value
                val served = engine.totalBytesServed
                val text = if (!st.running) {
                    "服务启动失败${st.lastError?.let { "：$it" } ?: ""}"
                } else if (served <= 0) {
                    "等待电脑连接 · ${st.urls.firstOrNull() ?: "无网络"}"
                } else {
                    "已传输 ${human(served)} · ${st.urls.firstOrNull() ?: ""}"
                }
                if (text != lastText) {
                    lastText = text
                    notificationManager().notify(NOTIFICATION_ID, buildNotification(text))
                }
                delay(1000)
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        if (intent?.action == ACTION_STOP) {
            AppGraph.get(this).server.stop()
            stopSelf()
            return START_NOT_STICKY
        }
        return START_STICKY
    }

    override fun onDestroy() {
        ticker?.cancel()
        isRunning = false
        AppGraph.get(this).server.stop()
        super.onDestroy()
    }

    private fun notificationManager() =
        getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= 26) {
            val ch = NotificationChannel(
                CHANNEL_ID, "同步服务", NotificationManager.IMPORTANCE_LOW,
            ).apply {
                description = "保持照片同步服务在后台运行"
                setShowBadge(false)
            }
            notificationManager().createNotificationChannel(ch)
        }
    }

    private fun buildNotification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stop = PendingIntent.getService(
            this, 1,
            Intent(this, SyncService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val builder = if (Build.VERSION.SDK_INT >= 26) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        return builder
            .setContentTitle("PhotoSync 运行中")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentIntent(open)
            .addAction(
                Notification.Action.Builder(null, "停止", stop).build(),
            )
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .build()
    }

    private fun human(b: Long): String = when {
        b > 1L shl 30 -> "%.1f GB".format(b / (1L shl 30).toDouble())
        b > 1L shl 20 -> "%.1f MB".format(b / (1L shl 20).toDouble())
        else -> "${b / 1024} KB"
    }

    companion object {
        private const val CHANNEL_ID = "photosync_service"
        private const val NOTIFICATION_ID = 8737
        const val ACTION_STOP = "dev.photosync.STOP"

        @Volatile var isRunning = false
            private set

        fun start(context: Context) {
            val i = Intent(context, SyncService::class.java)
            if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(i) else context.startService(i)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, SyncService::class.java))
        }
    }
}
