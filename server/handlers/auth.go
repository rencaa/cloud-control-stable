package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"cloud-control-server/middleware"
	"cloud-control-server/models"
	"cloud-control-server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct {
	DB *gorm.DB
}

type loginLimitEntry struct {
	Failures    int
	WindowStart time.Time
	BlockedTill time.Time
}

const loginLimitMaxEntries = 4096

var loginLimit = struct {
	sync.Mutex
	entries map[string]loginLimitEntry
}{entries: make(map[string]loginLimitEntry)}

// Login 用户登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	limitKey := c.ClientIP() + "|" + strings.ToLower(strings.TrimSpace(req.Username))
	if !allowLoginAttempt(limitKey) {
		c.JSON(200, models.Response{Code: 429, Message: "登录尝试过于频繁，请稍后再试"})
		return
	}

	var user models.User
	if err := h.DB.Where("username = ? AND status = 1", req.Username).First(&user).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "用户名或密码错误"})
		return
	}

	if !utils.CheckPassword(req.Password, user.Password) {
		c.JSON(200, models.Response{Code: 400, Message: "用户名或密码错误"})
		return
	}
	resetLoginAttempts(limitKey)

	// 加载用户角色
	if err := h.DB.Preload("Roles").First(&user, user.ID).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "加载用户权限失败"})
		return
	}

	accessToken, refreshToken, err := middleware.GenerateToken(user.ID, user.Username, user.TokenVersion)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "生成token失败"})
		return
	}

	// 记录登录日志
	loginLog := models.SystemLog{
		UserID:   user.ID,
		Username: user.Username,
		Action:   "login",
		Resource: "auth",
		Detail:   "用户登录成功",
		IP:       c.ClientIP(),
	}
	if err := h.DB.Create(&loginLog).Error; err != nil {
		log.Printf("write login log failed for user %d: %v", user.ID, err)
	}

	// 更新最后登录信息
	now := time.Now()
	if err := h.DB.Model(&user).Updates(map[string]interface{}{
		"last_login_at": now,
		"last_login_ip": c.ClientIP(),
	}).Error; err != nil {
		log.Printf("update last login failed for user %d: %v", user.ID, err)
	}

	c.JSON(200, models.Response{
		Code:    200,
		Message: "登录成功",
		Data: models.LoginResponse{
			Token:        accessToken,
			RefreshToken: refreshToken,
			User:         &user,
		},
	})
}

func allowLoginAttempt(key string) bool {
	now := time.Now()
	loginLimit.Lock()
	defer loginLimit.Unlock()
	entry, known := loginLimit.entries[key]
	if !known && len(loginLimit.entries) >= loginLimitMaxEntries {
		for existingKey, existing := range loginLimit.entries {
			if now.Sub(existing.WindowStart) > 30*time.Minute && now.After(existing.BlockedTill) {
				delete(loginLimit.entries, existingKey)
			}
		}
		if len(loginLimit.entries) >= loginLimitMaxEntries {
			// Do not allocate an entry for attacker-controlled usernames once the
			// limiter is full. Existing keys continue to receive normal handling.
			return false
		}
	}
	if !entry.BlockedTill.IsZero() && now.Before(entry.BlockedTill) {
		return false
	}
	if entry.WindowStart.IsZero() || now.Sub(entry.WindowStart) > 10*time.Minute {
		entry = loginLimitEntry{WindowStart: now}
	}
	entry.Failures++
	// 允许普通输入错误，但连续失败达到阈值后短暂冷却。
	if entry.Failures >= 8 {
		entry.BlockedTill = now.Add(5 * time.Minute)
	}
	loginLimit.entries[key] = entry
	return entry.BlockedTill.IsZero() || !now.Before(entry.BlockedTill)
}

func resetLoginAttempts(key string) {
	loginLimit.Lock()
	delete(loginLimit.entries, key)
	loginLimit.Unlock()
}

// Logout 用户登出
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(200, models.Response{Code: 200, Message: "登出成功"})
}

// RefreshToken 刷新token
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(200, models.Response{Code: 401, Message: "认证失败"})
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		c.JSON(200, models.Response{Code: 401, Message: "token格式无效"})
		return
	}
	claims, err := middleware.ParseRefreshToken(strings.TrimSpace(parts[1]))
	if err != nil {
		c.JSON(200, models.Response{Code: 401, Message: "token无效"})
		return
	}
	var user models.User
	if err := h.DB.Select("id", "username", "status", "token_version").First(&user, claims.UserID).Error; err != nil || user.Status != 1 || (user.TokenVersion != 0 && claims.TokenVersion != user.TokenVersion) {
		c.JSON(200, models.Response{Code: 401, Message: "用户不存在或刷新令牌已失效"})
		return
	}

	accessToken, refreshToken, err := middleware.GenerateToken(user.ID, user.Username, user.TokenVersion)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "刷新token失败"})
		return
	}

	c.JSON(200, models.Response{
		Code:    200,
		Message: "刷新成功",
		Data: gin.H{
			"token": accessToken, "refresh_token": refreshToken,
		},
	})
}

// GetUserInfo 获取当前用户信息
func (h *AuthHandler) GetUserInfo(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var user models.User
	if err := h.DB.Preload("Roles.Permissions").First(&user, userID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "用户不存在"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: user})
}

// ChangePassword 修改密码
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	userID := middleware.GetUserID(c)

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "用户不存在"})
		return
	}

	if !utils.CheckPassword(req.OldPassword, user.Password) {
		c.JSON(200, models.Response{Code: 400, Message: "原密码错误"})
		return
	}
	if err := utils.ValidatePassword(req.NewPassword); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "新密码至少12个字符、不能以空白开头或结尾，且不能超过72字节"})
		return
	}

	hashedPwd, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "密码加密失败"})
		return
	}
	if err := h.DB.Model(&user).Updates(map[string]interface{}{"password": hashedPwd, "token_version": gorm.Expr("token_version + 1")}).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "密码修改失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "密码修改成功"})
}

// ============================================
// 系统配置 (个性化管理)
// ============================================

// GetSystemConfig 获取系统配置
func (h *AuthHandler) GetSystemConfig(c *gin.Context) {
	var configs []models.SystemConfig
	h.DB.Find(&configs)

	result := make(map[string]string)
	publicKeys := map[string]bool{
		"site_name": true, "site_title": true, "site_description": true,
		"site_logo": true, "login_background": true, "login_background_image": true,
		"theme_primary_color": true,
	}
	for _, cfg := range configs {
		if publicKeys[cfg.ConfigKey] {
			result[cfg.ConfigKey] = cfg.ConfigValue
		}
	}

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: result})
}

// UpdateSystemConfig 更新系统配置
func (h *AuthHandler) UpdateSystemConfig(c *gin.Context) {
	var configs map[string]string
	if err := c.ShouldBindJSON(&configs); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	allowedKeys := map[string]bool{
		"site_name": true, "site_title": true, "site_description": true,
		"site_logo": true, "login_background": true, "login_background_image": true,
		"theme_primary_color": true,
	}

	for key, value := range configs {
		if !allowedKeys[key] {
			continue
		}
		h.DB.Where("config_key = ?", key).
			Assign(models.SystemConfig{ConfigValue: value}).
			FirstOrCreate(&models.SystemConfig{ConfigKey: key, ConfigValue: value})
	}

	c.JSON(200, models.Response{Code: 200, Message: "配置更新成功"})
}

// UpdateProfile 更新个人资料
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	// 只允许更新特定字段
	allowedFields := map[string]bool{
		"nickname": true, "email": true, "phone": true, "avatar": true,
	}
	for k := range updates {
		if !allowedFields[k] {
			delete(updates, k)
		}
	}

	h.DB.Model(&models.User{}).Where("id = ?", userID).Updates(updates)

	// 返回更新后的用户信息
	var user models.User
	h.DB.Preload("Roles.Permissions").First(&user, userID)

	c.JSON(200, models.Response{Code: 200, Message: "更新成功", Data: user})
}

// InitAdminUser 初始化管理员账号 (仅首次运行)
func InitAdminUser(db *gorm.DB) {
	initRBAC(db)
	_ = db.Model(&models.User{}).Where("token_version = 0").Update("token_version", 1).Error
	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		// 新安装必须通过环境变量指定密码；未指定时生成一次性随机密码并打印到启动日志。
		adminPassword := strings.TrimSpace(os.Getenv("CLOUD_ADMIN_PASSWORD"))
		if adminPassword == "" {
			adminPassword = generateBootstrapPassword()
			log.Printf("WARNING: CLOUD_ADMIN_PASSWORD 未设置，已生成一次性 admin 密码：%s", adminPassword)
		}
		if err := utils.ValidatePassword(adminPassword); err != nil {
			log.Printf("FATAL: CLOUD_ADMIN_PASSWORD 无效：至少12个字符、不能以空白开头或结尾，且不能超过72字节")
			return
		}
		hashedPwd, hashErr := utils.HashPassword(adminPassword)
		if hashErr != nil {
			log.Printf("FATAL: Failed to hash admin password: %v", hashErr)
			return
		}
		admin = models.User{Username: "admin", Password: hashedPwd, Nickname: "系统管理员", Status: 1}
		if createErr := db.Create(&admin).Error; createErr != nil {
			log.Printf("create admin failed: %v", createErr)
			return
		}
	} else if err != nil {
		log.Printf("query admin failed: %v", err)
		return
	}
	ensureAdminRole(db)

	// 初始化系统配置
	initConfigs := []models.SystemConfig{
		{ConfigKey: "site_name", ConfigValue: "通用云控系统", Description: "站点名称"},
		{ConfigKey: "site_title", ConfigValue: "通用云控系统", Description: "站点标题"},
		{ConfigKey: "site_description", ConfigValue: "专业的设备管理与控制系统", Description: "站点描述"},
		{ConfigKey: "site_logo", ConfigValue: "", Description: "站点Logo"},
		{ConfigKey: "login_background", ConfigValue: "#000000", Description: "登录背景色"},
		{ConfigKey: "login_background_image", ConfigValue: "/bg.jpg", Description: "登录背景图"},
		{ConfigKey: "theme_primary_color", ConfigValue: "#1890ff", Description: "主题色"},
	}
	for _, cfg := range initConfigs {
		db.Where(models.SystemConfig{ConfigKey: cfg.ConfigKey}).
			FirstOrCreate(&cfg)
	}

}

func ensureAdminRole(db *gorm.DB) {
	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		return
	}
	var role models.Role
	if err := db.Where("code = ?", "system_admin").First(&role).Error; err != nil {
		return
	}
	var associations []models.UserRole
	if err := db.Where("user_id = ? AND role_id = ?", admin.ID, role.ID).
		Order("id ASC").Find(&associations).Error; err != nil {
		log.Printf("query admin role failed: %v", err)
		return
	}
	if len(associations) == 0 {
		if err := db.Create(&models.UserRole{UserID: admin.ID, RoleID: role.ID}).Error; err != nil {
			log.Printf("create admin role failed: %v", err)
		}
		return
	}
	if len(associations) > 1 {
		duplicateIDs := make([]uint64, 0, len(associations)-1)
		for _, association := range associations[1:] {
			duplicateIDs = append(duplicateIDs, association.ID)
		}
		if err := db.Where("id IN ?", duplicateIDs).Delete(&models.UserRole{}).Error; err != nil {
			log.Printf("remove duplicate admin roles failed: %v", err)
		}
	}
}

func generateBootstrapPassword() string {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "ChangeMe-" + time.Now().Format("20060102150405")
	}
	return "Admin-" + base64.RawURLEncoding.EncodeToString(buffer)
}

func initRBAC(db *gorm.DB) {
	roles := []models.Role{
		{Name: "系统管理员", Code: "system_admin", Description: "系统最高权限管理员", IsSystem: 1},
		{Name: "管理员", Code: "admin", Description: "普通管理员", IsSystem: 1},
		{Name: "普通用户", Code: "user", Description: "普通用户", IsSystem: 1},
	}
	for i := range roles {
		var existing models.Role
		if err := db.Where("code = ?", roles[i].Code).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&roles[i]).Error; err != nil {
				log.Printf("create role %s failed: %v", roles[i].Code, err)
			}
		} else if err == nil {
			roles[i] = existing
		} else {
			log.Printf("query role %s failed: %v", roles[i].Code, err)
		}
	}

	// 创建权限
	permissions := []models.Permission{
		{Name: "设备管理-查看", Code: "device:read", Resource: "device", Action: "read"},
		{Name: "设备管理-创建", Code: "device:create", Resource: "device", Action: "create"},
		{Name: "设备管理-编辑", Code: "device:update", Resource: "device", Action: "update"},
		{Name: "设备管理-删除", Code: "device:delete", Resource: "device", Action: "delete"},
		{Name: "脚本管理-查看", Code: "script:read", Resource: "script", Action: "read"},
		{Name: "脚本管理-创建", Code: "script:create", Resource: "script", Action: "create"},
		{Name: "脚本管理-编辑", Code: "script:update", Resource: "script", Action: "update"},
		{Name: "脚本管理-删除", Code: "script:delete", Resource: "script", Action: "delete"},
		{Name: "任务管理-查看", Code: "task:read", Resource: "task", Action: "read"},
		{Name: "任务管理-创建", Code: "task:create", Resource: "task", Action: "create"},
		{Name: "任务管理-编辑", Code: "task:update", Resource: "task", Action: "update"},
		{Name: "任务管理-删除", Code: "task:delete", Resource: "task", Action: "delete"},
		{Name: "任务管理-控制", Code: "task:control", Resource: "task", Action: "control"},
		{Name: "资源管理-查看", Code: "resource:read", Resource: "resource", Action: "read"},
		{Name: "资源管理-上传", Code: "resource:create", Resource: "resource", Action: "create"},
		{Name: "资源管理-删除", Code: "resource:delete", Resource: "resource", Action: "delete"},
		{Name: "模板管理-查看", Code: "template:read", Resource: "template", Action: "read"},
		{Name: "模板管理-编辑", Code: "template:write", Resource: "template", Action: "write"},
		{Name: "数据管理-查看", Code: "data:read", Resource: "data", Action: "read"},
		{Name: "数据管理-编辑", Code: "data:write", Resource: "data", Action: "write"},
		{Name: "系统管理-用户", Code: "system:user", Resource: "system", Action: "user"},
		{Name: "系统管理-角色", Code: "system:role", Resource: "system", Action: "role"},
		{Name: "系统管理-权限", Code: "system:permission", Resource: "system", Action: "permission"},
		{Name: "系统管理-日志", Code: "system:log", Resource: "system", Action: "log"},
		{Name: "系统管理-配置", Code: "system:config", Resource: "system", Action: "config"},
	}
	for i := range permissions {
		var existing models.Permission
		if err := db.Where("code = ?", permissions[i].Code).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&permissions[i]).Error; err != nil {
				log.Printf("create permission %s failed: %v", permissions[i].Code, err)
			}
		} else if err == nil {
			permissions[i] = existing
		} else {
			log.Printf("query permission %s failed: %v", permissions[i].Code, err)
		}
	}

	// system_admin 拥有所有权限
	for _, perm := range permissions {
		if err := db.Where("role_id = ? AND permission_id = ?", roles[0].ID, perm.ID).FirstOrCreate(&models.RolePermission{RoleID: roles[0].ID, PermissionID: perm.ID}).Error; err != nil {
			log.Printf("grant system permission %s failed: %v", perm.Code, err)
		}
	}

	// admin 拥有非系统管理权限
	for _, perm := range permissions {
		if perm.Resource != "system" {
			if err := db.Where("role_id = ? AND permission_id = ?", roles[1].ID, perm.ID).FirstOrCreate(&models.RolePermission{RoleID: roles[1].ID, PermissionID: perm.ID}).Error; err != nil {
				log.Printf("grant admin permission %s failed: %v", perm.Code, err)
			}
		}
	}
}

// NewAuthHandler 创建Auth处理器
func NewAuthHandler(db *gorm.DB) *AuthHandler {
	return &AuthHandler{DB: db}
}

// ConfigHandler 配置处理器别名
type ConfigHandler = AuthHandler
