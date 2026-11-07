package com.teamsync.app

import android.os.Bundle
import android.view.View
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class MainActivity : TauriActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)
    // 全面屏 / 刘海屏 / 软键盘：把系统栏区域让出来，网页内容不被遮挡
    val root = findViewById<View>(android.R.id.content)
    ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
      val bars = insets.getInsets(
        WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout() or WindowInsetsCompat.Type.ime()
      )
      v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
      WindowInsetsCompat.CONSUMED
    }
    // 局域网发现依赖 UDP 广播/组播：Android 默认会丢弃这些包，App 运行期间持有 MulticastLock
    Net.acquireMulticast(this)
  }

  override fun onWebViewCreate(webView: WebView) {
    super.onWebViewCreate(webView)
    webView.addJavascriptInterface(TeamSyncBridge(this), "TeamSyncAndroid")
  }

  override fun onDestroy() {
    if (!SyncService.running) Net.releaseMulticast()
    super.onDestroy()
  }
}
