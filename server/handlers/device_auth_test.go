package handlers

import (
	"errors"
	"testing"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAuthenticateDeviceUsesPerDeviceToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Device{}); err != nil {
		t.Fatal(err)
	}
	token, err := GenerateDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	device := models.Device{DeviceID: "device-1", Name: "device-1", AuthTokenHash: hashDeviceToken(token)}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := AuthenticateDevice(db, device.DeviceID, token); !ok {
		t.Fatal("valid token rejected")
	}
	if _, ok := AuthenticateDevice(db, device.DeviceID, token+"-wrong"); ok {
		t.Fatal("invalid token accepted")
	}
	if _, ok := AuthenticateDevice(db, "device-2", token); ok {
		t.Fatal("token accepted for another device")
	}
	disabled := models.User{Username: "disabled-device-owner", Password: "hashed", Status: 1}
	if err := db.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&disabled).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	disabledToken, err := GenerateDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{DeviceID: "disabled-device", Name: "disabled-device", UserID: disabled.ID, AuthTokenHash: hashDeviceToken(disabledToken)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := AuthenticateDevice(db, "disabled-device", disabledToken); ok {
		t.Fatal("device belonging to a disabled user authenticated")
	}
}

func TestAuthenticateLegacyTokenlessDeviceIsCIDRScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Device{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "legacy-owner", Password: "hashed", Status: 1}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{DeviceID: "legacy-phone", Name: "legacy-phone", UserID: owner.ID}).Error; err != nil {
		t.Fatal(err)
	}
	previous := config.App
	config.App = &config.Config{Security: config.SecurityConfig{DeviceLegacyTokenlessCIDRs: []string{"192.168.20.0/24"}}}
	t.Cleanup(func() { config.App = previous })

	if _, ok := AuthenticateLegacyTokenlessDevice(db, "legacy-phone", "192.168.20.17"); !ok {
		t.Fatal("configured LAN device was rejected")
	}
	if _, ok := AuthenticateLegacyTokenlessDevice(db, "legacy-phone", "192.168.21.17"); ok {
		t.Fatal("device outside configured LAN was accepted")
	}
	if err := db.Model(&owner).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := AuthenticateLegacyTokenlessDevice(db, "legacy-phone", "192.168.20.17"); ok {
		t.Fatal("device belonging to disabled owner was accepted")
	}
}

func TestDeviceTopicAccessIsScoped(t *testing.T) {
	if !deviceTopicFilterAllowed("dev-1", "cloud/device/dev-1/command") {
		t.Fatal("own command topic rejected")
	}
	if deviceTopicFilterAllowed("dev-1", "cloud/device/dev-2/command") {
		t.Fatal("other command topic accepted")
	}
	if deviceTopicAllowed("dev-1", "cloud/device/dev-2/event") {
		t.Fatal("other event topic accepted")
	}
	if deviceTopicAllowed("dev-1", "cloud/device/dev-1/event") == false {
		t.Fatal("own event topic rejected")
	}
}

func TestAutoRegisterDeviceIssuesTokenAndAssignsOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Device{}, &models.DeviceAutoRegistration{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "owner", Password: "hashed", Status: 1}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	previous := config.App
	config.App = nil
	t.Cleanup(func() { config.App = previous })

	token, device, err := AutoRegisterDevice(db, "EC-test-device", registrationInfo{Name: "Test Phone", Model: "Pixel"}, "10.10.10.2")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || device.UserID != owner.ID || device.AuthTokenHash == token {
		t.Fatalf("unexpected auto-registration result: token=%q owner=%d hash=%q", token, device.UserID, device.AuthTokenHash)
	}
	if _, ok := AuthenticateDevice(db, device.DeviceID, token); !ok {
		t.Fatal("issued token does not authenticate")
	}
	var audit models.DeviceAutoRegistration
	if err := db.Where("device_id = ?", device.DeviceID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.UserID != owner.ID {
		t.Fatalf("audit owner=%d, want %d", audit.UserID, owner.ID)
	}
	if audit.ConfirmedAt != nil || audit.RecoveryExpires == nil {
		t.Fatal("new auto-registration should remain recoverable until the mobile client confirms persistence")
	}
	reissued, recovered, err := RecoverAutoRegistrationDevice(db, device.DeviceID, registrationInfo{Name: "Recovered"}, "10.10.10.2")
	if err != nil {
		t.Fatal(err)
	}
	if reissued == token || recovered.Name != "Recovered" {
		t.Fatal("recovery did not rotate token and refresh registration metadata")
	}
	if _, ok := AuthenticateDevice(db, device.DeviceID, token); ok {
		t.Fatal("old bootstrap token remained valid after recovery")
	}
	if err := ConfirmAutoRegistration(db, device.DeviceID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecoverAutoRegistrationDevice(db, device.DeviceID, registrationInfo{}, "10.10.10.2"); !errors.Is(err, ErrAutoRegistrationRecoveryDenied) {
		t.Fatalf("confirmed registration recovered unexpectedly: %v", err)
	}
	if _, _, err := AutoRegisterDevice(db, device.DeviceID, registrationInfo{}, "10.10.10.2"); err == nil {
		t.Fatal("duplicate device was auto-registered")
	}
}

func TestAutoRegisterHonorsDeviceQuota(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Device{}, &models.DeviceAutoRegistration{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "limited", Password: "hashed", Status: 1, DeviceQuota: 1}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	previous := config.App
	config.App = nil
	t.Cleanup(func() { config.App = previous })
	if _, _, err := AutoRegisterDevice(db, "quota-1", registrationInfo{}, "10.10.10.3"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AutoRegisterDevice(db, "quota-2", registrationInfo{}, "10.10.10.3"); !errors.Is(err, ErrDeviceQuotaExceeded) {
		t.Fatalf("quota error = %v, want ErrDeviceQuotaExceeded", err)
	}
}
