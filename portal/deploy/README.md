# SpeakUp Portal legacy 生产部署

> 本目录只保留现网 Portal-only 启动和 SQLite 备份的历史说明。新的 Release
> Candidate 禁止执行本目录的源码 `up --build`；请使用仓库根目录
> `deploy/production/` 的不可变制品契约。该契约当前只有只读校验入口，正式切换必须
> 等生产备份、migration 与回滚编排完成审核。

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

## 2. 证书生命周期

本目录不再提供旧的可变 Certbot 镜像和无条件 reload 安装入口。首次 Staging 签发、
现有 Production 证书扩展、精确 SAN/密钥/权限/有效期验证、renewal dry-run 与
systemd 自动续期统一使用仓库根目录的 [`deploy/tls/README.md`](../../deploy/tls/README.md)。

迁移前不得删除现有 `/opt/xe3-speakup-portal/certbot/conf` 账户和
`speak-up.top` lineage；新流程通过操作员注入该根目录来安全扩展它。本次仓库改动
本身不会签发证书、安装 Nginx 配置或修改服务器。
仓库删除文件不会自动停用服务器上曾经安装的 cron 或 `/usr/local/sbin` 副本；启用
新 systemd timer 前必须按新文档审计并停用旧调度，避免两个续期器并行运行。

## 3. 安装 SQLite 定时备份

备份任务使用 SQLite Online Backup API 读取活动数据库，不直接复制正在写入的
`portal.sqlite`。源 Docker volume 在一次性备份容器中只读挂载；完整备份先写入
`.partial-*` 目录，完成 SQLite `integrity_check`、大小和 SHA-256 复核后才原子发布。

运行备份前，先确认当前 Portal 镜像已包含
`/app/ops/portal-sqlite-backup.mjs`。安装固定脚本和 systemd units：

```sh
install -o root -g root -m 0755 \
  deploy/xe3-portal-sqlite-backup \
  /usr/local/sbin/xe3-portal-sqlite-backup
install -o root -g root -m 0644 \
  deploy/xe3-portal-sqlite-backup.service \
  deploy/xe3-portal-sqlite-backup.timer \
  deploy/xe3-portal-sqlite-restore-check.service \
  /etc/systemd/system/
install -d -o root -g root -m 0755 /etc/speakup
if [ ! -e /etc/speakup/portal-backup.env ]; then
  install -o root -g root -m 0600 \
    deploy/portal-backup.env.example \
    /etc/speakup/portal-backup.env
fi
```

`/etc/speakup/portal-backup.env` 不读取或包含 `portal.env` 的后台密码。启用前必须
显式填写以下非 Secret 配置，空值会使任务失败：

- `PORTAL_BACKUP_CONTAINER`：正式 Portal 容器名。
- `PORTAL_BACKUP_SOURCE_VOLUME`：挂载到容器 `/app/.wrangler` 的数据卷名。
- `PORTAL_BACKUP_IMAGE`：当前 Portal 镜像的本地 `sha256:...` image ID，或已拉取的
  `repository@sha256:...`；禁止使用可变 Tag。该镜像必须与运行中容器完全一致。
- `PORTAL_BACKUP_DEPLOYMENT_VERSION`：当前部署 manifest 中的明确版本或 revision。
- `PORTAL_BACKUP_RETENTION_DAYS`：经业务确认的本机成品保留天数，不提供默认值。
- `PORTAL_BACKUP_MAX_AGE_SECONDS`：经运维确认的新鲜度上限，不提供默认值。

systemd units 会在启动命令中把备份根目录固定为
`/var/lib/speakup/portal-backups`；环境文件不能覆盖该路径。

本地 image ID 可只读查询，不需要打开容器环境变量：

```sh
docker container inspect --format '{{.Image}}' xe3-speakup-portal
```

先手动执行一次备份和隔离恢复检查，二者都成功后再启用每日 timer：

```sh
systemctl daemon-reload
systemctl start xe3-portal-sqlite-backup.service
systemctl start xe3-portal-sqlite-restore-check.service
systemctl enable --now xe3-portal-sqlite-backup.timer
```

每个成品目录均为 `0700`，数据库、校验和和 `metadata.json` 均为 `0600`。元数据记录
生成时间、来源 volume、部署版本、字节数和 SHA-256。恢复检查只读挂载备份目录，
把最新成品复制到一次性匿名 Docker volume 后复核 SHA-256 和
`PRAGMA integrity_check`；`--rm` 会在检查结束后移除该匿名 volume。

查看 timer、最近结果和诊断日志：

```sh
systemctl list-timers xe3-portal-sqlite-backup.timer
systemctl status xe3-portal-sqlite-backup.service
systemctl status xe3-portal-sqlite-restore-check.service
journalctl -u xe3-portal-sqlite-backup.service --since today
journalctl -u xe3-portal-sqlite-restore-check.service --since today
```

禁用定时任务不会删除任何备份：

```sh
systemctl disable --now xe3-portal-sqlite-backup.timer
```

任何源卷/镜像/部署不一致、备份中断、元数据损坏、SHA-256 不一致、SQLite 完整性
错误或备份过期都会返回非零状态。可正常捕获的失败会清理本次临时目录；进程被强制
终止等无法执行清理的中断可能留下 `.partial-*`，它们不计为成功备份，也不会被保留
策略删除。保留策略只删除命名和元数据均有效且超过显式保留天数的已完成目录。
异地副本的目标、凭证和保留策略不属于此配置。

实现依据：SQLite 官方 [Online Backup API](https://www.sqlite.org/backup.html)、
Node.js 官方 [`sqlite.backup`](https://nodejs.org/api/sqlite.html#sqlitebackupsourceDb-path-options)、
Docker 官方 [Volumes](https://docs.docker.com/engine/storage/volumes/)，以及 systemd 官方
[`systemd.timer`](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
和 [`systemd.service`](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)。

## 4. 验证

```sh
docker compose -f deploy/compose.yaml ps
curl --fail http://127.0.0.1:18082/
curl --fail --location https://speak-up.top/
/usr/local/nginx/sbin/nginx -t
```

以下旧命令仅用于识别历史部署方式，禁止用于新的 Release Candidate：

```sh
docker compose -f deploy/compose.yaml up -d --build
```
