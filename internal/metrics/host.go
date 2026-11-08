// Package metrics samples host and per-server resource usage for the
// dashboard. Servers are read through systemd's cgroup counters (or /proc in
// dev mode); the host through gopsutil. Everything stays in memory.
package metrics

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// Host is one snapshot of the machine.
type Host struct {
	At            time.Time `json:"at"`
	CPUPercent    float64   `json:"cpuPercent"`
	CPUCount      int       `json:"cpuCount"`
	Load1         float64   `json:"load1"`
	MemTotal      uint64    `json:"memTotal"`
	MemUsed       uint64    `json:"memUsed"`
	MemAvailable  uint64    `json:"memAvailable"`
	DiskPath      string    `json:"diskPath"`
	DiskTotal     uint64    `json:"diskTotal"`
	DiskUsed      uint64    `json:"diskUsed"`
	DiskFree      uint64    `json:"diskFree"`
	UptimeSeconds uint64    `json:"uptimeSeconds"`
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
}

// SampleHost reads the host counters. The CPU figure is the usage since the
// previous call (gopsutil keeps the last reading), so call it periodically.
func SampleHost(ctx context.Context, diskPath string) Host {
	h := Host{At: time.Now(), DiskPath: diskPath}
	if pcts, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pcts) > 0 {
		h.CPUPercent = pcts[0]
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		h.CPUCount = n
	}
	if l, err := load.AvgWithContext(ctx); err == nil {
		h.Load1 = l.Load1
	}
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		h.MemTotal, h.MemUsed, h.MemAvailable = m.Total, m.Used, m.Available
	}
	if d, err := disk.UsageWithContext(ctx, diskPath); err == nil {
		h.DiskTotal, h.DiskUsed, h.DiskFree = d.Total, d.Used, d.Free
	}
	if info, err := host.InfoWithContext(ctx); err == nil {
		h.UptimeSeconds = info.Uptime
		h.Hostname = info.Hostname
		h.OS = info.Platform + " " + info.PlatformVersion
	}
	return h
}
