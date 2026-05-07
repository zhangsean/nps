package server

import (
	"fmt"
	"sort"
	"strings"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/tool"
	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type PortRange struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Label string `json:"label"`
}

type PortItem struct {
	Port      int    `json:"port"`
	Owner     string `json:"owner"`
	Tooltip   string `json:"tooltip"`
	Process   string `json:"process"`
	Pid       int32  `json:"pid"`
	IsCurrent bool   `json:"is_current"`
}

type PortListResult struct {
	Mode        string      `json:"mode"`
	RangeStart  int         `json:"range_start"`
	RangeEnd    int         `json:"range_end"`
	CurrentPort int         `json:"current_port"`
	FirstUsable int         `json:"first_usable"`
	Ranges      []PortRange `json:"ranges"`
	Rows        []*PortItem `json:"rows"`
}

type tunnelPortMeta struct {
	ClientRemark string
	Remark       string
	Target       string
}

type currentTaskInfo struct {
	Port         int
	ClientRemark string
	Remark       string
	Target       string
}

type portRowBuilder struct {
	Owner     string
	Tooltip   []string
	Process   string
	Pid       int32
	IsCurrent bool
}

type processPortMeta struct {
	Owner   string
	Process string
	Pid     int32
	Tooltip []string
}

func GetPortList(mode string, rangeStart, rangeEnd, currentTaskID int) (*PortListResult, error) {
	if mode != "tcp" && mode != "udp" {
		mode = "tcp"
	}

	ranges := getAllowedPortRanges()
	rangeStart, rangeEnd = normalizePortRange(rangeStart, rangeEnd, ranges)
	currentTask := getCurrentTaskInfo(currentTaskID, mode)
	tunnelMeta := getRunningTunnelPortMeta(mode, currentTaskID)
	connMeta := getProcessPortMeta(mode)
	rows := make(map[int]*portRowBuilder)
	ports := make([]int, 0)

	for port, meta := range connMeta {
		if port < rangeStart || port > rangeEnd {
			continue
		}
		row := ensurePortRow(rows, port, &ports)
		row.Owner = meta.Owner
		row.Process = meta.Process
		row.Pid = meta.Pid
		row.Tooltip = append(row.Tooltip, meta.Tooltip...)
	}

	for port, meta := range tunnelMeta {
		if port < rangeStart || port > rangeEnd {
			continue
		}
		row := ensurePortRow(rows, port, &ports)
		row.Owner = formatTunnelOwner(meta)
		row.Process = "nps"
		row.Tooltip = append(row.Tooltip, buildTunnelTooltip(meta)...)
	}

	if currentTask.Port >= rangeStart && currentTask.Port <= rangeEnd {
		meta := tunnelPortMeta{
			ClientRemark: currentTask.ClientRemark,
			Remark:       currentTask.Remark,
			Target:       currentTask.Target,
		}
		row := ensurePortRow(rows, currentTask.Port, &ports)
		row.Owner = formatTunnelOwner(meta)
		row.Process = "nps"
		row.IsCurrent = true
		row.Tooltip = append(row.Tooltip, buildTunnelTooltip(meta)...)
		if len(row.Tooltip) == 0 {
			row.Tooltip = append(row.Tooltip, "当前正在编辑的隧道端口")
		}
	}

	sort.Ints(ports)
	items := make([]*PortItem, 0, len(ports))
	blocked := make(map[int]bool)
	for _, port := range ports {
		row := rows[port]
		if !row.IsCurrent {
			blocked[port] = true
		}
		items = append(items, &PortItem{
			Port:      port,
			Owner:     defaultOwner(row.Owner, row.IsCurrent),
			Tooltip:   uniqueLines(row.Tooltip),
			Process:   row.Process,
			Pid:       row.Pid,
			IsCurrent: row.IsCurrent,
		})
	}

	return &PortListResult{
		Mode:        mode,
		RangeStart:  rangeStart,
		RangeEnd:    rangeEnd,
		CurrentPort: currentTask.Port,
		FirstUsable: getFirstUsablePort(rangeStart, rangeEnd, blocked),
		Ranges:      ranges,
		Rows:        items,
	}, nil
}

func getAllowedPortRanges() []PortRange {
	portList := tool.GetAllowPortList()
	if len(portList) == 0 {
		return splitRange(1, 65535)
	}

	portCopy := append([]int(nil), portList...)
	sort.Ints(portCopy)
	ranges := make([]PortRange, 0)
	start := portCopy[0]
	prev := start
	for i := 1; i < len(portCopy); i++ {
		if portCopy[i] == prev+1 {
			prev = portCopy[i]
			continue
		}
		ranges = append(ranges, splitRange(start, prev)...)
		start = portCopy[i]
		prev = portCopy[i]
	}
	ranges = append(ranges, splitRange(start, prev)...)
	return ranges
}

func normalizePortRange(rangeStart, rangeEnd int, ranges []PortRange) (int, int) {
	if len(ranges) == 0 {
		return 1, 200
	}
	if rangeStart <= 0 {
		rangeStart = ranges[0].Start
	}
	if rangeEnd < rangeStart {
		rangeEnd = rangeStart + 199
	}

	for _, r := range ranges {
		if rangeStart >= r.Start && rangeStart <= r.End {
			if rangeEnd > r.End {
				rangeEnd = r.End
			}
			return rangeStart, rangeEnd
		}
		if rangeStart < r.Start {
			rangeStart = r.Start
			if rangeEnd < rangeStart {
				rangeEnd = rangeStart
			}
			if rangeEnd > r.End {
				rangeEnd = r.End
			}
			return rangeStart, rangeEnd
		}
	}

	last := ranges[len(ranges)-1]
	return last.Start, last.End
}

func getCurrentTaskInfo(taskID int, mode string) currentTaskInfo {
	if taskID == 0 {
		return currentTaskInfo{}
	}
	t, err := file.GetDb().GetTask(taskID)
	if err != nil || t.Mode != mode {
		return currentTaskInfo{}
	}
	info := currentTaskInfo{
		Port:   t.Port,
		Remark: t.Remark,
	}
	if t.Client != nil {
		info.ClientRemark = t.Client.Remark
	}
	if t.Target != nil {
		info.Target = t.Target.TargetStr
	}
	return info
}

func getRunningTunnelPortMeta(mode string, currentTaskID int) map[int]tunnelPortMeta {
	meta := make(map[int]tunnelPortMeta)
	file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		t := value.(*file.Tunnel)
		if t.Id == currentTaskID || t.Mode != mode || t.Port <= 0 {
			return true
		}
		if _, ok := RunList.Load(t.Id); !ok {
			return true
		}
		item := tunnelPortMeta{
			Remark: t.Remark,
		}
		if t.Client != nil {
			item.ClientRemark = t.Client.Remark
		}
		if t.Target != nil {
			item.Target = t.Target.TargetStr
		}
		meta[t.Port] = item
		return true
	})
	return meta
}

func getProcessPortMeta(mode string) map[int]processPortMeta {
	result := make(map[int]processPortMeta)
	conns, err := gnet.Connections(mode)
	if err != nil {
		return result
	}

	nameCache := make(map[int32]string)
	for _, conn := range conns {
		if conn.Laddr.Port == 0 {
			continue
		}
		if mode == "tcp" && conn.Status != "LISTEN" {
			continue
		}

		port := int(conn.Laddr.Port)
		if _, ok := result[port]; ok {
			continue
		}

		pid := conn.Pid
		name := getProcessName(pid, nameCache)
		tooltip := make([]string, 0, 3)
		if name != "" {
			tooltip = append(tooltip, fmt.Sprintf("进程: %s", name))
		}
		if pid > 0 {
			tooltip = append(tooltip, fmt.Sprintf("PID: %d", pid))
		}
		if conn.Status != "" {
			tooltip = append(tooltip, "状态: "+conn.Status)
		}

		owner := name
		if owner == "" {
			owner = "系统进程"
		}
		if pid > 0 {
			owner = fmt.Sprintf("%s (%d)", owner, pid)
		}

		result[port] = processPortMeta{
			Owner:   owner,
			Process: name,
			Pid:     pid,
			Tooltip: tooltip,
		}
	}
	return result
}

func getProcessName(pid int32, cache map[int32]string) string {
	if pid <= 0 {
		return ""
	}
	if name, ok := cache[pid]; ok {
		return name
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		cache[pid] = ""
		return ""
	}
	name, err := p.Name()
	if err != nil {
		cache[pid] = ""
		return ""
	}
	cache[pid] = name
	return name
}

func ensurePortRow(rows map[int]*portRowBuilder, port int, ports *[]int) *portRowBuilder {
	if row, ok := rows[port]; ok {
		return row
	}
	row := &portRowBuilder{}
	rows[port] = row
	*ports = append(*ports, port)
	return row
}

func uniqueLines(lines []string) string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func defaultOwner(owner string, isCurrent bool) string {
	if owner != "" {
		return owner
	}
	if isCurrent {
		return "当前隧道"
	}
	return "系统进程"
}

func formatTunnelOwner(meta tunnelPortMeta) string {
	parts := make([]string, 0, 3)
	if meta.ClientRemark != "" {
		parts = append(parts, meta.ClientRemark)
	}
	if meta.Remark != "" {
		parts = append(parts, meta.Remark)
	}
	if meta.Target != "" {
		parts = append(parts, meta.Target)
	}
	if len(parts) == 0 {
		return "nps"
	}
	return strings.Join(parts, " / ")
}

func buildTunnelTooltip(meta tunnelPortMeta) []string {
	lines := make([]string, 0, 3)
	if meta.ClientRemark != "" {
		lines = append(lines, "客户端备注: "+meta.ClientRemark)
	}
	if meta.Remark != "" {
		lines = append(lines, "隧道备注: "+meta.Remark)
	}
	if meta.Target != "" {
		lines = append(lines, "隧道目标: "+meta.Target)
	}
	return lines
}

func getFirstUsablePort(start, end int, blocked map[int]bool) int {
	for port := start; port <= end; port++ {
		if !blocked[port] {
			return port
		}
	}
	return 0
}

func splitRange(start, end int) []PortRange {
	const chunkSize = 5000
	ranges := make([]PortRange, 0)
	for chunkStart := start; chunkStart <= end; chunkStart += chunkSize {
		chunkEnd := chunkStart + chunkSize - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		ranges = append(ranges, PortRange{
			Start: chunkStart,
			End:   chunkEnd,
			Label: fmt.Sprintf("%d-%d", chunkStart, chunkEnd),
		})
	}
	return ranges
}
