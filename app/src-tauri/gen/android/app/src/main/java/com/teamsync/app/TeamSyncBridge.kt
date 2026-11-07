package com.teamsync.app

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.Settings
import android.webkit.JavascriptInterface
import android.webkit.MimeTypeMap
import androidx.core.content.FileProvider
import java.io.File

/**
 * 暴露给网页的 window.TeamSyncAndroid（返回 "ok" / "error:<原因>"）：
 * - shareFile(path) / openFile(path)：通过 FileProvider 把团队文件夹中的文件交给系统分享面板 / 其它应用打开（只读授权）
 * - hasAllFilesAccess() / requestAllFilesAccess() / sharedStorageRoot()：可选的“所有文件访问权限”，用于把团队文件夹放到共享存储
 * - setBackgroundSync(on) / isBackgroundSync()：后台继续同步（前台服务 + 常驻通知）
 *
 * 只允许访问应用外部文件目录与共享存储中的文件；App 内部数据目录（设备证书/密钥、同步数据库）一律拒绝。
 */
class TeamSyncBridge(private val activity: Activity) {
  private fun allowedRoots(): List<File> {
    val roots = mutableListOf<File>()
    activity.getExternalFilesDir(null)?.canonicalFile?.let { roots.add(it) }
    if (hasAllFilesAccess()) roots.add(Environment.getExternalStorageDirectory().canonicalFile)
    return roots
  }

  private fun checkedFile(path: String): File {
    val f = File(path).canonicalFile
    val internal = activity.filesDir.parentFile?.canonicalFile
    require(internal == null || !f.path.startsWith(internal.path + File.separator)) { "不能导出 App 内部数据" }
    require(allowedRoots().any { f.path.startsWith(it.path + File.separator) }) { "只能导出团队文件夹中的文件" }
    require(f.isFile) { "文件不存在" }
    return f
  }

  private fun mimeOf(f: File): String =
    MimeTypeMap.getSingleton().getMimeTypeFromExtension(f.extension.lowercase()) ?: "application/octet-stream"

  private fun uriOf(f: File): Uri = FileProvider.getUriForFile(activity, activity.packageName + ".fileprovider", f)

  @JavascriptInterface
  fun shareFile(path: String): String = try {
    val f = checkedFile(path)
    val send = Intent(Intent.ACTION_SEND).apply {
      type = mimeOf(f)
      putExtra(Intent.EXTRA_STREAM, uriOf(f))
      addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    activity.runOnUiThread { activity.startActivity(Intent.createChooser(send, f.name)) }
    "ok"
  } catch (e: Exception) {
    "error:" + (e.message ?: e.javaClass.simpleName)
  }

  @JavascriptInterface
  fun openFile(path: String): String = try {
    val f = checkedFile(path)
    val view = Intent(Intent.ACTION_VIEW).apply {
      setDataAndType(uriOf(f), mimeOf(f))
      addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    activity.runOnUiThread {
      try {
        activity.startActivity(view)
      } catch (_: ActivityNotFoundException) {
        activity.startActivity(Intent.createChooser(view, f.name))
      }
    }
    "ok"
  } catch (e: Exception) {
    "error:" + (e.message ?: e.javaClass.simpleName)
  }

  @JavascriptInterface
  fun hasAllFilesAccess(): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.R && Environment.isExternalStorageManager()

  @JavascriptInterface
  fun requestAllFilesAccess(): String = try {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
      "error:需要 Android 11 及以上"
    } else {
      val i = Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:" + activity.packageName))
      activity.runOnUiThread {
        try {
          activity.startActivity(i)
        } catch (_: ActivityNotFoundException) {
          activity.startActivity(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
        }
      }
      "ok"
    }
  } catch (e: Exception) {
    "error:" + (e.message ?: e.javaClass.simpleName)
  }

  @JavascriptInterface
  fun sharedStorageRoot(): String = Environment.getExternalStorageDirectory().absolutePath

  @JavascriptInterface
  fun setBackgroundSync(on: Boolean): String = try {
    if (on) SyncService.start(activity) else SyncService.stop(activity)
    "ok"
  } catch (e: Exception) {
    "error:" + (e.message ?: e.javaClass.simpleName)
  }

  @JavascriptInterface
  fun isBackgroundSync(): Boolean = SyncService.running
}
