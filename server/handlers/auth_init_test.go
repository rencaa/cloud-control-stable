package handlers

import (
	"testing"

	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestInitAdminUserRepairsFreshDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Role{}, &models.Permission{}, &models.UserRole{}, &models.RolePermission{}, &models.SystemConfig{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUD_ADMIN_PASSWORD", "InitSmokePass-20260728!")
	InitAdminUser(db)
	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	var roleCount int64
	db.Model(&models.UserRole{}).Where("user_id = ?", admin.ID).Count(&roleCount)
	if roleCount != 1 {
		t.Fatalf("expected one admin role, got %d", roleCount)
	}
	var permissionCount int64
	db.Model(&models.Permission{}).Count(&permissionCount)
	if permissionCount == 0 {
		t.Fatal("permissions were not initialized")
	}

	// A second startup must be idempotent and must not create duplicates.
	InitAdminUser(db)
	var admins int64
	db.Model(&models.User{}).Where("username = ?", "admin").Count(&admins)
	if admins != 1 {
		t.Fatalf("expected one admin after restart, got %d", admins)
	}
}
