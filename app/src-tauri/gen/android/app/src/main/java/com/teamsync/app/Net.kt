package com.teamsync.app

import android.content.Context
import android.net.wifi.WifiManager

/** 局域网发现所需的组播锁（App 与后台同步服务共用，引用计数关闭）。 */
object Net {
  private var lock: WifiManager.MulticastLock? = null

  @Synchronized
  fun acquireMulticast(ctx: Context) {
    if (lock?.isHeld == true) return
    val wifi = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager ?: return
    lock = wifi.createMulticastLock("teamsync-discovery").apply {
      setReferenceCounted(false)
      try { acquire() } catch (_: Exception) {}
    }
  }

  @Synchronized
  fun releaseMulticast() {
    try { lock?.takeIf { it.isHeld }?.release() } catch (_: Exception) {}
    lock = null
  }
}
