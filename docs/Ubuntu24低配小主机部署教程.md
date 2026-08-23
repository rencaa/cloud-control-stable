# Ubuntu 24.04 低配小主机部署教程

适用配置：2 核 CPU、约 2 GiB 内存、16 GiB 系统盘。这里推荐原生 Ubuntu 24.04 + Docker Edge 模式，不安装宝塔。宝塔面板及其常驻组件会占用这台机器本就有限的内存和磁盘；若一定要使用宝塔，也只把它当作反向代理入口，不要再安装一套 MySQL、Redis 或 Docker 管理栈。

Edge 模式只运行两个容器：`server`（Go + SQLite + 内置 MQTT）和 `nginx`。MySQL、EMQX、Prometheus 默认都不启动。

## 最快方式：一键安装

如果 Windows 已能通过 SSH 登录 Ubuntu，直接在当前项目目录运行下面这一条命令即可。它会自动生成约 5 MiB 的部署包、上传、解压并执行服务器安装：

```powershell
cd C:\Users\Administrator\Desktop\yun\cloud-control-stable
powershell -ExecutionPolicy Bypass -File deploy\remote-install.ps1 `
  -Server ubuntu@服务器IP `
  -Domain control.example.com `
  -Email admin@example.com
```

需要使用 SSH 密钥时，先用普通 `ssh ubuntu@服务器IP` 确认能够登录；脚本会沿用系统 OpenSSH 的密钥和配置。

先按第 3 节把精简压缩包上传并解压。正式 HTTPS 环境只需执行一条命令：

```bash
cd /opt/cloud-control-stable
sudo bash deploy/install-edge.sh --production \
  --domain control.example.com --email admin@example.com
```

脚本会自动完成：系统与磁盘检查、Docker 官方仓库安装、1 GiB swap、随机 JWT 和管理员密码、低资源 `.env`、Let's Encrypt 证书、续期 hook、容器构建、启动和两分钟健康检查。

如果机器上还有旧服务，先使用隔离模式，不占用 80/443：

```bash
sudo bash deploy/install-edge.sh --staging --host 服务器IP
```

Windows 远程隔离安装则使用：

```powershell
powershell -ExecutionPolicy Bypass -File deploy\remote-install.ps1 `
  -Server ubuntu@服务器IP -Staging -HostName 服务器IP
```

安装成功后，访问地址和管理员密码会显示在终端，同时写入权限为 `600` 的 `/opt/cloud-control-stable/install-credentials.txt`。脚本遇到端口冲突会停止，不会杀进程或关闭旧服务；数据卷也不会被删除。

需要让第一批设备注册时可以加 `--enable-registration`，但设备领到令牌后必须把 `.env` 中的 `CLOUD_DEVICE_AUTO_REGISTER` 恢复为 `false` 并重建 server。下面各节保留为手工部署、排障和理解每一步时使用。

## 1. 部署前准备

- 将域名（示例为 `control.example.com`）解析到小主机公网 IP。
- 云厂商安全组只开放 SSH 端口、TCP 80 和 TCP 443。
- 公网不开放 18081 和 1883；设备统一使用 HTTPS/WSS。
- 先保留原服务。新版本用独立 Compose 项目名、独立数据卷和测试端口启动，验证后才切流量。

确认资源：

```bash
nproc
free -h
df -h /
uname -m
```

如果没有 swap，建议增加 1 GiB，防止镜像构建或瞬时峰值直接触发 OOM：

```bash
if ! swapon --show=NAME | grep -qx '/swapfile'; then
  if [ ! -f /swapfile ]; then
    sudo fallocate -l 1G /swapfile
    sudo chmod 600 /swapfile
    sudo mkswap /swapfile
  fi
  sudo swapon /swapfile
fi
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-cloud-control.conf
sudo sysctl --system
```

## 2. 安装 Docker Engine 和 Compose

下面使用 Docker 官方 apt 仓库，Ubuntu 24.04（Noble）受官方安装文档支持：

```bash
sudo apt update
sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
sudo docker run --rm hello-world
sudo docker compose version
```

官方参考：[Install Docker Engine on Ubuntu](https://docs.docker.com/engine/install/ubuntu/) 和 [Install Docker Compose](https://docs.docker.com/compose/install/)。

## 3. 上传独立稳定版目录

先在 Windows 开发机生成精简部署包。脚本只打包运行所需源码和配置，会排除历史数据库、日志、旧二进制、证书和 `.env`；实测压缩包约 5 MiB：

```powershell
cd C:\Users\Administrator\Desktop\yun\cloud-control-stable
powershell -ExecutionPolicy Bypass -File deploy\package-edge.ps1
scp "C:\Users\Administrator\Desktop\yun\cloud-control-stable\release\cloud-control-stable-edge.zip" ubuntu@服务器IP:/tmp/
```

在服务器安装解压工具并解压到独立目录：

```bash
sudo apt install -y unzip
sudo unzip /tmp/cloud-control-stable-edge.zip -d /opt
sudo chown -R "$USER":"$USER" /opt/cloud-control-stable
```

也可以使用 WinSCP 上传压缩包。不要把它解压覆盖到旧云控目录。

回到服务器：

```bash
cd /opt/cloud-control-stable
chmod +x deploy/*.sh
cp .env.edge.example .env
chmod 600 .env
```

## 4. 配置环境变量

先生成随机密钥：

```bash
openssl rand -base64 48
openssl rand -base64 32
```

编辑配置：

```bash
nano /opt/cloud-control-stable/.env
```

至少修改：

```dotenv
CLOUD_JWT_SECRET=第一条随机值
CLOUD_ADMIN_PASSWORD=第二条随机值或更强的独立密码
CLOUD_CORS_ORIGINS=https://control.example.com

# 公网部署先只使用 WSS，不暴露明文 MQTT。
MQTT_BIND_ADDRESS=127.0.0.1

# 初次测试使用不冲突的端口，不影响旧服务。
SERVER_LOOPBACK_PORT=18081
HTTP_PORT=18080
HTTPS_PORT=18443
EMBEDDED_MQTT_PORT=11883

CLOUD_DEVICE_AUTO_REGISTER=false
CLOUD_RELIABLE_DELIVERY_ENABLED=false
```

其余低配默认值已经控制为：服务容器 384 MiB、Nginx 96 MiB、上传总量 2 GiB、截图 256 MiB、Docker 日志最多约 15 MiB/容器、SQLite 每日备份并保留 7 份。

## 5. 先在隔离端口启动并验证

```bash
cd /opt/cloud-control-stable
sudo ./deploy/preflight.sh --edge --require-docker
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml up -d --build
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml ps
curl -fsS http://127.0.0.1:18081/healthz
curl -fsS http://127.0.0.1:18081/readyz
sudo ./deploy/edge-status.sh
```

预期返回 `{"status":"ok"}` 和 `{"status":"ready"}`。若失败：

```bash
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml logs --tail 200 server nginx
```

此时旧服务没有被停止，新服务使用独立数据卷和高位端口。

## 6. 配置 HTTPS/WSS

先安装 Certbot。在全新服务器、80 端口空闲时可直接使用 standalone 模式：

```bash
sudo apt update
sudo apt install -y certbot
sudo certbot certonly --standalone -d control.example.com --agree-tos -m 你的邮箱 --no-eff-email
sudo /opt/cloud-control-stable/deploy/sync-letsencrypt.sh control.example.com
```

如果旧服务占用 80 端口，不要强停它来试错。可先用 DNS challenge 申请证书，或在计划好的切换窗口短暂停止旧反向代理再执行 standalone。证书拿到后，新版本仍可在 18443 独立验证：

```bash
cd /opt/cloud-control-stable
sudo ./deploy/preflight.sh --edge --tls --require-docker
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml up -d --build
curl --resolve control.example.com:18443:127.0.0.1 https://control.example.com:18443/readyz
```

安装自动续期同步 hook：

```bash
sudo ln -sf /opt/cloud-control-stable/deploy/sync-letsencrypt.sh /etc/letsencrypt/renewal-hooks/deploy/cloud-control-sync.sh
sudo certbot renew --dry-run
```

脚本会复制续期后的证书并让 Nginx 平滑 reload。

## 7. 正式切换到 80/443

只有当高位端口验证通过，才进行切换。先备份旧系统，并记录旧服务的启动命令。然后将 `.env` 改为：

```dotenv
HTTP_PORT=80
HTTPS_PORT=443
SERVER_LOOPBACK_PORT=18081
MQTT_BIND_ADDRESS=127.0.0.1
EMBEDDED_MQTT_PORT=11883
```

在维护窗口释放原 80/443 端口，再执行：

```bash
cd /opt/cloud-control-stable
sudo ./deploy/preflight.sh --edge --tls --require-docker
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml up -d
curl -fsS https://control.example.com/readyz
```

不要使用 `down -v`，它会删除稳定版独立数据卷。

## 8. 修改并灰度设备客户端

在 `client/easyclick/src/js/main.js` 顶部把服务器地址改成 `wss://control.example.com/ws/...`，公网先设置：

```javascript
const CLOUD_TRANSPORT = "ws";
```

然后按以下顺序灰度：

1. 保持可靠投递关闭。
2. 临时将 `.env` 的 `CLOUD_DEVICE_AUTO_REGISTER=true`，重建 server，只让一台测试设备上线。
3. 确认设备收到并保存 `device_token`，随后把自动注册恢复为 `false`。
4. 验证命令、任务、断网重连、服务重启和重复命令不会重复执行。
5. 再将 `CLOUD_RELIABLE_DELIVERY_ENABLED=true`，先单设备、再小批量放量。

每次改 `.env` 后执行：

```bash
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml up -d --force-recreate server
```

## 9. 日常监控和磁盘控制

```bash
cd /opt/cloud-control-stable
sudo ./deploy/edge-status.sh
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml logs --tail 200
sudo docker system df
df -h /
```

建议设置每 5 分钟探测 `https://域名/readyz`。磁盘超过 75% 就处理，不要等到 SQLite 无法写入。确认旧镜像不再需要后，可清理未被任何容器使用的镜像：

```bash
sudo docker image prune -f
```

不要运行 `docker system prune --volumes`。

`/metrics` 已有堆内存、协程、数据库连接、队列、截图空间和资源空间指标；Nginx 默认只允许容器内网访问它。

## 10. 备份与异机保存

服务启动时会立即创建一次一致性 SQLite 备份，之后每天创建并只保留 7 份。将备份导出到挂载的异机磁盘目录：

```bash
sudo mkdir -p /mnt/backup/cloud-control
sudo /opt/cloud-control-stable/deploy/export-edge-backup.sh /mnt/backup/cloud-control
```

导出内容包含 SQLite 一致性备份和权限为 600 的 `.env`。截图和资源文件位于 `uploads-data` 卷，容量可达 2 GiB，建议直接同步到对象存储或另一台机器，避免在 16 GiB 系统盘上再复制一份。

只有同盘备份不算灾备。至少定期把导出目录传到另一台机器，并抽查数据库文件大小不为 0。

## 11. 更新与回滚

更新前先导出备份，然后上传新代码并构建：

```bash
cd /opt/cloud-control-stable
sudo ./deploy/export-edge-backup.sh /mnt/backup/cloud-control
sudo ./deploy/preflight.sh --edge --tls --require-docker
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml build server
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml up -d server nginx
curl -fsS https://control.example.com/readyz
```

可靠投递异常时，设为 `false` 并重建 server，不删除 `command_deliveries`。整套稳定版需要回滚时：

```bash
cd /opt/cloud-control-stable
sudo docker compose --project-name cloud-control-stable -f docker-compose.edge.yml -f docker-compose.tls.yml down
```

随后按原来的命令恢复旧服务。上面的 `down` 不带 `-v`，稳定版数据卷仍保留，修复后可继续使用。

## 12. 防火墙特别注意

Docker 官方文档明确提醒：容器发布端口可能绕过 UFW/firewalld 的常规规则。因此公网端口要优先在云厂商安全组限制；本方案还把管理端口和 MQTT 绑定到 `127.0.0.1`，只公开 Nginx 的 80/443。参考：[Docker and ufw](https://docs.docker.com/engine/install/ubuntu/#firewall-limitations)。
