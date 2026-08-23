# cloud-control-stable

Ubuntu 24.04 国内低配服务器一键安装包，支持 amd64 / arm64。

## 局域网一键安装

```bash
cd /tmp && curl -fL --retry 5 -o cloud-control-stable-cn-fast.tar.gz 'https://work.kd99.cn/https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-stable-cn-fast.tar.gz' && echo 'A0DA36209FF40C2C852B71A32BB6DE4022D6094C1B2F53000D5BCD7BE5340CBA  cloud-control-stable-cn-fast.tar.gz' | sha256sum -c - && tar -xzf cloud-control-stable-cn-fast.tar.gz && sudo bash cloud-control-cn/install-cn.sh --staging
```

安装完成后，通过 `http://服务器局域网IP:18080` 访问。

GitHub 原始下载地址：

```text
https://github.com/rencaa/cloud-control-stable/releases/latest/download/cloud-control-stable-cn-fast.tar.gz
```