//go:build windows

package trash

import (
	"fmt"
	"syscall"
	"unsafe"
)

type shFileOpStruct struct {
	Hwnd                 uintptr
	Func                 uint32
	From                 *uint16
	To                   *uint16
	Flags                uint16
	AnyOperationsAborted int32
	NameMappings         uintptr
	ProgressTitle        *uint16
}

var procSHFileOperationW = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

func toTrash(path string) error {
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // 双 \0 结尾
	op := shFileOpStruct{
		Func:  3, // FO_DELETE
		From:  &from[0],
		Flags: 0x40 | 0x10 | 0x4 | 0x400, // ALLOWUNDO | NOCONFIRMATION | SILENT | NOERRORUI
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 || op.AnyOperationsAborted != 0 {
		return fmt.Errorf("移到回收站失败（代码 %d）", r)
	}
	return nil
}
