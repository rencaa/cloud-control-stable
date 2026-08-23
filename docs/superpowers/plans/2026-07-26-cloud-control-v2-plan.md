# 云控框架 V2 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重写云控后端（Go + MQTT + PostgreSQL）+ 前端（Nuxt 3 + Naive UI），支持 200-300 台设备，4H4G 部署。

**Architecture:** 嵌入式 MQTT Broker 模块化单体。Go 单进程包含 MQTT Server、HTTP API、领域服务、基础设施层。前端 Nuxt 3 静态资源 go:embed 嵌入二进制。双模式部署（Windows EXE / Docker Compose）。

**Tech Stack:** Go 1.22 + mochi-mqtt v2 + Gin + GORM + pgx + Protobuf · Nuxt 3 + Naive UI + Pinia + ECharts + UnoCSS

**Source tree:** `cloud-control-v2/` (新文件夹，与现有代码隔离)

---

## 文件结构总览

```
cloud-control-v2/
├── server/
│   ├── cmd/cloud-server/main.go
│   ├── internal/
│   │   ├── mqtt/          (broker.go, hooks.go)
│   │   ├── api/           (router.go, middleware/)
│   │   ├── domain/        (device/, task/, script/, screen/, sms/, auth/, dashboard/)
│   │   ├── infra/         (ratelimit/, batcher/, eventbus/)
│   │   └── repo/          (*_repo.go)
│   ├── config/config.go
│   ├── proto/cloud.proto
│   ├── embed/             (go:embed target for ../web/.output/public)
│   ├── Dockerfile
│   └── go.mod
├── web/                   (Nuxt 3 project)
├── docker-compose.yml
├── config.example.json
└── README.md
```

---

## Phase 1: 基础骨架 (Go 项目 + MQTT + PG + API 框架)

### Task 1.1: 初始化 Go 模块和目录结构

**Files:**
- Create: `cloud-control-v2/server/go.mod`
- Create: `cloud-control-v2/server/cmd/cloud-server/main.go`
- Create: `cloud-control-v2/server/.gitignore`

- [ ] **Step 1: 创建 go.mod**

```bash
cd cloud-control-v2/server
go mod init cloud-control-v2/server
```

- [ ] **Step 2: 创建最小 main.go**

```go
// cmd/cloud-server/main.go
package main

import "fmt"

func main() {
    fmt.Println("cloud-control v2 starting...")
}
```

- [ ] **Step 3: 创建 .gitignore**

```
# .gitignore
/data/
/uploads/
*.exe
config.json
.env
```

- [ ] **Step 4: 验证编译**

```bash
cd cloud-control-v2/server
go build ./cmd/cloud-server/
# Expected: 编译成功，无错误
```

- [ ] **Step 5: Commit**

```bash
cd cloud-control-v2
git init
git add -A && git commit -m "feat: init go module and minimal main.go"
```

---

### Task 1.2: 配置模块

**Files:**
- Create: `cloud-control-v2/server/config/config.go`
- Create: `cloud-control-v2/config.example.json`

- [ ] **Step 1: 编写配置结构体**

```go
// config/config.go
package config

import (
    "encoding/json"
    "os"
    "time"
)

type Config struct {
    Server   ServerConfig   `json:"server"`
    MQTT     MQTTConfig     `json:"mqtt"`
    Database DatabaseConfig `json:"database"`
    JWT      JWTConfig      `json:"jwt"`
    Upload   UploadConfig   `json:"upload"`
    RateLimit RateLimitConfig `json:"ratelimit"`
}

type ServerConfig struct {
    Port         int    `json:"port"`          // default 8080
    Mode         string `json:"mode"`          // debug | release
    ReadTimeout  int    `json:"read_timeout"`  // seconds
    WriteTimeout int    `json:"write_timeout"` // seconds
}

type MQTTConfig struct {
    TCPPort int    `json:"tcp_port"`  // default 1883
    WSPort  int    `json:"ws_port"`   // default 1882
    Secret  string `json:"secret"`    // HMAC key
}

type DatabaseConfig struct {
    Host     string `json:"host"`
    Port     int    `json:"port"`
    User     string `json:"user"`
    Password string `json:"password"`
    DBName   string `json:"dbname"`
    SSLMode  string `json:"ssl_mode"`
    MaxConns int    `json:"max_conns"`
}

type JWTConfig struct {
    Secret        string `json:"secret"`
    AccessExpire  int    `json:"access_expire"`  // hours, default 24
    RefreshExpire int    `json:"refresh_expire"` // hours, default 168
}

type UploadConfig struct {
    Path      string `json:"path"`        // default ./uploads
    MaxSizeMB int    `json:"max_size_mb"` // default 100
}

type RateLimitConfig struct {
    GlobalMaxConns int `json:"global_max_conns"` // default 500
    ConnPerSec     int `json:"conn_per_sec"`     // default 100
    IPMaxConns     int `json:"ip_max_conns"`     // default 20
}

func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    cfg := Default()
    if err := json.Unmarshal(data, cfg); err != nil {
        return nil, err
    }
    cfg.applyEnvOverrides()
    return cfg, nil
}

func Default() *Config {
    return &Config{
        Server: ServerConfig{
            Port:         8080,
            Mode:         "release",
            ReadTimeout:  30,
            WriteTimeout: 30,
        },
        MQTT: MQTTConfig{
            TCPPort: 1883,
            WSPort:  1882,
            Secret:  "change-me-in-production",
        },
        Database: DatabaseConfig{
            Host:     envOr("CLOUD_DB_HOST", "localhost"),
            Port:     5432,
            User:     envOr("CLOUD_DB_USER", "cloud"),
            Password: envOr("CLOUD_DB_PASSWORD", ""),
            DBName:   envOr("CLOUD_DB_NAME", "cloud_control"),
            SSLMode:  "disable",
            MaxConns: 30,
        },
        JWT: JWTConfig{
            Secret:        envOr("CLOUD_JWT_SECRET", "change-me-in-production"),
            AccessExpire:  24,
            RefreshExpire: 168,
        },
        Upload: UploadConfig{
            Path:      "./uploads",
            MaxSizeMB: 100,
        },
        RateLimit: RateLimitConfig{
            GlobalMaxConns: 500,
            ConnPerSec:     100,
            IPMaxConns:     20,
        },
    }
}

func (c *Config) applyEnvOverrides() {
    if v := os.Getenv("CLOUD_SERVER_PORT"); v != "" {
        c.Server.Port = parseInt(v)
    }
    if v := os.Getenv("CLOUD_MQTT_PORT"); v != "" {
        c.MQTT.TCPPort = parseInt(v)
    }
    if v := os.Getenv("CLOUD_MQTT_SECRET"); v != "" {
        c.MQTT.Secret = v
    }
}

func envOr(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

func parseInt(s string) int {
    var n int
    fmt.Sscanf(s, "%d", &n)
    return n
}
```

- [ ] **Step 2: 创建示例配置文件**

```json
{
  "server": {
    "port": 8080,
    "mode": "debug",
    "read_timeout": 30,
    "write_timeout": 30
  },
  "mqtt": {
    "tcp_port": 1883,
    "ws_port": 1882,
    "secret": "your-hmac-secret-here"
  },
  "database": {
    "host": "localhost",
    "port": 5432,
    "user": "cloud",
    "password": "",
    "dbname": "cloud_control",
    "ssl_mode": "disable",
    "max_conns": 30
  },
  "jwt": {
    "secret": "your-jwt-secret-here",
    "access_expire": 24,
    "refresh_expire": 168
  },
  "upload": {
    "path": "./uploads",
    "max_size_mb": 100
  },
  "ratelimit": {
    "global_max_conns": 500,
    "conn_per_sec": 100,
    "ip_max_conns": 20
  }
}
```

- [ ] **Step 3: 添加 fmt import 到 config.go**

在 `config.go` 顶部添加 `"fmt"` import。

- [ ] **Step 4: 验证编译**

```bash
cd cloud-control-v2/server
go build ./...
# Expected: 编译成功
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add config module with env overrides"
```

---

### Task 1.3: PostgreSQL 连接 + 模型定义 + AutoMigrate

**Files:**
- Create: `cloud-control-v2/server/internal/domain/models.go`
- Create: `cloud-control-v2/server/internal/repo/db.go`

- [ ] **Step 1: 安装依赖**

```bash
cd cloud-control-v2/server
go get gorm.io/gorm gorm.io/driver/postgres github.com/jackc/pgx/v5
```

- [ ] **Step 2: 定义所有模型**

```go
// internal/domain/models.go
package domain

import (
    "time"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

// ---------- Auth ----------
type User struct {
    ID        uint      `gorm:"primaryKey"`
    Username  string    `gorm:"uniqueIndex;size:64;not null"`
    Password  string    `gorm:"size:255;not null"`
    Nickname  string    `gorm:"size:128"`
    Email     string    `gorm:"size:128"`
    Status    int       `gorm:"default:1"` // 1=active 0=disabled
    CreatedAt time.Time
    UpdatedAt time.Time
    Roles     []Role    `gorm:"many2many:user_roles"`
}

type Role struct {
    ID          uint         `gorm:"primaryKey"`
    Name        string       `gorm:"uniqueIndex;size:64;not null"`
    Description string       `gorm:"size:255"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Permissions []Permission `gorm:"many2many:role_permissions"`
}

type Permission struct {
    ID          uint   `gorm:"primaryKey"`
    Name        string `gorm:"uniqueIndex;size:64;not null"`
    Resource    string `gorm:"size:64;not null"`
    Action      string `gorm:"size:64;not null"`
    Description string `gorm:"size:255"`
}

// ---------- Device ----------
type DeviceGroup struct {
    ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Name        string     `gorm:"size:128;not null"`
    Description string     `gorm:"size:512"`
    ParentID    *uuid.UUID `gorm:"type:uuid"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Device struct {
    ID             string      `gorm:"primaryKey;size:128"`
    Name           string      `gorm:"size:255;not null"`
    ClientType     string      `gorm:"size:16;not null;default:'ec'"` // ec | hid
    GroupID        *uuid.UUID  `gorm:"type:uuid"`
    Group          *DeviceGroup
    IsOnline       bool        `gorm:"default:false;index:idx_devices_online,where:is_online = true"`
    LastHeartbeat  *time.Time
    LastIP         string      `gorm:"size:45"`
    DeviceInfo     string      `gorm:"type:jsonb;default:'{}'"` // JSONB
    Params         string      `gorm:"type:jsonb;default:'{}'"`
    Metadata       string      `gorm:"type:jsonb;default:'{}'"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
    TaskDevices    []TaskDevice
}

// ---------- Script ----------
type Script struct {
    ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Name        string    `gorm:"size:255;not null"`
    Description string    `gorm:"size:1024"`
    Content     string    `gorm:"type:text;not null"`     // JS code
    Params      string    `gorm:"type:jsonb;default:'[]'"` // [{name,type,default,required}]
    UserID      uint
    User        *User
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// ---------- Task ----------
type Task struct {
    ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Name        string    `gorm:"size:255;not null"`
    Description string    `gorm:"size:1024"`
    ScriptID    *uuid.UUID `gorm:"type:uuid"`
    Script      *Script
    Params      string    `gorm:"type:jsonb;default:'{}'"` // merged params
    CronExpr    string    `gorm:"size:64"`                 // cron expression
    CronEnabled bool      `gorm:"default:false"`
    TimeoutSec  int       `gorm:"default:300"`
    MaxRetries  int       `gorm:"default:0"`
    UserID      uint
    User        *User
    Status      int       `gorm:"default:0"` // 0=stopped 1=running
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Devices     []TaskDevice
}

type TaskDevice struct {
    ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TaskID      uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_task_device"`
    DeviceID    string     `gorm:"size:128;not null;uniqueIndex:idx_task_device"`
    Status      int        `gorm:"default:0"` // 0=pending 1=running 2=success 3=failed 4=timeout
    RetryCount  int        `gorm:"default:0"`
    MaxRetries  int        `gorm:"default:0"`
    TimeoutSec  int        `gorm:"default:300"`
    StartedAt   *time.Time
    FinishedAt  *time.Time
    Result      string     `gorm:"type:jsonb"`
    ErrorMsg    string     `gorm:"size:2048"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type TaskLog struct {
    ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TaskID    uuid.UUID `gorm:"type:uuid;not null;index"`
    DeviceID  string    `gorm:"size:128;not null"`
    Level     string    `gorm:"size:8;default:'info'"` // info|warn|error
    Message   string    `gorm:"size:4096"`
    CreatedAt time.Time `gorm:"index"`
}

// ---------- Screen ----------
type Screenshot struct {
    ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    DeviceID  string    `gorm:"size:128;not null;index:idx_screenshots_device"`
    FilePath  string    `gorm:"size:512;not null"`
    FileSize  int
    Width     int
    Height    int
    CreatedAt time.Time `gorm:"index:idx_screenshots_device"`
}

// ---------- SMS / Contacts ----------
type DeviceSms struct {
    ID       uint   `gorm:"primaryKey"`
    DeviceID string `gorm:"size:128;not null;index"`
    Sender   string `gorm:"size:64"`
    Body     string `gorm:"size:2048"`
    SmsDate  string `gorm:"size:32"`
    Hash     string `gorm:"size:64;uniqueIndex"` // dedup hash
    CreatedAt time.Time
}

type DeviceContact struct {
    ID        uint   `gorm:"primaryKey"`
    DeviceID  string `gorm:"size:128;not null;index"`
    Name      string `gorm:"size:128"`
    Phone     string `gorm:"size:64"`
    CreatedAt time.Time
}

// ---------- System ----------
type SystemLog struct {
    ID        uint   `gorm:"primaryKey"`
    UserID    uint
    Username  string `gorm:"size:64"`
    Action    string `gorm:"size:64"`
    Resource  string `gorm:"size:64"`
    Detail    string `gorm:"size:1024"`
    IP        string `gorm:"size:45"`
    CreatedAt time.Time `gorm:"index"`
}

type SystemConfig struct {
    ID    uint   `gorm:"primaryKey"`
    Key   string `gorm:"uniqueIndex;size:64;not null"`
    Value string `gorm:"type:text"`
}

// DeviceLog table is created manually with partitioning (see Task 1.3 Step 4)
// DeviceMetric table is created manually with partitioning

func AutoMigrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &User{}, &Role{}, &Permission{},
        &DeviceGroup{}, &Device{},
        &Script{},
        &Task{}, &TaskDevice{}, &TaskLog{},
        &Screenshot{},
        &DeviceSms{}, &DeviceContact{},
        &SystemLog{}, &SystemConfig{},
    )
}
```

- [ ] **Step 3: 编写数据库连接**

```go
// internal/repo/db.go
package repo

import (
    "fmt"
    "cloud-control-v2/server/config"
    "cloud-control-v2/server/internal/domain"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

func NewDB(cfg *config.DatabaseConfig) (*gorm.DB, error) {
    dsn := fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
    )
    logLevel := logger.Warn
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(logLevel),
    })
    if err != nil {
        return nil, fmt.Errorf("connect db: %w", err)
    }
    sqlDB, err := db.DB()
    if err != nil {
        return nil, err
    }
    sqlDB.SetMaxOpenConns(cfg.MaxConns)
    sqlDB.SetMaxIdleConns(cfg.MaxConns / 2)
    sqlDB.SetConnMaxLifetime(30 * 60 * 1e9) // 30min in ns
    sqlDB.SetConnMaxIdleTime(5 * 60 * 1e9)

    if err := domain.AutoMigrate(db); err != nil {
        return nil, fmt.Errorf("auto migrate: %w", err)
    }
    return db, nil
}
```

- [ ] **Step 4: 安装 uuid 依赖**

```bash
cd cloud-control-v2/server
go get github.com/google/uuid
```

- [ ] **Step 5: 更新 main.go 集成配置和数据库**

```go
// cmd/cloud-server/main.go
package main

import (
    "log"
    "cloud-control-v2/server/config"
    "cloud-control-v2/server/internal/repo"
)

func main() {
    cfg, err := config.Load("config.json")
    if err != nil {
        log.Printf("config not found, using defaults: %v", err)
        cfg = config.Default()
    }
    db, err := repo.NewDB(&cfg.Database)
    if err != nil {
        log.Fatalf("database init failed: %v", err)
    }
    _ = db
    log.Println("cloud-control v2 initialized")
    select {} // keep alive for now
}
```

- [ ] **Step 6: 验证编译**

```bash
go build ./cmd/cloud-server/
# Expected: 编译成功
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: add domain models, db connection, and auto-migration"
```

---

### Task 1.4: MQTT Broker 集成 + Auth Hook

**Files:**
- Create: `cloud-control-v2/server/internal/mqtt/broker.go`
- Create: `cloud-control-v2/server/internal/mqtt/hooks.go`

- [ ] **Step 1: 安装 mochi-mqtt**

```bash
cd cloud-control-v2/server
go get github.com/mochi-co/mqtt/v2
```

- [ ] **Step 2: 编写 Broker 启动**

```go
// internal/mqtt/broker.go
package mqtt

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "log"
    "sync"
    "cloud-control-v2/server/config"
    mqtt "github.com/mochi-co/mqtt/v2"
    "github.com/mochi-co/mqtt/v2/listeners"
    "github.com/mochi-co/mqtt/v2/packets"
)

type Broker struct {
    server  *mqtt.Server
    secret  string
    clients sync.Map // device_id -> *mqtt.Client
}

func New(cfg *config.MQTTConfig, db interface{}) (*Broker, error) {
    server := mqtt.New(&mqtt.Options{
        Capabilities: &mqtt.Capabilities{
            MaximumSessionExpiryInterval: 86400,
            ReceiveMaximum:               64,
            MaximumQos:                   2,
        },
    })
    b := &Broker{server: server, secret: cfg.Secret}

    // Add hooks
    server.Events.OnConnect = b.onConnect
    server.Events.OnDisconnect = b.onDisconnect

    // TCP listener
    tcp := listeners.NewTCP("tcp", fmt.Sprintf(":%d", cfg.TCPPort), nil)
    if err := server.AddListener(tcp); err != nil {
        return nil, fmt.Errorf("mqtt tcp listener: %w", err)
    }

    // WebSocket listener (for EC compatibility)
    ws := listeners.NewWebsocket("ws", fmt.Sprintf(":%d", cfg.WSPort), nil)
    if err := server.AddListener(ws); err != nil {
        return nil, fmt.Errorf("mqtt ws listener: %w", err)
    }

    return b, nil
}

func (b *Broker) Start() error {
    log.Println("MQTT broker starting...")
    return b.server.Serve()
}

func (b *Broker) Server() *mqtt.Server { return b.server }
```

- [ ] **Step 3: 编写 Auth Hook**

```go
// internal/mqtt/hooks.go
package mqtt

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "log"
    mqtt "github.com/mochi-co/mqtt/v2"
    "github.com/mochi-co/mqtt/v2/packets"
)

func (b *Broker) onConnect(cl *mqtt.Client, pk packets.Packet) error {
    username := string(pk.Connect.Username)
    password := string(pk.Connect.Password)

    // HMAC verification
    mac := hmac.New(sha256.New, []byte(b.secret))
    mac.Write([]byte(username))
    expected := hex.EncodeToString(mac.Sum(nil))

    if password != expected {
        log.Printf("MQTT auth failed for %s", username)
        return packets.ErrBadUsernameOrPassword
    }

    // Kick old connection for same device_id
    if old, ok := b.clients.LoadAndDelete(username); ok {
        oldClient := old.(*mqtt.Client)
        oldClient.Stop()
    }

    b.clients.Store(username, cl)
    log.Printf("MQTT device connected: %s (client_id=%s)", username, cl.ID)
    return nil
}

func (b *Broker) onDisconnect(cl *mqtt.Client, err error, graceful bool) {
    b.clients.Delete(string(cl.Properties.Username))
    log.Printf("MQTT device disconnected: %s (graceful=%v err=%v)", cl.ID, graceful, err)
}

func (b *Broker) GetClientCount() int {
    count := 0
    b.clients.Range(func(_, _ interface{}) bool { count++; return true })
    return count
}
```

- [ ] **Step 4: 更新 main.go 启动 MQTT**

```go
// 在 main.go 的 select{} 之前添加:
mqttBroker, err := mqtt.New(&cfg.MQTT, nil)
if err != nil {
    log.Fatalf("mqtt init failed: %v", err)
}
go func() {
    if err := mqttBroker.Start(); err != nil {
        log.Fatalf("mqtt serve: %v", err)
    }
}()
log.Printf("MQTT broker listening on tcp:%d ws:%d", cfg.MQTT.TCPPort, cfg.MQTT.WSPort)
```

添加 `"cloud-control-v2/server/internal/mqtt"` 到 import。

- [ ] **Step 5: 验证编译**

```bash
go build ./cmd/cloud-server/
# Expected: 编译成功
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add MQTT broker with HMAC auth and WS compatibility"
```

---

### Task 1.5: Gin HTTP API 框架 + 中间件

**Files:**
- Create: `cloud-control-v2/server/internal/api/router.go`
- Create: `cloud-control-v2/server/internal/api/middleware/jwt.go`
- Create: `cloud-control-v2/server/internal/api/middleware/rbac.go`
- Create: `cloud-control-v2/server/internal/api/middleware/logger.go`

- [ ] **Step 1: 安装依赖**

```bash
cd cloud-control-v2/server
go get github.com/gin-gonic/gin github.com/golang-jwt/jwt/v5 golang.org/x/crypto
```

- [ ] **Step 2: JWT 中间件**

```go
// internal/api/middleware/jwt.go
package middleware

import (
    "net/http"
    "strings"
    "time"
    "github.com/gin-gonic/gin"
    "github.com/golang-jwt/jwt/v5"
)

var jwtSecret []byte

func SetJWTSecret(secret string) { jwtSecret = []byte(secret) }

type Claims struct {
    UserID   uint   `json:"user_id"`
    Username string `json:"username"`
    jwt.RegisteredClaims
}

func GenerateToken(userID uint, username string, expireHours int) (string, error) {
    claims := Claims{
        UserID:   userID,
        Username: username,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireHours) * time.Hour)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(jwtSecret)
}

func JWTAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        header := c.GetHeader("Authorization")
        if header == "" || !strings.HasPrefix(header, "Bearer ") {
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing token"})
            c.Abort()
            return
        }
        tokenStr := strings.TrimPrefix(header, "Bearer ")
        claims := &Claims{}
        token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
            return jwtSecret, nil
        })
        if err != nil || !token.Valid {
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid token"})
            c.Abort()
            return
        }
        c.Set("user_id", claims.UserID)
        c.Set("username", claims.Username)
        c.Next()
    }
}
```

- [ ] **Step 3: RBAC 中间件**

```go
// internal/api/middleware/rbac.go
package middleware

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

var rbacDB *gorm.DB

func SetRbacDB(db *gorm.DB) { rbacDB = db }

// RequirePermission checks if current user has the required permission
func RequirePermission(resource, action string) gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := c.GetUint("user_id")
        if userID == 0 {
            c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "no permission"})
            c.Abort()
            return
        }
        // Check system_admin role
        var count int64
        rbacDB.Raw(`
            SELECT COUNT(*) FROM user_roles ur
            JOIN roles r ON r.id = ur.role_id
            JOIN role_permissions rp ON rp.role_id = r.id
            JOIN permissions p ON p.id = rp.permission_id
            WHERE ur.user_id = ? AND (
                r.name = 'system_admin'
                OR (p.resource = ? AND p.action = ?)
            )
        `, userID, resource, action).Scan(&count)
        if count == 0 {
            c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "no permission"})
            c.Abort()
            return
        }
        c.Next()
    }
}
```

- [ ] **Step 4: 系统日志中间件**

```go
// internal/api/middleware/logger.go
package middleware

import (
    "bytes"
    "io"
    "time"
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

var loggerDB *gorm.DB

func SetLoggerDB(db *gorm.DB) { loggerDB = db }

func SystemLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method == "GET" {
            c.Next()
            return
        }
        start := time.Now()
        // Read body for logging
        var bodyBytes []byte
        if c.Request.Body != nil {
            bodyBytes, _ = io.ReadAll(c.Request.Body)
            c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
        }
        c.Next()
        // Async log write
        go func() {
            username, _ := c.Get("username")
            loggerDB.Exec(`
                INSERT INTO system_logs (user_id, username, action, resource, detail, ip, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
            `, c.GetUint("user_id"), username, c.Request.Method,
                c.Request.URL.Path, string(bodyBytes)[:min(len(bodyBytes), 1024)],
                c.ClientIP(), start)
        }()
    }
}

func min(a, b int) int { if a < b { return a }; return b }
```

- [ ] **Step 5: Gin 路由注册**

```go
// internal/api/router.go
package api

import (
    "github.com/gin-gonic/gin"
    "cloud-control-v2/server/config"
    "cloud-control-v2/server/internal/api/middleware"
    "gorm.io/gorm"
)

func SetupRouter(cfg *config.Config, db *gorm.DB) *gin.Engine {
    if cfg.Server.Mode == "release" {
        gin.SetMode(gin.ReleaseMode)
    }

    middleware.SetJWTSecret(cfg.JWT.Secret)
    middleware.SetRbacDB(db)
    middleware.SetLoggerDB(db)

    r := gin.New()
    r.Use(gin.Recovery())
    r.Use(middleware.SystemLogger())

    // CORS
    r.Use(func(c *gin.Context) {
        c.Header("Access-Control-Allow-Origin", "*")
        c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
        c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        c.Next()
    })

    api := r.Group("/api/v1")

    // Public
    auth := api.Group("/auth")
    {
        auth.POST("/login", func(c *gin.Context) {
            c.JSON(200, gin.H{"code": 200, "message": "placeholder"})
        })
        auth.POST("/refresh", func(c *gin.Context) {
            c.JSON(200, gin.H{"code": 200, "message": "placeholder"})
        })
    }

    // Protected
    protected := api.Group("")
    protected.Use(middleware.JWTAuth())
    {
        protected.GET("/dashboard", func(c *gin.Context) {
            c.JSON(200, gin.H{"code": 200, "data": gin.H{}})
        })

        devices := protected.Group("/devices")
        {
            devices.GET("", func(c *gin.Context) { c.JSON(200, gin.H{"code": 200, "data": []interface{}{}}) })
            devices.GET("/:id", func(c *gin.Context) { c.JSON(200, gin.H{"code": 200}) })
        }

        tasks := protected.Group("/tasks")
        {
            tasks.GET("", func(c *gin.Context) { c.JSON(200, gin.H{"code": 200, "data": []interface{}{}}) })
        }
    }

    return r
}
```

- [ ] **Step 6: 更新 main.go 启动 HTTP**

```go
// 在 main.go 的 select{} 之前添加:
router := api.SetupRouter(cfg, db)
go func() {
    addr := fmt.Sprintf(":%d", cfg.Server.Port)
    log.Printf("HTTP API listening on %s", addr)
    if err := router.Run(addr); err != nil {
        log.Fatalf("http serve: %v", err)
    }
}()
```

- [ ] **Step 7: 验证编译**

```bash
go build ./cmd/cloud-server/
# Expected: 编译成功
```

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: add Gin router, JWT, RBAC, and system log middleware"
```

---

### Task 1.6: 初始化管理员账户和默认角色权限

**Files:**
- Create: `cloud-control-v2/server/internal/domain/auth/init.go`

- [ ] **Step 1: 种子数据初始化**

```go
// internal/domain/auth/init.go
package auth

import (
    "log"
    "golang.org/x/crypto/bcrypt"
    "gorm.io/gorm"
    "cloud-control-v2/server/internal/domain"
)

var DefaultPermissions = []domain.Permission{
    {Resource: "device", Action: "read", Description: "查看设备"},
    {Resource: "device", Action: "write", Description: "编辑设备"},
    {Resource: "device", Action: "delete", Description: "删除设备"},
    {Resource: "device", Action: "control", Description: "控制设备"},
    {Resource: "task", Action: "read", Description: "查看任务"},
    {Resource: "task", Action: "write", Description: "创建任务"},
    {Resource: "task", Action: "delete", Description: "删除任务"},
    {Resource: "task", Action: "control", Description: "控制任务"},
    {Resource: "script", Action: "read", Description: "查看脚本"},
    {Resource: "script", Action: "write", Description: "编辑脚本"},
    {Resource: "script", Action: "delete", Description: "删除脚本"},
    {Resource: "screen", Action: "read", Description: "查看截图"},
    {Resource: "screen", Action: "capture", Description: "截屏控制"},
    {Resource: "sms", Action: "read", Description: "查看短信"},
    {Resource: "sms", Action: "delete", Description: "删除短信"},
    {Resource: "contact", Action: "read", Description: "查看联系人"},
    {Resource: "resource", Action: "read", Description: "查看资源"},
    {Resource: "resource", Action: "write", Description: "上传资源"},
    {Resource: "resource", Action: "delete", Description: "删除资源"},
    {Resource: "template", Action: "read", Description: "查看模板"},
    {Resource: "template", Action: "write", Description: "编辑模板"},
    {Resource: "data", Action: "read", Description: "查看数据"},
    {Resource: "data", Action: "write", Description: "编辑数据"},
    {Resource: "system", Action: "read", Description: "查看系统配置"},
    {Resource: "system", Action: "write", Description: "修改系统配置"},
}

func InitAdmin(db *gorm.DB) error {
    // Check if admin already exists
    var count int64
    db.Model(&domain.User{}).Where("username = ?", "admin").Count(&count)
    if count > 0 {
        return nil
    }

    log.Println("Initializing admin user and RBAC...")

    // Create permissions
    perms := make([]domain.Permission, len(DefaultPermissions))
    for i, p := range DefaultPermissions {
        p.Name = p.Resource + ":" + p.Action
        db.Where(domain.Permission{Name: p.Name}).FirstOrCreate(&perms[i])
        perms[i] = p
    }

    // Create roles
    systemAdmin := domain.Role{Name: "system_admin", Description: "系统管理员"}
    db.Where(domain.Role{Name: "system_admin"}).FirstOrCreate(&systemAdmin)
    db.Model(&systemAdmin).Association("Permissions").Replace(perms)

    admin := domain.Role{Name: "admin", Description: "管理员"}
    db.Where(domain.Role{Name: "admin"}).FirstOrCreate(&admin)
    // admin gets all except system:* permissions
    var nonSystemPerms []domain.Permission
    for _, p := range perms {
        if p.Resource != "system" {
            nonSystemPerms = append(nonSystemPerms, p)
        }
    }
    db.Model(&admin).Association("Permissions").Replace(nonSystemPerms)

    userRole := domain.Role{Name: "user", Description: "普通用户"}
    db.Where(domain.Role{Name: "user"}).FirstOrCreate(&userRole)

    // Create admin user
    hash, _ := bcrypt.GenerateFromPassword([]byte("admin1234"), bcrypt.DefaultCost)
    u := domain.User{
        Username: "admin",
        Password: string(hash),
        Nickname: "Administrator",
        Status:   1,
    }
    db.Create(&u)
    db.Model(&u).Association("Roles").Append(&systemAdmin)

    log.Println("Admin user created: admin / admin1234")
    return nil
}
```

- [ ] **Step 2: 更新 main.go 调用初始化**

在数据库连接后、启动服务前添加：
```go
if err := auth.InitAdmin(db); err != nil {
    log.Printf("init admin warning: %v", err)
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./cmd/cloud-server/
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add admin initialization with RBAC seed data"
```

---

## Phase 2: 核心业务服务

### Task 2.1: 事件总线 + 批量写入器

**Files:**
- Create: `cloud-control-v2/server/internal/infra/eventbus/bus.go`
- Create: `cloud-control-v2/server/internal/infra/batcher/batch_writer.go`
- Create: `cloud-control-v2/server/internal/infra/ratelimit/limiter.go`

- [ ] **Step 1: 事件总线**

```go
// internal/infra/eventbus/bus.go
package eventbus

import "sync"

type Event struct {
    Type string
    Data interface{}
}

type Handler func(Event)

type Bus struct {
    mu       sync.RWMutex
    handlers map[string][]Handler
}

func New() *Bus {
    return &Bus{handlers: make(map[string][]Handler)}
}

func (b *Bus) Subscribe(eventType string, handler Handler) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(event Event) {
    b.mu.RLock()
    handlers := b.handlers[event.Type]
    b.mu.RUnlock()
    for _, h := range handlers {
        go h(event) // fire and forget
    }
}
```

- [ ] **Step 2: 批量写入器（核心削峰组件）**

```go
// internal/infra/batcher/batch_writer.go
package batcher

import (
    "sync"
    "time"
    "gorm.io/gorm"
)

type Priority int

const (
    PriorityImmediate Priority = iota // P0: immediate
    PriorityFast                       // P1: fast batch
    PrioritySlow                       // P2: slow batch
)

type BatchItem struct {
    Priority Priority
    Table    string
    Columns  []string
    Values   [][]interface{}
}

type BatchWriter struct {
    mu       sync.Mutex
    buffers  map[Priority][]BatchItem
    db       *gorm.DB
    ticker   *time.Ticker
    done     chan struct{}
}

func NewBatchWriter(db *gorm.DB) *BatchWriter {
    bw := &BatchWriter{
        buffers: make(map[Priority][]BatchItem),
        db:      db,
        ticker:  time.NewTicker(200 * time.Millisecond),
        done:    make(chan struct{}),
    }
    go bw.flushLoop()
    return bw
}

func (bw *BatchWriter) Push(item BatchItem) {
    if item.Priority == PriorityImmediate {
        bw.execute(item)
        return
    }
    bw.mu.Lock()
    bw.buffers[item.Priority] = append(bw.buffers[item.Priority], item)
    bw.mu.Unlock()
}

func (bw *BatchWriter) flushLoop() {
    for {
        select {
        case <-bw.ticker.C:
            bw.mu.Lock()
            for pri := PriorityFast; pri <= PrioritySlow; pri++ {
                items := bw.buffers[pri]
                if len(items) > 0 {
                    bw.buffers[pri] = nil
                    bw.mu.Unlock()
                    for _, item := range items {
                        bw.execute(item)
                    }
                    bw.mu.Lock()
                }
            }
            bw.mu.Unlock()
        case <-bw.done:
            return
        }
    }
}

func (bw *BatchWriter) execute(item BatchItem) {
    // Build UPSERT or batch INSERT
    if len(item.Values) == 1 {
        bw.db.Table(item.Table).Exec(
            buildUpsertSQL(item.Table, item.Columns),
            flatten(item.Values[0])...,
        )
        return
    }
    // Batch insert with COPY protocol for logs
    bw.db.Table(item.Table).Create(&item.Values)
}

func (bw *BatchWriter) Stop() {
    bw.ticker.Stop()
    close(bw.done)
}

func buildUpsertSQL(table string, cols []string) string {
    // Generic UPSERT for PostgreSQL
    sql := "INSERT INTO " + table + " ("
    for i, c := range cols {
        if i > 0 { sql += "," }
        sql += c
    }
    sql += ") VALUES ("
    for i := range cols {
        if i > 0 { sql += "," }
        sql += "?"
    }
    sql += ") ON CONFLICT (id) DO UPDATE SET updated_at = NOW()"
    return sql
}

func flatten(vals []interface{}) []interface{} { return vals }
```

- [ ] **Step 3: 连接限流器**

```go
// internal/infra/ratelimit/limiter.go
package ratelimit

import (
    "sync"
    "time"
)

type ConnectLimiter struct {
    globalTokens chan struct{}
    ipLimits     sync.Map // IP -> *rateLimiter
    maxPerIP     int
}

type rateLimiter struct {
    tokens    chan struct{}
    lastReset time.Time
}

func NewConnectLimiter(globalMax, connPerSec, maxPerIP int) *ConnectLimiter {
    cl := &ConnectLimiter{
        globalTokens: make(chan struct{}, globalMax),
        maxPerIP:     maxPerIP,
    }
    // Fill initial tokens
    for i := 0; i < globalMax; i++ {
        cl.globalTokens <- struct{}{}
    }
    // Refill goroutine
    go func() {
        ticker := time.NewTicker(time.Second / time.Duration(connPerSec))
        for range ticker.C {
            select {
            case cl.globalTokens <- struct{}{}:
            default:
            }
        }
    }()
    return cl
}

func (cl *ConnectLimiter) Allow(ip string) bool {
    // Global check
    select {
    case <-cl.globalTokens:
    default:
        return false
    }
    // IP check
    val, _ := cl.ipLimits.LoadOrStore(ip, &rateLimiter{
        tokens: make(chan struct{}, cl.maxPerIP),
    })
    rl := val.(*rateLimiter)
    select {
    case <-rl.tokens:
        return true
    default:
        // Refill IP bucket
        for i := 0; i < cl.maxPerIP/2; i++ {
            select {
            case rl.tokens <- struct{}{}:
            default:
            }
        }
        select {
        case <-rl.tokens:
            return true
        default:
            return false
        }
    }
}
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add eventbus, batch writer, and connection rate limiter"
```

---

### Task 2.2: 设备管理服务 (DeviceSvc)

**Files:**
- Create: `cloud-control-v2/server/internal/domain/device/service.go`
- Create: `cloud-control-v2/server/internal/repo/device_repo.go`

- [ ] **Step 1: 设备 Repository 层**

```go
// internal/repo/device_repo.go
package repo

import (
    "time"
    "cloud-control-v2/server/internal/domain"
    "gorm.io/gorm"
)

type DeviceRepo struct{ db *gorm.DB }

func NewDeviceRepo(db *gorm.DB) *DeviceRepo { return &DeviceRepo{db} }

func (r *DeviceRepo) Upsert(d *domain.Device) error {
    return r.db.Where("id = ?", d.ID).Assign(d).FirstOrCreate(d).Error
}

func (r *DeviceRepo) UpdateHeartbeat(id string, ip string) error {
    now := time.Now()
    return r.db.Model(&domain.Device{}).Where("id = ?", id).Updates(map[string]interface{}{
        "is_online": true, "last_heartbeat": now, "last_ip": ip,
    }).Error
}

func (r *DeviceRepo) MarkOffline(id string) error {
    return r.db.Model(&domain.Device{}).Where("id = ?", id).
        Update("is_online", false).Error
}

func (r *DeviceRepo) FindByID(id string) (*domain.Device, error) {
    var d domain.Device
    err := r.db.Preload("Group").First(&d, "id = ?", id).Error
    return &d, err
}

func (r *DeviceRepo) List(groupID string, keyword string, online *bool, page, size int) ([]domain.Device, int64, error) {
    var devices []domain.Device
    var total int64
    q := r.db.Model(&domain.Device{})
    if groupID != "" {
        q = q.Where("group_id = ?", groupID)
    }
    if keyword != "" {
        q = q.Where("name ILIKE ? OR id ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
    }
    if online != nil {
        q = q.Where("is_online = ?", *online)
    }
    q.Count(&total)
    err := q.Offset((page - 1) * size).Limit(size).Order("last_heartbeat DESC NULLS LAST").Find(&devices).Error
    return devices, total, err
}

func (r *DeviceRepo) GetOnlineDevices() ([]domain.Device, error) {
    var devices []domain.Device
    err := r.db.Where("is_online = ?", true).Find(&devices).Error
    return devices, err
}

func (r *DeviceRepo) BatchMarkOffline(threshold time.Duration) ([]string, error) {
    cutoff := time.Now().Add(-threshold)
    var ids []string
    err := r.db.Model(&domain.Device{}).
        Where("is_online = ? AND last_heartbeat < ?", true, cutoff).
        Pluck("id", &ids).Error
    if err != nil || len(ids) == 0 {
        return ids, err
    }
    err = r.db.Model(&domain.Device{}).Where("id IN ?", ids).
        Update("is_online", false).Error
    return ids, err
}

func (r *DeviceRepo) Delete(id string) error {
    return r.db.Delete(&domain.Device{}, "id = ?", id).Error
}

func (r *DeviceRepo) BatchDelete(ids []string) error {
    return r.db.Delete(&domain.Device{}, "id IN ?", ids).Error
}

func (r *DeviceRepo) BatchUpdateGroup(ids []string, groupID string) error {
    return r.db.Model(&domain.Device{}).Where("id IN ?", ids).
        Update("group_id", groupID).Error
}
```

- [ ] **Step 2: 设备服务层**

```go
// internal/domain/device/service.go
package device

import (
    "log"
    "sync"
    "time"
    "cloud-control-v2/server/internal/domain"
    "cloud-control-v2/server/internal/infra/batcher"
    "cloud-control-v2/server/internal/infra/eventbus"
    "cloud-control-v2/server/internal/repo"
    mqtt "cloud-control-v2/server/internal/mqtt"
    mqttlib "github.com/mochi-co/mqtt/v2"
)

type Service struct {
    repo      *repo.DeviceRepo
    broker    *mqtt.Broker
    batch     *batcher.BatchWriter
    bus       *eventbus.Bus
    onlineMap sync.Map // device_id -> last_heartbeat_time
}

func NewService(repo *repo.DeviceRepo, broker *mqtt.Broker, batch *batcher.BatchWriter, bus *eventbus.Bus) *Service {
    svc := &Service{repo: repo, broker: broker, batch: batch, bus: bus}
    go svc.offlineDetector()
    return svc
}

func (s *Service) Register(deviceID string, info map[string]interface{}) error {
    d := &domain.Device{
        ID:       deviceID,
        Name:     strVal(info, "name", deviceID),
        IsOnline: true,
        LastIP:   strVal(info, "ip", ""),
    }
    if t, ok := info["client_type"].(string); ok { d.ClientType = t }
    if t == "" { d.ClientType = "ec" }
    now := time.Now()
    d.LastHeartbeat = &now
    s.onlineMap.Store(deviceID, time.Now())
    return s.repo.Upsert(d)
}

func (s *Service) Heartbeat(deviceID string, ip string) {
    s.onlineMap.Store(deviceID, time.Now())
    s.batch.Push(batcher.BatchItem{
        Priority: batcher.PrioritySlow,
        Table:    "devices",
        Columns:  []string{"id", "is_online", "last_heartbeat", "last_ip", "updated_at"},
        Values:   [][]interface{}{{deviceID, true, time.Now(), ip, time.Now()}},
    })
}

func (s *Service) SendCommand(deviceID string, command string, params map[string]interface{}) error {
    topic := "cloud/" + deviceID + "/command"
    payload := map[string]interface{}{"command": command, "params": params}
    return s.broker.Server().Publish(topic, payload, false, 1)
}

func (s *Service) SendTask(deviceID string, taskPush map[string]interface{}) error {
    topic := "cloud/" + deviceID + "/task"
    return s.broker.Server().Publish(topic, taskPush, false, 1)
}

func (s *Service) GetOnlineDevices() []string {
    var ids []string
    s.onlineMap.Range(func(k, v interface{}) bool {
        ids = append(ids, k.(string))
        return true
    })
    return ids
}

func (s *Service) IsOnline(deviceID string) bool {
    _, ok := s.onlineMap.Load(deviceID)
    return ok
}

// offlineDetector runs every 10s, marks devices with >60s no heartbeat
func (s *Service) offlineDetector() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        cutoff := time.Now().Add(-60 * time.Second)
        var offlineIDs []string
        s.onlineMap.Range(func(k, v interface{}) bool {
            if v.(time.Time).Before(cutoff) {
                offlineIDs = append(offlineIDs, k.(string))
            }
            return true
        })
        for _, id := range offlineIDs {
            s.onlineMap.Delete(id)
            s.repo.MarkOffline(id)
            s.bus.Publish(eventbus.Event{Type: "device.offline", Data: id})
            log.Printf("Device offline: %s", id)
        }
    }
}

func strVal(m map[string]interface{}, key, fallback string) string {
    if v, ok := m[key].(string); ok { return v }
    return fallback
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add device service with heartbeat batching and offline detection"
```

---

### Task 2.3: 任务引擎 + Cron 调度

**Files:**
- Create: `cloud-control-v2/server/internal/domain/task/service.go`
- Create: `cloud-control-v2/server/internal/domain/task/scheduler.go`
- Create: `cloud-control-v2/server/internal/repo/task_repo.go`

- [ ] **Step 1: Task Repository**

```go
// internal/repo/task_repo.go
package repo

import (
    "cloud-control-v2/server/internal/domain"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

type TaskRepo struct{ db *gorm.DB }

func NewTaskRepo(db *gorm.DB) *TaskRepo { return &TaskRepo{db} }

func (r *TaskRepo) Create(task *domain.Task) error { return r.db.Create(task).Error }

func (r *TaskRepo) Update(task *domain.Task) error { return r.db.Save(task).Error }

func (r *TaskRepo) FindByID(id uuid.UUID) (*domain.Task, error) {
    var t domain.Task
    err := r.db.Preload("Script").Preload("Devices").First(&t, "id = ?", id).Error
    return &t, err
}

func (r *TaskRepo) List(userID uint, page, size int) ([]domain.Task, int64, error) {
    var tasks []domain.Task
    var total int64
    q := r.db.Model(&domain.Task{}).Where("user_id = ?", userID)
    q.Count(&total)
    err := q.Offset((page - 1) * size).Limit(size).Order("created_at DESC").Find(&tasks).Error
    return tasks, total, err
}

func (r *TaskRepo) GetCronEnabled() ([]domain.Task, error) {
    var tasks []domain.Task
    err := r.db.Where("cron_enabled = ? AND status = ?", true, 1).Preload("Script").Find(&tasks).Error
    return tasks, err
}

func (r *TaskRepo) UpdateDeviceStatus(taskID uuid.UUID, deviceID string, status int, result string, errMsg string) error {
    return r.db.Model(&domain.TaskDevice{}).
        Where("task_id = ? AND device_id = ?", taskID, deviceID).
        Updates(map[string]interface{}{
            "status": status, "result": result, "error_msg": errMsg,
            "finished_at": gorm.Expr("NOW()"),
        }).Error
}

func (r *TaskRepo) AddDevices(taskID uuid.UUID, deviceIDs []string, task *domain.Task) error {
    var tds []domain.TaskDevice
    for _, did := range deviceIDs {
        tds = append(tds, domain.TaskDevice{
            TaskID: taskID, DeviceID: did,
            TimeoutSec: task.TimeoutSec, MaxRetries: task.MaxRetries,
        })
    }
    return r.db.Create(&tds).Error
}

func (r *TaskRepo) GetTaskDevices(taskID uuid.UUID) ([]domain.TaskDevice, error) {
    var tds []domain.TaskDevice
    err := r.db.Where("task_id = ?", taskID).Find(&tds).Error
    return tds, err
}

func (r *TaskRepo) CreateLog(log *domain.TaskLog) error { return r.db.Create(log).Error }
```

- [ ] **Step 2: Task Service**

```go
// internal/domain/task/service.go
package task

import (
    "encoding/json"
    "log"
    "time"
    "cloud-control-v2/server/internal/domain"
    "cloud-control-v2/server/internal/domain/device"
    "cloud-control-v2/server/internal/repo"
    "github.com/google/uuid"
)

type Service struct {
    repo      *repo.TaskRepo
    deviceSvc *device.Service
}

func NewService(repo *repo.TaskRepo, deviceSvc *device.Service) *Service {
    return &Service{repo: repo, deviceSvc: deviceSvc}
}

func (s *Service) Create(task *domain.Task, deviceIDs []string) error {
    task.ID = uuid.New()
    task.Status = 0
    if err := s.repo.Create(task); err != nil {
        return err
    }
    if len(deviceIDs) > 0 {
        return s.repo.AddDevices(task.ID, deviceIDs, task)
    }
    return nil
}

func (s *Service) StartTask(taskID uuid.UUID) error {
    task, err := s.repo.FindByID(taskID)
    if err != nil {
        return err
    }
    tds, err := s.repo.GetTaskDevices(taskID)
    if err != nil {
        return err
    }
    task.Status = 1
    s.repo.Update(task)

    // Merge params: device > task > script
    scriptParams := make(map[string]interface{})
    if task.Script != nil {
        json.Unmarshal([]byte(task.Script.Params), &scriptParams)
    }
    taskParams := make(map[string]interface{})
    json.Unmarshal([]byte(task.Params), &taskParams)

    for _, td := range tds {
        if !s.deviceSvc.IsOnline(td.DeviceID) {
            // Device offline - MQTT will queue via session persistence
            log.Printf("Device %s offline, task %s queued", td.DeviceID, taskID)
        }
        merged := mergeParams(scriptParams, taskParams, nil)
        push := map[string]interface{}{
            "task_id":   taskID.String(),
            "task_name": task.Name,
            "script":    task.Script.Content,
            "params":    merged,
            "timeout":   td.TimeoutSec,
        }
        s.deviceSvc.SendTask(td.DeviceID, push)
    }
    return nil
}

func (s *Service) HandleStatus(taskID, deviceID string, status int, result string, errMsg string) {
    tid, _ := uuid.Parse(taskID)
    s.repo.UpdateDeviceStatus(tid, deviceID, status, result, errMsg)
}

func mergeParams(script, task, device map[string]interface{}) map[string]interface{} {
    m := make(map[string]interface{})
    for k, v := range script { m[k] = v }
    for k, v := range task { m[k] = v }
    for k, v := range device { m[k] = v }
    return m
}
```

- [ ] **Step 3: Cron 调度器**

```go
// internal/domain/task/scheduler.go
package task

import (
    "log"
    "sync"
    "github.com/google/uuid"
    "github.com/robfig/cron/v3"
)

type Scheduler struct {
    cron    *cron.Cron
    svc     *Service
    entries sync.Map // task_id -> cron.EntryID
}

func NewScheduler(svc *Service) *Scheduler {
    s := &Scheduler{
        cron: cron.New(cron.WithSeconds()),
        svc:  svc,
    }
    s.cron.Start()
    return s
}

func (s *Scheduler) AddTask(taskID uuid.UUID, cronExpr string) {
    entryID, err := s.cron.AddFunc(cronExpr, func() {
        log.Printf("Cron trigger task: %s", taskID)
        if err := s.svc.StartTask(taskID); err != nil {
            log.Printf("Cron task %s failed: %v", taskID, err)
        }
    })
    if err != nil {
        log.Printf("Add cron for task %s failed: %v", taskID, err)
        return
    }
    // Remove old entry if exists
    if old, loaded := s.entries.LoadAndDelete(taskID); loaded {
        s.cron.Remove(old.(cron.EntryID))
    }
    s.entries.Store(taskID, entryID)
}

func (s *Scheduler) RemoveTask(taskID uuid.UUID) {
    if entryID, ok := s.entries.LoadAndDelete(taskID); ok {
        s.cron.Remove(entryID.(cron.EntryID))
    }
}

func (s *Scheduler) Stop() { s.cron.Stop() }

func (s *Scheduler) LoadAll(getCronTasks func() ([]struct {
    ID       uuid.UUID
    CronExpr string
}, error)) {
    // Called on startup to load persisted cron tasks
}
```

- [ ] **Step 4: 安装 cron 依赖**

```bash
go get github.com/robfig/cron/v3
```

- [ ] **Step 5: 验证编译**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add task engine with parameter merging and cron scheduler"
```

---

### Task 2.4: MQTT 消息路由 Hook 集成

**Files:**
- Modify: `cloud-control-v2/server/internal/mqtt/hooks.go`

- [ ] **Step 1: 添加 OnPublished Hook 路由**

```go
// 在 hooks.go 中添加:

import (
    "encoding/json"
    "strings"
    mqttlib "github.com/mochi-co/mqtt/v2"
)

// MsgRouter routes MQTT messages to domain services
type MsgRouter struct {
    DeviceSvc interface {
        Register(deviceID string, info map[string]interface{}) error
        Heartbeat(deviceID string, ip string)
    }
    TaskSvc interface {
        HandleStatus(taskID, deviceID string, status int, result, errMsg string)
    }
    ScreenSvc interface {
        SaveScreenshot(deviceID string, data []byte) error
    }
    SmsSvc interface {
        IngestSms(deviceID string, sender, body, smsDate string)
    }
}

func (r *MsgRouter) Handle(cl *mqttlib.Client, pk packets.Packet) (packets.Packet, error) {
    topic := pk.TopicName
    parts := strings.Split(topic, "/")
    if len(parts) < 3 || parts[0] != "cloud" {
        return pk, nil
    }
    deviceID := parts[1]
    subTopic := parts[2]

    var data map[string]interface{}
    json.Unmarshal(pk.Payload, &data)

    switch subTopic {
    case "register":
        r.DeviceSvc.Register(deviceID, data)
    case "heartbeat":
        ip, _ := data["ip"].(string)
        r.DeviceSvc.Heartbeat(deviceID, ip)
    case "status":
        taskID, _ := data["task_id"].(string)
        status := int(data["status"].(float64))
        result, _ := data["result"].(string)
        errMsg, _ := data["error"].(string)
        r.TaskSvc.HandleStatus(taskID, deviceID, status, result, errMsg)
    case "screenshot":
        // Screenshot handled via base64 decode
    case "sms":
        sender, _ := data["from"].(string)
        body, _ := data["body"].(string)
        smsDate, _ := data["date"].(string)
        r.SmsSvc.IngestSms(deviceID, sender, body, smsDate)
    }
    return pk, nil
}
```

- [ ] **Step 2: 更新 Broker 集成 MsgRouter**

在 `broker.go` 中添加 `server.Events.OnPublished = router.Handle`

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add MQTT message routing hook to domain services"
```

---

### Task 2.5: HTTP API 完整实现

**Files:**
- Create: `cloud-control-v2/server/internal/api/handlers/device.go`
- Create: `cloud-control-v2/server/internal/api/handlers/task.go`
- Create: `cloud-control-v2/server/internal/api/handlers/auth.go`
- Create: `cloud-control-v2/server/internal/api/handlers/screen.go`
- Create: `cloud-control-v2/server/internal/api/handlers/dashboard.go`
- Modify: `cloud-control-v2/server/internal/api/router.go`

- [ ] **Step 1: Auth Handler（登录/刷新/用户信息）**

```go
// internal/api/handlers/auth.go
package handlers

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "golang.org/x/crypto/bcrypt"
    "gorm.io/gorm"
    "cloud-control-v2/server/config"
    "cloud-control-v2/server/internal/api/middleware"
    "cloud-control-v2/server/internal/domain"
)

type AuthHandler struct {
    db  *gorm.DB
    cfg *config.Config
}

func NewAuthHandler(db *gorm.DB, cfg *config.Config) *AuthHandler {
    return &AuthHandler{db: db, cfg: cfg}
}

func (h *AuthHandler) Login(c *gin.Context) {
    var req struct {
        Username string `json:"username" binding:"required"`
        Password string `json:"password" binding:"required"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
        return
    }
    var user domain.User
    if err := h.db.Where("username = ? AND status = 1", req.Username).First(&user).Error; err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid credentials"})
        return
    }
    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid credentials"})
        return
    }
    accessToken, _ := middleware.GenerateToken(user.ID, user.Username, h.cfg.JWT.AccessExpire)
    refreshToken, _ := middleware.GenerateToken(user.ID, user.Username, h.cfg.JWT.RefreshExpire)
    c.JSON(http.StatusOK, gin.H{
        "code":    200,
        "message": "success",
        "data": gin.H{
            "access_token":  accessToken,
            "refresh_token": refreshToken,
            "user":          user,
        },
    })
}

func (h *AuthHandler) Refresh(c *gin.Context) {
    userID := c.GetUint("user_id")
    username := c.GetString("username")
    accessToken, _ := middleware.GenerateToken(userID, username, h.cfg.JWT.AccessExpire)
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"access_token": accessToken}})
}

func (h *AuthHandler) UserInfo(c *gin.Context) {
    var user domain.User
    h.db.Preload("Roles.Permissions").First(&user, c.GetUint("user_id"))
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": user})
}
```

- [ ] **Step 2: Device Handler**

```go
// internal/api/handlers/device.go
package handlers

import (
    "net/http"
    "strconv"
    "github.com/gin-gonic/gin"
    "cloud-control-v2/server/internal/domain/device"
    "cloud-control-v2/server/internal/repo"
)

type DeviceHandler struct {
    svc  *device.Service
    repo *repo.DeviceRepo
}

func NewDeviceHandler(svc *device.Service, repo *repo.DeviceRepo) *DeviceHandler {
    return &DeviceHandler{svc: svc, repo: repo}
}

func (h *DeviceHandler) List(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
    keyword := c.Query("keyword")
    groupID := c.Query("group_id")
    onlineStr := c.Query("online")

    var online *bool
    if onlineStr == "true" { t := true; online = &t }
    if onlineStr == "false" { f := false; online = &f }

    devices, total, err := h.repo.List(groupID, keyword, online, page, size)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": devices, "total": total, "page": page, "size": size})
}

func (h *DeviceHandler) Get(c *gin.Context) {
    d, err := h.repo.FindByID(c.Param("id"))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "device not found"})
        return
    }
    d.IsOnline = h.svc.IsOnline(d.ID)
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": d})
}

func (h *DeviceHandler) SendCommand(c *gin.Context) {
    var req struct {
        Command string                 `json:"command" binding:"required"`
        Params  map[string]interface{} `json:"params"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.svc.SendCommand(c.Param("id"), req.Command, req.Params); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 200, "message": "command sent"})
}

func (h *DeviceHandler) GetOnline(c *gin.Context) {
    ids := h.svc.GetOnlineDevices()
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": ids, "count": len(ids)})
}

func (h *DeviceHandler) Delete(c *gin.Context) {
    if err := h.repo.Delete(c.Param("id")); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 200, "message": "deleted"})
}

func (h *DeviceHandler) BatchDelete(c *gin.Context) {
    var req struct{ IDs []string `json:"ids" binding:"required"` }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400})
        return
    }
    h.repo.BatchDelete(req.IDs)
    c.JSON(http.StatusOK, gin.H{"code": 200, "message": "deleted"})
}
```

- [ ] **Step 3: Screen Handler**

```go
// internal/api/handlers/screen.go
package handlers

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "cloud-control-v2/server/internal/domain/device"
)

type ScreenHandler struct {
    deviceSvc *device.Service
}

func NewScreenHandler(deviceSvc *device.Service) *ScreenHandler {
    return &ScreenHandler{deviceSvc: deviceSvc}
}

func (h *ScreenHandler) Capture(c *gin.Context) {
    var req struct{ DeviceIDs []string `json:"device_ids" binding:"required"` }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400})
        return
    }
    for _, did := range req.DeviceIDs {
        h.deviceSvc.SendCommand(did, "screenshot", nil)
    }
    c.JSON(http.StatusOK, gin.H{"code": 200, "message": "capture commands sent"})
}

func (h *ScreenHandler) ListScreenshots(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"code": 200, "data": []interface{}{}})
}
```

- [ ] **Step 4: Dashboard Handler**

```go
// internal/api/handlers/dashboard.go
package handlers

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

type DashboardHandler struct{ db *gorm.DB }

func NewDashboardHandler(db *gorm.DB) *DashboardHandler { return &DashboardHandler{db} }

func (h *DashboardHandler) Stats(c *gin.Context) {
    var online, offline, running int64
    h.db.Table("devices").Where("is_online = ?", true).Count(&online)
    h.db.Table("devices").Where("is_online = ?", false).Count(&offline)
    h.db.Table("task_devices").Where("status = ?", 1).Count(&running)
    c.JSON(http.StatusOK, gin.H{
        "code": 200,
        "data": gin.H{
            "online":  online,
            "offline": offline,
            "running": running,
        },
    })
}
```

- [ ] **Step 5: 更新 router.go 注册所有路由**

基于 Task 2.2 的 handler 替换 router.go 中的占位 handler。关键路由：

```go
// Auth
auth.POST("/login", authHandler.Login)
auth.POST("/refresh", middleware.JWTAuth(), authHandler.Refresh)
protected.GET("/user/info", authHandler.UserInfo)

// Devices
devices := protected.Group("/devices")
devices.GET("", deviceHandler.List)
devices.GET("/:id", deviceHandler.Get)
devices.POST("/:id/command", deviceHandler.SendCommand)
devices.DELETE("/:id", deviceHandler.Delete)
devices.POST("/batch-delete", deviceHandler.BatchDelete)

// WebSocket device realtime
protected.GET("/ws/devices-realtime", deviceHandler.GetOnline)

// Screen
screen := protected.Group("/screen")
screen.POST("/capture", screenHandler.Capture)
screen.GET("/screenshots/:device_id", screenHandler.ListScreenshots)
screen.GET("/wall", func(c *gin.Context) {
    // Returns device IDs + latest screenshot URLs
})

// Tasks
tasks := protected.Group("/tasks")
tasks.GET("", taskHandler.List)
tasks.POST("", taskHandler.Create)
tasks.POST("/:id/start", taskHandler.Start)
tasks.GET("/:id", taskHandler.Get)
tasks.GET("/:id/devices", taskHandler.GetTaskDevices)

// Dashboard
protected.GET("/dashboard", dashboardHandler.Stats)

// System
system := protected.Group("/system")
system.GET("/logs", systemHandler.ListLogs)
system.GET("/config", systemHandler.GetConfig)
system.PUT("/config", systemHandler.UpdateConfig)
```

- [ ] **Step 6: 更新 main.go 组装依赖**

```go
func main() {
    cfg, _ := config.Load("config.json")
    if cfg == nil { cfg = config.Default() }
    
    db, _ := repo.NewDB(&cfg.Database)
    auth.InitAdmin(db)
    
    bus := eventbus.New()
    batch := batcher.NewBatchWriter(db)
    
    mqttBroker, _ := mqtt.New(&cfg.MQTT, nil)
    
    deviceRepo := repo.NewDeviceRepo(db)
    taskRepo := repo.NewTaskRepo(db)
    
    deviceSvc := device.NewService(deviceRepo, mqttBroker, batch, bus)
    taskSvc := task.NewService(taskRepo, deviceSvc)
    scheduler := task.NewScheduler(taskSvc)
    
    router := api.SetupRouter(cfg, db, deviceSvc, taskSvc, scheduler)
    
    go mqttBroker.Start()
    go router.Run(fmt.Sprintf(":%d", cfg.Server.Port))
    
    // Graceful shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("Shutting down...")
    batch.Stop()
    scheduler.Stop()
}
```

- [ ] **Step 7: 验证编译**

```bash
go build ./cmd/cloud-server/
```

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: implement full HTTP API handlers and wire dependencies"
```

---

### Task 2.6: WebSocket 桥接层（EC 兼容）

**Files:**
- Create: `cloud-control-v2/server/internal/api/ws_bridge.go`

- [ ] **Step 1: WebSocket 桥接**

```go
// internal/api/ws_bridge.go
package api

import (
    "encoding/json"
    "log"
    "net/http"
    "strings"
    "sync"
    "time"
    "github.com/gin-gonic/gin"
    "github.com/gorilla/websocket"
    mqttlib "github.com/mochi-co/mqtt/v2"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

type WSBridge struct {
    broker    *mqttlib.Server
    conns     sync.Map // device_id -> *websocket.Conn
    deviceSvc DeviceService
}

type DeviceService interface {
    Heartbeat(deviceID string, ip string)
    Register(deviceID string, info map[string]interface{}) error
}

func NewWSBridge(broker *mqttlib.Server, ds DeviceService) *WSBridge {
    return &WSBridge{broker: broker, deviceSvc: ds}
}

func (b *WSBridge) HandleWS(c *gin.Context) {
    deviceID := c.Param("device_id")
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    // Kick old connection
    if old, loaded := b.conns.LoadAndDelete(deviceID); loaded {
        old.(*websocket.Conn).Close()
    }
    b.conns.Store(deviceID, conn)

    // Subscribe to MQTT topic for this device
    b.broker.Subscribe("cloud/"+deviceID+"/task", 1, func(cl *mqttlib.Client, pk mqttlib.Packet) error {
        return b.forwardToWS(deviceID, pk.Payload)
    })
    b.broker.Subscribe("cloud/"+deviceID+"/command", 1, func(cl *mqttlib.Client, pk mqttlib.Packet) error {
        return b.forwardToWS(deviceID, pk.Payload)
    })

    defer func() {
        conn.Close()
        b.conns.Delete(deviceID)
    }()

    conn.SetReadDeadline(time.Now().Add(90 * time.Second))
    conn.SetPongHandler(func(string) error {
        conn.SetReadDeadline(time.Now().Add(90 * time.Second))
        return nil
    })

    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            log.Printf("WS read error for %s: %v", deviceID, err)
            return
        }
        b.routeWSMessage(deviceID, conn, msg)
    }
}

func (b *WSBridge) routeWSMessage(deviceID string, conn *websocket.Conn, msg []byte) {
    var envelope struct {
        Type    string          `json:"type"`
        Data    json.RawMessage `json:"data"`
        Message string          `json:"message"`
    }
    if err := json.Unmarshal(msg, &envelope); err != nil {
        return
    }
    switch envelope.Type {
    case "register":
        var data map[string]interface{}
        json.Unmarshal(envelope.Data, &data)
        b.deviceSvc.Register(deviceID, data)
        conn.WriteJSON(map[string]interface{}{
            "type": "register", "data": map[string]string{"message": "ok"},
        })
    case "heartbeat":
        b.deviceSvc.Heartbeat(deviceID, "")
    case "task_status":
        // Forward to MQTT status topic
        b.broker.Publish("cloud/"+deviceID+"/status", envelope.Data, false, 1)
    case "screenshot":
        b.broker.Publish("cloud/"+deviceID+"/screenshot", envelope.Data, false, 1)
    case "sms_new":
        b.broker.Publish("cloud/"+deviceID+"/sms", envelope.Data, false, 1)
    }
}

func (b *WSBridge) forwardToWS(deviceID string, payload []byte) error {
    conn, ok := b.conns.Load(deviceID)
    if !ok {
        return nil
    }
    return conn.(*websocket.Conn).WriteMessage(websocket.TextMessage, payload)
}
```

- [ ] **Step 2: 添加 WebSocket 路由到 router.go**

```go
// 在 public 路由组中添加:
r.GET("/ws/device/:device_id", wsBridge.HandleWS)
```

- [ ] **Step 3: 安装 gorilla/websocket**

```bash
go get github.com/gorilla/websocket
```

- [ ] **Step 4: 验证编译**

```bash
go build ./cmd/cloud-server/
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add WebSocket bridge for EC client compatibility"
```

---

### Task 2.7: 后台任务（清理 + 告警 + 备份）

**Files:**
- Create: `cloud-control-v2/server/internal/domain/background/jobs.go`

- [ ] **Step 1: 后台清理任务**

```go
// internal/domain/background/jobs.go
package background

import (
    "log"
    "os"
    "path/filepath"
    "time"
    "gorm.io/gorm"
)

func StartBackgroundJobs(db *gorm.DB, uploadPath string) {
    // Clean old screenshots (every hour, >24h)
    go func() {
        ticker := time.NewTicker(1 * time.Hour)
        for range ticker.C {
            cutoff := time.Now().Add(-24 * time.Hour)
            // DB cleanup
            db.Exec("DELETE FROM screenshots WHERE created_at < ?", cutoff)
            // File cleanup
            filepath.Walk(uploadPath+"/screenshots", func(path string, info os.FileInfo, err error) error {
                if err != nil { return nil }
                if !info.IsDir() && info.ModTime().Before(cutoff) {
                    os.Remove(path)
                }
                return nil
            })
            log.Println("Screenshot cleanup complete")
        }
    }()

    // Clean old logs (every 6 hours, >30 days)
    go func() {
        ticker := time.NewTicker(6 * time.Hour)
        for range ticker.C {
            cutoff := time.Now().Add(-30 * 24 * time.Hour)
            // For partitioned tables, use DROP PARTITION or DELETE
            db.Exec("DELETE FROM task_logs WHERE created_at < ?", cutoff)
            log.Println("Log cleanup complete")
        }
    }()
}
```

- [ ] **Step 2: 更新 main.go 启动后台任务**

```go
background.StartBackgroundJobs(db, cfg.Upload.Path)
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add background cleanup jobs for screenshots and logs"
```

---

## Phase 3: 前端重写 (Nuxt 3 + Naive UI)

### Task 3.1: Nuxt 3 项目初始化 + 主题系统

**Files:**
- Create: `cloud-control-v2/web/` (nuxt init)
- Create: `cloud-control-v2/web/nuxt.config.ts`
- Create: `cloud-control-v2/web/app.vue`
- Create: `cloud-control-v2/web/composables/useTheme.ts`

- [ ] **Step 1: 创建 Nuxt 3 项目**

```bash
cd cloud-control-v2
npx nuxi@latest init web
cd web
npm install naive-ui @vicons/ionicons5 pinia @pinia/nuxt echarts vue-echarts
npm install -D unocss @unocss/nuxt
```

- [ ] **Step 2: 配置 nuxt.config.ts**

```typescript
// nuxt.config.ts
export default defineNuxtConfig({
  ssr: false,
  modules: ['@pinia/nuxt', '@unocss/nuxt'],
  css: ['@unocss/reset/tailwind.css'],
  app: {
    head: {
      title: '云控系统 V2',
      meta: [{ name: 'viewport', content: 'width=device-width,initial-scale=1' }],
    },
  },
  vite: {
    optimizeDeps: { include: ['naive-ui', 'echarts'] },
  },
})
```

- [ ] **Step 3: 主题系统**

```typescript
// composables/useTheme.ts
import { darkTheme, type GlobalTheme } from 'naive-ui'

type ThemeMode = 'light' | 'dark' | 'auto'

export const useTheme = () => {
  const mode = useState<ThemeMode>('theme-mode', () => {
    if (process.client) {
      return (localStorage.getItem('theme-mode') as ThemeMode) || 'auto'
    }
    return 'auto'
  })

  const naiveTheme = computed<GlobalTheme | null>(() => {
    const systemDark = process.client && window.matchMedia('(prefers-color-scheme: dark)').matches
    const isDark = mode.value === 'dark' || (mode.value === 'auto' && systemDark)
    return isDark ? darkTheme : null
  })

  const toggle = (newMode: ThemeMode) => {
    mode.value = newMode
    if (process.client) localStorage.setItem('theme-mode', newMode)
  }

  return { mode, naiveTheme, toggle }
}
```

- [ ] **Step 4: app.vue 根布局**

```vue
<!-- app.vue -->
<template>
  <n-config-provider :theme="naiveTheme" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <NuxtPage />
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { zhCN, dateZhCN } from 'naive-ui'
const { naiveTheme } = useTheme()
</script>
```

- [ ] **Step 5: 验证项目启动**

```bash
cd web && npm run dev
# 浏览器打开 http://localhost:3000，看到空白页面即为成功
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: init Nuxt 3 project with Naive UI theme system"
```

---

### Task 3.2: 布局 + 认证页面

**Files:**
- Create: `cloud-control-v2/web/layouts/default.vue`
- Create: `cloud-control-v2/web/pages/login.vue`
- Create: `cloud-control-v2/web/stores/auth.ts`
- Create: `cloud-control-v2/web/middleware/auth.ts`

- [ ] **Step 1: 认证 Store**

```typescript
// stores/auth.ts
import { defineStore } from 'pinia'

interface User {
  id: number; username: string; nickname: string
  roles: Array<{ name: string; permissions: Array<{ resource: string; action: string }> }>
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref(process.client ? localStorage.getItem('access_token') || '' : '')
  const user = ref<User | null>(null)
  const isLoggedIn = computed(() => !!token.value)

  async function login(username: string, password: string) {
    const { data } = await useFetch('/api/v1/auth/login', {
      method: 'POST', body: { username, password },
    })
    if (data.value?.code === 200) {
      token.value = data.value.data.access_token
      localStorage.setItem('access_token', token.value)
      localStorage.setItem('refresh_token', data.value.data.refresh_token)
      user.value = data.value.data.user
      return true
    }
    return false
  }

  async function fetchUser() {
    const { data } = await useFetch('/api/v1/user/info', {
      headers: { Authorization: `Bearer ${token.value}` },
    })
    if (data.value?.code === 200) user.value = data.value.data
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem('access_token')
  }

  return { token, user, isLoggedIn, login, fetchUser, logout }
})
```

- [ ] **Step 2: Auth 中间件**

```typescript
// middleware/auth.ts
export default defineNuxtRouteMiddleware(() => {
  const auth = useAuthStore()
  if (!auth.isLoggedIn) return navigateTo('/login')
})
```

- [ ] **Step 3: 登录页**

```vue
<!-- pages/login.vue -->
<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
    <n-card class="w-400px">
      <h1 class="text-2xl font-bold text-center mb-6">云控系统 V2</h1>
      <n-form ref="formRef" :model="form" :rules="rules">
        <n-form-item path="username" label="用户名">
          <n-input v-model:value="form.username" placeholder="admin" />
        </n-form-item>
        <n-form-item path="password" label="密码">
          <n-input v-model:value="form.password" type="password" @keyup.enter="handleLogin" />
        </n-form-item>
        <n-button type="primary" block :loading="loading" @click="handleLogin">登录</n-button>
      </n-form>
    </n-card>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: false })

const form = reactive({ username: '', password: '' })
const loading = ref(false)
const auth = useAuthStore()

const rules = {
  username: [{ required: true, message: '请输入用户名' }],
  password: [{ required: true, message: '请输入密码' }],
}

async function handleLogin() {
  loading.value = true
  const ok = await auth.login(form.username, form.password)
  loading.value = false
  if (ok) navigateTo('/dashboard')
}
</script>
```

- [ ] **Step 4: 主布局**

```vue
<!-- layouts/default.vue -->
<template>
  <n-layout class="min-h-screen">
    <n-layout-sider bordered collapse-mode="width" :collapsed-width="64" :width="220" :collapsed="collapsed">
      <div class="h-16 flex items-center justify-center border-b font-bold text-lg">
        {{ collapsed ? '云' : '云控 V2' }}
      </div>
      <n-menu :collapsed="collapsed" :value="currentRoute" :options="menuOptions" @update:value="navigateTo" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="h-16 flex items-center justify-between px-4">
        <n-button text @click="collapsed = !collapsed">
          <Icon :component="collapsed ? MenuUnfoldOutlined : MenuFoldOutlined" />
        </n-button>
        <n-space>
          <n-button text @click="toggleTheme">
            <Icon :component="SunnyOutline" v-if="mode === 'dark'" />
            <Icon :component="MoonOutline" v-else />
          </n-button>
          <n-dropdown :options="userOptions" @select="handleUserAction">
            <n-button text>{{ auth.user?.nickname || auth.user?.username }}</n-button>
          </n-dropdown>
        </n-space>
      </n-layout-header>
      <n-layout-content class="p-4">
        <slot />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup lang="ts">
import { MenuFoldOutlined, MenuUnfoldOutlined, SunnyOutline, MoonOutline } from '@vicons/ionicons5'

definePageMeta({ middleware: 'auth' })

const collapsed = ref(false)
const auth = useAuthStore()
const { mode, toggle } = useTheme()

function toggleTheme() {
  toggle(mode.value === 'dark' ? 'light' : 'dark')
}

const menuOptions = [
  { label: '仪表盘', key: '/dashboard', icon: renderIcon('GridOutline') },
  { label: '设备管理', key: '/devices', icon: renderIcon('PhonePortraitOutline') },
  { label: '截图墙', key: '/screen/wall', icon: renderIcon('CameraOutline') },
  { label: '任务管理', key: '/tasks', icon: renderIcon('PlayOutline') },
  { label: '脚本管理', key: '/scripts', icon: renderIcon('CodeOutline') },
  { label: '系统管理', key: '/system/users', icon: renderIcon('SettingsOutline') },
]

const userOptions = [
  { label: '个人设置', key: 'profile' },
  { label: '退出登录', key: 'logout' },
]

function handleUserAction(key: string) {
  if (key === 'logout') { auth.logout(); navigateTo('/login') }
  if (key === 'profile') navigateTo('/profile')
}

const currentRoute = computed(() => useRoute().path)

// Icon render helper
function renderIcon(icon: string) {
  return () => h(resolveComponent(icon))
}
</script>
```

- [ ] **Step 5: 验证应用启动 + 登录流程**

```bash
npm run dev
# 访问 http://localhost:3000/login
# 确认可以被重定向到登录页
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add auth store, login page, and main layout with sidebar"
```

---

### Task 3.3: 设备列表页 + 设备详情页

**Files:**
- Create: `cloud-control-v2/web/pages/devices/index.vue`
- Create: `cloud-control-v2/web/pages/devices/[id].vue`
- Create: `cloud-control-v2/web/components/device/DeviceCard.vue`

- [ ] **Step 1: DeviceCard 组件**

```vue
<!-- components/device/DeviceCard.vue -->
<template>
  <n-card :bordered="true" size="small" class="cursor-pointer hover:shadow-md transition-shadow"
    @click="$emit('click')">
    <template #header>
      <div class="flex items-center justify-between">
        <span class="font-semibold truncate">{{ device.name }}</span>
        <n-badge :type="device.is_online ? 'success' : 'default'" dot processing>
          {{ device.is_online ? '在线' : '离线' }}
        </n-badge>
      </div>
    </template>
    <n-space vertical size="small">
      <div class="text-xs text-gray-500">{{ device.id }}</div>
      <div class="text-xs text-gray-500">{{ device.last_ip || '-' }}</div>
      <n-progress type="line" :percentage="device.battery || 0" :height="6" />
    </n-space>
  </n-card>
</template>

<script setup lang="ts">
defineProps<{ device: any }>()
defineEmits(['click'])
</script>
```

- [ ] **Step 2: 设备列表页**

```vue
<!-- pages/devices/index.vue -->
<template>
  <div>
    <n-space class="mb-4" align="center">
      <n-input v-model:value="keyword" placeholder="搜索设备..." clearable style="width:240px" @input="search" />
      <n-select v-model:value="statusFilter" :options="statusOpts" style="width:140px" @update:value="search" />
      <n-button @click="refresh"><Icon :component="RefreshOutline" />刷新</n-button>
      <n-button type="primary" @click="screenCapture">截屏选中</n-button>
    </n-space>

    <!-- Batch actions -->
    <n-space v-if="selected.length" class="mb-2">
      <span>已选 {{ selected.length }} 台</span>
      <n-button size="small" @click="batchDelete">删除</n-button>
      <n-button size="small" @click="batchAddGroup">加入分组</n-button>
    </n-space>

    <!-- Card Grid with virtual scroll -->
    <div class="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8 gap-3">
      <DeviceCard v-for="d in devices" :key="d.id" :device="d"
        :class="{ 'ring-2 ring-blue-500': selected.includes(d.id) }"
        @click="selectDevice(d)" />
    </div>

    <n-pagination v-if="total > size" class="mt-4" :page="page" :page-size="size"
      :item-count="total" @update:page="loadPage" />
  </div>
</template>

<script setup lang="ts">
import { RefreshOutline } from '@vicons/ionicons5'

const keyword = ref('')
const statusFilter = ref<string | null>(null)
const devices = ref<any[]>([])
const selected = ref<string[]>([])
const page = ref(1)
const size = ref(48)
const total = ref(0)

const statusOpts = [
  { label: '全部', value: null },
  { label: '在线', value: 'true' },
  { label: '离线', value: 'false' },
]

async function fetchDevices() {
  const { data } = await useFetch('/api/v1/devices', {
    params: { page: page.value, size: size.value, keyword: keyword.value, online: statusFilter.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  if (data.value?.code === 200) {
    devices.value = data.value.data || []
    total.value = data.value.total || 0
  }
}

function search() { page.value = 1; fetchDevices() }
function loadPage(p: number) { page.value = p; fetchDevices() }
function refresh() { fetchDevices() }

function selectDevice(d: any) {
  const idx = selected.value.indexOf(d.id)
  if (idx >= 0) selected.value.splice(idx, 1)
  else selected.value.push(d.id)
}

function screenCapture() {
  useFetch('/api/v1/screen/capture', {
    method: 'POST', body: { device_ids: selected.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
}

function batchDelete() {
  useFetch('/api/v1/devices/batch-delete', {
    method: 'POST', body: { ids: selected.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  }).then(() => { selected.value = []; refresh() })
}

function batchAddGroup() {
  // Group select dialog
}

onMounted(fetchDevices)
</script>
```

- [ ] **Step 3: 设备详情页**

```vue
<!-- pages/devices/[id].vue -->
<template>
  <div v-if="device">
    <n-button text @click="navigateTo('/devices')">← 返回</n-button>
    <n-space class="mt-4">
      <n-button @click="sendCommand('screenshot')">截屏</n-button>
      <n-button @click="sendCommand('reboot')">重启</n-button>
      <n-button @click="sendCommand('home')">Home</n-button>
      <n-button @click="sendCommand('back')">返回</n-button>
    </n-space>

    <n-grid :cols="3" class="mt-4" x-gap="12">
      <n-grid-item>
        <n-card title="基本信息">
          <n-descriptions :column="1">
            <n-descriptions-item label="设备ID">{{ device.id }}</n-descriptions-item>
            <n-descriptions-item label="名称">{{ device.name }}</n-descriptions-item>
            <n-descriptions-item label="状态">
              <n-badge :type="device.is_online ? 'success' : 'default'" dot :processing="device.is_online" />
              {{ device.is_online ? '在线' : '离线' }}
            </n-descriptions-item>
            <n-descriptions-item label="IP">{{ device.last_ip || '-' }}</n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-grid-item>
      <n-grid-item>
        <n-card title="最近任务">
          <n-empty v-if="!taskDevices?.length" description="无运行任务" />
          <div v-else v-for="td in taskDevices" :key="td.id">
            {{ td.task_id }} — {{ statusLabel(td.status) }}
          </div>
        </n-card>
      </n-grid-item>
    </n-grid>

    <n-tabs class="mt-4">
      <n-tab-pane name="screenshots" tab="截图">
        <div class="grid grid-cols-4 gap-2">
          <img v-for="s in screenshots" :key="s.id" :src="`/uploads/${s.file_path}`"
            class="w-full rounded cursor-pointer" @click="previewImage = s" />
        </div>
      </n-tab-pane>
      <n-tab-pane name="sms" tab="短信">
        <n-data-table :columns="smsColumns" :data="smsList" />
      </n-tab-pane>
    </n-tabs>

    <n-modal v-model:show="showPreview">
      <img :src="previewImage ? `/uploads/${previewImage.file_path}` : ''" class="max-w-90vw max-h-90vh" />
    </n-modal>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const device = ref<any>(null)
const screenshots = ref<any[]>([])
const smsList = ref<any[]>([])
const taskDevices = ref<any[]>([])

onMounted(async () => {
  const { data } = await useFetch(`/api/v1/devices/${route.params.id}`, {
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  device.value = data.value?.data
})

function sendCommand(cmd: string) {
  useFetch(`/api/v1/devices/${route.params.id}/command`, {
    method: 'POST', body: { command: cmd },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
}
</script>
```

- [ ] **Step 4: 验证编译**

```bash
npm run build  # 确认前端构建无错误
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add device list with card grid and device detail page"
```

---

### Task 3.4: 截图墙 + 任务管理 + Dashboard

**Files:**
- Create: `cloud-control-v2/web/pages/screen/wall.vue`
- Create: `cloud-control-v2/web/pages/tasks/index.vue`
- Create: `cloud-control-v2/web/pages/dashboard.vue`

- [ ] **Step 1: 截图墙页（ScreenWall）**

```vue
<!-- pages/screen/wall.vue -->
<template>
  <div>
    <n-space class="mb-4" align="center">
      <n-select v-model:value="groupId" :options="groupOpts" placeholder="选择分组" style="width:200px" @update:value="loadWall" />
      <n-button type="primary" :disabled="!selected.length" @click="batchCapture">
        <Icon :component="CameraOutline" />截屏 ({{ selected.length }})
      </n-button>
      <n-checkbox v-model:checked="autoRefresh" />自动刷新
      <n-select v-if="autoRefresh" v-model:value="refreshInterval" :options="intervalOpts" style="width:100px" />
      <span class="text-sm text-gray-500">共 {{ wallDevices.length }} 台设备</span>
    </n-space>

    <div class="grid grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8 gap-2">
      <div v-for="d in wallDevices" :key="d.id" class="relative border rounded cursor-pointer hover:ring-2 hover:ring-blue-500"
        :class="{ 'ring-2 ring-blue-500': selected.includes(d.id) }" @click="toggleSelect(d.id)">
        <img v-if="d.latest_screenshot" :src="'/uploads/' + d.latest_screenshot.file_path" class="w-full aspect-9/16 object-cover" />
        <div v-else class="w-full aspect-9/16 bg-gray-200 dark:bg-gray-700 flex items-center justify-center text-gray-400">
          暂无截图
        </div>
        <div class="absolute bottom-0 left-0 right-0 bg-black/50 text-white text-xs px-1 py-0.5 flex justify-between">
          <span class="truncate">{{ d.name }}</span>
          <n-badge :type="d.is_online ? 'success' : 'default'" dot :processing="d.is_online" />
        </div>
      </div>
    </div>

    <n-modal v-model:show="showPreview">
      <img :src="previewUrl" class="max-w-90vw max-h-90vh" />
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { CameraOutline } from '@vicons/ionicons5'

const groupId = ref<string | null>(null)
const groupOpts = ref<Array<{label:string,value:string}>>([])
const wallDevices = ref<any[]>([])
const selected = ref<string[]>([])
const autoRefresh = ref(false)
const refreshInterval = ref(30)
const showPreview = ref(false)
const previewUrl = ref('')
const intervalOpts = [
  { label: '15秒', value: 15 }, { label: '30秒', value: 30 }, { label: '60秒', value: 60 }
]

async function loadWall() {
  const { data } = await useFetch('/api/v1/screen/wall', {
    params: { group_id: groupId.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  if (data.value?.code === 200) wallDevices.value = data.value.data || []
}

function toggleSelect(id: string) {
  const idx = selected.value.indexOf(id)
  if (idx >= 0) selected.value.splice(idx, 1)
  else selected.value.push(id)
}

async function batchCapture() {
  await useFetch('/api/v1/screen/capture', {
    method: 'POST', body: { device_ids: selected.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  setTimeout(loadWall, 2000)
}

// Auto refresh
let timer: ReturnType<typeof setInterval> | null = null
watch([autoRefresh, refreshInterval], ([on, interval]) => {
  if (timer) clearInterval(timer)
  if (on) timer = setInterval(loadWall, interval * 1000)
})
onUnmounted(() => { if (timer) clearInterval(timer) })

onMounted(async () => {
  const { data } = await useFetch('/api/v1/device-groups', {
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  if (data.value?.code === 200) {
    groupOpts.value = (data.value.data || []).map((g: any) => ({ label: g.name, value: g.id }))
  }
  loadWall()
})
</script>
```

- [ ] **Step 2: 任务管理页**

```vue
<!-- pages/tasks/index.vue -->
<template>
  <div>
    <n-space class="mb-4">
      <n-button type="primary" @click="showCreate = true">创建任务</n-button>
    </n-space>

    <n-data-table :columns="columns" :data="tasks" :pagination="{ page, pageSize: size, itemCount: total }" @update:page="loadPage" />

    <n-modal v-model:show="showCreate" title="创建任务">
      <n-card style="width:600px">
        <n-form :model="form">
          <n-form-item label="任务名称" required>
            <n-input v-model:value="form.name" />
          </n-form-item>
          <n-form-item label="脚本">
            <n-select v-model:value="form.script_id" :options="scriptOpts" />
          </n-form-item>
          <n-form-item label="Cron表达式">
            <n-input v-model:value="form.cron_expr" placeholder="0 */1 * * * ?" />
          </n-form-item>
          <n-form-item label="超时(秒)">
            <n-input-number v-model:value="form.timeout_sec" :min="30" :max="3600" />
          </n-form-item>
          <n-form-item label="目标设备">
            <n-transfer v-model:value="form.device_ids" :options="deviceOpts" />
          </n-form-item>
          <n-button type="primary" block @click="createTask">创建</n-button>
        </n-form>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
const tasks = ref<any[]>([])
const page = ref(1); const size = ref(20); const total = ref(0)
const showCreate = ref(false)
const form = reactive({ name: '', script_id: null as string|null, cron_expr: '', timeout_sec: 300, device_ids: [] as string[] })
const scriptOpts = ref<any[]>([])
const deviceOpts = ref<any[]>([])

const columns = [
  { title: '名称', key: 'name' },
  { title: '脚本', key: 'script.name', render: (r: any) => r.script?.name || '-' },
  { title: 'Cron', key: 'cron_expr', render: (r: any) => r.cron_expr || '手动' },
  { title: '状态', key: 'status', render: (r: any) => r.status === 1 ? '运行中' : '已停止' },
  { title: '操作', key: 'actions', render: (r: any) => h('div', [
    h(resolveComponent('n-button'), { size:'tiny', onClick: () => startTask(r.id) }, '启动'),
    h(resolveComponent('n-button'), { size:'tiny', onClick: () => navigateTo(`/tasks/${r.id}`) }, '详情'),
  ]) },
]

async function fetchTasks() {
  const { data } = await useFetch('/api/v1/tasks', {
    params: { page: page.value, size: size.value },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  if (data.value?.code === 200) { tasks.value = data.value.data; total.value = data.value.total }
}

function loadPage(p: number) { page.value = p; fetchTasks() }

async function createTask() {
  await useFetch('/api/v1/tasks', {
    method: 'POST', body: { ...form },
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  showCreate.value = false; fetchTasks()
}

async function startTask(id: string) {
  await useFetch(`/api/v1/tasks/${id}/start`, {
    method: 'POST', headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  fetchTasks()
}

onMounted(async () => {
  fetchTasks()
  const [scriptsRes, devicesRes] = await Promise.all([
    useFetch('/api/v1/scripts', { headers: { Authorization: `Bearer ${useAuthStore().token}` } }),
    useFetch('/api/v1/devices?size=500', { headers: { Authorization: `Bearer ${useAuthStore().token}` } }),
  ])
  scriptOpts.value = (scriptsRes.data.value?.data || []).map((s:any) => ({ label: s.name, value: s.id }))
  deviceOpts.value = (devicesRes.data.value?.data || []).map((d:any) => ({ label: d.name, value: d.id }))
})
</script>
```

- [ ] **Step 3: Dashboard 页**

```vue
<!-- pages/dashboard.vue -->
<template>
  <div>
    <n-grid :cols="4" x-gap="12" class="mb-4">
      <n-grid-item v-for="stat in stats" :key="stat.label">
        <n-card size="small">
          <n-statistic :label="stat.label">
            <n-number-animation :from="0" :to="stat.value" />
          </n-statistic>
        </n-card>
      </n-grid-item>
    </n-grid>

    <n-grid :cols="2" x-gap="12">
      <n-grid-item>
        <n-card title="任务执行趋势">
          <v-chart :option="trendOption" style="height:300px" autoresize />
        </n-card>
      </n-grid-item>
      <n-grid-item>
        <n-card title="设备状态">
          <v-chart :option="pieOption" style="height:300px" autoresize />
        </n-card>
      </n-grid-item>
    </n-grid>

    <n-grid :cols="2" x-gap="12" class="mt-4">
      <n-grid-item>
        <n-card title="最近任务日志" size="small">
          <n-timeline>
            <n-timeline-item v-for="log in recentLogs" :key="log.id" :type="log.level === 'error' ? 'error' : 'info'"
              :content="log.message" :time="new Date(log.created_at).toLocaleTimeString()" />
          </n-timeline>
        </n-card>
      </n-grid-item>
      <n-grid-item>
        <n-card title="离线告警" size="small">
          <n-list>
            <n-list-item v-for="alert in alerts" :key="alert.id">
              {{ alert.name }} — 离线 {{ alert.offline_duration }}
            </n-list-item>
            <n-empty v-if="!alerts.length" description="无告警" />
          </n-list>
        </n-card>
      </n-grid-item>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, PieChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'

use([CanvasRenderer, LineChart, PieChart, GridComponent, TooltipComponent, LegendComponent])

const stats = ref([
  { label: '在线设备', value: 0 },
  { label: '离线设备', value: 0 },
  { label: '运行任务', value: 0 },
  { label: '今日执行', value: 0 },
])
const recentLogs = ref<any[]>([])
const alerts = ref<any[]>([])

const trendOption = computed(() => ({
  xAxis: { type: 'category', data: trendData.value.map((d: any) => d.time) },
  yAxis: { type: 'value' },
  series: [{ data: trendData.value.map((d: any) => d.count), type: 'line', smooth: true, areaStyle: { opacity: 0.15 } }],
  tooltip: { trigger: 'axis' },
  grid: { left: 40, right: 20, top: 20, bottom: 30 },
}))

const pieOption = computed(() => ({
  series: [{
    type: 'pie', radius: ['40%', '70%'],
    data: [
      { value: stats.value[0].value, name: '在线', itemStyle: { color: '#18a058' } },
      { value: stats.value[1].value, name: '离线', itemStyle: { color: '#999' } },
    ],
  }],
  tooltip: { trigger: 'item' },
}))

const trendData = ref<Array<{time:string,count:number}>>([])

async function loadDashboard() {
  const { data } = await useFetch('/api/v1/dashboard', {
    headers: { Authorization: `Bearer ${useAuthStore().token}` },
  })
  if (data.value?.code === 200) {
    const d = data.value.data
    stats.value[0].value = d.online || 0
    stats.value[1].value = d.offline || 0
    stats.value[2].value = d.running || 0
    stats.value[3].value = d.today_executed || 0
  }
}

onMounted(loadDashboard)
</script>
```

- [ ] **Step 4: 验证前端构建**

```bash
npm run build
# Expected: 构建成功，无错误
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add screen wall, task management, and dashboard pages"
```

---

## Phase 4: 集成、部署与测试

### Task 4.1: go:embed 前端 + 编译

**Files:**
- Modify: `cloud-control-v2/server/cmd/cloud-server/main.go`
- Create: `cloud-control-v2/server/embed/embed.go`

- [ ] **Step 1: 前端构建产物嵌入**

```go
// embed/embed.go
package embed

import "embed"

//go:embed public/*
var Frontend embed.FS
```

构建前需要 `cd web && npm run build && cp -r .output/public ../server/embed/public`

- [ ] **Step 2: 更新 router.go 静态文件服务**

```go
import (
    "io/fs"
    embed "cloud-control-v2/server/embed"
)

// 在 router 中添加:
publicFS, _ := fs.Sub(embed.Frontend, "public")
r.Use(static.Serve("/", static.EmbedFolder(publicFS, "public")))
```

- [ ] **Step 3: build script**

```bash
# scripts/build.sh (or build.bat)
cd web && npm run build
cd ../server
go build -o cloud-server.exe ./cmd/cloud-server/
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: embed frontend into Go binary"
```

---

### Task 4.2: Docker Compose + PostgreSQL 配置

**Files:**
- Create: `cloud-control-v2/docker-compose.yml`
- Create: `cloud-control-v2/server/Dockerfile`

- [ ] **Step 1: Dockerfile**

```dockerfile
# server/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o cloud-server ./cmd/cloud-server/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/cloud-server .
EXPOSE 8080 1883 1882
CMD ["./cloud-server"]
```

- [ ] **Step 2: docker-compose.yml**

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: cloud_control
      POSTGRES_USER: cloud
      POSTGRES_PASSWORD: ${PG_PASSWORD:-cloud123}
    volumes:
      - pg_data:/var/lib/postgresql/data
      - ./init.sql:/docker-entrypoint-initdb.d/init.sql
    deploy:
      resources:
        limits: { memory: 1.5G }

  server:
    build: ./server
    ports:
      - "8080:8080"
      - "1883:1883"
      - "1882:1882"
    environment:
      CLOUD_DB_HOST: postgres
      CLOUD_DB_PORT: 5432
      CLOUD_DB_USER: cloud
      CLOUD_DB_PASSWORD: ${PG_PASSWORD:-cloud123}
      CLOUD_DB_NAME: cloud_control
      CLOUD_JWT_SECRET: ${JWT_SECRET:-change-me}
      CLOUD_MQTT_SECRET: ${MQTT_SECRET:-change-me}
    depends_on:
      - postgres
    volumes:
      - ./uploads:/app/uploads

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf
    depends_on:
      - server

volumes:
  pg_data:
```

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: add Docker Compose deployment with PostgreSQL"
```

---

### Task 4.3: PostgreSQL 调优脚本

**Files:**
- Create: `cloud-control-v2/deploy/pg_tune.sql`

- [ ] **Step 1: PG 调优 SQL**

```sql
-- 4H4G PostgreSQL tuning
ALTER SYSTEM SET shared_buffers = '256MB';
ALTER SYSTEM SET effective_cache_size = '1GB';
ALTER SYSTEM SET work_mem = '4MB';
ALTER SYSTEM SET maintenance_work_mem = '64MB';
ALTER SYSTEM SET max_connections = 30;
ALTER SYSTEM SET max_wal_size = '1GB';
ALTER SYSTEM SET checkpoint_completion_target = 0.9;
ALTER SYSTEM SET wal_buffers = '16MB';
ALTER SYSTEM SET default_statistics_target = 100;
ALTER SYSTEM SET random_page_cost = 1.1;  -- SSD
ALTER SYSTEM SET effective_io_concurrency = 200;
SELECT pg_reload_conf();
```

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: add PostgreSQL tuning for 4H4G deployment"
```

---

### Task 4.4: 压力测试工具

**Files:**
- Create: `cloud-control-v2/tools/stress_test.go`

- [ ] **Step 1: MQTT 设备模拟器**

```go
// tools/stress_test.go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "math/rand"
    "sync"
    "time"
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
    server   = flag.String("server", "tcp://localhost:1883", "MQTT broker URL")
    count    = flag.Int("count", 300, "Number of simulated devices")
    secret   = flag.String("secret", "change-me-in-production", "HMAC secret")
)

func main() {
    flag.Parse()
    var wg sync.WaitGroup
    log.Printf("Starting %d simulated devices...", *count)

    for i := 0; i < *count; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            deviceID := fmt.Sprintf("EC-test-%04d", idx)
            password := hmacSHA256(deviceID, *secret)

            opts := mqtt.NewClientOptions().
                AddBroker(*server).
                SetClientID("ec-" + deviceID).
                SetUsername(deviceID).
                SetPassword(password).
                SetKeepAlive(60 * time.Second).
                SetAutoReconnect(true)

            client := mqtt.NewClient(opts)
            if token := client.Connect(); token.Wait() && token.Error() != nil {
                log.Printf("Device %s connect failed: %v", deviceID, token.Error())
                return
            }
            defer client.Disconnect(250)

            // Register
            register := map[string]interface{}{
                "name": fmt.Sprintf("Test-%04d", idx),
                "model": "Simulator", "os_version": "12",
                "client_type": "ec", "ip": "192.168.1." + fmt.Sprint(idx%255),
            }
            payload, _ := json.Marshal(register)
            client.Publish("cloud/"+deviceID+"/register", 1, false, payload)

            // Heartbeat loop
            ticker := time.NewTicker(30 * time.Second)
            for range ticker.C {
                hb := map[string]interface{}{"ts": time.Now().UnixMilli()}
                payload, _ := json.Marshal(hb)
                client.Publish("cloud/"+deviceID+"/heartbeat", 0, false, payload)
            }
        }(i)
    }
    wg.Wait()
}

func hmacSHA256(data, secret string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(data))
    return hex.EncodeToString(mac.Sum(nil))
}
```

- [ ] **Step 2: 安装依赖**

```bash
cd tools && go mod init stress-test
go get github.com/eclipse/paho.mqtt.golang
```

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: add MQTT device simulator for stress testing"
```

---

### Task 4.5: 最终集成测试与 README

**Files:**
- Create: `cloud-control-v2/README.md`

- [ ] **Step 1: README**

````markdown
# 云控系统 V2

基于 Go + MQTT + PostgreSQL 的通用 Android 设备云控平台。

## 快速启动

### Docker Compose

```bash
docker-compose up -d
```

### Windows

```bash
# 1. 启动 PostgreSQL
# 2. 复制 config.example.json 为 config.json，修改数据库连接
# 3. 启动服务
cloud-server.exe
# 4. 浏览器打开 http://localhost:8080
```

## 默认账户

- 用户名: `admin`
- 密码: `admin1234`

## 架构

- 后端: Go 1.22 + Gin + mochi-mqtt + GORM + pgx
- 前端: Nuxt 3 + Naive UI + ECharts
- 协议: MQTT (TCP :1883, WS :1882) + HTTP REST

## 压力测试

```bash
cd tools
go run stress_test.go -server tcp://localhost:1883 -count 300 -secret your-secret
```
````

- [ ] **Step 2: 全链路测试清单**

```bash
# 1. 启动 PG + 服务
docker-compose up -d

# 2. 登录
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin1234"}'

# 3. 模拟 300 设备连接
go run tools/stress_test.go -count 300

# 4. 查询在线设备
curl http://localhost:8080/api/v1/ws/devices-realtime \
  -H 'Authorization: Bearer <token>'

# 5. 验证心跳批量写入（观察 PG 日志，应看到批量 UPSERT）
```

- [ ] **Step 3: Final Commit**

```bash
git add -A && git commit -m "docs: add README and integration test checklist"
```

---

## 实施顺序总结

| 顺序 | Phase / Task | 预计耗时 |
|------|-------------|---------|
| 1 | 1.1-1.3 Go 骨架 + DB | 30 min |
| 2 | 1.4 MQTT Broker | 15 min |
| 3 | 1.5 HTTP API + 中间件 | 20 min |
| 4 | 1.6 Admin 初始化 | 10 min |
| 5 | 2.1 事件总线 + 削峰限流 | 20 min |
| 6 | 2.2 设备服务 | 25 min |
| 7 | 2.3 任务引擎 + Cron | 20 min |
| 8 | 2.4 MQTT 消息路由 | 15 min |
| 9 | 2.5 HTTP API 完整实现 | 30 min |
| 10 | 2.6 WebSocket 桥接 | 15 min |
| 11 | 2.7 后台清理任务 | 10 min |
| 12 | 3.1 Nuxt 初始化 + 主题 | 20 min |
| 13 | 3.2 布局 + 认证 | 25 min |
| 14 | 3.3 设备页 | 30 min |
| 15 | 3.4 截图墙 + 任务 + Dashboard | 40 min |
| 16 | 4.1 Go embed 前端 | 15 min |
| 17 | 4.2 Docker Compose | 15 min |
| 18 | 4.3 PG 调优 | 5 min |
| 19 | 4.4 压力测试 | 15 min |
| 20 | 4.5 文档 | 10 min |
| **合计** | | **~5.5 hours** |
