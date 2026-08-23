package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var alertLastSent sync.Map
var alertHTTPClient = &http.Client{Timeout: 5 * time.Second}

var maintenanceHealth = struct {
	sync.RWMutex
	backupError   string
	backupErrorAt time.Time
	lastBackupOK  time.Time
}{}

const maxCleanupBatchesPerRun = 20

// StartBackgroundJobs 启动后台维护任务
func (s *TaskScheduler) StartBackgroundJobs() {
	if s == nil || s.stopping.Load() {
		return
	}
	s.backgroundOnce.Do(func() {
		jobs := []func(){s.cleanupOldData, s.sendAlerts, s.backupDatabase, s.maintainSQLite, s.watchTaskDeadlines}
		s.backgroundWG.Add(len(jobs))
		for _, job := range jobs {
			go func(run func()) {
				defer s.backgroundWG.Done()
				run()
			}(job)
		}
	})
}

func (s *TaskScheduler) maintainSQLite() {
	if config.App == nil || !strings.EqualFold(config.App.Database.Driver, "sqlite") {
		return
	}
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if result := s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); result.Error != nil {
				log.Printf("SQLite passive checkpoint failed: %v", result.Error)
			}
		}
	}
}

func (s *TaskScheduler) cleanupOldData() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	s.cleanupOldDataOnce()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.cleanupOldDataOnce()
		}
	}
}

func (s *TaskScheduler) cleanupOldDataOnce() {
	maintenance := maintenanceSettings()
	logs := deleteOldRowsInBatches(s.db, &models.DeviceLog{}, time.Now().Add(-time.Duration(maintenance.DeviceLogRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	metrics := deleteOldRowsInBatches(s.db, &models.DeviceMetric{}, time.Now().Add(-time.Duration(maintenance.MetricRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	deliveries := deleteTerminalDeliveriesInBatches(s.db, time.Now().Add(-time.Duration(maintenance.DeliveryRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	taskLogs := deleteOldRowsInBatches(s.db, &models.TaskLog{}, time.Now().Add(-time.Duration(maintenance.TaskLogRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	systemLogs := deleteOldRowsInBatches(s.db, &models.SystemLog{}, time.Now().Add(-time.Duration(maintenance.SystemLogRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	sms := deleteOldRowsInBatches(s.db, &models.DeviceSms{}, time.Now().Add(-time.Duration(maintenance.SMSRetentionDays)*24*time.Hour), maintenance.CleanupBatchSize)
	if logs+metrics+deliveries+taskLogs+systemLogs+sms > 0 {
		log.Printf("Cleanup: device_logs=%d metrics=%d deliveries=%d task_logs=%d system_logs=%d sms=%d", logs, metrics, deliveries, taskLogs, systemLogs, sms)
	}
}

func deleteTerminalDeliveriesInBatches(db *gorm.DB, cutoff time.Time, batchSize int) int64 {
	if db == nil || batchSize < 1 {
		return 0
	}
	var deleted int64
	for batch := 0; batch < maxCleanupBatchesPerRun; batch++ {
		var ids []uint64
		err := db.Model(&models.CommandDelivery{}).
			Where("status IN ? AND updated_at < ?", []string{deliveryAcknowledged, deliveryFailed}, cutoff).
			Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			return deleted
		}
		result := db.Where("id IN ?", ids).Delete(&models.CommandDelivery{})
		if result.Error != nil {
			log.Printf("Delivery cleanup batch failed: %v", result.Error)
			return deleted
		}
		deleted += result.RowsAffected
		if len(ids) < batchSize {
			return deleted
		}
	}
	return deleted
}

func (s *TaskScheduler) sendAlerts() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	s.sendAlertsOnce(time.Now())
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.sendAlertsOnce(now)
		}
	}
}

func (s *TaskScheduler) sendAlertsOnce(now time.Time) {
	webhookURL := ""
	if config.App != nil {
		webhookURL = strings.TrimSpace(config.App.Security.NotificationWebhookURL)
	}
	if webhookURL == "" {
		webhookURL = s.getConfig("notification_webhook_url")
	}
	parsed, err := url.Parse(webhookURL)
	if webhookURL == "" {
		return
	}
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		log.Printf("Alert: invalid webhook URL")
		return
	}
	settings := config.App.Alerts
	cooldown := time.Duration(settings.CooldownMinutes) * time.Minute

	var offline []models.Device
	s.db.Where("status = 0 AND last_heartbeat >= ? AND last_heartbeat < ?", now.Add(-30*time.Minute), now.Add(-5*time.Minute)).Limit(100).Find(&offline)
	for _, device := range offline {
		sendAlert(webhookURL, "offline:"+device.DeviceID, map[string]interface{}{
			"event": "device_offline", "device_id": device.DeviceID, "device_name": device.Name, "last_heartbeat": device.LastHeartbeat,
		}, cooldown)
	}

	var oldest sql.NullTime
	s.db.Model(&models.CommandDelivery{}).Select("MIN(created_at)").
		Where("status IN ?", []string{deliveryQueued, deliverySent}).Scan(&oldest)
	if oldest.Valid && now.Sub(oldest.Time) >= time.Duration(settings.DeliveryAgeMinutes)*time.Minute {
		sendAlert(webhookURL, "delivery_age", map[string]interface{}{
			"event": "command_delivery_delayed", "oldest_created_at": oldest.Time, "age_seconds": int(now.Sub(oldest.Time).Seconds()),
		}, cooldown)
	}
	var pressuredAgents int64
	agentOutboxThreshold := 500 * settings.QueueUsagePercent / 100
	s.db.Model(&models.Device{}).Where("agent_outbox_depth >= ?", agentOutboxThreshold).Count(&pressuredAgents)
	if pressuredAgents > 0 {
		sendAlert(webhookURL, "agent_outbox", map[string]interface{}{
			"event": "agent_outbox_pressure", "device_count": pressuredAgents, "threshold": agentOutboxThreshold,
		}, cooldown)
	}

	if Hub != nil {
		queues := []struct {
			name       string
			used, size int
		}{
			{"register", len(Hub.register), cap(Hub.register)}, {"unregister", len(Hub.unregister), cap(Hub.unregister)},
			{"database", len(Hub.dbJobs), cap(Hub.dbJobs)}, {"media", len(Hub.mediaJobs), cap(Hub.mediaJobs)},
		}
		for _, queue := range queues {
			percent := 0
			if queue.size > 0 {
				percent = queue.used * 100 / queue.size
			}
			if percent >= settings.QueueUsagePercent {
				sendAlert(webhookURL, "queue:"+queue.name, map[string]interface{}{
					"event": "queue_pressure", "queue": queue.name, "used": queue.used, "capacity": queue.size, "percent": percent,
				}, cooldown)
			}
		}
	}

	var laggedCron int64
	s.db.Model(&models.Task{}).Where("cron_enabled = ? AND next_run_at IS NOT NULL AND next_run_at < ?", true,
		now.Add(-time.Duration(settings.CronLagMinutes)*time.Minute)).Count(&laggedCron)
	if laggedCron > 0 {
		sendAlert(webhookURL, "cron_lag", map[string]interface{}{"event": "cron_lag", "task_count": laggedCron}, cooldown)
	}
	connections := recentDeviceConnections(now)
	if connections >= settings.ReconnectsPer5Min {
		sendAlert(webhookURL, "reconnect_storm", map[string]interface{}{
			"event": "device_reconnect_storm", "connections_5m": connections,
		}, cooldown)
	}
	if config.App != nil {
		probe := strings.TrimSpace(config.App.Upload.UploadPath)
		if probe == "" {
			probe = "."
		}
		used, free, _ := storageWatermark(probe, 0)
		if used >= config.App.Maintenance.DiskWarnPercent {
			sendAlert(webhookURL, "disk", map[string]interface{}{
				"event": "disk_usage_high", "used_percent": used, "available_bytes": free,
			}, cooldown)
		}
	}
	maintenanceHealth.RLock()
	backupError, backupErrorAt := maintenanceHealth.backupError, maintenanceHealth.backupErrorAt
	maintenanceHealth.RUnlock()
	if backupError != "" {
		sendAlert(webhookURL, "backup", map[string]interface{}{
			"event": "backup_failed", "message": backupError, "failed_at": backupErrorAt,
		}, cooldown)
	}
	alertLastSent.Range(func(key, value interface{}) bool {
		if sentAt, ok := value.(time.Time); ok && now.Sub(sentAt) > 24*time.Hour {
			alertLastSent.Delete(key)
		}
		return true
	})
}

func sendAlert(webhookURL, key string, payload map[string]interface{}, cooldown time.Duration) {
	if value, ok := alertLastSent.Load(key); ok && time.Since(value.(time.Time)) < cooldown {
		return
	}
	payload["server_time"] = time.Now().UTC()
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	request, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := alertHTTPClient.Do(request)
	if err != nil {
		return
	}
	_ = response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		alertLastSent.Store(key, time.Now())
	}
}

func (s *TaskScheduler) backupDatabase() {
	if err := s.backupDatabaseOnce(); err != nil {
		recordBackupFailure(err)
		log.Printf("Initial backup failed: %v", err)
	} else {
		recordBackupSuccess()
	}
	ticker := time.NewTicker(time.Duration(maintenanceSettings().BackupIntervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.backupDatabaseOnce(); err != nil {
				recordBackupFailure(err)
				log.Printf("Backup failed: %v", err)
			} else {
				recordBackupSuccess()
			}
		}
	}
}

func recordBackupFailure(err error) {
	maintenanceHealth.Lock()
	maintenanceHealth.backupError = err.Error()
	maintenanceHealth.backupErrorAt = time.Now()
	maintenanceHealth.Unlock()
}

func recordBackupSuccess() {
	maintenanceHealth.Lock()
	maintenanceHealth.backupError = ""
	maintenanceHealth.backupErrorAt = time.Time{}
	maintenanceHealth.lastBackupOK = time.Now()
	maintenanceHealth.Unlock()
}

func sendOfflineAlert(webhookURL string, device models.Device) {
	payload, err := json.Marshal(map[string]interface{}{"event": "device_offline", "device_id": device.DeviceID, "device_name": device.Name, "last_heartbeat": device.LastHeartbeat})
	if err != nil {
		return
	}
	request, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := alertHTTPClient.Do(request)
	if err != nil {
		return
	}
	_ = response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		alertLastSent.Store(device.DeviceID, time.Now())
	}
}

// deleteOldRowsInBatches keeps retention work short-lived, avoiding a long
// write lock when a device fleet has accumulated a large history.
func deleteOldRowsInBatches(db *gorm.DB, model interface{}, cutoff time.Time, batchSize int) int64 {
	if db == nil || batchSize < 1 {
		return 0
	}
	var deleted int64
	for batch := 0; batch < maxCleanupBatchesPerRun; batch++ {
		var ids []uint64
		if err := db.Model(model).Where("created_at < ?", cutoff).Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
			return deleted
		}
		result := db.Where("id IN ?", ids).Delete(model)
		if result.Error != nil {
			log.Printf("Cleanup batch failed: %v", result.Error)
			return deleted
		}
		deleted += result.RowsAffected
		if len(ids) < batchSize {
			return deleted
		}
	}
	return deleted
}

func (s *TaskScheduler) backupDatabaseOnce() error {
	if config.App == nil || strings.ToLower(config.App.Database.Driver) != "sqlite" {
		// MySQL backups are handled by the isolated deployment sidecar.
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	backupDir := filepath.Join("backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	estimatedBytes := int64(0)
	if databasePath := strings.TrimSpace(config.App.Database.DBName); databasePath != "" {
		if info, statErr := os.Stat(databasePath); statErr == nil {
			estimatedBytes = info.Size()
		}
	}
	if _, _, writable := storageWatermark(backupDir, estimatedBytes); !writable {
		return errors.New("backup skipped because disk reached the write-stop watermark")
	}
	path, err := filepath.Abs(filepath.Join(backupDir, "cloud_control-"+time.Now().Format("20060102-150405.000")+".db"))
	if err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM INTO ?", path); err != nil {
		return err
	}
	if err := verifySQLiteBackup(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := writeBackupChecksum(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}
	type backupEntry struct {
		name string
		mod  time.Time
	}
	backups := make([]backupEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".db") {
			if info, e := entry.Info(); e == nil {
				backups = append(backups, backupEntry{entry.Name(), info.ModTime()})
			}
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].mod.After(backups[j].mod) })
	retentionCount := maintenanceSettings().BackupRetentionCount
	if len(backups) > retentionCount {
		for _, old := range backups[retentionCount:] {
			_ = os.Remove(filepath.Join(backupDir, old.name))
			_ = os.Remove(filepath.Join(backupDir, old.name+".sha256"))
		}
	}
	log.Printf("Backup created: %s", path)
	return nil
}

func verifySQLiteBackup(path string) error {
	db, err := gorm.Open(sqlite.Open(path+"?mode=ro"), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open backup for verification: %w", err)
	}
	var result string
	if err := db.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return fmt.Errorf("backup integrity check: %w", err)
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		_ = sqlDB.Close()
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("backup integrity check returned %q", result)
	}
	return nil
}

func writeBackupChecksum(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	return os.WriteFile(path+".sha256", []byte(fmt.Sprintf("%x  %s\n", hash.Sum(nil), filepath.Base(path))), 0600)
}

func maintenanceSettings() config.MaintenanceConfig {
	if config.App != nil && config.App.Maintenance.CleanupBatchSize > 0 {
		return config.App.Maintenance
	}
	return config.MaintenanceConfig{
		DeviceLogRetentionDays: 30, MetricRetentionDays: 7, DeliveryRetentionDays: 30,
		TaskLogRetentionDays: 30, SystemLogRetentionDays: 30, SMSRetentionDays: 90,
		CleanupBatchSize: 1000, ScreenshotRetentionHours: 24,
		ScreenshotMaxBytes:  2 * 1024 * 1024 * 1024,
		BackupIntervalHours: 6, BackupRetentionCount: 14,
	}
}

func (s *TaskScheduler) getConfig(key string) string {
	var cfg models.SystemConfig
	if s.db.Where("config_key = ?", key).First(&cfg).Error == nil {
		return cfg.ConfigValue
	}
	return ""
}
