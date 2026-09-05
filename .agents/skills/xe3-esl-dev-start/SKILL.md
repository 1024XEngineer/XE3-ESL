---
name: xe3-esl-dev-start
description: 启动并排查 XE3-ESL 的本地 PostgreSQL、Go 后端和 Android Flutter 真机调试；当用户要启动本地全栈、连接 Android 真机、恢复 ADB 反向代理，或定位注册、对话、语音的本地网络与提供商错误时使用。
metadata:
  short-description: 启动并排查 XE3-ESL Android 本地全栈
---

# XE3-ESL Android 本地启动

用于当前仓库的本地开发调试，不用于生产部署。默认使用 PowerShell，并假定 Android 设备已通过 USB 开启调试。

## 启动原则

- 保留用户现有进程、容器、工作区修改和 `.env`；先检查再启动，不重复叠加同一服务。
- 不输出、复制到命令行参数或提交任何密钥。后端会向上查找最近的 `.env`，但已有进程环境变量优先，必须防止父进程中的旧变量遮蔽项目配置。
- 本项目固定使用 Compose 项目名 `xe3-esl`、PostgreSQL 主机端口 `55432`、后端地址 `127.0.0.1:18080`。不要停止或删除占用其他端口的无关服务。
- Android 全栈联调默认包含录音消息上传，因此必须启用对象存储。纯文字、无 OSS 的精简联调只能在用户明确要求时使用，并提前说明录音草稿、持久化录音、图片和文件上传不可用。
- 简历 OCR、SpatialWalk 和讯飞声学评分仍是按需能力；用户未要求调试时仅对本次后端子进程禁用，不修改 `.env`。
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

先确认仓库根目录存在 `.env`，只报告关键配置为 `missing`、`empty`、`set` 或 `shadowed`，绝不打印值。特别检查 `DASHSCOPE_API_KEY` 和所有 `OSS_*` 配置：如果父进程值与 `.env` 不同，后端会使用父进程值。

默认录音联调要求 `.env` 中的 `OSS_REGION`、`OSS_ENDPOINT`、`OSS_BUCKET` 和 `OSS_CREDENTIALS_PROVIDER` 均为 `set`；使用 environment credentials 时，访问凭据也必须为 `set`。缺少依赖时停止并报告，不能静默关闭 OSS 后继续启动。

对本次后端子进程移除下列外部覆盖，让项目 `.env` 成为详细配置的来源；`Remove-Item Env:` 只改变当前 PowerShell 会话及随后创建的子进程，不修改 `.env`、Windows 用户级或系统级环境变量：

```powershell
@(
  'TEXT_GENERATION_PROVIDER',
  'DASHSCOPE_API_KEY',
  'QIANWEN_BASE_URL',
  'QIANWEN_MODEL',
  'QIANWEN_SPEECH_FEEDBACK_MODEL',
  'OSS_ENABLED',
  'OSS_REGION',
  'OSS_ENDPOINT',
  'OSS_BUCKET',
  'OSS_AUDIO_PREFIX',
  'OSS_IMAGE_PREFIX',
  'OSS_RESUME_PREFIX',
  'OSS_SIGNED_URL_TTL',
  'OSS_CREDENTIALS_PROVIDER',
  'OSS_RAM_ROLE_NAME',
  'OSS_ACCESS_KEY_ID',
  'OSS_ACCESS_KEY_SECRET',
  'OSS_SESSION_TOKEN'
) | ForEach-Object {
  Remove-Item -Path "Env:$_" -ErrorAction SilentlyContinue
}
```

再设置仅属于本次本地进程的确定性覆盖：

```powershell
$env:DATABASE_URL='postgres://xe3_esl:local-development-only@127.0.0.1:55432/xe3_esl?sslmode=disable'
$env:QIANWEN_EVALUATION_MODEL='qwen3.7-plus-2026-05-26'
$env:QIANWEN_ASR_RECORDED_MODEL='fun-asr-flash-2026-06-15'
$env:QIANWEN_ASR_RECORDED_TIMEOUT='150s'
$env:QIANWEN_ASR_TIMEOUT='150s'
$env:OSS_ENABLED='1'
$env:RESUME_OCR_ENABLED='0'
$env:SPATIUS_ENABLED='false'
$env:APPID=''
$env:APIKey=''
$env:APISecret=''
$env:SERVER_HOST='127.0.0.1'
$env:SERVER_PORT='18080'
Set-Location '<REPO_ROOT>\server'
go run ./cmd/server
```

若 `.env` 使用了非默认 PostgreSQL 用户、密码或数据库名，不要把密钥打印出来；应从 `.env` 安全构造本次 `DATABASE_URL`，仅将端口替换为 `55432`。用户明确调试 OCR、SpatialWalk 或声学评分时，不要应用对应的禁用覆盖，并验证其依赖已配置。

只有用户明确要求纯文字、无 OSS 的精简联调时，才将本次子进程改为 `$env:OSS_ENABLED='0'`；必须同时说明该模式不支持录音草稿、持久化录音、图片和文件上传。

等待日志出现 `server started`，再验证：

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18080/readyz
```

必须返回 `200`。`readyz` 只证明基础服务就绪，不证明模型密钥或录音上传路由有效。

默认录音联调还要用不含真实令牌的 WebSocket 握手确认语音草稿路由已注册：

```powershell
$probeThreadId = '00000000-0000-4000-8000-000000000001'
curl.exe --max-time 5 -sS -D - -o NUL `
  -H 'Connection: Upgrade' `
  -H 'Upgrade: websocket' `
  -H 'Sec-WebSocket-Version: 13' `
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' `
  -H 'Sec-WebSocket-Protocol: speakup.voice-input.v1' `
  "http://127.0.0.1:18080/v1/agent-threads/$probeThreadId/voice-drafts/realtime"
```

无令牌探测返回 `401 Unauthorized` 是预期结果，证明路由存在且认证层生效；`404 Not Found` 表示语音草稿服务未装配，应先检查 OSS 是否实际启用以及媒体依赖是否启动，不能归因于手机网络。

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

按链路分层判断，不要把所有错误都归因于“手机没连后端”。

### 基础服务

- `docker compose -p xe3-esl -f compose.yaml ps`：PostgreSQL 为 `healthy`，端口为 `127.0.0.1:55432->5432/tcp`。
- `GET http://127.0.0.1:18080/readyz`：返回 `200`。
- `adb -s <DEVICE_ID> reverse --list`：包含 `tcp:18080 tcp:18080`。
- `adb -s <DEVICE_ID> shell pidof com.xengineer.speakup`：返回进程号。

### 手机到后端

让用户在 App 中触发一个请求，同时读取后端日志。出现对应 `/v1/...` 请求即证明手机已连接本地后端；没有请求时再检查 API base URL、reverse 和设备状态。不要用真实密码做诊断，也不要自动创建测试账号。

### 录音上传与语音草稿

App 显示“录音尚未上传，请检查网络后重试”只是客户端对 WebSocket 失败的通用分类，不足以证明网络断开。文字 HTTP 请求成功而录音失败时，按以下顺序判断：

1. 探测 `/v1/agent-threads/<UUID>/voice-drafts/realtime`；`404` 表示 OSS 或语音草稿服务未装配，`401` 表示路由正常存在。
2. 用户触发重试时读取后端日志；完全没有对应 WebSocket 请求才检查 API base URL、ADB reverse 和设备状态。
3. 查询是否新建 `agent_voice_drafts`；没有记录说明失败发生在草稿创建或 ASR 之前，不能归因于 ASR 模型。
4. 已有草稿且为 `failed` 或日志出现提供商错误时，再检查 ASR 认证、模型、超时和提供商响应。

### 模型与语音提供商

`readyz=200` 不能证明 Qwen、ASR 或 TTS 可用。对话联调需由用户发送一条最小测试消息，或在用户明确同意可能计费的测试后再主动调用。验收应看到：

- `agent.run.received` 后最终出现 `agent.run.completed`；
- 不出现 `agent.run.generation_failed`；
- `detail: text generation failed: authentication` 表示提供商凭证被拒绝，不是手机或 ADB 网络错误；
- 语音链路需要进一步出现 `agent.assistant_speech.completed`。

App 显示“回复中断了”时，先按时间关联后端日志：收到请求但出现 `provider_unavailable`、`authentication` 或超时，应处理提供商配置；后端完全没有请求时，才处理 ADB、本地 API 地址或 App 连接。

## 结束本地会话

正常退出 Flutter、ADB reverse 守护和 Go 会话。需要停止数据库但保留数据时：

```powershell
$env:POSTGRES_PORT='55432'
docker compose -p xe3-esl -f compose.yaml stop postgres
```

临时盘符不再需要时，确认它仍映射到本仓库后再执行 `subst X: /d`。除非用户明确要求清空本地数据，否则不要使用 `down -v`。
