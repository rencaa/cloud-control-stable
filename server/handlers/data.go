package handlers

import (
	"encoding/json"
	"errors"
	"strings"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DataHandler struct{ DB *gorm.DB }

func (h *DataHandler) templateAccess(templateID, userID uint64, action string) (*models.DataTemplate, bool) {
	var template models.DataTemplate
	if h.DB.First(&template, templateID).Error != nil {
		return nil, false
	}
	if middleware.IsSystemAdminUser(h.DB, userID) || template.UserID == userID {
		return &template, true
	}
	var permission models.DataPermission
	if h.DB.Where("template_id = ? AND user_id = ?", templateID, userID).First(&permission).Error != nil {
		return nil, false
	}
	allowed := (action == "read" && permission.CanRead == 1) ||
		(action == "write" && permission.CanWrite == 1) ||
		(action == "delete" && permission.CanDelete == 1)
	if !allowed {
		return nil, false
	}
	return &template, true
}

func validJSONDocument(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	var document interface{}
	return json.Unmarshal([]byte(value), &document) == nil
}

// ListDataTemplates only returns owned templates or templates explicitly shared
// through data_permissions. is_shared is retained for compatibility but is not
// treated as a global authorization grant.
func (h *DataHandler) ListDataTemplates(c *gin.Context) {
	userID := middleware.GetUserID(c)
	query := h.DB.Model(&models.DataTemplate{})
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ? OR EXISTS (SELECT 1 FROM data_permissions dp WHERE dp.template_id = data_templates.id AND dp.user_id = ? AND dp.can_read = 1)", userID, userID)
	}
	var templates []models.DataTemplate
	if err := query.Order("id DESC").Find(&templates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "读取失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: templates})
}

func (h *DataHandler) CreateDataTemplate(c *gin.Context) {
	var template models.DataTemplate
	if err := c.ShouldBindJSON(&template); err != nil || strings.TrimSpace(template.Name) == "" || !validJSONDocument(template.Fields) {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	template.UserID = middleware.GetUserID(c)
	template.ID, template.IsShared = 0, 0
	if err := h.DB.Create(&template).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "创建失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "创建成功", Data: template})
}

func (h *DataHandler) UpdateDataTemplate(c *gin.Context) {
	id := c.Param("id")
	var template models.DataTemplate
	if err := h.DB.First(&template, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "模板不存在"})
		return
	}
	if _, ok := h.templateAccess(template.ID, middleware.GetUserID(c), "write"); !ok {
		c.JSON(200, models.Response{Code: 403, Message: "无权修改此模板"})
		return
	}
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	allowed := map[string]bool{"name": true, "description": true, "fields": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}
	if fields, ok := updates["fields"].(string); ok && !validJSONDocument(fields) {
		c.JSON(200, models.Response{Code: 400, Message: "fields必须是合法JSON"})
		return
	}
	if err := h.DB.Model(&template).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

func (h *DataHandler) DeleteDataTemplate(c *gin.Context) {
	var template models.DataTemplate
	if err := h.DB.First(&template, c.Param("id")).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "模板不存在"})
		return
	}
	if _, ok := h.templateAccess(template.ID, middleware.GetUserID(c), "delete"); !ok {
		c.JSON(200, models.Response{Code: 403, Message: "无权删除此模板"})
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("template_id = ?", template.ID).Delete(&models.DataRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("template_id = ?", template.ID).Delete(&models.DataPermission{}).Error; err != nil {
			return err
		}
		return tx.Delete(&template).Error
	})
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

func (h *DataHandler) ListDataRecords(c *gin.Context) {
	var page models.PageRequest
	_ = c.ShouldBindQuery(&page)
	page.Normalize(20, 200)
	userID := middleware.GetUserID(c)
	query := h.DB.Model(&models.DataRecord{}).Joins("JOIN data_templates dt ON dt.id = data_records.template_id")
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("dt.user_id = ? OR EXISTS (SELECT 1 FROM data_permissions dp WHERE dp.template_id = data_records.template_id AND dp.user_id = ? AND dp.can_read = 1)", userID, userID)
	}
	if templateID := strings.TrimSpace(c.Query("template_id")); templateID != "" {
		query = query.Where("data_records.template_id = ?", templateID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "读取失败"})
		return
	}
	var records []models.DataRecord
	if err := query.Offset((page.Page - 1) * page.Size).Limit(page.Size).Order("data_records.id DESC").Find(&records).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "读取失败"})
		return
	}
	c.JSON(200, models.PageResponse{Code: 200, Message: "成功", Data: records, Total: total, Page: page.Page, Size: page.Size})
}

func (h *DataHandler) CreateDataRecord(c *gin.Context) {
	var record models.DataRecord
	if err := c.ShouldBindJSON(&record); err != nil || record.TemplateID == 0 || !validJSONDocument(record.Data) {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if _, ok := h.templateAccess(record.TemplateID, middleware.GetUserID(c), "write"); !ok {
		c.JSON(200, models.Response{Code: 403, Message: "无权写入此模板"})
		return
	}
	record.ID, record.UserID = 0, middleware.GetUserID(c)
	if err := h.DB.Create(&record).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "创建失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "创建成功", Data: record})
}

func (h *DataHandler) UpdateDataRecord(c *gin.Context) {
	var record models.DataRecord
	if err := h.DB.First(&record, c.Param("id")).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "记录不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	template, ok := h.templateAccess(record.TemplateID, userID, "write")
	if !ok && record.UserID == userID {
		template, ok = h.templateAccess(record.TemplateID, userID, "read")
	}
	if !ok || (template.UserID != userID && record.UserID != userID && !middleware.IsSystemAdminUser(h.DB, userID)) {
		c.JSON(200, models.Response{Code: 403, Message: "无权修改此记录"})
		return
	}
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	for key := range updates {
		if key != "data" {
			delete(updates, key)
		}
	}
	if data, ok := updates["data"].(string); ok && !validJSONDocument(data) {
		c.JSON(200, models.Response{Code: 400, Message: "data必须是合法JSON"})
		return
	}
	if err := h.DB.Model(&record).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

func (h *DataHandler) DeleteDataRecord(c *gin.Context) {
	var record models.DataRecord
	if err := h.DB.First(&record, c.Param("id")).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "记录不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	if _, ok := h.templateAccess(record.TemplateID, userID, "delete"); !ok && record.UserID != userID {
		c.JSON(200, models.Response{Code: 403, Message: "无权删除此记录"})
		return
	}
	if err := h.DB.Delete(&record).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

func (h *DataHandler) ListDataPermissions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	query := h.DB.Model(&models.DataPermission{}).Joins("JOIN data_templates dt ON dt.id = data_permissions.template_id")
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("dt.user_id = ?", userID)
	}
	if templateID := strings.TrimSpace(c.Query("template_id")); templateID != "" {
		query = query.Where("data_permissions.template_id = ?", templateID)
	}
	var permissions []models.DataPermission
	if err := query.Find(&permissions).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "读取失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: permissions})
}

func (h *DataHandler) SetDataPermission(c *gin.Context) {
	var perm models.DataPermission
	if err := c.ShouldBindJSON(&perm); err != nil || perm.TemplateID == 0 || perm.UserID == 0 {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if _, ok := h.templateAccess(perm.TemplateID, middleware.GetUserID(c), "write"); !ok {
		c.JSON(200, models.Response{Code: 403, Message: "无权设置此模板权限"})
		return
	}
	var user models.User
	if err := h.DB.First(&user, perm.UserID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "目标用户不存在"})
		return
	}
	perm.CanRead = clampFlag(perm.CanRead)
	perm.CanWrite = clampFlag(perm.CanWrite)
	perm.CanDelete = clampFlag(perm.CanDelete)
	var existing models.DataPermission
	err := h.DB.Where("template_id = ? AND user_id = ?", perm.TemplateID, perm.UserID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = h.DB.Create(&perm).Error
	} else if err == nil {
		err = h.DB.Model(&existing).Updates(map[string]interface{}{"can_read": perm.CanRead, "can_write": perm.CanWrite, "can_delete": perm.CanDelete}).Error
	}
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "权限设置失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "权限设置成功"})
}

func clampFlag(value int8) int8 {
	if value != 0 {
		return 1
	}
	return 0
}

func (h *DataHandler) ListDataLogs(c *gin.Context) {
	var page models.PageRequest
	_ = c.ShouldBindQuery(&page)
	page.Normalize(20, 200)
	query := h.DB.Model(&models.SystemLog{}).Where("resource = ?", "data")
	if !middleware.IsSystemAdminUser(h.DB, middleware.GetUserID(c)) {
		query = query.Where("user_id = ?", middleware.GetUserID(c))
	}
	var total int64
	_ = query.Count(&total)
	var logs []models.SystemLog
	_ = query.Offset((page.Page - 1) * page.Size).Limit(page.Size).Order("id DESC").Find(&logs).Error
	c.JSON(200, models.PageResponse{Code: 200, Message: "成功", Data: logs, Total: total, Page: page.Page, Size: page.Size})
}

func NewDataHandler(db *gorm.DB) *DataHandler { return &DataHandler{DB: db} }
