package handlers

import (
	"math"
	"strconv"
	"strings"

	"cloud-control-server/middleware"
	"cloud-control-server/models"
	"cloud-control-server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SystemHandler struct {
	DB *gorm.DB
}

// ============================================
// 用户管理 (管理员)
// ============================================

// ListUsers 用户列表
func (h *SystemHandler) ListUsers(c *gin.Context) {
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
	query := h.DB.Model(&models.User{}).Preload("Roles")

	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("username LIKE ? OR nickname LIKE ? OR email LIKE ?", kw, kw, kw)
	}

	query.Count(&total)

	var users []models.User
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&users)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    users,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// ListAdmins 管理员列表
func (h *SystemHandler) ListAdmins(c *gin.Context) {
	var users []models.User

	h.DB.Preload("Roles").
		Joins("JOIN user_roles ur ON ur.user_id = users.id").
		Joins("JOIN roles r ON r.id = ur.role_id").
		Where("r.code IN ?", []string{"system_admin", "admin"}).
		Group("users.id").
		Find(&users)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: users})
}

// CreateUser 创建用户
func (h *SystemHandler) CreateUser(c *gin.Context) {
	var req struct {
		Username    string `json:"username" binding:"required"`
		Password    string `json:"password" binding:"required"`
		Nickname    string `json:"nickname"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		DeviceQuota int    `json:"device_quota"`
		Status      int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if req.DeviceQuota < 0 || req.DeviceQuota > 1000000 {
		c.JSON(200, models.Response{Code: 400, Message: "设备配额无效"})
		return
	}
	if err := utils.ValidatePassword(req.Password); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "密码至少12个字符、不能以空白开头或结尾，且不能超过72字节"})
		return
	}

	// 检查用户名是否存在
	var count int64
	h.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(200, models.Response{Code: 400, Message: "用户名已存在"})
		return
	}

	hashedPwd, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "密码加密失败"})
		return
	}

	user := models.User{
		Username:    req.Username,
		Password:    hashedPwd,
		Nickname:    req.Nickname,
		Email:       req.Email,
		Phone:       req.Phone,
		DeviceQuota: req.DeviceQuota,
		Status:      req.Status,
	}
	if user.Status == 0 {
		user.Status = 1
	}

	if err := h.DB.Create(&user).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "用户名已存在或创建失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "用户创建成功", Data: user})
}

// UpdateUser 更新用户
func (h *SystemHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")

	var user models.User
	if err := h.DB.First(&user, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "用户不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	allowed := map[string]bool{"nickname": true, "email": true, "phone": true, "avatar": true, "status": true, "device_quota": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}
	if quota, exists := updates["device_quota"]; exists {
		value, ok := validDeviceQuota(quota)
		if !ok {
			c.JSON(200, models.Response{Code: 400, Message: "设备配额无效"})
			return
		}
		updates["device_quota"] = value
	}
	disableUser := false
	if status, exists := updates["status"]; exists {
		value, ok := validUserStatus(status)
		if !ok {
			c.JSON(200, models.Response{Code: 400, Message: "用户状态无效"})
			return
		}
		if value == 0 && (user.Username == "admin" || middleware.IsSystemAdminUser(h.DB, user.ID)) {
			c.JSON(200, models.Response{Code: 400, Message: "不能停用系统管理员"})
			return
		}
		updates["status"] = value
		disableUser = value == 0
		if disableUser {
			updates["token_version"] = user.TokenVersion + 1
		}
	}

	if err := h.DB.Model(&user).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}
	if disableUser && Hub != nil {
		Hub.DisconnectUserDevices(user.ID)
	}

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

func validUserStatus(value interface{}) (int8, bool) {
	switch v := value.(type) {
	case float64:
		if v == 0 || v == 1 {
			return int8(v), true
		}
	case int:
		if v == 0 || v == 1 {
			return int8(v), true
		}
	case int8:
		if v == 0 || v == 1 {
			return v, true
		}
	}
	return 0, false
}

func validDeviceQuota(value interface{}) (int, bool) {
	var quota int
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) || v < 0 || v > 1000000 {
			return 0, false
		}
		quota = int(v)
	case int:
		quota = v
	case int64:
		if v > 1000000 {
			return 0, false
		}
		quota = int(v)
	default:
		return 0, false
	}
	return quota, quota >= 0 && quota <= 1000000
}

// DeleteUser 删除用户
func (h *SystemHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	// 不允许删除admin
	var user models.User
	if err := h.DB.First(&user, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "用户不存在"})
		return
	}
	if user.Username == "admin" {
		c.JSON(200, models.Response{Code: 400, Message: "不能删除系统管理员"})
		return
	}

	if user.Username == "admin" || middleware.IsSystemAdminUser(h.DB, user.ID) {
		c.JSON(200, models.Response{Code: 400, Message: "不能删除系统管理员"})
		return
	}
	if owned, err := usersOwnRecords(h.DB, []uint64{user.ID}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "检查用户数据失败"})
		return
	} else if owned {
		c.JSON(200, models.Response{Code: 400, Message: "用户仍拥有设备或业务数据，请先转移或删除其数据"})
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := removeUserReferences(tx, []uint64{user.ID}); err != nil {
			return err
		}
		return tx.Delete(&models.User{}, user.ID).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// BatchDeleteUsers 批量删除用户
func (h *SystemHandler) BatchDeleteUsers(c *gin.Context) {
	var req models.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 1000 {
		c.JSON(200, models.Response{Code: 400, Message: "用户数量无效"})
		return
	}
	var protected int64
	h.DB.Model(&models.User{}).Joins("JOIN user_roles ur ON ur.user_id = users.id").Joins("JOIN roles r ON r.id = ur.role_id").Where("users.id IN ? AND (users.username = ? OR r.code = ?)", req.IDs, "admin", "system_admin").Count(&protected)
	if protected > 0 {
		c.JSON(200, models.Response{Code: 400, Message: "批量删除列表包含系统管理员"})
		return
	}
	ids := uniqueUint64(req.IDs)
	if owned, err := usersOwnRecords(h.DB, ids); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "检查用户数据失败"})
		return
	} else if owned {
		c.JSON(200, models.Response{Code: 400, Message: "批量删除包含仍拥有设备或业务数据的用户"})
		return
	}

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := removeUserReferences(tx, ids); err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&models.User{}).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "批量删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "批量删除成功"})
}

func usersOwnRecords(db *gorm.DB, userIDs []uint64) (bool, error) {
	userIDs = uniqueUint64(userIDs)
	if len(userIDs) == 0 {
		return false, nil
	}
	for _, model := range []interface{}{
		&models.Device{}, &models.DeviceGroup{}, &models.Script{}, &models.Task{},
		&models.Resource{}, &models.ParameterTemplate{}, &models.DataTemplate{}, &models.DataRecord{},
	} {
		var count int64
		if err := db.Model(model).Where("user_id IN ?", userIDs).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func removeUserReferences(tx *gorm.DB, userIDs []uint64) error {
	for _, deletion := range []struct {
		query string
		model interface{}
	}{
		{"user_id IN ?", &models.UserRole{}},
		{"from_user_id IN ? OR to_user_id IN ?", &models.ScriptShare{}},
		{"from_user_id IN ? OR to_user_id IN ?", &models.TaskShare{}},
		{"from_user_id IN ? OR to_user_id IN ?", &models.ResourceShare{}},
		{"user_id IN ?", &models.DataPermission{}},
		{"user_id IN ?", &models.DeviceAutoRegistration{}},
	} {
		args := []interface{}{userIDs}
		if strings.Count(deletion.query, "?") == 2 {
			args = append(args, userIDs)
		}
		if err := tx.Where(deletion.query, args...).Delete(deletion.model).Error; err != nil {
			return err
		}
	}
	return nil
}

// AssignUserRoles 分配用户角色
func (h *SystemHandler) AssignUserRoles(c *gin.Context) {
	id := c.Param("id")

	var req models.AssignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	userID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的用户ID"})
		return
	}
	var target models.User
	if err := h.DB.First(&target, userID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "用户不存在"})
		return
	}
	if len(req.RoleCodes) > 32 {
		c.JSON(200, models.Response{Code: 400, Message: "角色数量无效"})
		return
	}

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserRole{}).Error; err != nil {
			return err
		}
		for _, code := range uniqueStrings(req.RoleCodes) {
			var role models.Role
			if err := tx.Where("code = ?", code).First(&role).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.UserRole{UserID: userID, RoleID: role.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "角色不存在或分配失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "角色分配成功"})
}

// ============================================
// 角色管理
// ============================================

// ListRoles 角色列表
func (h *SystemHandler) ListRoles(c *gin.Context) {
	var roles []models.Role
	h.DB.Preload("Permissions").Order("id ASC").Find(&roles)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: roles})
}

// CreateRole 创建角色
func (h *SystemHandler) CreateRole(c *gin.Context) {
	var role models.Role
	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	role.ID = 0
	role.IsSystem = 0

	if err := h.DB.Create(&role).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "角色代码已存在"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "角色创建成功", Data: role})
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// UpdateRole 更新角色
func (h *SystemHandler) UpdateRole(c *gin.Context) {
	id := c.Param("id")

	var role models.Role
	if err := h.DB.First(&role, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "角色不存在"})
		return
	}

	// 不允许修改系统角色代码
	if role.IsSystem == 1 {
		c.JSON(200, models.Response{Code: 400, Message: "系统角色不可修改"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	delete(updates, "id")
	delete(updates, "is_system")
	h.DB.Model(&role).Updates(updates)

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteRole 删除角色
func (h *SystemHandler) DeleteRole(c *gin.Context) {
	id := c.Param("id")

	var role models.Role
	if err := h.DB.First(&role, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "角色不存在"})
		return
	}
	if role.IsSystem == 1 {
		c.JSON(200, models.Response{Code: 400, Message: "系统角色不可删除"})
		return
	}

	h.DB.Where("role_id = ?", id).Delete(&models.RolePermission{})
	h.DB.Where("role_id = ?", id).Delete(&models.UserRole{})
	h.DB.Delete(&models.Role{}, id)

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// ============================================
// 权限管理
// ============================================

// ListPermissions 权限列表
func (h *SystemHandler) ListPermissions(c *gin.Context) {
	var permissions []models.Permission
	h.DB.Find(&permissions)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: permissions})
}

// ============================================
// 系统日志
// ============================================

// ListSystemLogs 系统日志列表
func (h *SystemHandler) ListSystemLogs(c *gin.Context) {
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

	var total int64
	query := h.DB.Model(&models.SystemLog{})

	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("username LIKE ? OR action LIKE ? OR detail LIKE ?", kw, kw, kw)
	}

	query.Count(&total)

	var logs []models.SystemLog
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
// 额外需要的数据模型
// ============================================

// UserRole 用户角色关联 (复用models定义)
type UserRole = models.UserRole

// RolePermission 角色权限关联
type RolePermission = models.RolePermission

func NewSystemHandler(db *gorm.DB) *SystemHandler {
	return &SystemHandler{DB: db}
}
