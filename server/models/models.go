package models

import (
	"database/sql"
	"time"
)

// ============================================
// 用户与权限
// ============================================

// User 用户模型
type User struct {
	ID           uint64       `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string       `json:"username" gorm:"unique;size:64;not null"`
	Password     string       `json:"-" gorm:"size:255;not null"`
	Nickname     string       `json:"nickname" gorm:"size:128"`
	Email        string       `json:"email" gorm:"size:128"`
	Phone        string       `json:"phone" gorm:"size:32"`
	Avatar       string       `json:"avatar" gorm:"size:255"`
	Status       int8         `json:"status" gorm:"default:1"`
	TokenVersion uint64       `json:"-" gorm:"default:1"`
	DeviceQuota  int          `json:"device_quota" gorm:"default:0"`
	LastLoginAt  sql.NullTime `json:"last_login_at"`
	LastLoginIP  string       `json:"last_login_ip" gorm:"size:64"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	Roles        []Role       `json:"roles" gorm:"many2many:user_roles;foreignKey:id;joinForeignKey:user_id;References:id;joinReferences:role_id"`
}

func (User) TableName() string { return "users" }

// Role 角色模型
type Role struct {
	ID          uint64       `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string       `json:"name" gorm:"size:64;not null"`
	Code        string       `json:"code" gorm:"unique;size:64;not null"`
	Description string       `json:"description" gorm:"size:255"`
	IsSystem    int8         `json:"is_system" gorm:"default:0"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Permissions []Permission `json:"permissions" gorm:"many2many:role_permissions;foreignKey:id;joinForeignKey:role_id;References:id;joinReferences:permission_id"`
}

func (Role) TableName() string { return "roles" }

// Permission 权限模型
type Permission struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"size:64;not null"`
	Code        string    `json:"code" gorm:"unique;size:128;not null"`
	Resource    string    `json:"resource" gorm:"size:64;not null"`
	Action      string    `json:"action" gorm:"size:64;not null"`
	Description string    `json:"description" gorm:"size:255"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Permission) TableName() string { return "permissions" }

// ============================================
// 设备管理
// ============================================

// DeviceGroup 设备分组
type DeviceGroup struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"size:255"`
	UserID      uint64    `json:"user_id" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (DeviceGroup) TableName() string { return "device_groups" }

// Device 设备模型
type Device struct {
	ID               uint64       `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID         string       `json:"device_id" gorm:"unique;size:128;not null"`
	Name             string       `json:"name" gorm:"size:128"`
	Model            string       `json:"model" gorm:"size:128"`
	OSVersion        string       `json:"os_version" gorm:"size:64"`
	AgentVersion     string       `json:"agent_version" gorm:"size:32;index"`
	ProtocolVersion  int          `json:"protocol_version" gorm:"default:1"`
	Capabilities     string       `json:"capabilities" gorm:"type:text"`
	AgentOutboxDepth int          `json:"agent_outbox_depth" gorm:"default:0;index"`
	IP               string       `json:"ip" gorm:"size:64"`
	Status           int8         `json:"status" gorm:"default:0;index;index:idx_devices_user_status_heartbeat,priority:2"`
	Province         string       `json:"province" gorm:"size:32"`
	City             string       `json:"city" gorm:"size:32"` // 0离线 1在线 2忙碌 3执行中
	GroupID          uint64       `json:"group_id" gorm:"default:0;index"`
	UserID           uint64       `json:"user_id" gorm:"default:0;index;index:idx_devices_user_status_heartbeat,priority:1"`
	AuthTokenHash    string       `json:"-" gorm:"size:64;index"`
	DeviceToken      string       `json:"device_token,omitempty" gorm:"-"`
	DeviceParams     string       `json:"device_params" gorm:"type:json;null"`
	IsBuiltinTask    int8         `json:"is_builtin_task" gorm:"default:0"`
	LastHeartbeat    *time.Time   `json:"last_heartbeat" gorm:"index;index:idx_devices_user_status_heartbeat,priority:3"`
	RegisterAt       time.Time    `json:"register_at"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	Group            *DeviceGroup `json:"group,omitempty"`
}

func (Device) TableName() string { return "devices" }

// DeviceAutoRegistration records server-side zero-touch enrollment metadata.
// It is intentionally not exposed through the API; the device receives only
// its per-device token over the authenticated registration response.
type DeviceAutoRegistration struct {
	ID              uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID        string     `json:"device_id" gorm:"unique;size:128;not null"`
	UserID          uint64     `json:"user_id" gorm:"index;not null"`
	SourceIP        string     `json:"-" gorm:"size:64"`
	RegisteredAt    time.Time  `json:"registered_at"`
	LastIssuedAt    time.Time  `json:"last_issued_at"`
	ConfirmedAt     *time.Time `json:"confirmed_at" gorm:"index"`
	RecoveryExpires *time.Time `json:"recovery_expires_at" gorm:"column:recovery_expires_at;index"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (DeviceAutoRegistration) TableName() string { return "device_auto_registrations" }

// MQTTServiceAccount is used only by the API-to-broker bridge. Device
// credentials continue to live on the devices table.
type MQTTServiceAccount struct {
	ID           uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string    `json:"username" gorm:"uniqueIndex;size:128;not null"`
	PasswordHash string    `json:"-" gorm:"size:64;not null"`
	Enabled      bool      `json:"enabled" gorm:"default:true"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (MQTTServiceAccount) TableName() string { return "mqtt_service_accounts" }

// DeviceLog 设备日志
type DeviceLog struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  uint64    `json:"device_id" gorm:"not null;index;index:idx_device_logs_device_created,priority:1"`
	LogType   string    `json:"log_type" gorm:"size:32;default:info"`
	Message   string    `json:"message" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at" gorm:"index;index:idx_device_logs_device_created,priority:2"`
}

func (DeviceLog) TableName() string { return "device_logs" }

// ============================================
// 脚本管理
// ============================================

// Script 脚本模型
type Script struct {
	ID            uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name          string    `json:"name" gorm:"size:255;not null"`
	Description   string    `json:"description" gorm:"size:512"`
	Filename      string    `json:"filename" gorm:"size:255;not null"`
	Content       string    `json:"content" gorm:"type:longtext"`
	Params        string    `json:"params" gorm:"type:json;null"`
	UserID        uint64    `json:"user_id" gorm:"default:0"`
	IsShared      int8      `json:"is_shared" gorm:"default:0"`
	DownloadCount int       `json:"download_count" gorm:"default:0"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Script) TableName() string { return "scripts" }

// ============================================
// 任务管理
// ============================================

// Task 任务模型
type Task struct {
	ID             uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name           string     `json:"name" gorm:"size:255;not null"`
	Description    string     `json:"description" gorm:"size:512"`
	ScriptID       uint64     `json:"script_id" gorm:"not null"`
	Params         string     `json:"params" gorm:"type:json;null"`
	CronExpr       string     `json:"cron_expr" gorm:"size:128"`
	CronEnabled    bool       `json:"cron_enabled" gorm:"default:false"`
	CronTimezone   string     `json:"cron_timezone" gorm:"size:64;default:Asia/Shanghai"`
	MisfirePolicy  string     `json:"misfire_policy" gorm:"size:16;default:latest"`
	TimeoutSeconds int        `json:"timeout_seconds" gorm:"default:3600"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at" gorm:"index"`
	Status         int8       `json:"status" gorm:"default:0"` // 0停止 1运行中 2已完成
	Priority       int        `json:"priority" gorm:"default:0"`
	UserID         uint64     `json:"user_id" gorm:"default:0"`
	IsShared       int8       `json:"is_shared" gorm:"default:0"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Script         *Script    `json:"script,omitempty"`
	Devices        []Device   `json:"devices,omitempty" gorm:"-"`
}

func (Task) TableName() string { return "tasks" }

// TaskDevice 任务设备关联
type TaskDevice struct {
	ID         uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID     uint64     `json:"task_id" gorm:"not null;index;index:idx_task_device,priority:1;index:idx_task_devices_task_status,priority:1"`
	DeviceID   uint64     `json:"device_id" gorm:"not null;index;index:idx_task_device,priority:2"`
	Status     int8       `json:"status" gorm:"default:0;index"` // 0待执行 1执行中 2成功 3失败 4异常
	RunID      string     `json:"run_id" gorm:"size:96;index"`
	Result     string     `json:"result" gorm:"type:text"`
	StartedAt  *time.Time `json:"started_at"`
	DeadlineAt *time.Time `json:"deadline_at" gorm:"index"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Device     *Device    `json:"device,omitempty"`
}

func (TaskDevice) TableName() string { return "task_devices" }

// CommandDelivery is the durable outbox for opt-in reliable device commands.
// Existing deployments keep using the direct socket path until the feature flag
// is enabled, so this additive table is safe to create ahead of a rollout.
type CommandDelivery struct {
	ID             uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	CommandID      string     `json:"command_id" gorm:"size:96;not null;uniqueIndex:idx_delivery_command_device,priority:1"`
	DeviceID       string     `json:"device_id" gorm:"size:128;not null;uniqueIndex:idx_delivery_command_device,priority:2;index:idx_delivery_device"`
	TaskID         uint64     `json:"task_id" gorm:"default:0;index:idx_delivery_task"`
	RunID          string     `json:"run_id" gorm:"size:96;index"`
	MessageType    string     `json:"message_type" gorm:"size:32;not null"`
	Payload        string     `json:"-" gorm:"type:longtext;not null"`
	Status         string     `json:"status" gorm:"size:16;not null;index:idx_delivery_retry,priority:1"`
	Attempts       int        `json:"attempts" gorm:"default:0"`
	NextRetryAt    time.Time  `json:"next_retry_at" gorm:"index:idx_delivery_retry,priority:2"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	LastError      string     `json:"last_error" gorm:"size:512"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (CommandDelivery) TableName() string { return "command_deliveries" }

// ClientRelease is a signed, staged mobile-agent release. Only the public key
// is stored on the server; signing is performed offline by the release script.
type ClientRelease struct {
	ID              uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Version         string    `json:"version" gorm:"size:32;not null;uniqueIndex:idx_client_release_channel_version,priority:2"`
	Channel         string    `json:"channel" gorm:"size:24;not null;default:stable;uniqueIndex:idx_client_release_channel_version,priority:1;index"`
	DownloadURL     string    `json:"download_url" gorm:"size:1024;not null"`
	SHA256          string    `json:"sha256" gorm:"size:64;not null"`
	Signature       string    `json:"signature" gorm:"size:128;not null"`
	Status          string    `json:"status" gorm:"size:16;not null;default:draft;index"`
	RolloutPercent  int       `json:"rollout_percent" gorm:"default:0"`
	PreviousVersion string    `json:"previous_version" gorm:"size:32"`
	Notes           string    `json:"notes" gorm:"size:1024"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (ClientRelease) TableName() string { return "client_releases" }

// TaskLog 任务日志
type TaskLog struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID    uint64    `json:"task_id" gorm:"not null;index;index:idx_task_logs_task_created,priority:1"`
	DeviceID  uint64    `json:"device_id" gorm:"default:0;index"`
	LogType   string    `json:"log_type" gorm:"size:32;default:info"`
	Message   string    `json:"message" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at" gorm:"index;index:idx_task_logs_task_created,priority:2"`
}

func (TaskLog) TableName() string { return "task_logs" }

// ============================================
// 资源管理
// ============================================

// Resource 资源模型
type Resource struct {
	ID            uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name          string    `json:"name" gorm:"size:255;not null"`
	Filename      string    `json:"filename" gorm:"size:255;not null"`
	FilePath      string    `json:"file_path" gorm:"size:512;not null"`
	FileSize      int64     `json:"file_size" gorm:"default:0"`
	MimeType      string    `json:"mime_type" gorm:"size:128"`
	UserID        uint64    `json:"user_id" gorm:"default:0"`
	IsShared      int8      `json:"is_shared" gorm:"default:0"`
	DownloadCount int       `json:"download_count" gorm:"default:0"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Resource) TableName() string { return "resources" }

// ============================================
// 参数模板
// ============================================

// ParameterTemplate 参数模板
type ParameterTemplate struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Type        string    `json:"type" gorm:"size:32;not null"` // script/task/device
	Params      string    `json:"params" gorm:"type:json;not null"`
	Description string    `json:"description" gorm:"size:255"`
	UserID      uint64    `json:"user_id" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ParameterTemplate) TableName() string { return "parameter_templates" }

// ============================================
// 数据管理
// ============================================

// DataTemplate 数据模板
type DataTemplate struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"size:255"`
	Fields      string    `json:"fields" gorm:"type:json;not null"` // JSON array of field definitions
	UserID      uint64    `json:"user_id" gorm:"default:0"`
	IsShared    int8      `json:"is_shared" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (DataTemplate) TableName() string { return "data_templates" }

// DataRecord 数据记录
type DataRecord struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TemplateID uint64    `json:"template_id" gorm:"not null"`
	Data       string    `json:"data" gorm:"type:json;not null"`
	UserID     uint64    `json:"user_id" gorm:"default:0"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (DataRecord) TableName() string { return "data_records" }

// ============================================
// 系统管理
// ============================================

// SystemLog 系统日志
type SystemLog struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `json:"user_id" gorm:"default:0"`
	Username  string    `json:"username" gorm:"size:64"`
	Action    string    `json:"action" gorm:"size:64;not null"`
	Resource  string    `json:"resource" gorm:"size:64"`
	Detail    string    `json:"detail" gorm:"type:text"`
	IP        string    `json:"ip" gorm:"size:64"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}

func (SystemLog) TableName() string { return "system_logs" }

// SystemConfig 系统配置
type SystemConfig struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ConfigKey   string    `json:"config_key" gorm:"unique;size:128;not null"`
	ConfigValue string    `json:"config_value" gorm:"type:text"`
	Description string    `json:"description" gorm:"size:255"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (SystemConfig) TableName() string { return "system_configs" }

// ============================================
// API响应结构
// ============================================

// Response 统一API响应
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// PageResponse 分页响应
type PageResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	Size    int         `json:"size"`
}

// PageRequest 分页请求
type PageRequest struct {
	Page     int    `form:"page" json:"page"`
	Size     int    `form:"size" json:"size"`
	Keyword  string `form:"keyword" json:"keyword"`
	Status   int    `form:"status" json:"status"`
	SortBy   string `form:"sort_by" json:"sort_by"`
	SortDesc bool   `form:"sort_desc" json:"sort_desc"`
}

func (p *PageRequest) Normalize(defaultSize, maxSize int) {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.Size <= 0 {
		p.Size = defaultSize
	}
	if p.Size > maxSize {
		p.Size = maxSize
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	User         *User  `json:"user"`
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// AssignRoleRequest 分配角色请求
type AssignRoleRequest struct {
	RoleCodes []string `json:"role_codes" binding:"required"`
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs []uint64 `json:"ids" binding:"required"`
}

// BatchAddGroupRequest 批量添加分组
type BatchAddGroupRequest struct {
	DeviceIDs []uint64 `json:"device_ids" binding:"required"`
	GroupID   uint64   `json:"group_id" binding:"required"`
}

// TaskRepairRequest 任务修复请求
type TaskRepairRequest struct {
	DeviceIDs []uint64 `json:"device_ids" binding:"required"`
}

// ============================================
// 关联表模型 (用于直接操作)
// ============================================

// UserRole 用户角色关联
type UserRole struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `json:"user_id" gorm:"not null"`
	RoleID    uint64    `json:"role_id" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserRole) TableName() string { return "user_roles" }

// RolePermission 角色权限关联
type RolePermission struct {
	ID           uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	RoleID       uint64    `json:"role_id" gorm:"not null"`
	PermissionID uint64    `json:"permission_id" gorm:"not null"`
	CreatedAt    time.Time `json:"created_at"`
}

func (RolePermission) TableName() string { return "role_permissions" }

// ScriptShare 脚本共享
type ScriptShare struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ScriptID   uint64    `json:"script_id" gorm:"not null"`
	FromUserID uint64    `json:"from_user_id" gorm:"not null"`
	ToUserID   uint64    `json:"to_user_id" gorm:"not null"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ScriptShare) TableName() string { return "script_shares" }

// TaskShare 任务共享
type TaskShare struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID     uint64    `json:"task_id" gorm:"not null"`
	FromUserID uint64    `json:"from_user_id" gorm:"not null"`
	ToUserID   uint64    `json:"to_user_id" gorm:"not null"`
	CreatedAt  time.Time `json:"created_at"`
}

func (TaskShare) TableName() string { return "task_shares" }

// ResourceShare 资源共享
type ResourceShare struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ResourceID uint64    `json:"resource_id" gorm:"not null"`
	FromUserID uint64    `json:"from_user_id" gorm:"not null"`
	ToUserID   uint64    `json:"to_user_id" gorm:"not null"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ResourceShare) TableName() string { return "resource_shares" }

// DataPermission 数据权限
type DataPermission struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	TemplateID uint64    `json:"template_id" gorm:"not null"`
	UserID     uint64    `json:"user_id" gorm:"not null"`
	CanRead    int8      `json:"can_read" gorm:"default:1"`
	CanWrite   int8      `json:"can_write" gorm:"default:0"`
	CanDelete  int8      `json:"can_delete" gorm:"default:0"`
	CreatedAt  time.Time `json:"created_at"`
}

func (DataPermission) TableName() string { return "data_permissions" }

// DashboardStats 仪表盘统计
type DashboardStats struct {
	DeviceCount    int64 `json:"device_count"`
	OnlineCount    int64 `json:"online_count"`
	TaskCount      int64 `json:"task_count"`
	RunningCount   int64 `json:"running_count"`
	ScriptCount    int64 `json:"script_count"`
	ResourceCount  int64 `json:"resource_count"`
	UserCount      int64 `json:"user_count"`
	TodayTaskCount int64 `json:"today_task_count"`
}

// DeviceMetric 设备指标（心跳带上来的健康数据）
type DeviceMetric struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  uint64    `json:"device_id" gorm:"index:idx_metric_device_time,priority:1"`
	Battery   int       `json:"battery"`
	MemTotal  int64     `json:"mem_total"`
	MemAvail  int64     `json:"mem_avail"`
	DiskTotal int64     `json:"disk_total"`
	DiskAvail int64     `json:"disk_avail"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_metric_device_time,priority:2"`
}

// DeviceSms 设备短信记录
type DeviceSms struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  uint64    `json:"device_id" gorm:"index:idx_sms_device_time,priority:1"`
	Sender    string    `json:"sender" gorm:"size:64"`
	Body      string    `json:"body" gorm:"size:1024"`
	Type      int       `json:"type" gorm:"default:1"`
	SmsTime   int64     `json:"sms_time" gorm:"default:0;index:idx_sms_device_time,priority:2"`
	DedupKey  *string   `json:"-" gorm:"size:64;uniqueIndex"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}

// DeviceContact 设备通讯录
type DeviceContact struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  uint64    `json:"device_id" gorm:"index:idx_contact_device_phone,priority:1"`
	Name      string    `json:"name" gorm:"size:128"`
	Phone     string    `json:"phone" gorm:"size:32;index:idx_contact_device_phone,priority:2"`
	DedupKey  *string   `json:"-" gorm:"size:64;uniqueIndex"`
	CreatedAt time.Time `json:"created_at"`
}
