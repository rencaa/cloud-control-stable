package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDeleteTerminalDeliveriesKeepsPendingAndRecentRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.CommandDelivery{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-45 * 24 * time.Hour)
	recent := time.Now()
	records := []models.CommandDelivery{
		{CommandID: "old-ack", DeviceID: "d1", MessageType: "command", Payload: "{}", Status: deliveryAcknowledged, NextRetryAt: old, CreatedAt: old, UpdatedAt: old},
		{CommandID: "old-failed", DeviceID: "d1", MessageType: "command", Payload: "{}", Status: deliveryFailed, NextRetryAt: old, CreatedAt: old, UpdatedAt: old},
		{CommandID: "old-queued", DeviceID: "d1", MessageType: "command", Payload: "{}", Status: deliveryQueued, NextRetryAt: old, CreatedAt: old, UpdatedAt: old},
		{CommandID: "recent-ack", DeviceID: "d1", MessageType: "command", Payload: "{}", Status: deliveryAcknowledged, NextRetryAt: recent, CreatedAt: recent, UpdatedAt: recent},
	}
	if err := db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	if deleted := deleteTerminalDeliveriesInBatches(db, time.Now().Add(-30*24*time.Hour), 1); deleted != 2 {
		t.Fatalf("expected two deleted terminal rows, got %d", deleted)
	}
	var remaining int64
	if err := db.Model(&models.CommandDelivery{}).Count(&remaining).Error; err != nil || remaining != 2 {
		t.Fatalf("expected two remaining rows, count=%d err=%v", remaining, err)
	}
}

func TestSQLiteBackupWorksWithFewerThanRetentionLimit(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporaryDirectory := t.TempDir()
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	previousConfig := config.App
	config.App = &config.Config{Database: config.DatabaseConfig{Driver: "sqlite"}}
	t.Cleanup(func() { config.App = previousConfig })

	db, err := gorm.Open(sqlite.Open(filepath.Join(temporaryDirectory, "source.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	scheduler := NewTaskScheduler(db)
	if err := scheduler.backupDatabaseOnce(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(temporaryDirectory, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	backupCount, checksumCount := 0, 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".db") {
			backupCount++
		}
		if strings.HasSuffix(entry.Name(), ".db.sha256") {
			checksumCount++
		}
	}
	if backupCount != 1 || checksumCount != 1 {
		t.Fatalf("expected one verified backup and checksum, got backups=%d checksums=%d", backupCount, checksumCount)
	}
}
