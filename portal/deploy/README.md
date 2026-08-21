# SpeakUp Portal 生产部署

本目录用于把 `prototype/` 作为独立的外部 Live Demo 部署到
`speak-up.top`。正式 Flutter 客户端和 Go 服务不依赖此部署。

## 前置条件

- `speak-up.top` 与 `www.speak-up.top` 已解析到服务器。
- 服务器已安装 Docker、Docker Compose、`flock` 和 Nginx。
- `prototype/` 的内容位于 `/opt/xe3-speakup-portal/`。
- Nginx 会加载 `/usr/local/nginx/conf/conf.d/*.conf`。若实际安装目录不同，
  下方命令中的目录需随服务器配置调整。
- `PORTAL_ADMIN_PASSWORD` 必须通过部署环境或托管平台的 Secret 注入，
  不得作为 Vite 构建变量传入或写入镜像。

以下服务器命令需由 `root` 执行，或按实际环境添加 `sudo`。

## 1. 启动 Portal 容器

```sh
cd /opt/xe3-speakup-portal
docker compose -f deploy/compose.yaml up -d --build
```

Compose 只将容器的 `3000` 端口映射到主机回环地址
`127.0.0.1:18082`，外部流量必须经过 Nginx。
正式 Nginx 配置会分别限制埋点、报名和管理接口的请求频率，并限制请求体大小。
Compose 仅信任 `speak-up.top` 与 `www.speak-up.top` 的代理主机信息，
用于还原 Nginx 终止 TLS 前的 HTTPS 请求来源。

## 2. 首次签发证书

先安装只监听 HTTP 的临时配置，让 ACME challenge 可访问：

```sh
install -d /usr/local/nginx/conf/conf.d
install -m 0644 \
  deploy/xe3-speakup-portal-http.conf \
  /usr/local/nginx/conf/conf.d/xe3-speakup-portal.conf
/usr/local/nginx/sbin/nginx -t
/usr/local/nginx/sbin/nginx -s reload
```

若 Nginx 尚未启动，最后一条命令改为
`/usr/local/nginx/sbin/nginx`。确认 80 端口已从公网放行后签发证书：

```sh
CERTBOT_EMAIL=ops@example.com ./deploy/xe3-issue-cert.sh
```

签发成功后安装正式 HTTPS 配置：

```sh
install -m 0644 \
  deploy/xe3-speakup-portal.conf \
  /usr/local/nginx/conf/conf.d/xe3-speakup-portal.conf
/usr/local/nginx/sbin/nginx -t
/usr/local/nginx/sbin/nginx -s reload
```

## 3. 安装自动续期

```sh
install -o root -g root -m 0755 \
  deploy/xe3-renew-cert.sh \
  /usr/local/sbin/xe3-renew-cert
install -m 0644 \
  deploy/xe3-speakup-certbot.cron \
  /etc/cron.d/xe3-speakup-certbot
```

cron 只执行由 `root` 安装到 `/usr/local/sbin/` 的固定副本，避免普通部署
用户修改工作目录后获得 root 执行权限。续期脚本发生变更时，需要重新执行
上述 `install` 命令。

## 4. 验证

```sh
docker compose -f deploy/compose.yaml ps
curl --fail http://127.0.0.1:18082/
curl --fail --location https://speak-up.top/
/usr/local/nginx/sbin/nginx -t
```

更新代码后，在 `/opt/xe3-speakup-portal/` 重新执行：

```sh
docker compose -f deploy/compose.yaml up -d --build
```
