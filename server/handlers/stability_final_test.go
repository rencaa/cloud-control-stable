package handlers

import (
	"fmt"
	"sync/atomic"
	"testing"

	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestWeightedQueueBudgetRejectsMemoryAmplification(t *testing.T) {
	var bytes atomic.Int64
	if !reserveQueueBytes(&bytes, 6, 10) {
		t.Fatal("first reservation should fit")
	}
	if reserveQueueBytes(&bytes, 5, 10) {
		t.Fatal("reservation beyond byte budget must be rejected")
	}
	if got := bytes.Load(); got != 6 {
		t.Fatalf("rejected reservation changed accounting: %d", got)
	}
	bytes.Add(-6)
	if !reserveQueueBytes(&bytes, 10, 10) {
		t.Fatal("exact byte budget should fit")
	}
}

func TestCompactDeviceResponseIsBounded(t *testing.T) {
	raw := make([]interface{}, 0, maxResponseItems+50)
	for i := 0; i < maxResponseItems+50; i++ {
		raw = append(raw, map[string]interface{}{"from": fmt.Sprintf("sender-%d", i), "body": string(make([]rune, 2048)), "date": float64(i)})
	}
	sms, _, weight := compactDeviceResponse("read_sms", map[string]interface{}{"sms": raw})
	if len(sms) != maxResponseItems {
		t.Fatalf("expected %d bounded items, got %d", maxResponseItems, len(sms))
	}
	if len([]rune(sms[0].Body)) != 1024 {
		t.Fatalf("body was not truncated: %d runes", len([]rune(sms[0].Body)))
	}
	if weight > maxDBQueueBytes {
		t.Fatalf("compacted response unexpectedly exceeds queue budget: %d", weight)
	}
}

func TestBatchImportsDeduplicateAndReplaceContacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DeviceSms{}, &models.DeviceContact{}); err != nil {
		t.Fatal(err)
	}
	items := []smsInput{{Sender: "10086", Body: "hello", SMSDate: 123}}
	persistSMSBatch(db, 1, items)
	persistSMSBatch(db, 1, items)
	var smsCount int64
	db.Model(&models.DeviceSms{}).Count(&smsCount)
	if smsCount != 1 {
		t.Fatalf("SMS import was not idempotent: %d", smsCount)
	}

	persistContactBatch(db, 1, []contactInput{{Name: "A", Phone: "1"}, {Name: "B", Phone: "2"}})
	persistContactBatch(db, 1, []contactInput{{Name: "C", Phone: "3"}})
	var contacts []models.DeviceContact
	db.Where("device_id = ?", 1).Find(&contacts)
	if len(contacts) != 1 || contacts[0].Phone != "3" {
		t.Fatalf("contact snapshot was not replaced: %#v", contacts)
	}
}
