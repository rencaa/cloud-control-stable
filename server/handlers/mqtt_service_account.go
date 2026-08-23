package handlers

import (
	"fmt"
	"strings"

	"cloud-control-server/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsureMQTTServiceAccount upserts the external-broker bridge credential using
// the same SHA-256 representation already used by device credentials.
func EnsureMQTTServiceAccount(db *gorm.DB, username, password string) error {
	username = strings.TrimSpace(username)
	if db == nil || username == "" || password == "" {
		return fmt.Errorf("mqtt bridge credentials are required")
	}
	account := models.MQTTServiceAccount{
		Username:     username,
		PasswordHash: hashDeviceToken(password),
		Enabled:      true,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "username"}},
		DoUpdates: clause.AssignmentColumns([]string{"password_hash", "enabled", "updated_at"}),
	}).Create(&account).Error
}
