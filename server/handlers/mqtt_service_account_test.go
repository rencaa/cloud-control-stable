package handlers

import (
	"testing"

	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestEnsureMQTTServiceAccountCreatesAndRotatesCredential(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.MQTTServiceAccount{}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMQTTServiceAccount(db, "bridge", "first-password"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMQTTServiceAccount(db, "bridge", "rotated-password"); err != nil {
		t.Fatal(err)
	}

	var account models.MQTTServiceAccount
	if err := db.Where("username = ?", "bridge").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.PasswordHash != hashDeviceToken("rotated-password") || !account.Enabled {
		t.Fatal("bridge credential was not rotated atomically")
	}
	var count int64
	if err := db.Model(&models.MQTTServiceAccount{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected one bridge account, count=%d err=%v", count, err)
	}
}
