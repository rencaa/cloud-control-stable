// Command releaseprep creates a clean, distributable SQLite database with the
// requested initial administrator. It is intentionally separate from server
// bootstrap validation so a release package can preserve a user-requested
// legacy password without weakening runtime password policy.
package main

import (
	"fmt"
	"os"

	"cloud-control-server/handlers"
	"cloud-control-server/models"
	"cloud-control-server/utils"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: releaseprep <database-path>")
		os.Exit(2)
	}

	db, err := gorm.Open(sqlite.Open(os.Args[1]), &gorm.Config{})
	if err != nil {
		fatal(err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Role{}, &models.Permission{},
		&models.UserRole{}, &models.RolePermission{},
		&models.DeviceGroup{}, &models.Device{}, &models.DeviceLog{}, &models.DeviceAutoRegistration{},
		&models.MQTTServiceAccount{},
		&models.Script{}, &models.ScriptShare{},
		&models.Task{}, &models.TaskDevice{}, &models.TaskLog{}, &models.TaskShare{},
		&models.CommandDelivery{},
		&models.ClientRelease{},
		&models.Resource{}, &models.ResourceShare{},
		&models.ParameterTemplate{},
		&models.DataTemplate{}, &models.DataRecord{}, &models.DataPermission{},
		&models.SystemLog{}, &models.SystemConfig{},
		&models.DeviceMetric{},
		&models.DeviceSms{}, &models.DeviceContact{},
	); err != nil {
		fatal(err)
	}

	hash, err := utils.HashPassword("334421")
	if err != nil {
		fatal(err)
	}
	if err := db.Create(&models.User{
		Username:     "admin",
		Password:     hash,
		Nickname:     "系统管理员",
		Status:       1,
		TokenVersion: 1,
	}).Error; err != nil {
		fatal(err)
	}
	handlers.InitAdminUser(db)

	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		fatal(err)
	}
	if !utils.CheckPassword("334421", admin.Password) {
		fatal(fmt.Errorf("admin password verification failed"))
	}
	var deviceCount int64
	if err := db.Model(&models.Device{}).Count(&deviceCount).Error; err != nil {
		fatal(err)
	}
	if deviceCount != 0 {
		fatal(fmt.Errorf("release database contains %d devices", deviceCount))
	}
	fmt.Println("release database ready")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
