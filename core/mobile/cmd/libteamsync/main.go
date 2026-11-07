// libteamsync：给 Tauri 移动端（Rust）调用的 C 接口。
// Android 编译为 c-shared（libteamsynccore.so，打包进 jniLibs），iOS 编译为 c-archive（静态链接进 App）。
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/lan-sync/teamsync/core/mobile/mobilecore"
)

type result struct {
	Port  int    `json:"port,omitempty"`
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

// TeamSyncStart 启动内核（阻塞直到同步引擎就绪或失败），返回 JSON：{"port":..,"token":..} 或 {"error":".."}。
// 返回的字符串需用 TeamSyncFree 释放。
//
//export TeamSyncStart
func TeamSyncStart(home, foldersDir, token *C.char, apiPort C.int) *C.char {
	n, err := mobilecore.Start(mobilecore.Config{
		Home: C.GoString(home), FoldersDir: C.GoString(foldersDir), Token: C.GoString(token), APIPort: int(apiPort), NoTrash: true,
	})
	var r result
	if err != nil {
		r.Error = err.Error()
	} else {
		r.Port, r.Token = n.Port(), n.Token()
	}
	b, _ := json.Marshal(r)
	return C.CString(string(b))
}

// TeamSyncStop 优雅停止同步引擎与本地 API。
//
//export TeamSyncStop
func TeamSyncStop() { mobilecore.Stop() }

//export TeamSyncFree
func TeamSyncFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
