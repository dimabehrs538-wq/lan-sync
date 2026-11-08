package engine

import (
	"path/filepath"
	"strconv"
	"strings"
)

const pidFileName = "teamsync-syncthing.pid"

// isOrphanOf 解析 `ps -o ppid=,comm=` 的输出：父进程为 1（已被 launchd/init 收养）且可执行文件名与 binary 相同。
func isOrphanOf(psOut, binary string) bool {
	f := strings.Fields(strings.TrimSpace(psOut))
	if len(f) < 2 {
		return false
	}
	ppid, err := strconv.Atoi(f[0])
	if err != nil || ppid != 1 {
		return false
	}
	comm := strings.Join(f[1:], " ")
	return filepath.Base(comm) == filepath.Base(binary)
}
