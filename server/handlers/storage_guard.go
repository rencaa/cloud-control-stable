package handlers

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cloud-control-server/config"
)

var diskWarningState = struct {
	sync.Mutex
	last time.Time
}{}

func storageWatermark(path string, incomingBytes int64) (usedPercent int, freeBytes uint64, writable bool) {
	if incomingBytes < 0 {
		return 100, 0, false
	}
	probe := path
	for {
		if info, err := os.Stat(probe); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return 0, 0, true
		}
		probe = parent
	}
	total, available, ok := filesystemSpace(probe)
	if !ok || total == 0 {
		return 0, 0, true
	}
	projectedAvailable := available
	if uint64(incomingBytes) >= projectedAvailable {
		projectedAvailable = 0
	} else {
		projectedAvailable -= uint64(incomingBytes)
	}
	usedPercent = int((total - projectedAvailable) * 100 / total)
	warnAt, stopAt := 80, 90
	if config.App != nil {
		if config.App.Maintenance.DiskWarnPercent > 0 {
			warnAt = config.App.Maintenance.DiskWarnPercent
		}
		if config.App.Maintenance.DiskStopWritesPercent > warnAt {
			stopAt = config.App.Maintenance.DiskStopWritesPercent
		}
	}
	if usedPercent >= warnAt {
		diskWarningState.Lock()
		if time.Since(diskWarningState.last) >= 10*time.Minute {
			log.Printf("Storage watermark warning: path=%s used=%d%% available=%d", probe, usedPercent, available)
			diskWarningState.last = time.Now()
		}
		diskWarningState.Unlock()
	}
	return usedPercent, available, usedPercent < stopAt
}
