package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const defaultJWTSecret = "cloud-control-secret-key-2026"
const legacyReleaseJWTSecret = "iRjzkYivtFyjJzVhYpaiWbCdCxzyfDQWhFIBOFI24R4hA11FBzJRFEQWTiGmF1xU"

// Config 应用配置
type Config struct {
	Server      ServerConfig      `json:"server"`
	Database    DatabaseConfig    `json:"database"`
	JWT         JWTConfig         `json:"jwt"`
	Upload      UploadConfig      `json:"upload"`
	MQTT        MQTTConfig        `json:"mqtt"`
	Security    SecurityConfig    `json:"security"`
	Reliability ReliabilityConfig `json:"reliability"`
	Maintenance MaintenanceConfig `json:"maintenance"`
	Alerts      AlertConfig       `json:"alerts"`
	Updates     UpdateConfig      `json:"updates"`
}

// ServerConfig HTTP服务器配置
type ServerConfig struct {
	Port            int    `json:"port"`
	MQTTPort        int    `json:"mqtt_port"`
	BindAddress     string `json:"bind_address"`
	MQTTBindAddress string `json:"mqtt_bind_address"`
	Mode            string `json:"mode"` // debug, release
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver          string `json:"driver"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	User            string `json:"user"`
	Password        string `json:"password"`
	DBName          string `json:"dbname"`
	Charset         string `json:"charset"`
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	SQLiteCacheKB   int    `json:"sqlite_cache_kb"`
	SQLiteMmapBytes int64  `json:"sqlite_mmap_bytes"`
}

// JWTConfig JWT配置
type JWTConfig struct {
	Secret      string `json:"secret"`
	ExpireHours int    `json:"expire_hours"` // token过期时间(小时)
}

// UploadConfig 上传配置
type UploadConfig struct {
	MaxSize       int64  `json:"max_size"`        // 最大上传大小(字节)
	MaxTotalBytes int64  `json:"max_total_bytes"` // 资源文件总配额
	UploadPath    string `json:"upload_path"`     // 上传目录
}

// MQTTConfig MQTT broker 配置。用户名和密码留空时保持兼容模式，
// 设置后 broker 会要求设备使用 MQTT CONNECT 用户名/密码。
type MQTTConfig struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	BrokerURL string `json:"broker_url"`
	ClientID  string `json:"client_id"`
}

// SecurityConfig 安全相关配置。
type SecurityConfig struct {
	// CORSOrigins 为空时保持本地旧行为；生产环境建议配置明确的前端来源。
	CORSOrigins []string `json:"cors_origins"`
	// NotificationWebhookURL 用于可选的短信/告警通知，不再把密钥写入源码。
	NotificationWebhookURL string `json:"notification_webhook_url"`
	DeviceAuthRequired     bool   `json:"device_auth_required"`
	// DeviceAutoRegister permits a previously unknown device to complete one
	// first registration without a mobile-side credential. The server assigns
	// the device to AutoRegisterUserID (or the first active user when zero).
	DeviceAutoRegister bool   `json:"device_auto_register"`
	AutoRegisterUserID uint64 `json:"auto_register_user_id"`
	// DeviceAutoRegisterRateLimit is the maximum number of first-registration
	// attempts accepted from one source address per minute.
	DeviceAutoRegisterRateLimit int `json:"device_auto_register_rate_limit"`
	// DeviceAutoRegisterRequireTLS prevents credential bootstrap over plaintext
	// WebSocket when the deployment is configured for WSS/HTTPS.
	DeviceAutoRegisterRequireTLS bool `json:"device_auto_register_require_tls"`
	// DeviceLegacyTokenlessCIDRs is an explicit, local-network-only
	// compatibility allowlist for older agents that cannot persist a device
	// token. An empty list keeps token authentication mandatory.
	DeviceLegacyTokenlessCIDRs []string `json:"device_legacy_tokenless_cidrs"`
	// DeviceWSAttemptsPerMinute and DeviceWSMaxConnectionsPerIP protect the
	// tokenless LAN endpoint from reconnect storms without adding any mobile-
	// side credential or configuration step.
	DeviceWSAttemptsPerMinute   int `json:"device_ws_attempts_per_minute"`
	DeviceWSMaxConnectionsPerIP int `json:"device_ws_max_connections_per_ip"`
	// TrustedProxyCIDRs controls which direct peers may supply forwarded client
	// addresses and HTTPS markers. Keep this narrow; Docker deployments set the
	// private bridge range explicitly.
	TrustedProxyCIDRs []string `json:"trusted_proxy_cidrs"`
}

// ReliabilityConfig controls durable command delivery and scheduler recovery.
type ReliabilityConfig struct {
	ReliableDeliveryEnabled bool `json:"reliable_delivery_enabled"`
	RetrySeconds            int  `json:"retry_seconds"`
	MaxRetrySeconds         int  `json:"max_retry_seconds"`
	MaxAttempts             int  `json:"max_attempts"`
	RetryBatchSize          int  `json:"retry_batch_size"`
	DeliveryTTLHours        int  `json:"delivery_ttl_hours"`
	CronCatchupHours        int  `json:"cron_catchup_hours"`
}

// MaintenanceConfig bounds data growth on small disks.
type MaintenanceConfig struct {
	DeviceLogRetentionDays   int   `json:"device_log_retention_days"`
	MetricRetentionDays      int   `json:"metric_retention_days"`
	DeliveryRetentionDays    int   `json:"delivery_retention_days"`
	TaskLogRetentionDays     int   `json:"task_log_retention_days"`
	SystemLogRetentionDays   int   `json:"system_log_retention_days"`
	SMSRetentionDays         int   `json:"sms_retention_days"`
	CleanupBatchSize         int   `json:"cleanup_batch_size"`
	ScreenshotRetentionHours int   `json:"screenshot_retention_hours"`
	ScreenshotMaxBytes       int64 `json:"screenshot_max_bytes"`
	BackupIntervalHours      int   `json:"backup_interval_hours"`
	BackupRetentionCount     int   `json:"backup_retention_count"`
	DiskWarnPercent          int   `json:"disk_warn_percent"`
	DiskStopWritesPercent    int   `json:"disk_stop_writes_percent"`
}

// AlertConfig keeps health alerts dependency-free for small edge hosts.
type AlertConfig struct {
	DeliveryAgeMinutes int `json:"delivery_age_minutes"`
	QueueUsagePercent  int `json:"queue_usage_percent"`
	CronLagMinutes     int `json:"cron_lag_minutes"`
	ReconnectsPer5Min  int `json:"reconnects_per_5_minutes"`
	CooldownMinutes    int `json:"cooldown_minutes"`
}

// UpdateConfig configures signed agent release manifests. PublicKey is a
// base64-encoded Ed25519 public key; the private key never belongs on server.
type UpdateConfig struct {
	PublicKey string `json:"public_key"`
}

// App 全局配置实例
var App *Config

// Load 加载配置
func Load(path string) error {
	App = &Config{
		Server: ServerConfig{
			Port:     8080,
			MQTTPort: 1883,
			Mode:     "debug",
		},
		Database: DatabaseConfig{
			Driver:          "sqlite",
			Host:            "127.0.0.1",
			Port:            3306,
			User:            "root",
			Password:        "root",
			DBName:          "cloud_control",
			Charset:         "utf8mb4",
			SQLiteCacheKB:   32768,
			SQLiteMmapBytes: 268435456,
		},
		JWT: JWTConfig{
			Secret:      defaultJWTSecret,
			ExpireHours: 24,
		},
		Upload: UploadConfig{
			MaxSize:       200 * 1024 * 1024, // 200MB
			MaxTotalBytes: 8 * 1024 * 1024 * 1024,
			UploadPath:    "./uploads",
		},
		MQTT: MQTTConfig{},
		Security: SecurityConfig{
			CORSOrigins:                 []string{},
			DeviceAuthRequired:          true,
			DeviceAutoRegister:          false,
			DeviceAutoRegisterRateLimit: 10,
			DeviceWSAttemptsPerMinute:   120,
			DeviceWSMaxConnectionsPerIP: 8,
			TrustedProxyCIDRs:           []string{"127.0.0.1/32", "::1/128"},
		},
		Reliability: ReliabilityConfig{
			ReliableDeliveryEnabled: true,
			RetrySeconds:            15,
			MaxRetrySeconds:         300,
			MaxAttempts:             2048,
			RetryBatchSize:          100,
			DeliveryTTLHours:        168,
			CronCatchupHours:        24,
		},
		Maintenance: MaintenanceConfig{
			DeviceLogRetentionDays:   30,
			MetricRetentionDays:      7,
			DeliveryRetentionDays:    30,
			TaskLogRetentionDays:     30,
			SystemLogRetentionDays:   30,
			SMSRetentionDays:         90,
			CleanupBatchSize:         1000,
			ScreenshotRetentionHours: 24,
			ScreenshotMaxBytes:       2 * 1024 * 1024 * 1024,
			BackupIntervalHours:      6,
			BackupRetentionCount:     14,
			DiskWarnPercent:          80,
			DiskStopWritesPercent:    90,
		},
		Alerts: AlertConfig{
			DeliveryAgeMinutes: 10,
			QueueUsagePercent:  70,
			CronLagMinutes:     5,
			ReconnectsPer5Min:  30,
			CooldownMinutes:    15,
		},
		Updates: UpdateConfig{},
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("read config error: %w", err)
			}
			fmt.Printf("Warning: config file not found: %s, creating a safe default\n", path)
		} else if err := json.Unmarshal(data, App); err != nil {
			return fmt.Errorf("parse config error: %v", err)
		}
	}

	// 环境变量优先，方便桌面版、服务版和容器部署使用同一份程序。
	secretFromEnvironment := false
	if value := strings.TrimSpace(os.Getenv("CLOUD_JWT_SECRET")); value != "" {
		App.JWT.Secret = value
		secretFromEnvironment = true
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_SERVER_BIND_ADDRESS")); value != "" {
		App.Server.BindAddress = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_MQTT_BIND_ADDRESS")); value != "" {
		App.Server.MQTTBindAddress = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_MQTT_USERNAME")); value != "" {
		App.MQTT.Username = value
	}
	if value := os.Getenv("CLOUD_MQTT_PASSWORD"); value != "" {
		App.MQTT.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_MQTT_BROKER_URL")); value != "" {
		App.MQTT.BrokerURL = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_MQTT_CLIENT_ID")); value != "" {
		App.MQTT.ClientID = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_NOTIFY_WEBHOOK_URL")); value != "" {
		App.Security.NotificationWebhookURL = value
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_CORS_ORIGINS")); value != "" {
		origins := make([]string, 0, 4)
		for _, origin := range strings.Split(value, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				origins = append(origins, origin)
			}
		}
		App.Security.CORSOrigins = origins
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_DEVICE_AUTH_REQUIRED")); value != "" {
		App.Security.DeviceAuthRequired = !strings.EqualFold(value, "false") && value != "0"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_DEVICE_AUTO_REGISTER")); value != "" {
		App.Security.DeviceAutoRegister = !strings.EqualFold(value, "false") && value != "0"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_AUTO_REGISTER_USER_ID")); value != "" {
		if id, err := strconv.ParseUint(value, 10, 64); err == nil {
			App.Security.AutoRegisterUserID = id
		}
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_DEVICE_AUTO_REGISTER_RATE_LIMIT")); value != "" {
		if limit, err := strconv.Atoi(value); err == nil {
			App.Security.DeviceAutoRegisterRateLimit = limit
		}
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_DEVICE_AUTO_REGISTER_REQUIRE_TLS")); value != "" {
		App.Security.DeviceAutoRegisterRequireTLS = !strings.EqualFold(value, "false") && value != "0"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_TRUSTED_PROXY_CIDRS")); value != "" {
		App.Security.TrustedProxyCIDRs = splitNonEmpty(value)
	}
	applyPositiveIntEnv("CLOUD_DEVICE_WS_ATTEMPTS_PER_MINUTE", &App.Security.DeviceWSAttemptsPerMinute)
	applyPositiveIntEnv("CLOUD_DEVICE_WS_MAX_CONNECTIONS_PER_IP", &App.Security.DeviceWSMaxConnectionsPerIP)
	if value := strings.TrimSpace(os.Getenv("CLOUD_RELIABLE_DELIVERY_ENABLED")); value != "" {
		App.Reliability.ReliableDeliveryEnabled = !strings.EqualFold(value, "false") && value != "0"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_RELIABLE_DELIVERY_RETRY_SECONDS")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			App.Reliability.RetrySeconds = seconds
		}
	}
	applyPositiveIntEnv("CLOUD_RELIABLE_DELIVERY_MAX_RETRY_SECONDS", &App.Reliability.MaxRetrySeconds)
	if value := strings.TrimSpace(os.Getenv("CLOUD_RELIABLE_DELIVERY_MAX_ATTEMPTS")); value != "" {
		if attempts, err := strconv.Atoi(value); err == nil {
			App.Reliability.MaxAttempts = attempts
		}
	}
	if value := strings.TrimSpace(os.Getenv("CLOUD_RELIABLE_DELIVERY_RETRY_BATCH_SIZE")); value != "" {
		if batchSize, err := strconv.Atoi(value); err == nil {
			App.Reliability.RetryBatchSize = batchSize
		}
	}
	applyPositiveIntEnv("CLOUD_RELIABLE_DELIVERY_TTL_HOURS", &App.Reliability.DeliveryTTLHours)
	applyPositiveIntEnv("CLOUD_CRON_CATCHUP_HOURS", &App.Reliability.CronCatchupHours)
	applyPositiveIntEnv("CLOUD_ALERT_DELIVERY_AGE_MINUTES", &App.Alerts.DeliveryAgeMinutes)
	applyPositiveIntEnv("CLOUD_ALERT_QUEUE_USAGE_PERCENT", &App.Alerts.QueueUsagePercent)
	applyPositiveIntEnv("CLOUD_ALERT_CRON_LAG_MINUTES", &App.Alerts.CronLagMinutes)
	applyPositiveIntEnv("CLOUD_ALERT_RECONNECTS_PER_5_MIN", &App.Alerts.ReconnectsPer5Min)
	applyPositiveIntEnv("CLOUD_ALERT_COOLDOWN_MINUTES", &App.Alerts.CooldownMinutes)
	if value := strings.TrimSpace(os.Getenv("CLOUD_UPDATE_PUBLIC_KEY")); value != "" {
		App.Updates.PublicKey = value
	}
	applyPositiveIntEnv("CLOUD_DB_MAX_OPEN_CONNS", &App.Database.MaxOpenConns)
	applyPositiveIntEnv("CLOUD_DB_MAX_IDLE_CONNS", &App.Database.MaxIdleConns)
	applyPositiveIntEnv("CLOUD_SQLITE_CACHE_KB", &App.Database.SQLiteCacheKB)
	applyPositiveInt64Env("CLOUD_SQLITE_MMAP_BYTES", &App.Database.SQLiteMmapBytes)
	applyPositiveInt64Env("CLOUD_UPLOAD_MAX_SIZE", &App.Upload.MaxSize)
	applyPositiveInt64Env("CLOUD_UPLOAD_MAX_TOTAL_BYTES", &App.Upload.MaxTotalBytes)
	if value := strings.TrimSpace(os.Getenv("CLOUD_UPLOAD_PATH")); value != "" {
		App.Upload.UploadPath = value
	}
	applyPositiveIntEnv("CLOUD_DEVICE_LOG_RETENTION_DAYS", &App.Maintenance.DeviceLogRetentionDays)
	applyPositiveIntEnv("CLOUD_METRIC_RETENTION_DAYS", &App.Maintenance.MetricRetentionDays)
	applyPositiveIntEnv("CLOUD_DELIVERY_RETENTION_DAYS", &App.Maintenance.DeliveryRetentionDays)
	applyPositiveIntEnv("CLOUD_TASK_LOG_RETENTION_DAYS", &App.Maintenance.TaskLogRetentionDays)
	applyPositiveIntEnv("CLOUD_SYSTEM_LOG_RETENTION_DAYS", &App.Maintenance.SystemLogRetentionDays)
	applyPositiveIntEnv("CLOUD_SMS_RETENTION_DAYS", &App.Maintenance.SMSRetentionDays)
	applyPositiveIntEnv("CLOUD_CLEANUP_BATCH_SIZE", &App.Maintenance.CleanupBatchSize)
	applyPositiveIntEnv("CLOUD_SCREENSHOT_RETENTION_HOURS", &App.Maintenance.ScreenshotRetentionHours)
	applyPositiveInt64Env("CLOUD_SCREENSHOT_MAX_BYTES", &App.Maintenance.ScreenshotMaxBytes)
	applyPositiveIntEnv("CLOUD_BACKUP_INTERVAL_HOURS", &App.Maintenance.BackupIntervalHours)
	applyPositiveIntEnv("CLOUD_BACKUP_RETENTION_COUNT", &App.Maintenance.BackupRetentionCount)
	applyPositiveIntEnv("CLOUD_DISK_WARN_PERCENT", &App.Maintenance.DiskWarnPercent)
	applyPositiveIntEnv("CLOUD_DISK_STOP_WRITES_PERCENT", &App.Maintenance.DiskStopWritesPercent)
	if needsGeneratedJWTSecret(App.JWT.Secret) {
		if generated, err := randomSecret(); err == nil {
			App.JWT.Secret = generated
			if !secretFromEnvironment && path != "" {
				if err := persistConfig(path); err != nil {
					fmt.Printf("Warning: generated JWT secret could not be persisted: %v\n", err)
				}
			}
		}
	}

	return nil
}

func needsGeneratedJWTSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	return secret == "" || secret == defaultJWTSecret || secret == legacyReleaseJWTSecret
}

func persistConfig(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(App, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

// Validate 检查不会因为环境不同而静默退化的配置。
func Validate() error {
	if App == nil {
		return fmt.Errorf("configuration is not loaded")
	}
	if App.Server.Port < 0 || App.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", App.Server.Port)
	}
	if App.Server.MQTTPort < 0 || App.Server.MQTTPort > 65535 {
		return fmt.Errorf("invalid mqtt port: %d", App.Server.MQTTPort)
	}
	for name, address := range map[string]string{
		"server bind_address": App.Server.BindAddress,
		"mqtt bind_address":   App.Server.MQTTBindAddress,
	} {
		if address != "" && net.ParseIP(strings.TrimSpace(address)) == nil {
			return fmt.Errorf("invalid %s: %s", name, address)
		}
	}
	if App.JWT.ExpireHours <= 0 {
		return fmt.Errorf("jwt expire_hours must be positive")
	}
	if len(strings.TrimSpace(App.JWT.Secret)) < 32 {
		return fmt.Errorf("jwt secret must contain at least 32 characters")
	}
	if App.Upload.MaxSize <= 0 {
		return fmt.Errorf("upload max_size must be positive")
	}
	if App.Upload.MaxTotalBytes < App.Upload.MaxSize {
		return fmt.Errorf("upload max_total_bytes must be at least max_size")
	}
	if App.Database.MaxOpenConns < 0 || App.Database.MaxIdleConns < 0 ||
		(App.Database.MaxOpenConns > 0 && App.Database.MaxIdleConns > App.Database.MaxOpenConns) {
		return fmt.Errorf("invalid database connection pool limits")
	}
	if App.Database.SQLiteCacheKB < 1024 || App.Database.SQLiteMmapBytes < 1048576 {
		return fmt.Errorf("invalid SQLite memory limits")
	}
	if (App.MQTT.Username == "") != (App.MQTT.Password == "") {
		return fmt.Errorf("mqtt username and password must be configured together")
	}
	if App.MQTT.BrokerURL != "" && !strings.HasPrefix(App.MQTT.BrokerURL, "tcp://") && !strings.HasPrefix(App.MQTT.BrokerURL, "tls://") && !strings.HasPrefix(App.MQTT.BrokerURL, "ws://") && !strings.HasPrefix(App.MQTT.BrokerURL, "wss://") {
		return fmt.Errorf("mqtt broker_url must use tcp, tls, ws, or wss")
	}
	if App.MQTT.BrokerURL != "" && (App.MQTT.Username == "" || App.MQTT.Password == "") {
		return fmt.Errorf("external mqtt requires a bridge username and password")
	}
	if App.Security.DeviceAutoRegister && !App.Security.DeviceAuthRequired {
		return fmt.Errorf("device_auto_register requires device_auth_required")
	}
	if App.Security.DeviceAutoRegister && App.Security.DeviceAutoRegisterRateLimit < 1 {
		return fmt.Errorf("device_auto_register_rate_limit must be positive")
	}
	if App.Security.DeviceWSAttemptsPerMinute < 1 || App.Security.DeviceWSAttemptsPerMinute > 10000 ||
		App.Security.DeviceWSMaxConnectionsPerIP < 1 || App.Security.DeviceWSMaxConnectionsPerIP > 1000 {
		return fmt.Errorf("invalid device websocket source limits")
	}
	for _, value := range App.Security.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q", value)
		}
	}
	if App.Reliability.RetrySeconds < 1 {
		return fmt.Errorf("reliable_delivery retry_seconds must be positive")
	}
	if App.Reliability.MaxRetrySeconds < App.Reliability.RetrySeconds || App.Reliability.MaxRetrySeconds > 3600 {
		return fmt.Errorf("reliable_delivery max_retry_seconds must be between retry_seconds and 3600")
	}
	if App.Reliability.MaxAttempts < 1 {
		return fmt.Errorf("reliable_delivery max_attempts must be positive")
	}
	if App.Reliability.RetryBatchSize < 1 || App.Reliability.RetryBatchSize > 1000 {
		return fmt.Errorf("reliable_delivery retry_batch_size must be between 1 and 1000")
	}
	if App.Reliability.DeliveryTTLHours < 1 || App.Reliability.DeliveryTTLHours > 720 ||
		App.Reliability.CronCatchupHours < 1 || App.Reliability.CronCatchupHours > 168 {
		return fmt.Errorf("invalid reliable delivery TTL or cron catch-up window")
	}
	if App.Maintenance.DeviceLogRetentionDays < 1 || App.Maintenance.MetricRetentionDays < 1 ||
		App.Maintenance.DeliveryRetentionDays < 1 || App.Maintenance.TaskLogRetentionDays < 1 ||
		App.Maintenance.SystemLogRetentionDays < 1 || App.Maintenance.SMSRetentionDays < 1 ||
		App.Maintenance.CleanupBatchSize < 1 ||
		App.Maintenance.CleanupBatchSize > 5000 || App.Maintenance.ScreenshotRetentionHours < 1 ||
		App.Maintenance.ScreenshotMaxBytes < 1 || App.Maintenance.BackupIntervalHours < 1 ||
		App.Maintenance.BackupRetentionCount < 1 || App.Maintenance.BackupRetentionCount > 100 {
		return fmt.Errorf("invalid maintenance retention or quota settings")
	}
	if App.Maintenance.DiskWarnPercent < 1 || App.Maintenance.DiskWarnPercent >= 100 ||
		App.Maintenance.DiskStopWritesPercent <= App.Maintenance.DiskWarnPercent ||
		App.Maintenance.DiskStopWritesPercent >= 100 {
		return fmt.Errorf("invalid disk watermarks")
	}
	if App.Alerts.DeliveryAgeMinutes < 1 || App.Alerts.QueueUsagePercent < 1 || App.Alerts.QueueUsagePercent > 99 ||
		App.Alerts.CronLagMinutes < 1 || App.Alerts.ReconnectsPer5Min < 1 || App.Alerts.CooldownMinutes < 1 {
		return fmt.Errorf("invalid alert configuration")
	}
	return nil
}

func applyPositiveIntEnv(name string, target *int) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			*target = parsed
		}
	}
}

func applyPositiveInt64Env(name string, target *int64) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			*target = parsed
		}
	}
}

func splitNonEmpty(value string) []string {
	result := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DSN 返回MySQL连接字符串
func (d *DatabaseConfig) DSN() string {
	connection := mysql.NewConfig()
	connection.User = d.User
	connection.Passwd = d.Password
	connection.Net = "tcp"
	connection.Addr = fmt.Sprintf("%s:%d", d.Host, d.Port)
	connection.DBName = d.DBName
	connection.ParseTime = true
	connection.Loc = time.Local
	if d.Charset != "" {
		connection.Params = map[string]string{"charset": d.Charset}
	}
	return connection.FormatDSN()
}
