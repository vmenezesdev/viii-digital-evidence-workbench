package main

import (
	"os"
	"strings"
	"syscall"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/spikework"
)

func recordWorkerResources(result *spikework.Output) {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) == nil {
		result.ProcessCPUNS = usage.Utime.Nano() + usage.Stime.Nano()
		result.ProcessPeakRSSBytes = usage.Maxrss * 1024
	}
	if quota, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		result.ProcessCPUQuota = strings.TrimSpace(string(quota))
	}
}
