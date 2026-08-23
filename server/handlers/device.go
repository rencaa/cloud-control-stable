package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DeviceHandler struct {
	DB *gorm.DB
}

// ListDevices 设备列表
func (h *DeviceHandler) ListDevices(c *gin.Context) {
	var page models.PageRequest
	c.ShouldBindQuery(&page)
	if page.Page <= 0 {
		page.Page = 1
	}
	if page.Size <= 0 {
		page.Size = 10
	}
	if page.Size > 200 {
		page.Size = 200
	}

	var total int64
	query := h.DB.Model(&models.Device{}).Preload("Group")

	// 搜索
	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("name LIKE ? OR device_id LIKE ? OR model LIKE ? OR ip LIKE ?", kw, kw, kw, kw)
	}

	// 状态筛选（仅当URL中显式传了status参数时才过滤）
	if c.Query("status") != "" {
		query = query.Where("status = ?", page.Status)
	}

	// 分组筛选
	if groupID := c.Query("group_id"); groupID != "" {
		query = query.Where("group_id = ?", groupID)
	}

	// 数据隔离：只有 system_admin 可以跨用户查看设备。
	userID := middleware.GetUserID(c)
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ?", userID)
	}

	query.Count(&total)

	var devices []models.Device
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&devices)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    devices,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// CreateDevice 注册设备
func (h *DeviceHandler) CreateDevice(c *gin.Context) {
	var device models.Device
	if err := c.ShouldBindJSON(&device); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	device.UserID = middleware.GetUserID(c)
	device.DeviceID = strings.TrimSpace(device.DeviceID)
	device.ID = 0
	device.Status = 0
	device.LastHeartbeat = nil
	device.RegisterAt = time.Time{}
	token, err := GenerateDeviceToken()
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "设备凭证生成失败"})
		return
	}
	device.AuthTokenHash = hashDeviceToken(token)
	device.DeviceToken = token

	// 自动生成device_id
	if device.DeviceID == "" {
		device.DeviceID = fmt.Sprintf("DEV-%d", time.Now().UnixMilli())
	}
	if !validDeviceID(device.DeviceID) {
		c.JSON(200, models.Response{Code: 400, Message: "设备ID格式无效"})
		return
	}
	if device.GroupID != 0 && !ensureGroupAccess(c, h.DB, fmt.Sprint(device.GroupID), true) {
		return
	}

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := enforceDeviceQuotaForUser(tx, device.UserID); err != nil {
			return err
		}
		return tx.Create(&device).Error
	}); err != nil {
		if errors.Is(err, ErrDeviceQuotaExceeded) {
			c.JSON(200, models.Response{Code: 400, Message: "设备数量已达配额上限"})
			return
		}
		c.JSON(200, models.Response{Code: 400, Message: "设备ID已存在或参数错误"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "设备注册成功，请保存设备凭证", Data: device})
}

// RotateDeviceToken invalidates the previous device credential immediately.
func (h *DeviceHandler) RotateDeviceToken(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceAccess(c, h.DB, id, true) {
		return
	}
	var device models.Device
	if err := h.DB.First(&device, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	token, err := GenerateDeviceToken()
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "设备凭证生成失败"})
		return
	}
	if err := h.DB.Model(&device).Update("auth_token_hash", hashDeviceToken(token)).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "设备凭证保存失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "设备凭证已更新，请保存新凭证", Data: gin.H{"device_id": device.DeviceID, "device_token": token}})
}

// UpdateDevice 更新设备信息
func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceAccess(c, h.DB, id, true) {
		return
	}

	var device models.Device
	if err := h.DB.First(&device, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	allowed := map[string]bool{"name": true, "model": true, "os_version": true, "province": true, "city": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}

	if err := h.DB.Model(&device).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}

	// 记录设备日志
	h.DB.Create(&models.DeviceLog{
		DeviceID: device.ID,
		LogType:  "info",
		Message:  "设备信息已更新",
	})

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteDevice 删除设备
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceAccess(c, h.DB, id, true) {
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, model := range []interface{}{&models.TaskDevice{}, &models.TaskLog{}, &models.DeviceLog{}, &models.DeviceMetric{}, &models.DeviceSms{}, &models.DeviceContact{}} {
			if err := tx.Where("device_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&models.Device{}, id).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// BatchDeleteDevices 批量删除设备
func (h *DeviceHandler) BatchDeleteDevices(c *gin.Context) {
	var req models.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !h.validateDeviceBatchAccess(c, req.IDs) {
		return
	}

	ids := uniqueUint64(req.IDs)
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, model := range []interface{}{&models.TaskDevice{}, &models.TaskLog{}, &models.DeviceLog{}, &models.DeviceMetric{}, &models.DeviceSms{}, &models.DeviceContact{}} {
			if err := tx.Where("device_id IN ?", ids).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Where("id IN ?", ids).Delete(&models.Device{}).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "批量删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "批量删除成功"})
}

// BatchResetDevices 批量重置设备执行状态
func (h *DeviceHandler) BatchResetDevices(c *gin.Context) {
	var req models.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !h.validateDeviceBatchAccess(c, req.IDs) {
		return
	}

	h.DB.Model(&models.TaskDevice{}).
		Where("device_id IN ? AND status IN (1,3,4)", req.IDs).
		Update("status", 0)

	c.JSON(200, models.Response{Code: 200, Message: "批量重置成功"})
}

// BatchAddDeviceGroup 批量添加设备到分组
func (h *DeviceHandler) BatchAddDeviceGroup(c *gin.Context) {
	var req models.BatchAddGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !h.validateDeviceBatchAccess(c, req.DeviceIDs) {
		return
	}
	if !ensureGroupAccess(c, h.DB, fmt.Sprint(req.GroupID), true) {
		return
	}

	if err := h.DB.Model(&models.Device{}).Where("id IN ?", uniqueUint64(req.DeviceIDs)).Update("group_id", req.GroupID).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "分组设置失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "分组设置成功"})
}

// BatchBuiltinTask 批量内置任务
func (h *DeviceHandler) BatchBuiltinTask(c *gin.Context) {
	var req models.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !h.validateDeviceBatchAccess(c, req.IDs) {
		return
	}

	h.DB.Model(&models.Device{}).Where("id IN ?", req.IDs).Update("is_builtin_task", 1)

	c.JSON(200, models.Response{Code: 200, Message: "内置任务设置成功"})
}

// UpdateDeviceParams 更新设备参数
func (h *DeviceHandler) UpdateDeviceParams(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceAccess(c, h.DB, id, true) {
		return
	}

	var params map[string]interface{}
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	encoded, err := json.Marshal(params)
	if err != nil || len(encoded) > maxScriptParamsBytes {
		c.JSON(200, models.Response{Code: 400, Message: "设备参数超过 64 KiB 限制"})
		return
	}
	if err := h.DB.Model(&models.Device{}).Where("id = ?", id).Update("device_params", string(encoded)).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "设备参数保存失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "参数更新成功"})
}

// GetDeviceLogs 获取设备日志
func (h *DeviceHandler) GetDeviceLogs(c *gin.Context) {
	var page models.PageRequest
	c.ShouldBindQuery(&page)
	if page.Page <= 0 {
		page.Page = 1
	}
	if page.Size <= 0 {
		page.Size = 20
	}
	if page.Size > 200 {
		page.Size = 200
	}

	id := c.Param("id")
	if !ensureDeviceAccess(c, h.DB, id, false) {
		return
	}

	var total int64
	query := h.DB.Model(&models.DeviceLog{}).Where("device_id = ?", id)
	if logType := strings.TrimSpace(c.Query("log_type")); logType != "" {
		query = query.Where("log_type = ?", normalizeDeviceLogType(logType))
	}
	query.Count(&total)

	var logs []models.DeviceLog
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&logs)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    logs,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// ============================================
// 设备分组管理
// ============================================

// ListDeviceGroups 分组列表
func (h *DeviceHandler) ListDeviceGroups(c *gin.Context) {
	var groups []models.DeviceGroup
	query := h.DB.Order("id DESC")
	userID := middleware.GetUserID(c)
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ?", userID)
	}
	query.Find(&groups)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: groups})
}

// CreateDeviceGroup 创建分组
func (h *DeviceHandler) CreateDeviceGroup(c *gin.Context) {
	var group models.DeviceGroup
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	group.UserID = middleware.GetUserID(c)

	h.DB.Create(&group)
	c.JSON(200, models.Response{Code: 200, Message: "分组创建成功", Data: group})
}

// UpdateDeviceGroup 更新分组
func (h *DeviceHandler) UpdateDeviceGroup(c *gin.Context) {
	id := c.Param("id")
	if !ensureGroupAccess(c, h.DB, id, true) {
		return
	}

	var group models.DeviceGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "分组不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	allowed := map[string]bool{"name": true, "description": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}
	if err := h.DB.Model(&group).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteDeviceGroup 删除分组
func (h *DeviceHandler) DeleteDeviceGroup(c *gin.Context) {
	id := c.Param("id")
	if !ensureGroupAccess(c, h.DB, id, true) {
		return
	}

	// 将分组下设备移出
	h.DB.Model(&models.Device{}).Where("group_id = ?", id).Update("group_id", 0)

	h.DB.Delete(&models.DeviceGroup{}, id)
	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// ResetGroupDevices 重置分组设备状态
func (h *DeviceHandler) ResetGroupDevices(c *gin.Context) {
	id := c.Param("id")
	if !ensureGroupAccess(c, h.DB, id, true) {
		return
	}

	// 获取分组下所有设备
	var deviceIDs []uint64
	h.DB.Model(&models.Device{}).Where("group_id = ?", id).Pluck("id", &deviceIDs)

	if len(deviceIDs) > 0 {
		h.DB.Model(&models.TaskDevice{}).
			Where("device_id IN ? AND status IN (1,3,4)", deviceIDs).
			Update("status", 0)
	}

	c.JSON(200, models.Response{Code: 200, Message: "重置成功"})
}

// GetGroupDevices 获取分组下设备列表
func (h *DeviceHandler) GetGroupDevices(c *gin.Context) {
	id := c.Param("id")
	if !ensureGroupAccess(c, h.DB, id, false) {
		return
	}
	var page models.PageRequest
	_ = c.ShouldBindQuery(&page)
	page.Normalize(50, 200)
	query := h.DB.Model(&models.Device{}).Where("group_id = ?", id)
	if page.Keyword != "" {
		keyword := "%" + page.Keyword + "%"
		query = query.Where("name LIKE ? OR device_id LIKE ?", keyword, keyword)
	}
	var total int64
	query.Count(&total)
	var devices []models.Device
	query.Order("id ASC").Offset((page.Page - 1) * page.Size).Limit(page.Size).Find(&devices)
	c.JSON(200, models.PageResponse{Code: 200, Message: "成功", Data: devices, Total: total, Page: page.Page, Size: page.Size})
}

func (h *DeviceHandler) validateDeviceBatchAccess(c *gin.Context, ids []uint64) bool {
	ids = uniqueUint64(ids)
	if len(ids) == 0 || len(ids) > 10000 {
		c.JSON(200, models.Response{Code: 400, Message: "设备数量无效"})
		return false
	}
	if middleware.IsSystemAdminUser(h.DB, middleware.GetUserID(c)) {
		return true
	}
	var count int64
	h.DB.Model(&models.Device{}).
		Where("id IN ? AND user_id = ?", ids, middleware.GetUserID(c)).
		Count(&count)
	if count != int64(len(ids)) {
		c.JSON(200, models.Response{Code: 403, Message: "包含无权操作的设备"})
		return false
	}
	return true
}

func NewDeviceHandler(db *gorm.DB) *DeviceHandler {
	return &DeviceHandler{DB: db}
}
