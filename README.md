# cloud-control-stable

Ubuntu 24.04 国内低配服务器局域网免令牌版，支持 amd64 / arm64。手机只需要填写服务器局域网 IP。

## 服务器一键安装

```bash
cd /tmp && U='https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-stable-cn-fast.tar.gz' && (curl -fL --retry 5 -o cloud-control-stable-cn-fast.tar.gz "$U" || wget -O cloud-control-stable-cn-fast.tar.gz "$U") && echo 'C720E319208EB3A3643C9363612F189B6E2DFC7247D2CF73C799DD073CFAD3F0  cloud-control-stable-cn-fast.tar.gz' | sha256sum -c - && tar -xzf cloud-control-stable-cn-fast.tar.gz && sudo bash cloud-control-cn/install-cn.sh --lan
```

安装后通过 `http://服务器局域网IP:18080` 访问。TCP 18080 不要开放或映射到公网。

## EasyClick 手机工程 v91

- [work.kd99.cn 加速下载](https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-easyclick-lan-v91.zip)
- SHA256：`6419EB61563049AABABDFB7E9B95F8E003C0F05DB2554E45F661476980A3202C`

导入工程后，手机界面只填写服务器局域网 IP（例如 `192.168.1.100`），端口自动使用 `18080`，设备 ID 自动生成，不使用设备令牌。