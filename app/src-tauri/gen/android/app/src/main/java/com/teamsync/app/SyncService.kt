package com.teamsync.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.os.PowerManager

/**
 * 可选的“后台继续同步”：前台服务 + 常驻通知，让系统在 App 切到后台后不立即回收进程
 * （同步引擎运行在 App 进程内）。由用户在设置页开启/关闭，不会开机自启。
 * Android 15 起 dataSync 类型前台服务每 24 小时最多运行约 6 小时，超时后系统会调用 onTimeout，这里随即停止。
 */
class SyncService : Service() {
  companion object {
    @Volatile var running = false
    private const val CHANNEL = "teamsync-background"
    private const val ID = 1001

    fun start(ctx: Context) {
      val i = Intent(ctx, SyncService::class.java)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) ctx.startForegroundService(i) else ctx.startService(i)
    }

    fun stop(ctx: Context) {
      ctx.stopService(Intent(ctx, SyncService::class.java))
    }
  }

  private var wake: PowerManager.WakeLock? = null

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val nm = getSystemService(NotificationManager::class.java)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      nm.createNotificationChannel(NotificationChannel(CHANNEL, "后台同步", NotificationManager.IMPORTANCE_LOW))
    }
    val open = PendingIntent.getActivity(
      this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
      PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
    )
    val n: Notification = (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(this, CHANNEL) else Notification.Builder(this))
      .setSmallIcon(R.mipmap.ic_launcher)
      .setContentTitle("团队同步正在后台运行")
      .setContentText("保持与局域网内成员同步；可在 App 设置中关闭")
      .setContentIntent(open)
      .setOngoing(true)
      .build()
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
      startForeground(ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
    } else {
      startForeground(ID, n)
    }
    running = true
    Net.acquireMulticast(this)
    if (wake == null) {
      // 部分唤醒锁：屏幕关闭时 CPU 继续处理同步（最长 6 小时，与系统对 dataSync 前台服务的限制一致）
      wake = (getSystemService(Context.POWER_SERVICE) as PowerManager)
        .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "teamsync:sync").apply { acquire(6 * 60 * 60 * 1000L) }
    }
    return START_NOT_STICKY
  }

  override fun onTimeout(startId: Int, fgsType: Int) {
    stopSelf()
  }

  override fun onDestroy() {
    running = false
    try { wake?.takeIf { it.isHeld }?.release() } catch (_: Exception) {}
    wake = null
    super.onDestroy()
  }
}
