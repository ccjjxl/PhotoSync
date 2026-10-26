package dev.photosync

import android.app.Application
import android.content.Context
import dev.photosync.core.MediaRepository
import dev.photosync.core.Settings
import dev.photosync.server.ServerEngine
import dev.photosync.server.ThumbnailProvider
import dev.photosync.service.SyncService

/**
 * Tiny service locator. Keeps the server, media index and settings process-wide
 * so the Activity and the foreground Service share one instance.
 */
class AppGraph private constructor(context: Context) {

    val settings = Settings(context)
    val repository = MediaRepository(context)
    val thumbnails = ThumbnailProvider(context, repository, context.cacheDir)
    val server = ServerEngine(context, repository, settings, thumbnails)

    fun startServer() {
        server.start()
        SyncService.start(appContext)
    }

    fun stopServer() {
        server.stop()
        SyncService.stop(appContext)
    }

    private lateinit var appContext: Context

    companion object {
        @Volatile private var instance: AppGraph? = null

        fun get(context: Context): AppGraph =
            instance ?: synchronized(this) {
                instance ?: AppGraph(context.applicationContext).also {
                    it.appContext = context.applicationContext
                    instance = it
                }
            }

        fun peek(): AppGraph? = instance
    }
}

class PhotoSyncApp : Application() {
    override fun onCreate() {
        super.onCreate()
        AppGraph.get(this)
    }
}
