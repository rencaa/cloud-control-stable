# 云控稳定版（隔离实现）

这是全新的独立目录。它不会覆盖、移动或删除原项目；容器、端口和数据卷都可独立验证，确认后再切换流量。

## 已完成的稳定性优化

- 命令可靠投递：先落库再发送，支持 ACK、超时重试、失败状态和服务重启续投。
- 客户端幂等：EasyClick 持久化最近 500 个 `cmd_id`，重复命令只回复 ACK，不重复执行。
- 设备令牌：首次 WSS 注册后签发独立令牌，WS/MQTT 后续均校验令牌，数据库只保存 SHA-256 摘要。
- 断线恢复：内置 MQTT 和 WS 双通道，MQTT 不可用时客户端回退到 WS。
- 容量保护：限制上传单文件、上传总量、截图总量、内存帧缓存、数据库连接和后台 worker 数量。
- 磁盘水位：达到 80% 记录告警，预计写入达到 90% 时拒绝资源上传、截图和新备份，防止 16 GiB 系统盘写满。
- 连接防抖：手机仍然只填服务器地址、无需令牌；服务端按来源 IP 限制异常重连频率和并发连接数。
- 自动治理：启动即清理过期数据并创建 SQLite 备份，之后按周期清理日志、指标、投递记录、截图和旧备份。
- 可观测性：`/healthz`、`/readyz`、`/metrics`，包含队列、连接池、投递、堆内存、协程数、磁盘利用率和剩余空间。
- 自愈与日志：每分钟健康检查，连续失败 3 次才重启；服务和 Nginx 日志按 20 MiB / 7 份轮转。
- 隔离回滚：升级失败自动恢复；成功升级后也可执行 `sudo cloud-control-native-rollback` 回到上一软件版本，数据不删除。

## 国内服务器一键安装（推荐）

2 核 / 2 GiB / 16 GiB 的 Ubuntu 24.04 国内服务器，推荐使用原生精简包：不安装 Docker、不访问 Docker Hub、不在服务器安装 Go 或编译；安装依赖时临时使用阿里云镜像，失败自动回退服务器原有软件源。

服务器已经连接 SSH 时，局域网免令牌版直接执行下面这一条；它通过 `work.kd99.cn` 国内加速下载，并在安装前自动校验 SHA-256：

```bash
cd /tmp && curl -fL --retry 5 -O https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-stable-cn-fast.tar.gz -O https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-stable-cn-fast.tar.gz.sha256 && sha256sum -c cloud-control-stable-cn-fast.tar.gz.sha256 && tar -xzf cloud-control-stable-cn-fast.tar.gz && sudo bash cloud-control-cn/install-cn.sh --lan
```

手机工程可从 [国内加速下载](https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-easyclick-lan-v91.zip)。

先在当前 Windows 电脑生成并上传安装包：

```powershell
powershell -ExecutionPolicy Bypass -File deploy\package-cn.ps1
scp release\cloud-control-stable-cn-fast.tar.gz ubuntu@服务器IP:/tmp/
```

SSH 登录服务器后，正式 HTTPS 环境只执行这一条：

```bash
cd /tmp && tar -xzf cloud-control-stable-cn-fast.tar.gz && sudo bash cloud-control-cn/install-cn.sh --domain control.example.com --email admin@example.com
```

局域网手机免令牌接入使用独立的 18080 端口；安装器只接受 RFC1918 私网地址，不会停止原服务：

```bash
cd /tmp && tar -xzf cloud-control-stable-cn-fast.tar.gz && sudo bash cloud-control-cn/install-cn.sh --lan
```

安装后手机只需要在界面填写服务器局域网 IP（例如 `192.168.1.100`），端口会自动使用 `18080`，设备 ID 自动生成，不发送也不校验设备令牌。不要把 TCP 18080 映射或开放到公网。

需要每天把 SQLite 备份同步到 USB/挂载盘时，安装命令增加 `--backup-target /mnt/cloud-backup`；同步到另一台局域网 Linux 主机时增加 `--backup-target backup@192.168.1.20:/srv/cloud-backup --backup-key /root/.ssh/id_ed25519`。

完整说明、运维和回滚见 [国内 Ubuntu 24.04 服务器一键安装](docs/国内服务器一键安装.md)。

## 2 核 / 2 GiB / 16 GiB 推荐模式

使用 `docker-compose.edge.yml`：Go 服务 + SQLite + 内置 MQTT + Nginx，不启动 MySQL、EMQX 和 Prometheus。已配置容器内存/CPU/PID 上限、Docker 日志轮转、只读根文件系统和低磁盘保留策略。

Ubuntu 24.04 正式环境一键安装（自动安装 Docker、创建 swap、生成密码、申请证书、启动并健康检查）：

```bash
sudo bash deploy/install-edge.sh --production \
  --domain control.example.com --email admin@example.com
```

也可以直接在当前 Windows 电脑上一条命令完成打包、上传和远程安装：

```powershell
powershell -ExecutionPolicy Bypass -File deploy\remote-install.ps1 `
  -Server ubuntu@服务器IP `
  -Domain control.example.com `
  -Email admin@example.com
```

如果当前服务器已有服务，先用不占用 80/443 的隔离模式：

```bash
sudo bash deploy/install-edge.sh --staging --host 服务器IP
```

安装完成后，管理员随机密码会显示在终端，并保存到权限为 `600` 的 `install-credentials.txt`。脚本发现端口冲突时会直接停止，不会关闭已有服务。

手工启动方式：

```bash
cp .env.edge.example .env
# 修改 .env 中的域名、管理员密码和随机密钥
chmod +x deploy/*.sh
./deploy/preflight.sh --edge
docker compose --project-name cloud-control-stable -f docker-compose.edge.yml up -d --build
./deploy/edge-status.sh
```

完整命令、HTTPS、灰度切换、备份和回滚请看 [Ubuntu 24.04 低配小主机部署教程](docs/Ubuntu24低配小主机部署教程.md)。

在 Windows 上生成约 5 MiB 的精简部署包（自动排除历史数据库、日志、旧二进制和密钥）：

```powershell
powershell -ExecutionPolicy Bypass -File deploy\package-edge.ps1
```

## 标准 MySQL 模式

普通服务器使用 `docker-compose.yml`。在内存较小但必须使用 MySQL 时叠加 `docker-compose.low-mysql.yml`：

```bash
docker compose --project-name cloud-control-stable -f docker-compose.yml -f docker-compose.low-mysql.yml up -d --build
```

Prometheus 已改为可选 profile，只有明确需要时才启动：

```bash
docker compose --project-name cloud-control-stable --profile monitoring up -d
```

## HTTPS/WSS

先将证书放到 `deploy/tls/fullchain.pem` 和 `deploy/tls/privkey.pem`，再叠加 TLS 配置：

```bash
./deploy/preflight.sh --edge --tls --require-docker
docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml up -d --build
```

公网设备优先使用 WSS。若未为 MQTT 单独配置 TLS，应将 `MQTT_BIND_ADDRESS=127.0.0.1`，客户端使用 WS 通道，避免明文 MQTT 暴露公网。

## 安全注册顺序

1. 先部署 HTTPS/WSS，保持 `CLOUD_RELIABLE_DELIVERY_ENABLED=false`。
2. 临时设置 `CLOUD_DEVICE_AUTO_REGISTER=true`，只让待注册测试设备上线。
3. 确认客户端保存 `device_token` 并返回 `register_ack`。
4. 立即恢复 `CLOUD_DEVICE_AUTO_REGISTER=false` 并重建服务。
5. 单设备验证命令、任务、断网重连和服务重启，再灰度开启可靠投递。

## 运维与回滚

```bash
./deploy/edge-status.sh
./deploy/export-edge-backup.sh /挂载的异机或对象存储目录
docker compose -f docker-compose.edge.yml logs --tail 200 server nginx
```

- 可靠投递异常：设置 `CLOUD_RELIABLE_DELIVERY_ENABLED=false`，重建 server；保留 `command_deliveries` 供审计。
- 稳定版异常：在新目录执行 `docker compose -f docker-compose.edge.yml down`，不要加 `-v`，再恢复原服务。
- 详细灰度方法见 [可靠投递灰度与回滚](docs/可靠投递灰度与回滚.md)。

Windows 开发机可运行：

```powershell
powershell -ExecutionPolicy Bypass -File deploy/preflight.ps1 -Edge
```
