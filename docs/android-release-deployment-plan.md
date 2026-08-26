# SpeakUp Android 发布与服务器部署方案

> 状态：实施基线
> 更新时间：2026-08-27
> 关联 Issue：[#819](https://github.com/1024XEngineer/XE3-ESL/issues/819)
> 目标 Milestone：MS4

## 1. 目的

本文整理 SpeakUp 首次 Android 对外发布所需的测试、打包、审核、部署、
数据保护、回滚和观测流程。目标不是让“代码能在服务器上运行”，而是让每次
上线都能回答以下问题：

- 上线的是哪个 Git commit 和版本？
- 哪些自动化测试与人工验收已经通过？
- Portal、后端和 APK 分别对应哪个不可变制品？
- 谁批准了生产部署，何时部署，结果如何？
- 数据如何备份，应用如何回滚？
- 上线后如何确认 API、第三方服务和用户主链路正常？

本文不记录生产 IP、SSH 账号、密码、密钥或真实环境变量。生产访问资料应保留
在团队内部运维记录和密码管理器中，不进入公开仓库。

本文是随代码版本维护的长期发布契约。GitHub Issue 只跟踪实施范围、验收和
进度，不能作为唯一方案来源；一次性截图、原始测试数据和现场日志不进入本文。

## 2. 范围与阶段边界

### 2.1 本阶段包含

- Android arm64 APK 直接下载与安装。
- Portal 网站及 APK 下载入口。
- Go 后端服务。
- PostgreSQL 核心业务数据库。
- Portal 现有 SQLite 报名与访问数据。
- 对象存储中的录音、简历和图片等文件。
- Nginx、域名、HTTPS、配置与密钥管理。
- PR 门禁、Release Candidate、测试环境、人工审核和生产部署。
- 数据备份、migration、回滚、日志、指标和基础告警。

### 2.2 本阶段不包含

- iOS TestFlight 或 App Store 发布。
- Android 应用商店发布或 AAB。
- App 内自动升级。
- 多服务器高可用、自动扩缩容或 Kubernetes。
- 为追求统一而立即把 Portal SQLite 迁移到 PostgreSQL。

## 3. 已核验事实

### 3.1 仓库与 CI

- `Quality` 已按变更范围执行 Flutter、Go、API 和 Portal 检查；面向 `main`
  的 Release PR 会执行全部检查。
- Go 与 Flutter 已生成覆盖率，并与目标分支最近的成功基线比较。
- 覆盖率评论会显示 Current、Baseline 和 Delta。
- 覆盖率基线缺失时会失败，不再静默跳过。
- Portal 已执行 Node 22、Lint、测试、生产构建和 Docker 构建。
- `Quality gate` 已作为固定汇总检查存在，但 GitHub Ruleset 尚未把它配置为
  Required Status Check；该严格平台门禁已在 #831 中明确暂缓。
- 当前 GitHub Ruleset 要求 PR 和两个 Approval，但没有把 `Quality gate`
  配置为 Required Status Check，因此 CI 失败在平台层面仍不能可靠阻止合并。
- 当前 Release 只有版本记录，没有 APK、生产镜像或自动部署。

### 3.2 现有生产 Portal

- 现有 Portal、Nginx 和 TLS 正常运行，应用端口仅绑定宿主机回环地址。
- 线上实际版本晚于旧运维记录，镜像使用本地 `latest`，发布源码不是 Git
  worktree，无法从线上状态反查准确 commit。
- Portal SQLite 完整性检查正常，数据保存在 Docker Volume 中。
- #829 已完成首次一致性备份、SHA-256 和恢复验证；定时备份、异地副本与
  周期性恢复演练仍未建立。
- 容器没有 Docker healthcheck，Docker 和 Nginx 日志没有明确的容量轮转策略。
- #816 已从官方仓库移除 Codex Sites 元数据，Portal 只保留自有服务器部署方向。

### 3.3 Android

- Flutter 工程当前版本为 `0.1.0+1`。
- Android release 当前仍使用 debug signing，不能作为正式对外安装包。
- 当前 Android 构建只保留 `arm64-v8a`，首版 APK 因而只承诺支持 arm64
  Android 设备。
- Android 开发者验证已在 2026 年开始分阶段实施。2026-09-30 的首批强制范围
  不包含网站直接侧载，但 2027 年计划扩展到全球认证设备；正式 package name、
  签名证书和开发者账号仍应在首发阶段固定并准备注册。

## 4. 已确认决策

- 第一阶段只做 Android，不让 iOS 审核阻塞上线。
- Portal 和 Go 后端统一部署到自有服务器，不使用 Codex Sites 作为生产目标。
- Portal 提供 Android APK 下载链接。
- 自动化可以部署，但进入生产前必须暂停并等待人工审核。
- 合并、发版和部署是三个不同动作；合入代码不等于已经上线。
- 生产服务器不再现场从源码重新构建；CI 构建并测试制品，服务器只部署指定版本。
- 生产数据必须在部署前备份，且需要可验证的恢复方案。

## 5. 核心术语

| 名称 | 含义 |
| --- | --- |
| PR Gate | 决定代码是否允许合入的自动化测试与审核门禁 |
| Release Candidate | 从官方 `main` 精确 commit 构建、等待验收的内部 Candidate；不等于正式发布 |
| Stable Tag | Candidate 通过验收后才创建的不可变 `vX.Y.Z` Git 引用 |
| GitHub Release | 基于 Stable Tag 发布用户制品和更新说明的正式发布记录 |
| Artifact | APK、Portal 镜像、后端镜像、校验和与发布清单 |
| Staging | 部署 Candidate 制品、与生产隔离并供团队验证的真实测试环境 |
| Production | 已批准正式版本面向普通用户运行的环境 |
| Deploy | 把已经构建好的指定版本放到目标环境运行 |
| Promote | 将已经验证的候选版本批准进入生产 |

Release Candidate、Stable Tag、GitHub Release、Staging 部署和 Production 部署是
五种不同状态。Candidate 构建成功不表示已经部署 Staging；创建 Stable Tag 不表示
GitHub Release 已发布；发布 GitHub Release 也不表示 Production 已完成部署或切换。

测试环境不等于 Mock，也不要求购买第二台服务器。首版可以在同一物理服务器上
使用不同的容器、端口、域名和数据库建立隔离环境，并通过密码或 IP 白名单只向
团队开放。

## 6. 分支与发布主链

### 6.1 分支职责

- `dev` 是日常集成分支，功能 PR 合入 `dev`。
- `main` 是发布分支，只通过 `dev -> main` 的 Release PR 接收候选版本。
- Release PR 合并后，官方仓库 `main` 的 `push` 自动以精确 commit 触发内部
  Candidate 构建，不提前创建 `vX.Y.Z` Tag。不存在“合入 main 后再把全部代码
  额外推送一次”。
- Stable Tag 只留给通过验收、准备进入正式发布流程的同一 commit 和制品。
- `main` 上的 Release merge commit 不会自动回到 `dev`，因此 GitHub 可能显示
  `dev` 落后若干 merge commit。这是历史拓扑差异，不等于 `dev` 缺少代码。
- 判断是否需要同步时，必须检查 `main` 独有的非 merge commit 和实际 tree diff，
  不能只看 Ahead/Behind 数字。
- 如果 `main` 未来出现 `dev` 没有的真实代码改动，应先通过独立 PR 把该改动带回
  `dev`，再创建 Release PR；禁止直接推送官方 `dev` 或 `main`。

2026-08-21 核验结果：`main` 独有项只有历史 Release merge commit，没有 `dev`
缺失的普通代码提交，因此当前不需要把旧 `main` 反向合入 `dev`。

### 6.2 目标发布主链

```text
功能 PR
  -> PR Gate 与 Review
  -> 合入 dev
  -> Release PR: dev -> main
  -> 官方 main `push` 自动以精确 SHA 触发内部 Release Candidate
  -> 全量测试并构建完整、可追溯制品
  -> 成功后由独立 Staging Workflow 自动部署同一 Candidate
  -> 自动冒烟 + 真人验收
  -> 等待 Production 人工批准
  -> 生产数据备份与 migration
  -> 部署同一版本的 Portal/后端制品
  -> 上传正式 APK，但暂不更新公开入口
  -> 生产冒烟
  -> 基于同一制品创建 GitHub Release 草稿并上传完整资产
  -> 原子更新 Portal APK 下载入口
  -> 校验公开 APK、更新日志与同一发布时间
  -> 发布不可变 vX.Y.Z Tag、GitHub Release 与部署记录
```

任一自动化检查失败时流程停止。没有人工批准时，生产环境不发生变化。

## 7. Workflow 设计

### 7.1 PR Gate

现有 `Quality gate` 是固定的最终汇总检查。仓库协作规则要求合并前该检查成功、
Review 意见解决且人工验收满足当前 Issue；GitHub Ruleset 的 Required Status
Check 严格模式已暂缓，不在本阶段擅自重新启用。

| 变更范围 | 阻塞检查 |
| --- | --- |
| Flutter | format、analyze、unit/widget、coverage、编译验证 |
| Go | format、vet、unit/integration、coverage |
| API | OpenAPI、JSON Schema 与跨端契约 |
| Database | migration 追加策略、up/down/up、PostgreSQL 集成、readiness |
| Portal | `npm ci`、build、test、lint、Docker build |
| Deploy | Dockerfile、Compose、Nginx 和脚本静态校验 |

覆盖率第一阶段采用以下规则：

- Go 与 Flutter 总覆盖率不得低于目标分支基线。
- 基线缺失时失败，不再静默跳过。
- PR 评论继续展示总覆盖率和文件级下降。
- Portal 先要求测试与构建通过，不在缺乏可靠采集方式时虚构覆盖率数字。
- 累积真实 PR 数据后，再单独评审是否启用 changed-lines coverage；建议初始
  候选值为 80%，但不在本方案中直接固化。

未来如重新启用严格 Ruleset，目标配置为：

- `Quality gate` 为 Required Status Check。
- 保留两个 Approval。
- Review conversation 必须全部解决。
- 最新一次可审查 Push 必须由其他人确认。
- `dev`、`main` 禁止直接 Push。
- `main` 只接收经过全量 Release PR 检查的版本变更。

### 7.2 Release Candidate

Release Candidate Workflow 仅响应官方仓库 `refs/heads/main` 的 `push`，并将
GitHub Actions 事件提供的精确 `github.sha` 作为 Candidate commit。Workflow 必须
先验证：

- 事件为 `push`，仓库为 `1024XEngineer/XE3-ESL`，ref 为官方 `main`。
- checkout HEAD 与完整小写 Candidate SHA 一致，且 commit 位于 `main` 的
  first-parent 历史。
- Candidate 的 versionName 或 commit 尚未被 Stable Tag 使用。
- Flutter versionName 使用 `X.Y.Z`，且版本与 Android versionCode 均高于上一
  稳定发布版本。
- 现有 Stable Tag 集合与运行时远端精确一致，且全部位于 `main` first-parent；
  Tag、versionName 和 versionCode 继续保持单调。
- 全量测试通过。

Candidate 身份由 `version`、`versionCode`、`git_sha` 和 GitHub Actions run ID
共同组成。run ID 由 Actions 运行及 manifest 的 `quality_run_url` 保留；内部
Candidate 不创建、模拟或要求 `vX.Y.Z` Stable Tag。

一次构建产生：

- Portal OCI 镜像及 digest。
- Go 后端 OCI 镜像及 digest。
- Staging APK。
- 正式签名并冻结的 Production APK。
- 两个 APK 各自的 SHA-256。
- `release-manifest.json`。

发布清单至少包含：

```json
{
  "version": "0.1.1",
  "git_sha": "...",
  "portal_image_digest": "sha256:...",
  "server_image_digest": "sha256:...",
  "staging_apk_sha256": "...",
  "production_apk_sha256": "...",
  "production_apk_size_bytes": 12345678,
  "application_id": "com.xengineer.speakup",
  "minimum_android_api": 24,
  "abis": ["arm64-v8a"],
  "database_schema_version": "...",
  "quality_run_url": "..."
}
```

镜像可使用语义版本标签方便识别，但部署必须引用 digest，不能引用可变
`latest`。

Release Candidate 只构建并上传上述制品，不提前创建 Stable Tag 或 GitHub
Release。独立 Staging Workflow 自动部署同一 Candidate；Production Workflow 在
Staging 成功后进入 Environment 人工审批，批准后才取得生产凭证并继续正式发布。

Android 正式签名采用以下已确认边界：

- GitHub Environment 固定为 `android-release-signing`。
- 不设置 Required reviewer，避免在 Candidate 构建阶段提前暂停；唯一人工发布
  审批保留在后续 `production` Environment。
- 正式签名证书 Owner 为 `Lq0412`。
- 证书与密码均保存两份：团队密码管理器一份、离线加密备份一份，不能只保存在
  GitHub Secret。
- keystore 只以 `SPEAKUP_ANDROID_KEYSTORE_BASE64` Environment Secret 注入临时
  Runner；构建结束后删除，不进入 Artifact、日志或仓库。

首次运行前，管理员必须先创建该 Environment，将 Deployment branches and tags
设为 Selected branches and tags，并且只允许 Branch pattern `main`；随后才能添加
以下 Environment Secrets。Candidate 的正式签名不需要 Tag pattern：

- `SPEAKUP_ANDROID_KEYSTORE_BASE64`
- `SPEAKUP_ANDROID_KEY_ALIAS`
- `SPEAKUP_ANDROID_STORE_PASSWORD`
- `SPEAKUP_ANDROID_KEY_PASSWORD`
- `SPEAKUP_ANDROID_CERT_SHA256`

如果 `android-release-signing` 没有允许 `main`，由 `main` 的 `push` 触发的
Candidate 将无法进入签名 job 或取得 Environment Secrets。不能通过移出
Environment 或复制 Secrets 绕过该边界。

正式 Tag 还需要单独的 Tag ruleset 禁止更新和删除。Candidate Workflow 不创建
Tag；保留的 Stable Tag 校验会继续检查运行时本地/远端集合、Tag 格式、commit、
`main` 历史和版本单调性，但单次 Workflow 无法重建运行前已经被移动或删除的历史，
因此不能替代平台 Tag ruleset。

### 7.3 Staging 与 Production Deploy

Release Candidate Workflow 本身不直接执行部署。独立的 Staging Workflow 仅在
由官方 `main` 的 `push` 触发的 Candidate 成功后自动部署同一制品；成功后独立
Production Workflow 校验 Candidate 与 Staging 回执，并停在 GitHub Environment
等待人工批准。

Deploy Workflow 接收确定的 `version` 和 `environment`，读取 Release manifest
后部署现有制品，不重新构建。

Staging 主机 contract 先把配置和命令依赖拆成两个互不重叠的接口：

| 输入 | 职责与命令 | 调用身份所需访问 |
| --- | --- | --- |
| `staging-runtime.env` | 只包含 PostgreSQL、Portal、Server runtime 输入；`validate`、`deploy`、`verify`、`status`、`down` 只接受 `--runtime-env-file`，负责 manifest、Compose、migration、loopback health、锁和 receipt | 读取 manifest、runtime env 及其引用的 Server env，访问 Staging Docker/Compose runtime，并按命令访问锁和新 receipt；不需要 edge env、TLS key、htpasswd、ACME 或 public root |
| `staging-edge.env` | 只包含 TLS certificate/key、htpasswd、ACME root 和 public root；`render-nginx` 只接受 `--edge-env-file` 与 `--output` | 读取 edge env 及其引用的边缘文件、遍历边缘目录并创建指定输出；不需要 manifest、runtime/Server env、receipt、Docker 或 Compose |

`staging.speak-up.top` 与 `staging-api.speak-up.top` 固定为仓库 contract，不在两个
env 中重复配置。两类 env 使用精确 allowlist，未知、重复、交叉 key 或 ambient
environment 补值均 fail closed，错误不得输出 Secret 值。现有合并式 `staging.env`
迁移时必须拆出两个 mode `0400`/`0600`、由各自执行 UID 持有的文件，删除两个旧
hostname key，并分别替换为 `--runtime-env-file` 和 `--edge-env-file`；旧
`--env-file` 明确失败，不提供兼容 fallback。

这次拆分只消除 runtime 命令读取 edge Secret 的功能依赖，不构成主机级强制隔离。
当前 runtime 路径仍会调用 Docker Compose；原始 rootful Docker socket、`docker`
group、无限制 `sudo` 或 root SSH 都具有主机 root 级影响面，未来 CI 不得获得这些
能力。受限 broker/forced-command、独立 Staging daemon 或更强 VM/主机边界、专用
身份和资源限制属于后续独立任务。#1001 对应 PR 不创建 Workflow、主机用户、ACL、
sudoers、systemd service 或 broker，也不授权 CI 连接服务器。

Staging 自动部署并验证：

- Portal、后端和隔离数据库启动成功。
- `/health`、`/readyz` 与公网 HTTPS 正常。
- migration 在隔离数据库成功。
- 对象存储、文本生成、ASR、TTS 和 OCR 使用受控测试账号完成有限真实冒烟。
- Staging APK 完成安装、启动和核心业务链路验收。

Staging APK 与 Production APK 来自同一 commit，但 API 基础地址不同，所以不应
声称它们是完全相同的二进制文件。Production APK 必须在进入 Staging 前完成正式
签名、计算 SHA-256 并冻结；生产部署不得重新签名或替换该文件。生产部署后只对
Release manifest 指向的同一 Production APK 完成安装、启动与生产 API 连通性检查，
检查通过后才能更新公开下载入口。

Production 使用 GitHub Environment：

- 必须由指定发布负责人批准。
- 当前单人发布阶段允许指定负责人批准自己发起的部署，但不能跳过显式批准；增加
  第二位发布负责人后启用禁止自审。
- 审批前不释放生产环境 SSH 凭证和 Secret。
- 同一时间只允许一个生产部署。
- 只允许官方 `main` 的已验证 Candidate 部署；Stable Tag 只在 Production 成功、
  APK 公开入口校验完成后由 Workflow 创建。

Production 部署成功后的发布顺序固定为：创建或复用 Release 草稿、上传并校验全部
正式资产、通过受限 broker 激活 APK 指针、公网校验 APK 和 changelog、最后发布
GitHub Release。仓库启用 immutable releases，`v*.*.*` Tag ruleset 禁止更新和
删除。Production job 仅在 Environment 批准后取得 `contents: write` 并创建新
Tag；已发布的历史 Tag 不能移动或删除。

同一 Production workflow run 的后续 attempt 可以复用已成功的相同部署回执，
只执行只读 verify，不重复备份、migration 或容器重启。不同 workflow run、不同
Candidate、不同 Staging 回执或不同 bundle 均 fail closed。

### 7.4 备份任务

日常备份不依赖 GitHub Actions，应由生产服务器 `systemd timer` 执行。GitHub
Workflow 只负责审计最近一次备份是否存在、是否过期以及恢复演练是否按期完成。

## 8. 服务器目标布局

以下为逻辑布局，不记录真实服务器地址：

```text
Nginx
  production portal domain -> Portal container
  production API domain    -> Go server container
  staging portal domain    -> Staging Portal container
  staging API domain       -> Staging Go server container
  /downloads/              -> Versioned APK directory

Docker
  production: portal, server, postgres
  staging:    portal, server, postgres
```

生产目录建议按职责拆分：

```text
/opt/speakup/
  deploy/        # versioned Compose and audited deployment scripts
  releases/      # deployment manifests, not application source builds
  downloads/     # versioned APK and checksums
  backups/       # local short-term backups

/etc/speakup/
  portal.env     # mode 600
  server.env     # mode 600
```

`server.env` 中的 `AGENT_RUN_LOOP_TIMEOUT` 是一次 Agent 工具循环的端到端预算，
与文本 Provider 单次请求超时保持独立。当前部署值为 `150s`，并且必须严格大于
当前启用的 `QIANWEN_TIMEOUT` 或 `QINIU_AI_TIMEOUT`，否则服务启动失败。

生产 Workflow 不应以 root 身份执行任意 SSH 命令。后续应建立受限 deploy 身份，
只允许执行审核过的部署入口。PostgreSQL、Go 端口和 Portal 内部端口不得直接暴露
公网，公网只开放必要的 SSH、HTTP 和 HTTPS 入口，并同时核对主机防火墙与云安全组。

## 9. Android APK 分发

Production APK 要求：

- 使用固定的正式签名证书。
- `versionName` 与 Release Tag 一致。
- `versionCode` 每次发布单调递增。
- API 使用稳定生产域名，不写服务器 IP 或临时端口。
- CI 验证 APK 签名、SHA-256、包名、版本和 arm64 架构。
- 正式签名私钥只存在于密码管理器与受保护的 GitHub Environment Secret。

APK 不打入 Portal Docker 镜像。严格的公开 bundle 由同一次 Release Candidate
运行产生的 v1 `release-manifest.json`、Production APK 和人工明确给出的规范 UTC
发布时间生成。构建器核对文件名、大小、APK SHA-256、签名证书指纹与 Android
契约，并输出以下固定目录：

```text
/downloads/android/v0.1.1/speakup-v0.1.1-production-arm64.apk
/downloads/android/v0.1.1/speakup-v0.1.1-production-arm64.apk.sha256
/downloads/android/v0.1.1/release.json
/downloads/android/release.json
```

版本目录不覆盖。Portal 同源读取当前 `release.json`，只有严格校验元数据后才展示
唯一的版本化 APK 链接、版本、文件大小、发布日期、兼容范围、APK SHA-256 和签名
证书 SHA-256。缺失元数据表示准备中；网络、JSON 或协议错误表示不可用，均不猜测
下载地址。Production 先发布并验证版本目录，只有生产冒烟成功后才原子切换当前
`release.json`；不存在 `latest.apk` 或可变 APK 路径。

首版是网页直接分发 APK，用户需要允许浏览器安装未知来源应用。它不提供 iOS
安装，也不等同于 Google Play 发布。

网站分发不代表可以忽略 Android 开发者验证。首发 APK 可以先按当前直接侧载规则
发布，但应使用稳定 package name 和正式签名证书，并登记 Android Developer
Console 账号、身份验证和 package name 注册负责人，为 2027 年全球执行做准备。

## 10. 数据保存与备份

### 10.1 PostgreSQL

保存核心业务数据，包括用户、训练计划、会话、作答、报告、学习档案、任务状态
和必要的使用记录。数据目录使用独立持久卷，不进入应用容器可写层。

### 10.2 对象存储

录音、简历、图片等大文件存入对象存储。PostgreSQL 只保存文件身份、归属、状态
和受控访问信息，不保存大文件正文。

### 10.3 Portal SQLite

Portal 报名和访问事件继续使用独立 SQLite。它与产品核心业务边界不同，首版无需
为了技术统一立即迁移，但必须建立一致性备份和恢复验证。

### 10.4 备份规则

- Portal SQLite 每日执行在线一致性备份。
- PostgreSQL 每日执行逻辑备份。
- 每次生产部署前额外生成一份带版本标识的备份。
- 本机保留短期副本，另存一份到独立对象存储或其他服务器。
- 备份生成 SHA-256，记录时间、来源、schema version 和大小。
- 定期从备份恢复到隔离环境并执行完整性检查；只确认“文件存在”不算恢复验证。
- Secret 由密码管理器备份，不与数据库备份混放。

具体保留周期在第一次正式发布前根据数据量和合规要求确认，不在未测量数据增长
速度时猜测固定天数。

## 11. Migration 与回滚

### 11.1 数据库变更

- 已发布 migration 不修改，只新增后续 migration。
- 使用 expand/contract：先新增兼容结构，再迁移读写，最后在后续版本删除旧结构。
- 新后端上线时必须允许上一版 App 在兼容窗口内继续使用。
- Production migration 失败时停止部署，不切换应用版本。
- 日常应用回滚不自动执行 down migration。

### 11.2 Portal 与后端

- 部署前记录当前镜像 digest。
- 新版本先通过容器和 HTTP 健康检查，再切换流量。
- 健康检查失败时恢复上一版镜像 digest。
- 回滚后再次执行公网与核心 API 冒烟。

### 11.3 APK

- 问题版本尚未公开时，不激活它的当前 `release.json`。
- 已公开时立即停止分发问题 APK并恢复上一版本下载入口。
- 已经安装新版的 Android 用户通常不能安装更低 versionCode；需要发布更高
  versionCode 的 hotfix，不能把“恢复下载链接”误认为完成客户端回滚。

## 12. API 连通性与主链路验证

自动检查分为四层：

1. 进程存活：`/health`。
2. 数据库和必要依赖就绪：`/readyz`。
3. Nginx、HTTPS、域名、CORS 与公网 API 正常。
4. 安装 APK 后通过真实 API 完成核心用户链路。

首发核心链路：

```text
安装并打开 App
  -> 建立用户身份
  -> 创建一种练习
  -> 发送文字或语音
  -> 收到 AI 回复
  -> 播放 TTS
  -> 结束练习
  -> 生成报告
  -> 在复盘中打开报告
```

第三方验证至少覆盖：

- 文本生成。
- 实时或录音 ASR。
- TTS。
- 对象存储上传、下载和受控 URL。
- 启用时的 OCR。

真实第三方冒烟使用专门测试身份和预算，不在每个普通 PR 中调用。确定性单元、契约
和集成测试阻塞 PR；真实供应商测试阻塞 Release Candidate 的上线批准。

## 13. 观测与 API 使用统计

### 13.1 HTTP 服务指标

- 按路由模板统计请求数，不记录带用户 ID 的实际路径。
- 2xx、4xx、5xx 与超时数。
- P50、P95、P99 延迟。
- 当前并发、数据库连接和任务队列状态。
- 容器重启、CPU、内存、磁盘和证书有效期。

### 13.2 第三方 API 指标

按 provider 和能力统计：

- 调用次数、成功、失败、超时和重试。
- 延迟。
- Token、音频时长、存储量和流量等可计费单位。
- 稳定错误类别与估算费用。

### 13.3 产品指标

- Portal APK 下载次数。
- App 首次启动和版本分布。
- 活跃用户、创建练习、完成练习。
- 报告创建数、成功率和耗时。
- 主链路各阶段的失败数量。

运维指标适合进入指标系统，业务事件进入产品数据存储，结构化日志用于按 request ID
诊断。三者不得混成一张业务表。日志和指标不得记录后台密码、签名密钥、完整简历、
完整语音、用户对话正文或联系方式。

首版至少需要：

- 结构化 JSON 日志与 request ID。
- 服务 `/metrics` 或等价指标出口，不暴露公网。
- API 成功率、P95、第三方错误率、报告成功率、磁盘与备份新鲜度看板。
- 5xx、数据库不可用、备份过期、磁盘不足和证书临期告警。

## 14. 上线审核清单

### 14.1 自动门禁

- [ ] PR Gate 全绿。
- [ ] Candidate 来自官方 `main` 精确 SHA，version、versionCode、SHA 和 run ID
      已记录。
- [ ] Production 成功后，Stable Tag、commit 和版本一致且不可修改。
- [ ] Portal/后端镜像 digest 已记录。
- [ ] APK 签名、版本、架构和 SHA-256 正确。
- [ ] Staging migration、健康检查和真实供应商冒烟通过。
- [ ] 最近备份有效，且恢复演练未过期。

### 14.2 人工验收

- [ ] Portal 桌面和移动端可访问。
- [ ] Staging APK 可安装、启动和重新打开。
- [ ] 核心文字、语音、报告和复盘链路通过。
- [ ] 已知限制和回滚版本明确。
- [ ] 发布负责人明确点击批准 Production。

### 14.3 生产后验证

- [ ] Portal、后台、API、health 和 readiness 正常。
- [ ] Production APK 可下载、校验和安装。
- [ ] 核心 API 和一条受控业务链路通过。
- [ ] APK 公开入口已更新到正确版本。
- [ ] 错误率、延迟、报告成功率和资源无异常。
- [ ] GitHub Release 与 Deployment 记录完整。

## 15. 后续实施顺序

每一项应建立单一 Issue，从最新 `upstream/dev` 创建短分支，并单独验收。

### Phase 0：保护现网与收敛来源

1. [x] #816：移除 Portal Codex Sites 耦合，只保留服务器部署能力。
2. [x] #829：完成 Portal SQLite 首次一致性备份、校验和与恢复验证。
3. [ ] 把当前线上 Portal 镜像 digest、数据卷和部署清单纳入版本化发布记录，
   不删除旧容器或数据。

### Phase 1：完成自动质量检查

4. [x] #825：将 Portal build/test/lint/Docker build 纳入 Quality。
5. [x] #830：覆盖率基线缺失时 fail closed，并稳定输出 `Quality gate`。
6. [~] #831：GitHub Required Status Check 严格模式按当前决定暂缓；在重新批准前
   不作为 Android 首发实施项。

### Phase 2：建立可追溯制品

7. 为 Go 后端增加最小生产多阶段 Dockerfile 和健康检查。
8. 建立 Android 正式签名、版本规则、Staging/Production API 配置和 APK 校验。
9. 建立不依赖 Stable Tag 的内部 Release Candidate Workflow、OCI 镜像、APK、
   SHA-256 与 manifest。
10. 为 Portal 增加版本化 APK 下载展示，但在生产验证前不公开新版本。

### Phase 3：测试环境与生产部署

11. 在现有服务器建立隔离的 Staging 容器、数据库、域名和访问保护。
12. 建立 Deploy Workflow：固定制品、部署锁、migration、健康检查与 GitHub
    Environment 审核。
13. 建立生产 Nginx API/APK 路由、受限 deploy 身份和主机/云防火墙规则。
14. 完成一次从 Staging 验收到 Production 批准、部署、公开 APK 和应用回滚的演练。

### Phase 4：数据保护与观测

15. 建立 SQLite/PostgreSQL 定时备份、异地副本、备份新鲜度检查和恢复演练。
16. 建立日志轮转、HTTP/第三方 API/业务指标、基础看板与告警。
17. 建立旧 App API 兼容窗口、最低支持版本和 Android hotfix 流程。

## 16. 后续阶段仍待确认

- Android Developer Console 使用个人或组织账号，以及 package name 注册负责人。
- Staging 使用独立子域名还是仅内部可达的预览入口。
- Production 审批人及应急替代审批人。
- PostgreSQL 首版是否与应用同机运行。
- 备份异地目标、保留周期和恢复演练频率。
- 指标与告警采用自建方案还是托管服务。
- Portal 报名数据的保留、导出与删除规则。

这些选择会影响凭证、DNS、成本和数据责任，应在对应实施 Issue 开始前确认，不能由
Workflow 猜测或静默采用默认值。

## 17. 官方依据

- [GitHub Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [GitHub Reviewing deployments](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/review-deployments)
- [GitHub secure use of self-hosted runners](https://docs.github.com/en/actions/reference/security/secure-use)
- [Docker Engine security and daemon attack surface](https://docs.docker.com/engine/security/)
- [Docker group root-level privileges](https://docs.docker.com/engine/install/linux-postinstall/)
- [Android Sign your app](https://developer.android.com/studio/publish/app-signing)
- [Android Version your app](https://developer.android.com/studio/publish/versioning)
- [Android Release through a website](https://developer.android.com/studio/publish#publishing-website)
- [Android Developer Verification FAQ](https://developer.android.com/developer-verification/guides/faq)
- [Android Developer Console full distribution](https://developer.android.com/developer-verification/guides/full-distribution)
