package handlers

import (
	"testing"

	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newSystemTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Device{}, &models.DeviceGroup{}, &models.Script{}, &models.Task{},
		&models.Resource{}, &models.ParameterTemplate{}, &models.DataTemplate{}, &models.DataRecord{},
		&models.UserRole{}, &models.ScriptShare{}, &models.TaskShare{}, &models.ResourceShare{},
		&models.DataPermission{}, &models.DeviceAutoRegistration{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestUsersOwnRecordsBlocksDeletionWhenBusinessDataExists(t *testing.T) {
	db := newSystemTestDB(t)
	owner := models.User{Username: "owner", Password: "hashed"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	owned, err := usersOwnRecords(db, []uint64{owner.ID})
	if err != nil || owned {
		t.Fatalf("empty user records: owned=%v err=%v", owned, err)
	}
	if err := db.Create(&models.Device{DeviceID: "owner-device", UserID: owner.ID}).Error; err != nil {
		t.Fatal(err)
	}
	owned, err = usersOwnRecords(db, []uint64{owner.ID})
	if err != nil || !owned {
		t.Fatalf("device ownership was not detected: owned=%v err=%v", owned, err)
	}
}

func TestRemoveUserReferencesRemovesOnlyAffectedUser(t *testing.T) {
	db := newSystemTestDB(t)
	owner := models.User{Username: "owner", Password: "hashed"}
	other := models.User{Username: "other", Password: "hashed"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, record := range []interface{}{
		&models.UserRole{UserID: owner.ID, RoleID: 1},
		&models.ScriptShare{ScriptID: 1, FromUserID: owner.ID, ToUserID: other.ID},
		&models.ScriptShare{ScriptID: 2, FromUserID: other.ID, ToUserID: other.ID},
		&models.TaskShare{TaskID: 1, FromUserID: other.ID, ToUserID: owner.ID},
		&models.ResourceShare{ResourceID: 1, FromUserID: owner.ID, ToUserID: other.ID},
		&models.DataPermission{TemplateID: 1, UserID: owner.ID},
		&models.DeviceAutoRegistration{DeviceID: "owner-audit", UserID: owner.ID},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := removeUserReferences(db, []uint64{owner.ID, owner.ID}); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		model interface{}
		query string
		args  []interface{}
	}{
		{&models.UserRole{}, "user_id = ?", []interface{}{owner.ID}},
		{&models.ScriptShare{}, "from_user_id = ? OR to_user_id = ?", []interface{}{owner.ID, owner.ID}},
		{&models.TaskShare{}, "from_user_id = ? OR to_user_id = ?", []interface{}{owner.ID, owner.ID}},
		{&models.ResourceShare{}, "from_user_id = ? OR to_user_id = ?", []interface{}{owner.ID, owner.ID}},
		{&models.DataPermission{}, "user_id = ?", []interface{}{owner.ID}},
		{&models.DeviceAutoRegistration{}, "user_id = ?", []interface{}{owner.ID}},
	} {
		var count int64
		if err := db.Model(check.model).Where(check.query, check.args...).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("references for owner remained in %T: %d", check.model, count)
		}
	}
	var unrelated int64
	if err := db.Model(&models.ScriptShare{}).Where("from_user_id = ? AND to_user_id = ?", other.ID, other.ID).Count(&unrelated).Error; err != nil {
		t.Fatal(err)
	}
	if unrelated != 1 {
		t.Fatalf("unrelated sharing record was removed: %d", unrelated)
	}
}
