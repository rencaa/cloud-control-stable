package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGeneratesAndPersistsUniqueJWTSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"jwt":{"secret":"","expire_hours":24}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	first := App.JWT.Secret
	if len(first) < 32 || needsGeneratedJWTSecret(first) {
		t.Fatalf("invalid generated secret: %q", first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.JWT.Secret != first {
		t.Fatal("generated secret was not persisted")
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if App.JWT.Secret != first {
		t.Fatal("persisted secret changed after reload")
	}
}

func TestLoadAppliesLowResourceLimits(t *testing.T) {
	t.Setenv("CLOUD_JWT_SECRET", "test-low-resource-secret-01234567890123456789")
	t.Setenv("CLOUD_DB_MAX_OPEN_CONNS", "4")
	t.Setenv("CLOUD_DB_MAX_IDLE_CONNS", "2")
	t.Setenv("CLOUD_SQLITE_CACHE_KB", "8192")
	t.Setenv("CLOUD_SQLITE_MMAP_BYTES", "67108864")
	t.Setenv("CLOUD_SERVER_BIND_ADDRESS", "127.0.0.1")
	t.Setenv("CLOUD_MQTT_BIND_ADDRESS", "127.0.0.1")
	t.Setenv("CLOUD_UPLOAD_MAX_SIZE", "67108864")
	t.Setenv("CLOUD_UPLOAD_MAX_TOTAL_BYTES", "2147483648")
	t.Setenv("CLOUD_DEVICE_LOG_RETENTION_DAYS", "7")
	t.Setenv("CLOUD_METRIC_RETENTION_DAYS", "3")
	t.Setenv("CLOUD_DELIVERY_RETENTION_DAYS", "7")
	t.Setenv("CLOUD_CLEANUP_BATCH_SIZE", "500")
	t.Setenv("CLOUD_SCREENSHOT_RETENTION_HOURS", "6")
	t.Setenv("CLOUD_SCREENSHOT_MAX_BYTES", "268435456")
	t.Setenv("CLOUD_BACKUP_INTERVAL_HOURS", "24")
	t.Setenv("CLOUD_BACKUP_RETENTION_COUNT", "7")
	t.Setenv("CLOUD_DEVICE_WS_ATTEMPTS_PER_MINUTE", "120")
	t.Setenv("CLOUD_DEVICE_WS_MAX_CONNECTIONS_PER_IP", "8")
	t.Setenv("CLOUD_DISK_WARN_PERCENT", "80")
	t.Setenv("CLOUD_DISK_STOP_WRITES_PERCENT", "90")
	if err := Load(""); err != nil {
		t.Fatal(err)
	}
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if App.Database.MaxOpenConns != 4 || App.Database.MaxIdleConns != 2 ||
		App.Database.SQLiteCacheKB != 8192 || App.Database.SQLiteMmapBytes != 67108864 ||
		App.Server.BindAddress != "127.0.0.1" || App.Server.MQTTBindAddress != "127.0.0.1" ||
		App.Upload.MaxTotalBytes != 2147483648 || App.Maintenance.ScreenshotMaxBytes != 268435456 ||
		App.Maintenance.BackupRetentionCount != 7 || App.Security.DeviceWSAttemptsPerMinute != 120 ||
		App.Security.DeviceWSMaxConnectionsPerIP != 8 || App.Maintenance.DiskWarnPercent != 80 ||
		App.Maintenance.DiskStopWritesPercent != 90 {
		t.Fatalf("low-resource environment was not applied: %+v", App)
	}
}
