package engine

import (
	"regexp"
	"strconv"
	"strings"
)

const pidFileName = "teamsync-syncthing.pid"

var psLine = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(.*?)\s*$`)

// staleTargets 解析 `ps -axww -o pid=,ppid=,command=` 的输出，找出上次残留、需要结束的 syncthing 进程。
// Syncthing v2 即使带 --no-restart 也是“监控进程 + 实际工作进程”两层（工作进程持有数据目录锁），
// 因此除了记录的 PID（监控进程），还要处理它的子进程，以及已被 launchd 收养（父进程为 1）的同一程序、同一数据目录的进程。
// 只匹配命令行以 binary 开头且带 --home=<home> 的进程，不会误伤其他程序或其他数据目录的 syncthing。
func staleTargets(psOut, binary, home string, recorded, self int) []int {
	homeArg := " --home=" + home
	var out []int
	for _, l := range strings.Split(psOut, "\n") {
		m := psLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		ppid, _ := strconv.Atoi(m[2])
		cmd := m[3]
		if pid <= 1 || pid == self {
			continue
		}
		if !strings.HasPrefix(cmd, binary+" ") || !(strings.Contains(cmd, homeArg+" ") || strings.HasSuffix(cmd, homeArg)) {
			continue
		}
		if ppid == 1 || (recorded > 1 && (pid == recorded || ppid == recorded)) {
			out = append(out, pid)
		}
	}
	return out
}
