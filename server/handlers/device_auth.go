package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const autoRegistrationRecoveryWindow = 5 * time.Minute

var ErrDeviceQuotaExceeded = errors.New("device quota exceeded")
var ErrAutoRegistrationRecoveryDenied = errors.New("auto-registration recovery denied")

// GenerateDeviceToken creates a high-entropy per-device credential. It is
// returned only when a device is created or its credential is rotated.
func GenerateDeviceToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// AuthenticateDevice verifies a device token without ever storing the raw
// token in the database.
func AuthenticateDevice(db *gorm.DB, deviceID, token string) (models.Device, bool) {
	var device models.Device
	if db == nil || strings.TrimSpace(deviceID) == "" || strings.TrimSpace(token) == "" {
		return device, false
	}
	if err := db.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		return device, false
	}
	if device.AuthTokenHash == "" || device.AuthTokenHash != hashDeviceToken(token) {
		return device, false
	}
	// Legacy/system devices with user_id=0 remain supported. Devices owned by
	// a disabled or deleted account must not be allowed to reconnect.
	if device.UserID == 0 {
		return device, true
	}
	var user models.User
	if err := db.Select("id", "status").First(&user, device.UserID).Error; err != nil || user.Status != 1 {
		return device, false
	}
	return device, true
}

// AutoRegisterDevice creates the first identity for a previously unknown
// device and returns its raw token exactly once to the live connection.
func AutoRegisterDevice(db *gorm.DB, deviceID string, info registrationInfo, sourceIP string) (string, models.Device, error) {
	var device models.Device
	if db == nil {
		return "", device, gorm.ErrInvalidDB
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", device, gorm.ErrInvalidData
	}
	token, err := GenerateDeviceToken()
	if err != nil {
		return "", device, err
	}
	info = normalizeRegistrationInfo(info)
	sourceIP = trimRegistrationField(sourceIP, 64)
	now := time.Now()
	recoveryExpires := now.Add(autoRegistrationRecoveryWindow)
	err = db.Transaction(func(tx *gorm.DB) error {
		owner, err := autoRegisterOwner(tx)
		if err != nil {
			return err
		}
		if err := enforceDeviceQuota(tx, owner); err != nil {
			return err
		}
		device = models.Device{
			DeviceID: deviceID, Name: info.Name, Model: info.Model, OSVersion: info.OSVersion,
			AgentVersion: info.AgentVersion, ProtocolVersion: info.ProtocolVersion, Capabilities: info.Capabilities,
			IP: info.IP, Province: info.Province, City: info.City, Status: 1, UserID: owner.ID,
			AuthTokenHash: hashDeviceToken(token), LastHeartbeat: &now, RegisterAt: now,
		}
		if device.Name == "" {
			device.Name = deviceID
		}
		if err := tx.Create(&device).Error; err != nil {
			return err
		}
		return tx.Create(&models.DeviceAutoRegistration{
			DeviceID: deviceID, UserID: owner.ID, SourceIP: sourceIP, RegisteredAt: now,
			LastIssuedAt: now, RecoveryExpires: &recoveryExpires,
		}).Error
	})
	if err != nil {
		return "", models.Device{}, err
	}
	return token, device, nil
}

// RecoverAutoRegistrationDevice reissues a token only for an unconfirmed,
// short-lived bootstrap from the same source address. It covers the narrow
// case where the first WebSocket response was lost before local persistence.
func RecoverAutoRegistrationDevice(db *gorm.DB, deviceID string, info registrationInfo, sourceIP string) (string, models.Device, error) {
	var device models.Device
	if db == nil {
		return "", device, gorm.ErrInvalidDB
	}
	token, err := GenerateDeviceToken()
	if err != nil {
		return "", device, err
	}
	info = normalizeRegistrationInfo(info)
	sourceIP = trimRegistrationField(sourceIP, 64)
	now := time.Now()
	recoveryExpires := now.Add(autoRegistrationRecoveryWindow)
	err = db.Transaction(func(tx *gorm.DB) error {
		var audit models.DeviceAutoRegistration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ?", deviceID).First(&audit).Error; err != nil {
			return ErrAutoRegistrationRecoveryDenied
		}
		if audit.ConfirmedAt != nil || audit.RecoveryExpires == nil || !audit.RecoveryExpires.After(now) || audit.SourceIP != sourceIP {
			return ErrAutoRegistrationRecoveryDenied
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ? AND user_id = ?", deviceID, audit.UserID).First(&device).Error; err != nil {
			return ErrAutoRegistrationRecoveryDenied
		}
		updates := map[string]interface{}{"auth_token_hash": hashDeviceToken(token), "status": 1, "last_heartbeat": now}
		if info.Name != "" {
			updates["name"] = info.Name
		}
		if info.Model != "" {
			updates["model"] = info.Model
		}
		if info.OSVersion != "" {
			updates["os_version"] = info.OSVersion
		}
		if info.AgentVersion != "" {
			updates["agent_version"] = info.AgentVersion
		}
		if info.ProtocolVersion > 0 {
			updates["protocol_version"] = info.ProtocolVersion
		}
		if info.Capabilities != "" {
			updates["capabilities"] = info.Capabilities
		}
		if info.IP != "" {
			updates["ip"] = info.IP
		}
		if info.Province != "" {
			updates["province"] = info.Province
		}
		if info.City != "" {
			updates["city"] = info.City
		}
		if err := tx.Model(&device).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&audit).Updates(map[string]interface{}{"last_issued_at": now, "recovery_expires_at": recoveryExpires}).Error; err != nil {
			return err
		}
		return tx.Where("device_id = ?", deviceID).First(&device).Error
	})
	if err != nil {
		return "", models.Device{}, err
	}
	return token, device, nil
}

func ConfirmAutoRegistration(db *gorm.DB, deviceID string, userID uint64) error {
	if db == nil || userID == 0 {
		return nil
	}
	now := time.Now()
	return db.Model(&models.DeviceAutoRegistration{}).
		Where("device_id = ? AND user_id = ? AND confirmed_at IS NULL", deviceID, userID).
		Updates(map[string]interface{}{"confirmed_at": now, "recovery_expires_at": nil}).Error
}

func autoRegisterOwner(db *gorm.DB) (models.User, error) {
	var user models.User
	if config.App != nil && config.App.Security.AutoRegisterUserID != 0 {
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", config.App.Security.AutoRegisterUserID, 1).First(&user).Error; err != nil {
			return user, err
		}
		return user, nil
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status = ?", 1).Order("id ASC").First(&user).Error; err != nil {
		return user, err
	}
	return user, nil
}

func enforceDeviceQuota(db *gorm.DB, user models.User) error {
	if user.Status != 1 {
		return gorm.ErrRecordNotFound
	}
	if user.DeviceQuota <= 0 {
		return nil
	}
	var count int64
	if err := db.Model(&models.Device{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
		return err
	}
	if count >= int64(user.DeviceQuota) {
		return ErrDeviceQuotaExceeded
	}
	return nil
}

func enforceDeviceQuotaForUser(db *gorm.DB, userID uint64) error {
	if userID == 0 {
		return gorm.ErrRecordNotFound
	}
	var user models.User
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", userID, 1).First(&user).Error; err != nil {
		return err
	}
	return enforceDeviceQuota(db, user)
}

func normalizeRegistrationInfo(info registrationInfo) registrationInfo {
	return registrationInfo{
		Name: trimRegistrationField(info.Name, 128), Model: trimRegistrationField(info.Model, 128),
		OSVersion: trimRegistrationField(info.OSVersion, 64), AgentVersion: trimRegistrationField(info.AgentVersion, 32),
		ProtocolVersion: info.ProtocolVersion, Capabilities: trimRegistrationField(info.Capabilities, 2048),
		IP:       trimRegistrationField(info.IP, 64),
		Province: trimRegistrationField(info.Province, 32), City: trimRegistrationField(info.City, 32),
	}
}

func trimRegistrationField(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}
