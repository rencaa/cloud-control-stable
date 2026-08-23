# 云控框架 V2 重建设计文档

> 日期: 2026-07-26  
> 目标: 支持 200-300 台 Android 设备的云控平台重写  
> 服务器规格: 4H4G  
> 客户端: EasyClick + HID 双模式

---

## 1. 需求概述

### 1.1 背景
现有云控框架（v72）基于 Go + Gin + SQLite + WebSocket，已在生产环境运行。但在扩展到 200-300 台设备时存在瓶颈：
- SQLite 单写者锁限制高并发写入
- WebSocket 缺乏 QoS 保证，消息可能丢失
- 无消息削峰机制，心跳洪峰冲击数据库
- 无连接限流，网络抖动后重连风暴
- FrameCache 硬上限 200 设备
- 前端 ScreenWall 无虚拟滚动，大量 DOM 节点性能差

### 1.2 目标
重写后端架构，适配 200-300 台设备，保持 Android 客户端脚本（666 文件夹）兼容，前端同步重写以提升性能和 UI。

### 1.3 关键决策
| 决策项 | 选择 |
|--------|------|
| 通信协议 | 嵌入式 MQTT Broker（mochi-co/mqtt） |
| 数据库 | PostgreSQL + pgx 连接池 |
| 后端语言 | Go 1.22 |
| 前端框架 | Nuxt 3 + Naive UI + UnoCSS |
| 部署模式 | 双模式：Windows 单 EXE + Docker Compose |
| 消息格式 | Protobuf（精简）+ JSON（兼容） |
| 客户端 | 666 脚本保留，适配 MQTT 协议 |
| 帧流 | 不做实时帧，改为按需截图 |
| 主题模式 | 自动跟随系统 + 手动切换 |

---

## 2. 整体架构

### 2.1 架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                        4H4G 服务器                                │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    Go Binary (单进程)                         │ │
│  │                                                              │ │
│  │  ┌────────────┐  ┌──────────────────┐  ┌─────────────────┐  │ │
│  │  │  MQTT      │  │   HTTP/WS API    │  │  Nuxt 3 静态资源 │  │ │
│  │  │  Server    │  │   (Gin Router)   │  │  (embed.FS)     │  │ │
│  │  │  :1883     │  │   :8080          │  │                 │  │ │
│  │  └─────┬──────┘  └────────┬─────────┘  └─────────────────┘  │ │
│  │        │                  │                                   │ │
│  │        │    ┌─────────────┴──────────────┐                   │ │
│  │        │    │      中间件链               │                   │ │
│  │        │    │  JWT · RBAC · 限流 · 日志  │                   │ │
│  │        │    └─────────────┬──────────────┘                   │ │
│  │        │                  │                                   │ │
│  │  ┌─────┴──────────────────┴──────────────────────────────┐  │ │
│  │  │              领域服务层 (Domain Services)              │  │ │
│  │  │                                                       │  │ │
│  │  │  DeviceSvc │ TaskSvc │ ScriptSvc │ ScreenSvc │ SmsSvc │  │ │
│  │  │  AuthSvc   │ DashboardSvc │ TemplateSvc │ DataSvc    │  │ │
│  │  │                                                       │  │ │
│  │  │  ┌──────────────────────────────────────────────────┐ │  │ │
│  │  │  │         基础设施层 (Infrastructure)               │ │  │ │
│  │  │  │  RateLimiter · MsgBatcher · EventBus · Cron      │ │  │ │
│  │  │  │  ScreenshotStore · MetricsCollector               │ │  │ │
│  │  │  └──────────────────┬───────────────────────────────┘ │  │ │
│  │  │                     │                                 │  │ │
│  │  │           ┌─────────┴─────────┐                       │  │ │
│  │  │           │  Repository 层    │                       │  │ │
│  │  │           │  (GORM + pgx)     │                       │  │ │
│  │  │           └───────────────────┘                       │  │ │
│  │  └──────────────────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────┘
```

### 2.2 目录结构

```
server/
├── cmd/
│   └── cloud-server/
│       └── main.go              # 入口：组装依赖、启动服务
├── internal/
│   ├── mqtt/                    # MQTT Broker 包装层
│   │   ├── broker.go            # mochi-mqtt 启动与管理
│   │   └── hooks.go             # Auth Hook + 消息路由 Hook
│   ├── api/                     # HTTP API 层
│   │   ├── router.go            # Gin 路由注册
│   │   └── middleware/
│   │       ├── jwt.go
│   │       ├── rbac.go
│   │       ├── ratelimit.go
│   │       └── logger.go
│   ├── domain/                  # 领域服务
│   │   ├── device/
│   │   │   ├── service.go       # 设备注册、心跳、状态管理
│   │   │   └── model.go
│   │   ├── task/
│   │   │   ├── service.go       # 任务 CRUD + 分发引擎
│   │   │   ├── scheduler.go     # Cron 定时调度
│   │   │   └── model.go
│   │   ├── script/
│   │   │   ├── service.go       # 脚本管理 + 参数合并
│   │   │   └── model.go
│   │   ├── screen/
│   │   │   ├── service.go       # 截图指令 + 存储管理
│   │   │   └── model.go
│   │   ├── sms/
│   │   │   └── service.go       # 短信/联系人采集
│   │   ├── auth/
│   │   │   ├── service.go       # 登录/注册/RBAC
│   │   │   └── model.go
│   │   ├── dashboard/
│   │   │   └── service.go       # 统计聚合
│   │   ├── template/
│   │   │   └── service.go       # 参数模板 + 数据模板
│   │   └── data/
│   │       └── service.go       # 数据记录管理
│   ├── infra/                   # 基础设施
│   │   ├── ratelimit/
│   │   │   └── limiter.go       # TokenBucket 连接限流
│   │   ├── batcher/
│   │   │   └── batch_writer.go  # 消息削峰批量写入
│   │   ├── eventbus/
│   │   │   └── bus.go           # 内部事件总线
│   │   └── metrics/
│   │       └── collector.go     # 运行时指标收集
│   └── repo/                    # 数据访问层
│       ├── device_repo.go
│       ├── task_repo.go
│       ├── script_repo.go
│       ├── screen_repo.go
│       ├── sms_repo.go
│       ├── auth_repo.go
│       └── dashboard_repo.go
├── config/
│   └── config.go                # 配置定义与加载
├── proto/                       # Protobuf 定义
│   └── cloud.proto
└── embed/                       # 前端嵌入（go:embed）

web/                             # Nuxt 3 前端
├── nuxt.config.ts
├── app.vue
├── pages/
│   ├── login.vue
│   ├── dashboard.vue
│   ├── dashboard/fullscreen.vue
│   ├── devices/
│   │   ├── index.vue
│   │   ├── [id].vue
│   │   ├── groups.vue
│   │   └── logs.vue
│   ├── screen/wall.vue
│   ├── tasks/
│   │   ├── index.vue
│   │   └── logs.vue
│   ├── scripts/index.vue
│   ├── data/
│   ├── system/
│   └── profile.vue
├── components/
│   ├── device/
│   │   ├── DeviceCard.vue
│   │   ├── DeviceTable.vue
│   │   └── OnlineBadge.vue
│   ├── screen/
│   │   ├── ScreenGrid.vue
│   │   └── ScreenCell.vue
│   ├── task/
│   │   ├── TaskForm.vue
│   │   └── TaskProgress.vue
│   └── common/
│       ├── AppLayout.vue
│       ├── SearchBar.vue
│       └── BatchActions.vue
├── composables/
│   ├── useDeviceList.ts
│   ├── useScreenWall.ts
│   ├── useAuth.ts
│   └── useWebSocket.ts
├── stores/
│   ├── auth.ts
│   └── devices.ts
├── server/api/proxy/[...].ts
└── public/config.json
```

### 2.3 4H4G 资源分配

| 组件 | 内存预算 | 说明 |
|------|---------|------|
| Go 服务 + MQTT | ~1.5 GB | goroutine 300连接~30MB，MQTT订阅树~100MB，截图缓冲~50MB，批量写入缓冲~100MB，其余业务~1.2GB |
| PostgreSQL | ~1.5 GB | `shared_buffers=256MB`, `work_mem=4MB`, `max_connections=30`, `effective_cache_size=1GB` |
| 系统/缓冲 | ~1 GB | OS 文件缓存、网络缓冲 |
| **合计** | **~4 GB** | |

---

## 3. MQTT 协议设计

### 3.1 Topic 树

```
cloud/{device_id}/
├── register          ← 设备→服务  注册 (QoS 1, Retained)
├── heartbeat         ← 设备→服务  心跳 (QoS 0)
├── status            ← 设备→服务  任务状态/日志 (QoS 1)
├── screenshot        ← 设备→服务  截图 (QoS 1)
├── sms               ← 设备→服务  短信上报 (QoS 1)
├── contacts          ← 设备→服务  联系人上报 (QoS 1)
├── task              → 服务→设备  推送任务 (QoS 1)
├── command           → 服务→设备  控制指令 (QoS 1)
└── config            → 服务→设备  配置更新 (QoS 1, Retained)
```

### 3.2 连接认证

```
Client ID  =  "{client_type}-{device_id}"    // ec-xxx 或 hid-xxx
Username   =  device_id
Password   =  HMAC-SHA256(device_id, server_secret)
Will Topic =  "cloud/{device_id}/status"
Will Msg   =  {"online": false, "ts": <unix_ms>}
Keep Alive =  60s
```

MQTT OnConnect Hook 验证 HMAC 密码，通过则注册连接，拒绝则返回 CONNACK_REFUSED。

### 3.3 消息格式

双格式支持，服务端同时解析 Protobuf 和 JSON：

- **Protobuf（推荐）**：新 HID 客户端使用，心跳 ~40B，比 JSON ~200B 节省 5x
- **JSON（兼容）**：EC 客户端过渡期使用，格式与现有协议兼容

服务端 MQTT Hook 根据消息 Content-Type 自动选择解析器。两种格式最终映射到相同的内部结构体。

```protobuf
message CloudMessage {
  string msg_id = 1;       // UUID v7，幂等去重
  int64  ts     = 2;       // 毫秒时间戳
  oneof payload {
    Heartbeat    heartbeat    = 10;
    TaskPush     task_push    = 11;
    TaskStatus   task_status  = 12;
    Command      command      = 13;
    Screenshot   screenshot   = 14;
    SmsReport    sms_report   = 15;
    ConfigUpdate config_update= 16;
    Register     register     = 17;
    LogEntry     log_entry    = 18;
  }
}
```

### 3.4 MQTT Hook 路由

| Hook | 逻辑 |
|------|------|
| OnConnect | 验证 HMAC → 注册 ClientMap → 踢旧连接 → 发遗嘱 |
| OnPublished (订阅 `cloud/#`) | 按 topic 路由到对应 Domain Service |
| OnDisconnect | 标记离线 → 清理会话 → EventBus.Publish("device.offline") |

### 3.5 离线消息队列

MQTT Session Persistence 天然支持。设备离线期间，服务端对 `cloud/{id}/task` 和 `cloud/{id}/command` 的 Publish 自动排队。设备重连后自动投递，无需任务调度器额外重试。

---

## 4. 消息削峰与连接限流

### 4.1 问题分析

300 台设备产生三类流量尖刺：
- **连接风暴**：网络抖动后 300 台同时重连
- **心跳洪峰**：每 30s 一波 300 条同时到达
- **任务回执**：批量下发后 300 条状态同时回来

### 4.2 连接限流（ConnectLimiter）

```
全局 TokenBucket:
  - 容量 500（最大并发连接）
  - 速率 100/s
  - 拿不到 token → CONNACK_REFUSED → 客户端 1-5s 随机退避

同 device_id 策略:
  - 新连接踢旧连接，发遗嘱消息
  - 保证同一设备只有一个在线会话

单 IP 限制:
  - sync.Map[ip]*rateLimiter
  - 20 conn/s
```

### 4.3 消息削峰流水线（BatchWriter）

```
┌──────────────────────────────────────────────────┐
│                                                  │
│  消息到达 → 按类型分 RingBuffer → 定时/定量 flush│
│                                                  │
│  优先级:                                         │
│  🔴 P0 立即  : task_status, register, command_ack│
│               (不缓冲，直接处理)                  │
│  🟡 P1 快批  : sms, screenshot, contacts         │
│               (500ms 或 10条 flush)              │
│  🟢 P2 慢批  : heartbeat, log                   │
│               (2s 或 50条 flush)                 │
│                                                  │
│  削峰效果: 300 条 → 6 条 SQL（降 ~50 倍）        │
│  延迟代价: 心跳 max 2s, 紧急 0ms                 │
└──────────────────────────────────────────────────┘
```

### 4.4 反压机制

```
Buffer 80% 满:
  → 暂停接收 P2 消息 (MQTT PUBACK 失败，设备降速)
  → 加速 flush P1 (缩到 200ms)
  → P0 不受影响
  → Buffer 降到 50% 时恢复
```

### 4.5 任务指令实时通道

任务下发是 P0 优先级，服务端直接 MQTT Publish→设备立即收到（QoS 1），不经任何缓冲层。

### 4.6 PostgreSQL 连接池

```go
pgxpool.Config{
    MaxConns:          30,    // 4H4G 安全值
    MinConns:          5,
    MaxConnLifetime:   30 * time.Minute,
    MaxConnIdleTime:   5 * time.Minute,
    HealthCheckPeriod: 30 * time.Second,
}

// 30 连接分配:
//  8  → API 请求 (Gin handler)
// 12  → MQTT 消息处理 (批量写入)
//  5  → 后台任务 (cleanup, cron)
//  5  → 预留 buffer
```

---

## 5. 核心领域服务

### 5.1 设备管理服务 (DeviceSvc)

**注册流程：**
1. 设备 MQTT Connect（HMAC 验证）
2. 发送 register 消息 {device_info}
3. DeviceSvc.Register() → Upsert 设备记录 + 标记 online
4. 返回 register ack

**心跳流程：**
1. 设备每 30s 发送 heartbeat (QoS 0)
2. RingBuffer.Push(device_id, ts)
3. Batcher 每 2s flush → 批量 UPSERT 所有 dirty 设备的 last_heartbeat
4. 独立 goroutine 每 10s 检查：last_heartbeat > 60s → 标记 offline → EventBus 通知

**在线设备查询：**
- 维护内存 `sync.Map[device_id]*DeviceOnline`，记录心跳时间
- HTTP API 直接读内存 + DB fallback，毫秒级响应

### 5.2 任务引擎 (TaskSvc)

**任务生命周期：**
```
pending → running → success
                  → failed  (设备返回失败)
                  → timeout (可配置，默认 5min)
```

**参数合并：** device.params > task.params > script.default_params（三级覆盖）

**任务分发：**
1. StartTask → 查询关联设备列表
2. 对每个设备：MQTT Publish(`cloud/{id}/task`, TaskPush{QoS 1})
3. 设备离线 → MQTT Session 自动排队，重连后投递
4. 设备执行 → 通过 task_status topic 回报状态
5. 服务端更新 task_devices 表 → EventBus.Publish("task.updated")

**Cron 调度：**
- robfig/cron 库，每个 cron job 独立 goroutine
- 到点自动 StartTask(全部关联设备)
- 支持暂停/恢复/删除 cron job

**批量控制：**
- 批量启动/停止/重置任务
- 并发 MQTT Publish，不阻塞

### 5.3 屏幕服务 (ScreenSvc)

**不做实时帧流。** 改为按需截图模式：

**截图触发：**
- 用户在前端点击"截屏"（单设备或批量）
- 自动刷新模式：定时（可配 15s/30s/60s）批量截屏

**流程：**
1. HTTP API → ScreenSvc.Capture(device_ids)
2. 对每个设备：MQTT Publish(`cloud/{id}/command`, {command: "screenshot"})
3. 设备截图 → MQTT Publish(`cloud/{id}/screenshot`, {jpeg_base64})
4. 服务端：解码 → 存文件 `uploads/screenshots/{id}_{ts}.jpg` → 写 DB 记录
5. 每小时清理 >24h 的截图文件

**截图墙 API：**
- `GET /api/v1/screen/wall?group=X` → 返回设备列表 + 最新截图缩略图 URL
- 前端点击缩略图 → 大图弹窗

### 5.4 日志采集

```
设备日志 (QoS 0)
    → RingBuffer (每设备)
    → LogBatcher 每 5s 或 500 条 flush
    → PostgreSQL COPY 协议批量 INSERT
    → 按天分区表，>30 天自动 DROP PARTITION
```

### 5.5 短信/联系人采集

- 设备主动上报 (QoS 1)，服务端去重（发送者+内容+设备ID）
- 支持 Webhook 转发到外部系统
- HTTP API 支持分页查询、导出

---

## 6. 数据库设计

### 6.1 核心表

```sql
-- 设备表
CREATE TABLE devices (
    id            VARCHAR(128) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    client_type   VARCHAR(16) NOT NULL,        -- 'ec' | 'hid'
    group_id      UUID REFERENCES device_groups(id),
    is_online     BOOLEAN DEFAULT FALSE,
    last_heartbeat TIMESTAMPTZ,
    last_ip       INET,
    device_info   JSONB DEFAULT '{}',
    params        JSONB DEFAULT '{}',
    metadata      JSONB DEFAULT '{}',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_devices_online ON devices(is_online) WHERE is_online = TRUE;
CREATE INDEX idx_devices_group ON devices(group_id);

-- 任务设备关联
CREATE TABLE task_devices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    device_id     VARCHAR(128) NOT NULL REFERENCES devices(id),
    status        SMALLINT DEFAULT 0,          -- 0=pending 1=running 2=success 3=failed 4=timeout
    retry_count   SMALLINT DEFAULT 0,
    max_retries   SMALLINT DEFAULT 0,
    timeout_sec   INT DEFAULT 300,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    result        JSONB,
    error_msg     TEXT,
    UNIQUE(task_id, device_id)
);

-- 设备指标（按月分区）
CREATE TABLE device_metrics (
    device_id     VARCHAR(128) NOT NULL,
    ts            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cpu           SMALLINT,
    mem           SMALLINT,
    battery       SMALLINT,
    network_type  VARCHAR(16),
    extra         JSONB
) PARTITION BY RANGE (ts);

-- 设备日志（按天分区）
CREATE TABLE device_logs (
    id            BIGSERIAL,
    device_id     VARCHAR(128) NOT NULL,
    ts            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    level         VARCHAR(8),
    message       TEXT,
    task_id       UUID
) PARTITION BY RANGE (ts);

-- 截图记录
CREATE TABLE screenshots (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     VARCHAR(128) NOT NULL,
    file_path     VARCHAR(512) NOT NULL,
    file_size     INT,
    width         SMALLINT,
    height        SMALLINT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_screenshots_device ON screenshots(device_id, created_at DESC);

-- RBAC 表（保留现有结构）
-- users, roles, permissions, user_roles, role_permissions
-- 默认 admin:admin1234, 3 角色, 25 权限

-- 其余表（沿用现有 schema，适配 PostgreSQL 类型）:
-- device_groups, scripts, script_shares, tasks, task_logs, task_shares,
-- resources, resource_shares, parameter_templates, data_templates,
-- data_records, data_permissions, system_logs, system_configs,
-- device_sms, device_contacts
```

### 6.2 Schema 变更要点

| 变更 | 原因 |
|------|------|
| PG 分区表 (metrics/logs) | 300 设备高频写入 + 自动清理，避免 DELETE 巨量数据 |
| JSONB 替代 TEXT | 参数模板/动态字段可索引查询 |
| INET 类型 | IP 原生支持，方便限流查询 |
| 设备级 timeout/retry | 单设备粒度，灵活调度 |
| UNIQUE(task_id, device_id) | 防重复下发 |
| 部分索引 WHERE is_online | 在线设备查询毫秒级 |
| COPY 协议批量写日志 | 比逐条 INSERT 快 10x+ |

---

## 7. 前端设计

### 7.1 技术栈

| 层 | 选型 | 说明 |
|---|------|------|
| 框架 | Nuxt 3 | SSR/SSG 混合、自动代码分割、文件路由 |
| UI 库 | Naive UI | 轻量 Tree-shaking、设计现代、暗色模式内置 |
| 图表 | ECharts 5 + vue-echarts | Dashboard 大屏 |
| 代码编辑器 | Monaco Editor | 脚本编辑 |
| 状态管理 | Pinia | Nuxt 3 原生集成 |
| CSS | UnoCSS | 按需原子化 CSS |
| 主题 | Naive UI 暗色模式 + 手动/自动切换 | 跟随系统 + 手动覆盖 |

### 7.2 核心页面布局

**设备列表（默认卡片网格模式 + 可切换表格模式）：**
- 搜索栏 + 分组筛选 + 状态筛选
- 虚拟滚动卡片网格（300 设备 < 500ms 首渲）
- 每卡片：设备名、在线呼吸灯、当前任务、电量环、IP
- 批量操作栏：启动任务、加入分组、重置

**截图墙：**
- 分组选择 + 全选/截屏/停止 + 自动刷新配置
- 网格展示设备最新截图 + 时间戳
- 点击放大 + 连续截屏模式
- 截屏中状态指示

**Dashboard：**
- 在线/离线/运行中/今日执行 四卡片
- 任务执行趋势折线图 + 设备状态饼图
- 最近任务日志实时列表
- 设备告警/离线列表

**设备详情页：**
- 基本信息 + 当前任务 + 实时指标 三卡片
- 截图/短信/联系人/日志 Tab 切换
- 快捷操作：截屏、重启、启动/停止任务

**主题切换：**
- Naive UI `darkTheme` + `useDark()` composable
- localStorage 存储用户偏好（light/dark/auto）
- Dashboard 图表配色跟随主题切换

### 7.3 性能目标

| 页面 | 指标 | 目标 |
|------|------|------|
| 设备列表 | 300 卡片首渲 | < 500ms |
| 设备列表 | 搜索过滤 | < 100ms |
| 截图墙 | 300 设备视图加载 | < 1s |
| 截图墙 | 批量截屏触发到首张返回 | < 3s |
| Dashboard | ECharts 渲染 | < 1s |

---

## 8. 部署方案

### 8.1 Windows 单 EXE 部署

```
cloud-server.exe
├── (嵌入前端静态资源 via go:embed)
├── config.json          # 运行时配置
├── uploads/             # 截图存储
└── 依赖: PostgreSQL (本地或同网段)
```

System tray 保留：左键打开浏览器，右键菜单。

### 8.2 Docker Compose 部署

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: cloud_control
      POSTGRES_USER: cloud
      POSTGRES_PASSWORD: ${PG_PASSWORD}
    volumes:
      - pg_data:/var/lib/postgresql/data
    deploy:
      resources:
        limits: { memory: 1.5G }

  server:
    build: ./server
    ports:
      - "8080:8080"
      - "1883:1883"
    environment:
      DB_HOST: postgres
      DB_PORT: 5432
    depends_on: [postgres]
    volumes:
      - ./uploads:/app/uploads
      - ./config.json:/app/config.json

  nginx:  # 可选
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf
    depends_on: [server]
```

### 8.3 开发/生产切换

配置中 `db.host` 和 `db.port` 区分：
- 开发：`localhost:5432`（同机 PG）
- 生产：独立 PG 地址

---

## 9. 关键依赖

### Go 模块

```
go 1.22

require (
    github.com/gin-gonic/gin           // HTTP 框架
    github.com/mochi-co/mqtt/v2        // 嵌入式 MQTT Broker
    github.com/jackc/pgx/v5            // PostgreSQL 驱动
    gorm.io/gorm                       // ORM
    gorm.io/driver/postgres            // GORM PG 方言
    github.com/golang-jwt/jwt/v5       // JWT
    golang.org/x/crypto                // bcrypt
    github.com/robfig/cron/v3          // Cron 调度
    google.golang.org/protobuf         // Protobuf
    github.com/google/uuid             // UUID v7
)
```

### 前端模块

```json
{
  "dependencies": {
    "nuxt": "^3.x",
    "naive-ui": "^2.x",
    "@vicons/ionicons5": "^0.x",
    "pinia": "^2.x",
    "echarts": "^5.x",
    "vue-echarts": "^6.x",
    "monaco-editor": "^0.x",
    "unocss": "^0.x"
  }
}
```

---

## 10. 与现有系统的兼容

### 10.1 Android 客户端（666 文件夹）

现有 EC 脚本使用 WebSocket 直连 `ws://192.168.20.88:8080/ws/device/{id}`。需要适配为 MQTT 协议：

**方案：在 EC 脚本中添加 MQTT 客户端支持**

EasyClick 支持 Java 互操作，可以通过 `java` 对象调用 Paho MQTT 库或使用 EC 内置的 WebSocket 通过 MQTT over WebSocket 连接 mochi-mqtt（mochi 同时支持 TCP :1883 和 WS :1882）。

**过渡方案：保留 WebSocket 兼容层**

在 Go 服务端同时暴露 WebSocket 端点 `/ws/device/{id}`，内部桥接到 MQTT：
- WebSocket 消息 → 桥接层 → MQTT Publish（上行）
- MQTT Subscribe → 桥接层 → WebSocket 推送（下行）

这样 666 脚本无需修改即可运行，后续逐步迁移到原生 MQTT。

### 10.2 HID 客户端

HID 功能已在 `common.js` 中实现（HID_CENTER_URL），通过 HTTP 与 HID 中心通信。云控框架不需要干预 HID 通信，只需要：
- 脚本中保留现有 HID 调用
- 云控下发任务时，脚本自行判断是否调用 HID

---

## 11. 安全

| 措施 | 说明 |
|------|------|
| MQTT HMAC 认证 | 设备连接需验证 HMAC-SHA256(device_id, secret) |
| MQTT ACL | 设备只能 publish/subscribe `cloud/{自己的id}/#` |
| JWT 认证 | HTTP API 全部需要 Bearer Token |
| RBAC | 25 权限 + 3 角色，保留现有结构 |
| CORS 白名单 | 生产环境限制允许的 Origin |
| PG 密码 | 环境变量注入，不写配置文件 |
| 脚本执行隔离 | 设备端脚本执行，服务端不执行用户代码 |

---

## 12. 迁移策略

### 12.1 四阶段迁移

| 阶段 | 内容 | 风险 |
|------|------|------|
| **Phase 1: 基础骨架** | Go 项目搭建 + MQTT Broker 集成 + PG Schema + API 框架 | 低 |
| **Phase 2: 核心业务** | Device/Task/Script/Screen 四个核心服务 + 削峰限流 | 中 |
| **Phase 3: 前端重写** | Nuxt 3 + Naive UI，API 对接 | 中 |
| **Phase 4: 客户端适配** | EC 脚本 MQTT 适配 + 全链路压测 300 设备 | 高 |

### 12.2 灰度切换

1. 新服务部署在新端口（如 8081）
2. 少量设备切到新服务验证
3. 逐步扩大比例
4. 全量切换后下线旧服务

---

## 附录 A: 配置结构

```go
type Config struct {
    Server   ServerConfig   // port, mode, read_timeout, write_timeout
    MQTT     MQTTConfig     // tcp_port, ws_port, secret
    Database DatabaseConfig // host, port, user, password, dbname, max_conns
    JWT      JWTConfig      // secret, access_expire, refresh_expire
    Upload   UploadConfig   // path, max_size_mb
    RateLimit RateLimitConfig // global_max_conns, conn_per_sec, ip_max_conns
}
```

## 附录 B: 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CLOUD_DB_HOST` | PG 主机 | localhost |
| `CLOUD_DB_PORT` | PG 端口 | 5432 |
| `CLOUD_DB_USER` | PG 用户 | cloud |
| `CLOUD_DB_PASSWORD` | PG 密码 | (必填) |
| `CLOUD_DB_NAME` | PG 库名 | cloud_control |
| `CLOUD_MQTT_SECRET` | MQTT HMAC 密钥 | (随机生成) |
| `CLOUD_JWT_SECRET` | JWT 签名密钥 | (随机生成) |
| `CLOUD_SERVER_PORT` | HTTP 端口 | 8080 |
| `CLOUD_MQTT_PORT` | MQTT TCP 端口 | 1883 |
