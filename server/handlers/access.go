package handlers

import (
	"errors"
	"strconv"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errResourceForbidden = errors.New("resource access denied")

// loadTaskForUser 读取任务时允许拥有者和共享任务；写操作只允许拥有者或系统管理员。
func loadTaskForUser(db *gorm.DB, id uint64, userID uint64, write bool, preloadScript bool) (*models.Task, error) {
	var task models.Task
	query := db
	if preloadScript {
		query = query.Preload("Script")
	}
	if err := query.First(&task, id).Error; err != nil {
		return nil, err
	}
	if middleware.IsSystemAdminUser(db, userID) || task.UserID == userID {
		return &task, nil
	}
	var shared int64
	if !write {
		db.Model(&models.TaskShare{}).Where("task_id = ? AND to_user_id = ?", id, userID).Count(&shared)
	}
	if !write && shared > 0 {
		return &task, nil
	}
	return nil, errResourceForbidden
}

func deviceIDsForUser(db *gorm.DB, ids []uint64, userID uint64) ([]uint64, error) {
	ids = uniqueUint64(ids)
	if len(ids) == 0 || middleware.IsSystemAdminUser(db, userID) {
		return ids, nil
	}
	var allowed []uint64
	if err := db.Model(&models.Device{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Pluck("id", &allowed).Error; err != nil {
		return nil, err
	}
	if len(allowed) != len(ids) {
		return nil, errResourceForbidden
	}
	return allowed, nil
}

func uniqueUint64(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func ensureTaskAccess(c *gin.Context, db *gorm.DB, id string, write bool) bool {
	taskID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的任务ID"})
		return false
	}
	_, err = loadTaskForUser(db, taskID, middleware.GetUserID(c), write, false)
	if err == nil {
		return true
	}
	if errors.Is(err, errResourceForbidden) {
		c.JSON(200, models.Response{Code: 403, Message: "无权访问此任务"})
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return false
	}
	c.JSON(200, models.Response{Code: 500, Message: "读取任务失败"})
	return false
}

func ensureDeviceAccess(c *gin.Context, db *gorm.DB, id string, write bool) bool {
	deviceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的设备ID"})
		return false
	}
	var device models.Device
	if err := db.First(&device, deviceID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return false
	}
	userID := middleware.GetUserID(c)
	if middleware.IsSystemAdminUser(db, userID) || device.UserID == userID {
		return true
	}
	_ = write
	c.JSON(200, models.Response{Code: 403, Message: "无权访问此设备"})
	return false
}

func ensureDeviceStringAccess(c *gin.Context, db *gorm.DB, deviceID string) bool {
	var device models.Device
	if err := db.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return false
	}
	userID := middleware.GetUserID(c)
	if middleware.IsSystemAdminUser(db, userID) || device.UserID == userID {
		return true
	}
	c.JSON(200, models.Response{Code: 403, Message: "无权访问此设备"})
	return false
}

func ensureGroupAccess(c *gin.Context, db *gorm.DB, id string, write bool) bool {
	groupID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的分组ID"})
		return false
	}
	var group models.DeviceGroup
	if err := db.First(&group, groupID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "分组不存在"})
		return false
	}
	userID := middleware.GetUserID(c)
	if middleware.IsSystemAdminUser(db, userID) || group.UserID == userID {
		return true
	}
	_ = write
	c.JSON(200, models.Response{Code: 403, Message: "无权访问此分组"})
	return false
}

func ensureResourceAccess(c *gin.Context, db *gorm.DB, id string, write bool) bool {
	resourceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的资源ID"})
		return false
	}
	var resource models.Resource
	if err := db.First(&resource, resourceID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "资源不存在"})
		return false
	}
	userID := middleware.GetUserID(c)
	var shared int64
	if !write {
		db.Model(&models.ResourceShare{}).Where("resource_id = ? AND to_user_id = ?", resourceID, userID).Count(&shared)
	}
	if middleware.IsSystemAdminUser(db, userID) || resource.UserID == userID || (!write && shared > 0) {
		return true
	}
	c.JSON(200, models.Response{Code: 403, Message: "无权访问此资源"})
	return false
}
