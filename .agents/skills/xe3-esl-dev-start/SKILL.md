---
name: xe3-esl-dev-start
description: 启动并排查 XE3-ESL 的本地 PostgreSQL、Go 后端和 Android Flutter 真机调试；当用户要启动本地全栈、连接 Android 真机、恢复 ADB 反向代理，或定位注册、对话、语音、上传与数字人的本地网络及提供商错误时使用。
metadata:
  short-description: 启动并排查 XE3-ESL Android 本地全栈
---

# XE3-ESL Android 本地启动

用于当前仓库的本地开发调试，不用于生产部署。默认使用 PowerShell，并假定 Android 设备已通过 USB 开启调试。

## 启动原则

- 保留用户现有进程、容器、工作区修改和 `.env`；先检查再启动，不重复叠加同一服务。
- 不输出、复制到命令行参数或提交任何密钥。后端会向上查找最近的 `.env`，但已有进程环境变量优先，必须防止父进程中的旧变量遮蔽项目配置。
- 默认启动项目 `.env` 中配置的完整能力，包括对象存储、简历 OCR、SpatialReal 数字人和讯飞声学评分；不得为了简化启动而默认注入禁用开关或清空凭证。
- 只有用户明确要求精简联调或隔离某个外部提供商时，才对当前后端子进程临时禁用对应能力。应用覆盖前说明被禁用的能力、受影响的移动端入口，以及覆盖只对本次进程生效。
- 移动端暴露的能力必须与后端启动配置一致。联调范围变化并涉及此前禁用的能力时，先按完整配置重启后端；不要把未注册路由导致的 `404` 误判为手机网络问题。
- 本项目固定使用 Compose 项目名 `xe3-esl`、PostgreSQL 主机端口 `55432`、后端地址 `127.0.0.1:18080`。不要停止或删除占用其他端口的无关服务。
- 保持 Go、Flutter 及可选的 ADB reverse 守护会话运行，并记录会话，方便读取日志和正常停止。

## 1. 启动 PostgreSQL

在仓库根目录确认 Docker 引擎可用。若 Docker Desktop 未运行，可启动并等待 `docker info` 成功；不要反复创建容器。

```powershell
$env:POSTGRES_PORT='55432'
docker compose -p xe3-esl -f compose.yaml up -d postgres
docker compose -p xe3-esl -f compose.yaml ps
```

只有 PostgreSQL 显示 `healthy` 才继续。若 `55432` 被占用，先报告占用进程并选择是否复用正确的项目容器；不要终止无关进程。除非用户明确允许删除本地数据，否则禁止 `down -v`。

## 2. 启动 Go 后端

Go module 位于 `server`，必须从 `<REPO_ROOT>\server` 运行 `go run ./cmd/server`。

### 完整能力预检

先确认仓库根目录存在 `.env`。默认按完整联调启动，只报告以下状态，绝不打印配置值：

- 普通开关：`enabled`、`disabled`、`missing` 或 `shadowed`；
- 凭证和其他敏感配置：`set`、`empty`、`missing` 或 `shadowed`。

至少检查：

- 文本、ASR 与 TTS：`TEXT_GENERATION_PROVIDER`、`DASHSCOPE_API_KEY` 及对应 Qwen 配置；
- 聊天录音、图片和简历文件：`OSS_ENABLED`、`OBJECT_STORAGE_PROVIDER`、Region、Endpoint、Bucket、Credentials Provider，以及所选凭证来源要求的 Access Key 或 RAM Role；
- 简历 OCR：`RESUME_OCR_ENABLED`、`PADDLEOCR_ACCESS_TOKEN`；
- 数字人：`SPATIUS_ENABLED`、`SPATIUS_REGION`、`SPATIUS_APP_ID`、`SPATIUS_API_KEY`；
- 声学评分：`APPID`、`APIKey`、`APISecret`。

开关已启用但必要配置缺失或部分填写时，不要静默禁用、伪造值或继续宣称完整能力已启动。报告具体能力不可用；仍能安全启动其他能力时可以继续，但验收必须保留该限制。

### 清除父进程覆盖

对本次后端子进程移除可能遮蔽项目 `.env` 的旧值，让 `.env` 成为应用配置来源；不要修改用户级或系统级环境变量：

```powershell
@(
  'TEXT_GENERATION_PROVIDER',
  'DASHSCOPE_API_KEY',
  'QIANWEN_BASE_URL',
  'QIANWEN_MODEL',
  'QIANWEN_SPEECH_FEEDBACK_MODEL',
  'OSS_ENABLED',
  'OBJECT_STORAGE_PROVIDER',
  'OSS_REGION',
  'OSS_ENDPOINT',
  'OSS_BUCKET',
  'OSS_CREDENTIALS_PROVIDER',
  'OSS_RAM_ROLE_NAME',
  'OSS_ACCESS_KEY_ID',
  'OSS_ACCESS_KEY_SECRET',
  'OSS_SESSION_TOKEN',
  'OSS_AUDIO_PREFIX',
  'OSS_IMAGE_PREFIX',
  'OSS_RESUME_PREFIX',
  'OSS_SIGNED_URL_TTL',
  'QINIU_KODO_S3_REGION',
  'QINIU_KODO_S3_ENDPOINT',
  'QINIU_KODO_S3_BUCKET',
  'RESUME_OCR_ENABLED',
  'PADDLEOCR_ACCESS_TOKEN',
  'PADDLEOCR_BASE_URL',
  'RESUME_OCR_TIMEOUT',
  'SPATIUS_ENABLED',
  'SPATIUS_REGION',
  'SPATIUS_CONSOLE_BASE_URL',
  'SPATIUS_APP_ID',
  'SPATIUS_API_KEY',
  'SPATIUS_TOKEN_TTL',
  'SPATIUS_TIMEOUT',
  'APPID',
  'APIKey',
  'APISecret',
  'XFYUN_ISE_ENDPOINT',
  'XFYUN_ISE_TIMEOUT'
) | ForEach-Object {
  Remove-Item -Path "Env:$_" -ErrorAction SilentlyContinue
}
```

再设置仅属于本次本地基础设施的确定性覆盖。不要在默认完整模式中设置 `OSS_ENABLED=0`、`RESUME_OCR_ENABLED=0`、`SPATIUS_ENABLED=false`，也不要清空任何凭证：

```powershell
$env:DATABASE_URL='postgres://xe3_esl:local-development-only@127.0.0.1:55432/xe3_esl?sslmode=disable'
$env:QIANWEN_EVALUATION_MODEL='qwen3.7-plus-2026-05-26'
$env:QIANWEN_ASR_RECORDED_MODEL='fun-asr-flash-2026-06-15'
$env:QIANWEN_ASR_RECORDED_TIMEOUT='150s'
$env:QIANWEN_ASR_TIMEOUT='150s'
$env:SERVER_HOST='127.0.0.1'
$env:SERVER_PORT='18080'
Set-Location '<REPO_ROOT>\server'
go run ./cmd/server
```

若 `.env` 使用了非默认 PostgreSQL 用户、密码或数据库名，不要把密钥打印出来；应从 `.env` 安全构造本次 `DATABASE_URL`，仅将端口替换为 `55432`。

### 用户明确要求的精简联调

只有用户明确要求禁用某项能力时，才在完成上述清理后为当前后端子进程设置对应覆盖：

```powershell
# 按用户明确要求选择，不要默认整段执行。
$env:OSS_ENABLED='0'          # 禁用聊天录音草稿、图片和持久化文件能力
$env:RESUME_OCR_ENABLED='0'   # 禁用简历 OCR
$env:SPATIUS_ENABLED='false'  # 禁用 SpatialReal 数字人 token
```

讯飞声学评分没有独立开关；仅在用户明确要求禁用时，才为当前后端子进程将 `APPID`、`APIKey`、`APISecret` 设置为空值，以显式遮蔽 `.env` 中的凭证，并明确说明影响。默认完整模式禁止应用这组空值覆盖。

一旦用户开始测试被禁用的移动端入口，先停止旧后端，恢复默认完整配置后重新启动；不得沿用旧进程继续排查。

### 启动验收

等待日志出现 `server started`，再验证：

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18080/readyz
```

必须返回 `200`。`readyz` 只证明基础服务就绪，不证明模型密钥有效或可选路由已经挂载。

根据联调范围，由用户在 App 中触发相应能力并观察后端路由。需要鉴权或 WebSocket Upgrade 的路由可以因缺少合法请求返回 `400` 或 `401`，但完整模式下不得在路由层返回 `<unmatched> 404`。重点关注：

- 聊天录音：`/v1/agent-threads/:thread_id/voice-drafts/realtime`；
- 实时输入转写：`/v1/agent-threads/:thread_id/voice-transcriptions/realtime`；
- 数字人 token：`/v1/practice-sessions/:practice_session_id/avatar-session-token`。

不要使用真实密码、伪造用户 ID、真实录音或伪造凭证执行启动探针。

## 3. 连接 Android 真机

优先使用本机常用路径 `E:\DevTools\Android\platform-tools\adb.exe` 与 `E:\DevTools\flutter\bin\flutter.bat`；不存在时再从 PATH 查找。确认恰好选择一个状态为 `device` 的目标，并记录设备 ID：

```powershell
& $adb devices -l
```

建立并验证反向代理：

```powershell
& $adb -s <DEVICE_ID> reverse tcp:18080 tcp:18080
& $adb -s <DEVICE_ID> reverse --list
```

输出必须包含 `tcp:18080 tcp:18080`。手机通过 USB reverse 访问电脑的 `127.0.0.1:18080`，无需处于同一 Wi-Fi。

### ADB 重连恢复

`adb reverse` 绑定当前设备 transport；USB 短暂断连、ADB daemon 重启、重新授权或 transport ID 变化都会清空映射。出现 `Lost connection to device`、App 突然网络失败，或设备曾反复重连时：

1. 再次执行 `adb devices -l`，等待同一设备恢复为 `device`。
2. 重新执行并核对 `adb reverse tcp:18080 tcp:18080`。
3. 确认 `adb shell pidof com.xengineer.speakup` 是否仍有进程。
4. App 仍运行时先尝试 `flutter attach -d <DEVICE_ID>`；附加失败再重新执行 `flutter run`，不要先 `flutter clean`。

对于已知不稳定的 USB 连接，可保留一个安静的守护会话：每 5 秒检查设备状态和 reverse 列表，仅在映射缺失时重新建立；设备离线时等待，不重启 ADB server，不产生重复日志。Go 或 Flutter 会话结束时停止守护会话。

## 4. 启动 Flutter staging debug

从 `mobile` 运行，并始终显式传入本地 API 地址，防止 staging debug 回退到线上地址：

```powershell
& $flutter run -d <DEVICE_ID> --flavor staging `
  '--dart-define=SPEAKUP_API_BASE_URL=http://127.0.0.1:18080'
```

仓库路径含中文时，先检查目标盘符未被其他路径占用，再临时映射到纯 ASCII 路径。`X:` 仅为示例：

```powershell
subst X: '<REPO_ROOT>'
Set-Location X:\mobile
& $flutter run -d <DEVICE_ID> --flavor staging `
  '--android-project-arg=android.overridePathCheck=true' `
  '--android-project-arg=kotlin.incremental=false' `
  '--dart-define=SPEAKUP_API_BASE_URL=http://127.0.0.1:18080'
```

不要在每次启动前执行 `flutter clean`。只有日志明确出现中文路径检查、Kotlin 增量缓存、盘符根目录不一致或无法通过普通重试恢复的构建缓存错误时，才执行一次 `flutter clean` 后重试。首次完整 Gradle 构建可能需要数分钟；后续优先使用 `r` 热重载、`R` 热重启或增量重跑。

安装时报 `INSTALL_FAILED_UPDATE_INCOMPATIBLE` 时，可让 Flutter 卸载同包名旧签名应用后重装，但先说明这会清除该 App 的本地数据。

## 5. 分层验收与排障

按链路分层判断，不要把所有错误都归因于“手机没连后端”，也不要仅凭 UI 中的“检查网络”文案判定网络故障。

### 基础服务

- `docker compose -p xe3-esl -f compose.yaml ps`：PostgreSQL 为 `healthy`，端口为 `127.0.0.1:55432->5432/tcp`。
- `GET http://127.0.0.1:18080/readyz`：返回 `200`。
- `adb -s <DEVICE_ID> reverse --list`：包含 `tcp:18080 tcp:18080`。
- `adb -s <DEVICE_ID> shell pidof com.xengineer.speakup`：返回进程号。

### 手机到后端

让用户在 App 中触发一个请求，同时按时间读取后端日志：

- 后端无请求：检查 API base URL、ADB 设备状态和 reverse；
- `<unmatched> 404`：检查客户端路径，以及对应能力是否因启动配置未注册；
- 路由命中但返回 `401` 或 `403`：检查登录与鉴权；
- 路由命中但返回 `503`：检查外部提供商、凭证、配额或能力依赖；
- WebSocket Upgrade 成功后中断：检查协议事件、ASR、超时和设备连接。

不要用真实密码做诊断，也不要自动创建测试账号。

### 模型、语音与数字人提供商

`readyz=200` 不能证明 Qwen、ASR、TTS、ISE 或 SpatialReal 可用。涉及可能计费的真实调用时，让用户主动在 App 中触发，或先取得用户明确同意。

对话验收应看到：

- `agent.run.received` 后最终出现 `agent.run.completed`；
- 不出现 `agent.run.generation_failed`；
- `detail: text generation failed: authentication` 表示提供商凭证被拒绝，不是手机或 ADB 网络错误；
- 语音链路进一步出现 `agent.assistant_speech.completed`。

数字人验收应按顺序看到：

- `avatar-session-token` 返回 `200`；
- `sdk_initialize.completed`；
- `device_support.completed reported_supported=true`；
- `asset_load.completed`；
- `surface.first_frame`；
- `connection.changed state=connected`。

首次角色模型下载可能超过 UI 的短等待时间。用户切出练习页会销毁 renderer 并取消加载；验收首次下载时提醒用户保持 App 前台，不能把页面降级提示本身当作最终 SDK 失败。

## 结束本地会话

正常退出 Flutter、ADB reverse 守护和 Go 会话。需要停止数据库但保留数据时：

```powershell
$env:POSTGRES_PORT='55432'
docker compose -p xe3-esl -f compose.yaml stop postgres
```

临时盘符不再需要时，确认它仍映射到本仓库后再执行 `subst X: /d`。除非用户明确要求清空本地数据，否则不要使用 `down -v`。
