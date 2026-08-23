package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// MetricsHandler exposes a deliberately small, dependency-free Prometheus
// endpoint. It is intended for an internal-only network location.
func MetricsHandler(db *gorm.DB, hub *WSHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		online := 0
		registerDepth, unregisterDepth, dbDepth, mediaDepth := 0, 0, 0, 0
		if hub != nil {
			online = len(hub.GetOnlineDevices())
			registerDepth = len(hub.register)
			unregisterDepth = len(hub.unregister)
			dbDepth = len(hub.dbJobs)
			mediaDepth = len(hub.mediaJobs)
			writeMetric64(c.Writer, "cloud_control_hub_queue_bytes", "queue=\"database\"", hub.dbQueueBytes.Load())
			writeMetric64(c.Writer, "cloud_control_hub_queue_bytes", "queue=\"media\"", hub.mediaBytes.Load())
		}

		fmt.Fprintln(c.Writer, "# HELP cloud_control_online_devices Current connected devices.")
		fmt.Fprintln(c.Writer, "# TYPE cloud_control_online_devices gauge")
		fmt.Fprintln(c.Writer, "cloud_control_online_devices "+strconv.Itoa(online))
		writeMetric(c.Writer, "cloud_control_hub_queue_depth", "queue=\"register\"", registerDepth)
		writeMetric(c.Writer, "cloud_control_hub_queue_depth", "queue=\"unregister\"", unregisterDepth)
		writeMetric(c.Writer, "cloud_control_hub_queue_depth", "queue=\"database\"", dbDepth)
		writeMetric(c.Writer, "cloud_control_hub_queue_depth", "queue=\"media\"", mediaDepth)
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		writeMetric64(c.Writer, "cloud_control_go_heap_bytes", "", int64(memory.HeapAlloc))
		writeMetric(c.Writer, "cloud_control_go_goroutines", "", runtime.NumGoroutine())
		screenshotDiskState.Lock()
		screenshotBytes := screenshotDiskState.bytes
		screenshotDiskState.Unlock()
		writeMetric64(c.Writer, "cloud_control_screenshot_bytes", "", screenshotBytes)
		if config.App != nil {
			writeMetric64(c.Writer, "cloud_control_screenshot_quota_bytes", "", config.App.Maintenance.ScreenshotMaxBytes)
			writeMetric64(c.Writer, "cloud_control_resource_quota_bytes", "", config.App.Upload.MaxTotalBytes)
			probe := strings.TrimSpace(config.App.Upload.UploadPath)
			if probe == "" {
				probe = "."
			}
			usedPercent, freeBytes, _ := storageWatermark(probe, 0)
			writeMetric(c.Writer, "cloud_control_disk_used_percent", "", usedPercent)
			writeMetric64(c.Writer, "cloud_control_disk_available_bytes", "", int64(freeBytes))
			if strings.EqualFold(config.App.Database.Driver, "sqlite") {
				databasePath := strings.TrimSpace(config.App.Database.DBName)
				if info, err := os.Stat(databasePath); err == nil {
					writeMetric64(c.Writer, "cloud_control_sqlite_file_bytes", "file=\"database\"", info.Size())
				}
				if info, err := os.Stat(databasePath + "-wal"); err == nil {
					writeMetric64(c.Writer, "cloud_control_sqlite_file_bytes", "file=\"wal\"", info.Size())
				}
			}
		}

		if db == nil {
			return
		}
		if sqlDB, err := db.DB(); err == nil {
			stats := sqlDB.Stats()
			writeMetric(c.Writer, "cloud_control_db_connections", "state=\"open\"", stats.OpenConnections)
			writeMetric(c.Writer, "cloud_control_db_connections", "state=\"in_use\"", stats.InUse)
			writeMetric(c.Writer, "cloud_control_db_connections", "state=\"idle\"", stats.Idle)
			writeMetric(c.Writer, "cloud_control_db_wait_count", "", int(stats.WaitCount))
		}
		var resourceBytes int64
		if err := db.Model(&models.Resource{}).Select("COALESCE(SUM(file_size), 0)").Scan(&resourceBytes).Error; err == nil {
			writeMetric64(c.Writer, "cloud_control_resource_bytes", "", resourceBytes)
		}

		if reliableDeliveryEnabled() {
			for _, status := range []string{deliveryQueued, deliverySent, deliveryAcknowledged, deliveryFailed} {
				var count int64
				if err := db.Model(&models.CommandDelivery{}).Where("status = ?", status).Count(&count).Error; err == nil {
					writeMetric(c.Writer, "cloud_control_command_deliveries", "status=\""+status+"\"", int(count))
				}
			}
			var oldest sql.NullTime
			if err := db.Model(&models.CommandDelivery{}).Select("MIN(created_at)").Where("status IN ?", []string{deliveryQueued, deliverySent}).Scan(&oldest).Error; err == nil && oldest.Valid {
				writeMetric64(c.Writer, "cloud_control_command_delivery_oldest_age_seconds", "", int64(time.Since(oldest.Time).Seconds()))
			}
		}
		var overdueRuns int64
		if err := db.Model(&models.TaskDevice{}).Where("status = ? AND deadline_at IS NOT NULL AND deadline_at < ?", 1, time.Now()).Count(&overdueRuns).Error; err == nil {
			writeMetric64(c.Writer, "cloud_control_task_runs_overdue", "", overdueRuns)
		}
		for _, protocol := range []int{1, 2} {
			var count int64
			query := db.Model(&models.Device{})
			if protocol == 1 {
				query = query.Where("protocol_version IS NULL OR protocol_version < 2")
			} else {
				query = query.Where("protocol_version >= 2")
			}
			if err := query.Count(&count).Error; err == nil {
				writeMetric64(c.Writer, "cloud_control_devices_by_protocol", "protocol=\""+strconv.Itoa(protocol)+"\"", count)
			}
		}
		maintenanceHealth.RLock()
		lastBackupOK := maintenanceHealth.lastBackupOK
		backupFailed := maintenanceHealth.backupError != ""
		maintenanceHealth.RUnlock()
		if !lastBackupOK.IsZero() {
			writeMetric64(c.Writer, "cloud_control_last_backup_success_unixtime", "", lastBackupOK.Unix())
		}
		if backupFailed {
			writeMetric(c.Writer, "cloud_control_backup_failed", "", 1)
		} else {
			writeMetric(c.Writer, "cloud_control_backup_failed", "", 0)
		}
	}
}

func writeMetric64(writer http.ResponseWriter, name, labels string, value int64) {
	if labels != "" {
		fmt.Fprintf(writer, "%s{%s} %d\n", name, labels, value)
		return
	}
	fmt.Fprintf(writer, "%s %d\n", name, value)
}

func writeMetric(writer http.ResponseWriter, name, labels string, value int) {
	if labels != "" {
		fmt.Fprintf(writer, "%s{%s} %d\n", name, labels, value)
		return
	}
	fmt.Fprintf(writer, "%s %d\n", name, value)
}
