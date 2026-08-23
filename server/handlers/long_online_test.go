package handlers

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestReliableRetryDelayIsExponentiallyBounded(t *testing.T) {
	previous := config.App
	config.App = &config.Config{Reliability: config.ReliabilityConfig{
		RetrySeconds: 15, MaxRetrySeconds: 300, MaxAttempts: 2048,
		RetryBatchSize: 50, DeliveryTTLHours: 168,
	}}
	defer func() { config.App = previous }()

	if got := reliableRetryDelay(0); got != 15*time.Second {
		t.Fatalf("first retry = %s", got)
	}
	if got := reliableRetryDelay(99); got != 5*time.Minute {
		t.Fatalf("bounded retry = %s", got)
	}
}

func TestNewTaskRunSupersedesOfflineOlderRun(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:long-online-delivery?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.CommandDelivery{}, &models.TaskDevice{}, &models.Device{}); err != nil {
		t.Fatal(err)
	}
	previous := config.App
	config.App = &config.Config{Reliability: config.ReliabilityConfig{
		ReliableDeliveryEnabled: true, RetrySeconds: 15, MaxRetrySeconds: 300,
		MaxAttempts: 2048, RetryBatchSize: 50, DeliveryTTLHours: 168,
	}}
	defer func() { config.App = previous }()

	hub := NewWSHub(db)
	first := WSMessage{Type: "task", Data: map[string]interface{}{
		"cmd_id": "cmd-old", "task_id": 7, "run_id": "run-old",
	}}
	if err := hub.SendCommandWithDelivery("offline-1", first, 7); !errors.Is(err, ErrDeviceOffline) {
		t.Fatalf("first send error = %v", err)
	}
	second := WSMessage{Type: "task", Data: map[string]interface{}{
		"cmd_id": "cmd-new", "task_id": 7, "run_id": "run-new",
	}}
	if err := hub.SendCommandWithDelivery("offline-1", second, 7); !errors.Is(err, ErrDeviceOffline) {
		t.Fatalf("second send error = %v", err)
	}

	var oldDelivery, newDelivery models.CommandDelivery
	if err := db.Where("command_id = ?", "cmd-old").First(&oldDelivery).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("command_id = ?", "cmd-new").First(&newDelivery).Error; err != nil {
		t.Fatal(err)
	}
	if oldDelivery.Status != deliveryFailed || oldDelivery.LastError != "superseded by newer task run" {
		t.Fatalf("old delivery not superseded: %#v", oldDelivery)
	}
	if newDelivery.Status != deliveryQueued || newDelivery.RunID != "run-new" {
		t.Fatalf("new delivery not queued: %#v", newDelivery)
	}
}

func TestMQTTQoS1CommandPacketRoundTrip(t *testing.T) {
	encoded := makeMQTTPublishQoS1("cloud/device/device-1/command", []byte(`{"type":"task"}`), 42, true)
	packet, err := readMQTTPacket(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if packet.header != 0x3a {
		t.Fatalf("header = 0x%02x", packet.header)
	}
	publish, err := parseMQTTPublish(packet.header, packet.body)
	if err != nil {
		t.Fatal(err)
	}
	if publish.qos != 1 || publish.packetID != 42 || string(publish.payload) != `{"type":"task"}` {
		t.Fatalf("publish = %#v", publish)
	}
}

func TestCronParserSupportsSecondsAndDescriptors(t *testing.T) {
	for _, expression := range []string{"*/5 * * * * *", "@every 30s"} {
		schedule, err := cronSchedule(expression)
		if err != nil {
			t.Fatalf("parse %q: %v", expression, err)
		}
		if next := schedule.Next(time.Now()); next.IsZero() {
			t.Fatalf("next time is zero for %q", expression)
		}
	}
}
