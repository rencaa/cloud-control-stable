package handlers

import (
	"strings"
	"testing"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newDeliveryTestHub(t *testing.T, enabled bool) (*WSHub, *gorm.DB, chan []byte) {
	t.Helper()
	databaseName := strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+databaseName+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.CommandDelivery{}); err != nil {
		t.Fatalf("migrate command deliveries: %v", err)
	}
	originalConfig := config.App
	config.App = &config.Config{Reliability: config.ReliabilityConfig{
		ReliableDeliveryEnabled: enabled,
		RetrySeconds:            1,
		MaxAttempts:             3,
		RetryBatchSize:          10,
	}}
	t.Cleanup(func() { config.App = originalConfig })

	hub := NewWSHub(db)
	send := make(chan []byte, 4)
	hub.clients["device-1"] = &WSClient{DeviceID: "device-1", Send: send}
	return hub, db, send
}

func TestReliableDeliveryDisabledUsesDirectSend(t *testing.T) {
	hub, db, send := newDeliveryTestHub(t, false)
	message := WSMessage{Type: "command", Data: gin.H{"cmd_id": "disabled-1", "command": "home"}}
	if err := hub.SendCommandWithDelivery("device-1", message, 0); err != nil {
		t.Fatalf("direct send: %v", err)
	}
	select {
	case <-send:
	case <-time.After(time.Second):
		t.Fatal("direct message was not queued")
	}
	var count int64
	if err := db.Model(&models.CommandDelivery{}).Count(&count).Error; err != nil {
		t.Fatalf("count durable deliveries: %v", err)
	}
	if count != 0 {
		t.Fatalf("disabled delivery unexpectedly persisted %d records", count)
	}
}

func TestReliableDeliveryPersistsAndAcknowledges(t *testing.T) {
	hub, db, send := newDeliveryTestHub(t, true)
	message := WSMessage{Type: "command", Data: gin.H{"cmd_id": "reliable-1", "command": "home"}}
	if err := hub.SendCommandWithDelivery("device-1", message, 0); err != nil {
		t.Fatalf("reliable send: %v", err)
	}
	select {
	case <-send:
	case <-time.After(time.Second):
		t.Fatal("reliable message was not queued")
	}

	var delivery models.CommandDelivery
	if err := db.Where("command_id = ? AND device_id = ?", "reliable-1", "device-1").First(&delivery).Error; err != nil {
		t.Fatalf("load delivery: %v", err)
	}
	if delivery.Status != deliverySent || delivery.Attempts != 1 {
		t.Fatalf("unexpected sent state: status=%s attempts=%d", delivery.Status, delivery.Attempts)
	}

	hub.AcknowledgeCommandDelivery("device-1", "reliable-1", true, "received")
	if err := db.First(&delivery, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery: %v", err)
	}
	if delivery.Status != deliveryAcknowledged || delivery.AcknowledgedAt == nil {
		t.Fatalf("ACK was not persisted: status=%s acknowledged_at=%v", delivery.Status, delivery.AcknowledgedAt)
	}
}

func TestReliableDeliveryKeepsOfflineCommandForRetry(t *testing.T) {
	hub, db, _ := newDeliveryTestHub(t, true)
	message := WSMessage{Type: "command", Data: gin.H{"cmd_id": "offline-1", "command": "home"}}
	if err := hub.SendCommandWithDelivery("offline-device", message, 0); err != ErrDeviceOffline {
		t.Fatalf("offline send error = %v, want %v", err, ErrDeviceOffline)
	}
	var delivery models.CommandDelivery
	if err := db.Where("command_id = ? AND device_id = ?", "offline-1", "offline-device").First(&delivery).Error; err != nil {
		t.Fatalf("load offline delivery: %v", err)
	}
	if delivery.Status != deliveryQueued || delivery.Attempts != 1 || delivery.NextRetryAt.Before(time.Now()) {
		t.Fatalf("offline command was not retained for retry: %+v", delivery)
	}
}
