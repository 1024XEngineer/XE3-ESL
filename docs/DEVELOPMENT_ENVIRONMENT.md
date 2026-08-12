# SpeakUp 新机器开发环境与启动指南

本文档用于在一台新的 macOS 开发机上，从零搭建 XE3-ESL / SpeakUp 的完整本地开发环境，并启动 Flutter 客户端、Go 后端和 PostgreSQL。文档以仓库当前 `dev` 分支为准。

> 安全提示：不要复制、提交或通过聊天发送真实 `.env`、API Key、Access Key、数据库密码、测试账号密码或私有语音文件。新机器上的密钥应通过团队批准的密码管理器或密钥管理服务交付。

## 1. 系统组成

项目由以下部分组成：

| 目录 | 技术 | 用途 |
| --- | --- | --- |
| `mobile/` | Flutter / Dart | iOS 与 Android 客户端 |
| `server/` | Go | HTTP、WebSocket、业务逻辑、供应商集成 |
| `api/` | Node.js | OpenAPI、JSON Schema 和契约校验 |
| `compose.yaml` | Docker Compose | 本地 PostgreSQL + pgvector |
| `tools/` | zsh / shell | iOS 模拟器、Android 真机和专项验证入口 |

本地 App 并不直接访问云数据库。标准开发链路为：

```text
iOS Simulator / Android 真机
              ↓ HTTP / WebSocket
       本机 Go Server
              ↓
 Docker PostgreSQL + pgvector
              ↓
千问 / 讯飞 / OSS / OCR / SpatialWalk（按配置启用）
```

## 2. 版本基线

### 2.1 仓库要求

| 工具或平台 | 要求 | 来源 |
| --- | --- | --- |
| Flutter | `3.44.6` | README 与 CI |
| Dart | `3.12.2` | `mobile/pubspec.yaml` / Flutter 3.44.6 内置 |
| Go | `1.26.5` | `server/go.mod` toolchain 与 CI |
| Node.js | `>=22.12.0` | `api/package.json`；CI 使用 `24.18.0` |
| npm | 与所选 Node.js 配套 | 使用 `package-lock.json` 和 `npm ci` |
| Java | `17` | Android Gradle 配置 |
| Gradle | `9.1.0` | Gradle Wrapper 自动下载 |
| iOS Deployment Target | `16.0` | `mobile/ios/Podfile` |
| Android minSdk | `24` | `mobile/android/app/build.gradle.kts` |
| Android ABI | `arm64-v8a` | AvatarKit 与 Android 构建配置 |
| PostgreSQL | 容器固定为 pgvector PostgreSQL 18 Bookworm 镜像 | `compose.yaml` |

### 2.2 已验证的 macOS 参考组合

以下是编写本文档时实际验证过的组合，不代表所有工具的最低版本：

```text
macOS 26.6 (x86_64)
Xcode 26.6
Flutter 3.44.6
Dart 3.12.2
Go 1.26.5
Node.js 24.18.0
npm 11.16.0
Docker Engine 29.6.2
Docker Compose 5.3.1
CocoaPods 1.17.0
Git 2.50.1
```

如果使用不同版本，应至少满足上表“仓库要求”，并在提交代码前执行第 11 节的完整检查。

## 3. 新机器准备清单

准备以下账号、权限和硬件：

- GitHub 账号，并有权读取官方仓库及创建 Fork/PR。
- 一台可运行 Xcode 和 Docker Desktop 的 macOS 机器。
- iOS 开发：完整 Xcode 与至少一个 iPhone Simulator Runtime。
- Android 开发：Android Studio、Android SDK、USB 数据线和一台已授权的 `arm64-v8a` Android 真机。
- 能访问 GitHub、Flutter/Go/npm 依赖源、Docker Registry 和项目使用的云供应商。
- 至少一个有效的 DashScope API Key。当前后端启动必须加载文本生成、Embedding、ASR 和 TTS 配置。
- `REVIEW_HISTORY_CURSOR_SIGNING_KEY`。它必须是恰好 32 字节随机值的无填充 base64url 编码。
- 若要使用可选能力，还需讯飞、SpatialWalk、阿里云 OSS 或七牛 Kodo、PaddleOCR 对应凭证。

建议预留至少 30 GB 可用磁盘空间，用于 Xcode Simulator、Flutter、Docker 镜像、Gradle 和 CocoaPods 缓存。

### 3.1 操作系统支持边界

| 开发目标 | macOS | Windows | Linux |
| --- | --- | --- | --- |
| Go Server / API 契约 / PostgreSQL | 支持 | 工具链本身可运行，但仓库一键脚本不是 PowerShell | 支持基础工具链，但需自行适配脚本 |
| Android arm64 真机 | 仓库脚本正式支持 | 可手动运行 Flutter/adb | 可手动运行 Flutter/adb |
| iOS Simulator / iOS 构建 | 唯一支持平台 | 不支持 | 不支持 |
| 仓库 `make dev-*` 脚本 | 以 macOS `zsh` 为目标 | 不直接支持 | 需要兼容性验证 |

如果“另一台机器”需要完整覆盖 iOS 与 Android，必须选择 macOS。Windows 或 Linux 只能搭建后端、契约检查和 Android 子集，本文档中的 Xcode、Simulator、CocoaPods 与 iOS 步骤不适用。

## 4. 安装基础工具

### 4.1 Xcode Command Line Tools

从 App Store 或 Apple Developer 下载并安装完整 Xcode，然后执行：

```shell
xcode-select --install
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -runFirstLaunch
```

接受许可证并验证：

```shell
xcodebuild -version
xcrun simctl list devices
git --version
```

### 4.2 Homebrew（推荐）

如果新机器没有 Homebrew，按照 [brew.sh](https://brew.sh/) 的官方说明安装。安装完成后，根据 Homebrew 输出把 `brew shellenv` 加入 `~/.zprofile`，然后重新打开终端。

常用辅助工具：

```shell
brew install go node@24 cocoapods gh
```

`curl`、`rsync`、`lsof`、`zsh` 和 `make` 通常随 macOS/Xcode Command Line Tools 提供。验证：

```shell
command -v curl rsync lsof zsh make
```

### 4.3 Flutter 3.44.6

推荐使用版本管理器（例如 FVM），或者把 Flutter 3.44.6 SDK 解压到固定目录。无论采用哪种方式，终端中的 `flutter` 必须指向 3.44.6。

验证：

```shell
flutter --version
dart --version
flutter doctor -v
```

期望看到 Flutter `3.44.6` 和 Dart `3.12.2`。根据 `flutter doctor -v` 修复 iOS、Android 或 CocoaPods 项目，直到目标平台不再有阻断错误。

### 4.4 Go 1.26.5

安装后验证：

```shell
go version
```

期望为 `go1.26.5`。`server/go.mod` 同时声明了 `go 1.26.0` 和 `toolchain go1.26.5`。

### 4.5 Node.js 与 npm

推荐使用 Node.js `24.18.0`，它与 CI 一致；最低不得低于 `22.12.0`。

```shell
node --version
npm --version
```

如果用 Homebrew 安装了 keg-only 的 `node@24`，按 Homebrew 提示把它加入 `PATH`。

### 4.6 Docker Desktop

安装并启动 Docker Desktop，等待状态显示 Engine 已运行，然后验证：

```shell
docker --version
docker compose version
docker info
```

项目使用 Compose v2 子命令 `docker compose`，不是旧版 `docker-compose`。

## 5. 配置 iOS 开发环境

1. 打开 Xcode。
2. 在 Xcode Settings → Platforms 中安装至少一个 iOS Simulator Runtime。
3. 打开 Simulator：

   ```shell
   open -a Simulator
   ```

4. 确认至少一个 iPhone 为 Booted：

   ```shell
   xcrun simctl list devices booted
   ```

5. 验证 CocoaPods：

   ```shell
   pod --version
   ```

项目的 iOS 最低版本是 16.0。`make dev-ios-simulator` 必须使用已经启动的 iPhone Simulator；如果同时启动多个模拟器，可设置 `IOS_SIMULATOR_ID`。

### iOS 模拟器的数字人限制

iOS Simulator 入口会把 `mobile/` 复制到临时目录，并仅在临时副本中用 stub 替换 AvatarKit，同时传入：

```text
SPEAKUP_AVATAR_ENABLED=false
```

因此模拟器使用静态画面及语音/文字降级链路。它不会修改正式工作区中的 AvatarKit 依赖，也不影响 iOS 真机或 Android 真机。

## 6. 配置 Android 开发环境

### 6.1 安装 Android Studio 与 SDK

安装 Android Studio，并通过 SDK Manager 安装：

- Android SDK Platform（满足当前 Flutter `compileSdkVersion`）。
- Android SDK Platform-Tools（提供 `adb`）。
- Android SDK Build-Tools。
- Android SDK Command-line Tools。
- Android Studio 内置 JDK，或独立 JDK 17。

确保 `adb` 在 `PATH` 中。常见配置如下，实际路径以 Android Studio 为准：

```shell
export ANDROID_HOME="$HOME/Library/Android/sdk"
export PATH="$ANDROID_HOME/platform-tools:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
```

不要把上述示例中的 `$HOME` 替换为项目路径。

验证：

```shell
adb version
java -version
flutter doctor -v
```

Java 应为 17。如果终端找不到 Java，但 Android Studio 已安装，可以按本机 Android Studio 的 JBR 路径设置 `JAVA_HOME`。

### 6.2 准备 Android 真机

1. 在手机上启用“开发者选项”和“USB 调试”。
2. 使用 USB 数据线连接 Mac。
3. 解锁手机并允许这台电脑调试。
4. 检查设备：

   ```shell
   adb devices
   adb shell getprop ro.product.cpu.abilist
   ```

状态必须为 `device`，ABI 列表必须包含 `arm64-v8a`。当前脚本明确排除 Android Emulator，并且 AvatarKit 不支持本项目的 x86/x86_64 Android 构建。

## 7. 获取仓库并配置远端

仓库协作约定：

- `upstream` 必须是官方仓库：`https://github.com/1024XEngineer/XE3-ESL.git`
- `origin` 必须是你自己的 Fork。
- 禁止直接推送官方 `dev` 或 `main`。

推荐流程：

```shell
git clone https://github.com/<你的 GitHub 用户名>/XE3-ESL.git
cd XE3-ESL
git remote add upstream https://github.com/1024XEngineer/XE3-ESL.git
git remote set-url origin https://github.com/<你的 GitHub 用户名>/XE3-ESL.git
git fetch --all --prune
git switch dev
git merge --ff-only upstream/dev
git push origin dev
```

检查远端：

```shell
git remote -v
```

如果使用 SSH，可以把 `origin` 改成个人 Fork 的 SSH 地址，但 `upstream` 仍应指向官方主仓。建议同时安装并登录 GitHub CLI：

```shell
gh auth login
gh auth status
```

## 8. 安装项目依赖

在仓库根目录执行：

```shell
cd mobile
flutter pub get --enforce-lockfile
cd ../server
go mod download
cd ../api
npm ci
cd ..
```

说明：

- Flutter 使用 `mobile/pubspec.lock`。
- Go 使用 `server/go.mod` 和 `server/go.sum`。
- API 校验使用 `api/package-lock.json`，必须使用 `npm ci`，不要用 `npm install` 随意改锁文件。
- Gradle Wrapper 会在首次 Android 构建时下载 Gradle 9.1.0。
- CocoaPods 会在首次 iOS 构建时安装或解析 Pods。

## 9. 配置 `.env`

### 9.1 创建本地文件

```shell
cp .env.example .env
chmod 600 .env
```

`.env` 位于仓库根目录，已被 Git 忽略，不得提交。Go Server 会向上查找并加载 `.env`；已有的进程环境变量优先于文件中的同名值。

不要把旧机器的 `.env` 通过 Git、邮件或聊天明文传输。安全迁移方式是：

1. 在团队密码管理器中保存每项密钥。
2. 在新机器复制 `.env.example`。
3. 从密码管理器逐项填入新文件。
4. 对支持轮换的供应商优先创建新机器专用密钥。
5. 用 `git status --short` 确认 `.env` 未被跟踪。

### 9.2 后端能够启动的必要配置

本地启动不是纯 mock。即使关闭数字人、对象存储和 OCR，仍需要以下配置：

| 配置组 | 必填项 | 说明 |
| --- | --- | --- |
| PostgreSQL | `DATABASE_URL` | 本地 Compose 默认值见下方 |
| 文本生成 | `TEXT_GENERATION_PROVIDER` 及所选供应商的 URL、3 个模型名、API Key | `qianwen` 或 `qiniu` |
| Embedding | `EMBEDDING_PROVIDER=qianwen`、Embedding URL/模型/维度、`DASHSCOPE_API_KEY` | 当前维度必须为 1024 |
| ASR | `SPEECH_RECOGNITION_PROVIDER=qianwen`、ASR URL/模型、`DASHSCOPE_API_KEY` | 实时模型固定为 `fun-asr-realtime` |
| 录音 ASR | `QIANWEN_ASR_RECORDED_MODEL` | 当前固定为 `fun-asr-flash-2026-06-15` |
| TTS | `SPEECH_SYNTHESIS_PROVIDER=qianwen`、TTS URL/模型/音色/语言、`DASHSCOPE_API_KEY` | 用于问题和回复语音 |
| Review 历史游标 | `REVIEW_HISTORY_CURSOR_SIGNING_KEY` | 必须编码恰好 32 个随机字节 |

本地数据库默认配置：

```dotenv
POSTGRES_DB=xe3_esl
POSTGRES_USER=xe3_esl
POSTGRES_PASSWORD=local-development-only
POSTGRES_PORT=5432
DATABASE_URL=postgres://xe3_esl:local-development-only@127.0.0.1:5432/xe3_esl?sslmode=disable
MIGRATION_ENV=local
```

为 `REVIEW_HISTORY_CURSOR_SIGNING_KEY` 生成新密钥：

```shell
openssl rand -base64 32 | tr '+/' '-_' | tr -d '='
```

把输出安全填入：

```dotenv
REVIEW_HISTORY_CURSOR_SIGNING_KEY=<生成的值>
```

此值应在同一环境的重启和多副本之间保持稳定；轮换会使尚未过期的 Review 历史游标失效。

开发脚本会使用 shell 的 `source` 加载 `.env`。因此它必须是可信的 shell 变量赋值文件：不要复制来源不明的 `.env`，不要在其中加入命令、命令替换或其他可执行逻辑；包含空格或 shell 特殊字符的普通配置值应正确引用。密钥本身不得含空白或控制字符。

### 9.3 推荐的本地最小功能开关

若只需运行主要对话、雅思和场景练习，可先关闭以下可选能力：

```dotenv
SPATIUS_ENABLED=0
OSS_ENABLED=0
RESUME_OCR_ENABLED=0
QIANWEN_LIVE_TEST=0
QINIU_LLM_LIVE_TEST=0
QIANWEN_ASR_LIVE_TEST=0
QIANWEN_TTS_LIVE_TEST=0
XFYUN_ISE_LIVE_TEST=0
OSS_LIVE_TEST=0
KODO_LIVE_TEST=0
RESUME_OCR_LIVE_TEST=0
```

“最小”仅表示关闭可选集成；文本生成、Embedding、ASR、TTS 和数据库依然是真实依赖。

### 9.4 环境变量完整分类

以下以 `.env.example` 为权威默认模板。变量留空是否允许，取决于对应功能是否启用。

#### 服务与数据库

| 变量 | 用途 |
| --- | --- |
| `POSTGRES_DB` | Compose 创建的数据库名 |
| `POSTGRES_USER` | Compose 数据库用户 |
| `POSTGRES_PASSWORD` | Compose 数据库密码 |
| `POSTGRES_PORT` | 映射到本机的 PostgreSQL 端口 |
| `DATABASE_URL` | Go Server 和迁移命令使用的连接串 |
| `MIGRATION_ENV` | 破坏性迁移保护；本地使用 `local` |
| `SERVER_HOST` | Server 监听地址，默认 `0.0.0.0`；开发脚本强制 `127.0.0.1` |
| `SERVER_PORT` | Server 端口，默认 `8080`；开发脚本使用 `SPEAKUP_DEV_PORT` |
| `LOG_LEVEL` | 日志级别，默认 `info` |
| `TRUSTED_PROXY_CIDRS` | 生产代理 CIDR，逗号分隔；普通本地开发留空 |
| `TRUSTED_PROXY_HEADER` | 可信代理头；普通本地开发留空 |

#### 文本生成与 Embedding

| 变量 | 用途 |
| --- | --- |
| `TEXT_GENERATION_PROVIDER` | `qianwen` 或 `qiniu` |
| `AGENT_CONTEXT_MAX_CHARACTERS` | Agent 上下文字符上限；模板为 12000 |
| `DASHSCOPE_API_KEY` | 千问文本、Embedding、ASR、TTS 共用密钥 |
| `QIANWEN_BASE_URL` | 千问兼容 OpenAI 文本接口 |
| `QIANWEN_MODEL` | Agent 模型 |
| `QIANWEN_EVALUATION_MODEL` | 评估模型 |
| `QIANWEN_SPEECH_FEEDBACK_MODEL` | 口语反馈模型 |
| `QIANWEN_TIMEOUT` | 文本请求超时，Go duration |
| `QIANWEN_MAX_OUTPUT_TOKENS` | 最大输出 token |
| `QINIU_AI_BASE_URL` | 七牛 MaaS 文本接口 |
| `QINIU_AI_MODEL` | 七牛 Agent 模型 |
| `QINIU_AI_EVALUATION_MODEL` | 七牛评估模型 |
| `QINIU_AI_SPEECH_FEEDBACK_MODEL` | 七牛口语反馈模型 |
| `QINIU_AI_TIMEOUT` | 七牛请求超时 |
| `QINIU_AI_MAX_OUTPUT_TOKENS` | 七牛最大输出 token |
| `QINIU_AI_API_KEY` | 七牛 MaaS API Key |
| `EMBEDDING_PROVIDER` | 当前只支持 `qianwen` |
| `QIANWEN_EMBEDDING_BASE_URL` | Embedding API 地址 |
| `QIANWEN_EMBEDDING_MODEL` | Embedding 模型 |
| `QIANWEN_EMBEDDING_DIMENSIONS` | 当前必须为 1024 |
| `QIANWEN_EMBEDDING_TIMEOUT` | Embedding 超时 |

如果 `TEXT_GENERATION_PROVIDER=qianwen`，`DASHSCOPE_API_KEY` 同时满足文本、Embedding 和语音依赖。如果选择 `qiniu` 做文本生成，仍然需要 `DASHSCOPE_API_KEY` 为 Embedding、ASR 和 TTS 服务。

#### ASR、TTS 与临时音频

| 变量 | 用途 |
| --- | --- |
| `SPEECH_RECOGNITION_PROVIDER` | 当前只支持 `qianwen` |
| `QIANWEN_ASR_BASE_URL` | ASR 地址 |
| `QIANWEN_ASR_MODEL` | 实时 ASR，必须为 `fun-asr-realtime` |
| `QIANWEN_ASR_TIMEOUT` | 实时 ASR 超时，至少 150s |
| `QIANWEN_ASR_RECORDED_MODEL` | 录音 ASR，必须为模板指定模型 |
| `QIANWEN_ASR_RECORDED_TIMEOUT` | 录音 ASR 超时 |
| `SPEECH_SYNTHESIS_PROVIDER` | 当前只支持 `qianwen` |
| `QIANWEN_TTS_BASE_URL` | TTS 地址 |
| `QIANWEN_TTS_MODEL` | TTS 模型 |
| `QIANWEN_TTS_VOICE` | 音色 |
| `QIANWEN_TTS_LANGUAGE` | 语言提示，模板为 `en` |
| `QIANWEN_TTS_TIMEOUT` | TTS 超时 |
| `QIANWEN_TTS_TEMP_DIRECTORY` | 可选临时目录；留空使用系统目录 |
| `VOICE_TEMP_AUDIO_LIFETIME` | 未确认音频寿命 |
| `VOICE_TEMP_AUDIO_MAX_ITEMS` | 进程级最大项目数 |
| `VOICE_TEMP_AUDIO_MAX_BYTES` | 进程级最大字节数 |
| `VOICE_TEMP_AUDIO_MAX_ITEMS_PER_USER` | 每用户最大项目数 |
| `VOICE_TEMP_AUDIO_MAX_BYTES_PER_USER` | 每用户最大字节数 |
| `VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES` | 全局并发录音数 |
| `VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER` | 每用户并发录音数 |
| `VOICE_AUDIO_READ_TIMEOUT` | 实时音频读取超时 |
| `VOICE_RECORDED_AUDIO_READ_TIMEOUT` | 录音文件读取超时 |

#### 讯飞 ISE（可选）

`APPID`、`APIKey`、`APISecret` 三项全部留空表示不启用声学评分。只填写部分凭证会导致配置错误。完整启用时还可配置 `XFYUN_ISE_ENDPOINT` 和 `XFYUN_ISE_TIMEOUT`。

真实 ISE 测试另外使用 `XFYUN_ISE_LIVE_TEST`、`XFYUN_ISE_LIVE_TEST_AUDIO` 和 `XFYUN_ISE_LIVE_TEST_TEXT`。音频与参考文本必须是非敏感测试数据。

#### SpatialWalk 数字人（可选）

`SPATIUS_ENABLED=0` 时，其他 SpatialWalk 变量可留空。启用时必须填写：

- `SPATIUS_REGION`
- 与 Region 匹配的 `SPATIUS_CONSOLE_BASE_URL`
- `SPATIUS_APP_ID`
- `SPATIUS_AVATAR_ID`
- `SPATIUS_API_KEY`
- `SPATIUS_TOKEN_TTL`（1m 到 10m）
- `SPATIUS_TIMEOUT`（大于 0 且不超过 30s）

iOS Simulator 启动脚本始终关闭客户端数字人；需要验证真实 AvatarKit 时使用支持的 iOS 真机或 arm64 Android 真机，并确保服务端也启用 SpatialWalk。

#### 对象存储（可选）

`OSS_ENABLED=0` 时远端存储凭证可留空。本地 iOS 模拟器若因代理无法访问 OSS，只能在当前工作区本地 `.env` 中设置：

```dotenv
OSS_ENABLED=0
RESUME_OCR_ENABLED=0
```

不得提交这两个本地改动，也不得改成 mock。

阿里云 OSS 启用时设置 `OBJECT_STORAGE_PROVIDER=aliyun_oss`，并配置：

| 变量 | 用途 |
| --- | --- |
| `OSS_REGION` | OSS Region |
| `OSS_ENDPOINT` | 无凭证、Query 或 Fragment 的 HTTPS Origin |
| `OSS_BUCKET` | Bucket 名称 |
| `OSS_CREDENTIALS_PROVIDER` | `environment` 或 `ecs_role` |
| `OSS_RAM_ROLE_NAME` | 使用 ECS Role 时的可选角色名 |
| `OSS_ACCESS_KEY_ID` | 环境凭证 Access Key ID |
| `OSS_ACCESS_KEY_SECRET` | 环境凭证 Secret |
| `OSS_SESSION_TOKEN` | 临时环境凭证 Token，可选 |
| `OSS_AUDIO_PREFIX` | 必须保持 `audio/v1` |
| `OSS_IMAGE_PREFIX` | 必须保持 `image/v1` |
| `OSS_RESUME_PREFIX` | 必须保持 `resume/v1` |
| `OSS_SIGNED_URL_TTL` | 签名 URL TTL，必须大于 0 且不超过 2m |

本地机器通常使用 `OSS_CREDENTIALS_PROVIDER=environment`；生产 ECS 可使用 `ecs_role`。

七牛 Kodo 启用时设置 `OBJECT_STORAGE_PROVIDER=qiniu_kodo`，填写 `QINIU_KODO_S3_REGION`、对应的 `QINIU_KODO_S3_ENDPOINT`、`QINIU_KODO_S3_BUCKET`、`QINIU_ACCESS_KEY`、`QINIU_SECRET_KEY`，并在控制台启用服务端加密后设置 `QINIU_KODO_SERVER_SIDE_ENCRYPTION=1`。

#### 简历 OCR（可选）

`RESUME_OCR_ENABLED=0` 时无需 Token。启用时必须填写 `PADDLEOCR_ACCESS_TOKEN`，并可配置 `PADDLEOCR_BASE_URL` 与 `RESUME_OCR_TIMEOUT`。

#### Live Test 变量（默认全部关闭）

这些变量只用于显式真实供应商测试，可能发送数据并产生费用：

- `QIANWEN_LIVE_TEST`
- `QINIU_LLM_LIVE_TEST`
- `QIANWEN_ASR_LIVE_TEST` / `QIANWEN_ASR_LIVE_TEST_AUDIO`
- `QIANWEN_TTS_LIVE_TEST`
- `XFYUN_ISE_LIVE_TEST` / 音频与参考文本变量
- `OSS_LIVE_TEST`
- `KODO_LIVE_TEST`
- `RESUME_OCR_LIVE_TEST` / `RESUME_OCR_LIVE_TEST_PDF`

普通开发和 CI 应保持为 `0`。部分 Live Test Make target 故意不加载 `.env`，运行前必须在当前 shell 显式 `export` 所需变量。

### 9.5 开发脚本与客户端编译变量

这些变量通常不写入 `.env`，而是在命令前设置或通过 `--dart-define` 传入：

| 变量 | 使用位置 | 用途 |
| --- | --- | --- |
| `SPEAKUP_DEV_PORT` | iOS/Android 开发脚本 | 本地后端端口，默认 18080 |
| `IOS_SIMULATOR_ID` | iOS 脚本 | 多模拟器时指定设备 UUID |
| `ANDROID_DEVICE_ID` | Android 脚本 | 多真机时指定 adb 序列号 |
| `SPEAKUP_SIMULATOR_WORK_DIR` | iOS 脚本 | 覆盖临时移动端副本目录；一般不设置 |
| `SPEAKUP_API_BASE_URL` | Dart compile-time define | 客户端 HTTP/WebSocket 后端基地址 |
| `SPEAKUP_AVATAR_ENABLED` | Dart compile-time define | 是否装配客户端 Avatar；模拟器脚本固定为 false |

`SPEAKUP_API_BASE_URL` 和 `SPEAKUP_AVATAR_ENABLED` 是编译时值，不是运行时读取的 `.env`。不得通过 Dart define 放入服务端密钥。

### 9.6 测试专用变量

| 变量 | 用途 |
| --- | --- |
| `TEST_DATABASE_URL` | 启用 PostgreSQL 集成测试；必须指向可丢弃测试库 |
| `SPEAKUP_E2E_EMAIL` | 已有 E2E 测试账号 |
| `SPEAKUP_E2E_PASSWORD` | E2E 测试账号密码，不得提交 |
| `SPEAKUP_E2E_WAV_BASE64` | 本地持有的非敏感英语 WAV 的 Base64，仅用于显式 E2E |
| `SPEAKUP_E2E_CAPTURE_HOLD_MS` | E2E 录音保持时长 |
| `SPEAKUP_E2E_VALIDATE_AUDIO_MEDIA` | 是否验证音频媒体链路 |

服务端还有少量专项 Live Test 变量：

- `QIANWEN_IMAGE_LIVE_TEST`
- `QIANWEN_MEMORY_LIVE_TEST`
- `QIANWEN_RESUME_LIVE_TEST`
- `QIANWEN_RESUME_LIVE_TEST_PDF`
- `QIANWEN_RESUME_EXPECT_GPA`
- `QIANWEN_RESUME_EXPECT_AWARDS`
- `XFYUN_ISE_LIVE_TEST_CATEGORY`
- `XFYUN_ISE_LIVE_TEST_TOPIC_TITLE`

它们不属于普通启动配置，也未列入 `.env.example`；只有运行对应测试文件时才应按测试源码显式设置，默认保持未设置。

## 10. 首次启动

### 10.1 iOS Simulator（推荐的 macOS 首次启动方式）

先打开并启动一个 iPhone Simulator，然后在仓库根目录执行：

```shell
make dev-ios-simulator
```

脚本会自动完成：

1. 检查 `curl`、`docker`、`flutter`、`go`、`lsof`、`rsync`、`xcrun`。
2. 加载根目录 `.env`。
3. 确认 iPhone Simulator 已启动。
4. 将 `mobile/` 同步到系统临时目录。
5. 仅在临时目录覆盖 AvatarKit。
6. 启动 PostgreSQL 并等待健康检查。
7. 执行所有数据库迁移。
8. 确认 IELTS 题库已发布。
9. 构建并启动真实 Go 后端。
10. 等待 `GET /readyz` 成功。
11. 启动 Flutter App，客户端数字人明确禁用。

如果 18080 已占用：

```shell
SPEAKUP_DEV_PORT=18081 make dev-ios-simulator
```

如果有多个模拟器：

```shell
xcrun simctl list devices booted
IOS_SIMULATOR_ID=<设备 UUID> make dev-ios-simulator
```

运行过程中按 `r` 热重载，按 `R` 热重启，按 `q` 结束 App 与本次脚本启动的后端。PostgreSQL 容器会继续运行。

### 10.2 Android arm64 真机

连接并授权设备后执行：

```shell
make dev-android
```

脚本会启动数据库与后端，通过 `adb reverse` 将手机的本地端口映射到 Mac，构建 `android-arm64` Debug APK、安装、启动并 attach Flutter。

多设备时：

```shell
adb devices
ANDROID_DEVICE_ID=<设备序列号> make dev-android
```

自定义端口：

```shell
SPEAKUP_DEV_PORT=18081 make dev-android
```

### 10.3 单独启动后端（高级调试）

如果不使用封装脚本：

```shell
docker compose -p xe3-esl -f compose.yaml up -d --wait postgres
cd server
go run ./cmd/migrate up
go run ./cmd/ielts-bank-import \
  -file data/ielts/2026-05-08-mainland.json \
  -publish-if-empty
SERVER_HOST=127.0.0.1 SERVER_PORT=18080 go run ./cmd/server
```

另一个终端检查：

```shell
curl --fail http://127.0.0.1:18080/readyz
```

手动运行 Flutter 时必须传入本机后端 URL：

```shell
cd mobile
flutter run \
  --dart-define=SPEAKUP_API_BASE_URL=http://127.0.0.1:18080 \
  --dart-define=SPEAKUP_AVATAR_ENABLED=false
```

Android 真机直接访问 `127.0.0.1` 指向手机自己，因此手动运行前需要 `adb reverse tcp:18080 tcp:18080`，或使用 Mac 在局域网中的可达地址并调整 Server 监听与防火墙。

### 10.4 真机签名与 Release 构建边界

- iOS 真机需要 Apple Developer Team、有效证书、Provisioning Profile，并在 Xcode Runner Target 中选择正确 Team。凭证只存放在 Keychain/Xcode，不得提交。
- Android 当前 `release` Build Type 仍使用 Debug signing config，目的是让本地 `flutter run --release` 可执行；它不是可发布到应用商店的生产签名方案。
- 正式 Android 发布必须另行配置受保护的 keystore、CI Secret 和签名流程，不能把 keystore 或密码提交到仓库。
- 本文的验收目标是本地开发启动，不代表已完成 App Store / Play Store 发布环境。

## 11. 安装完成后的验证

### 11.1 快速版本检查

```shell
flutter --version
dart --version
go version
node --version
npm --version
docker compose version
xcodebuild -version
pod --version
git remote -v
```

Android 还需：

```shell
java -version
adb devices
```

### 11.2 完整质量检查

在仓库根目录执行：

```shell
make check
```

它依次覆盖：

- Flutter 锁文件依赖、格式、静态分析和测试。
- Go 格式、`go vet` 和全部单元测试。
- OpenAPI、WebSocket、JSON Schema 与契约 Fixtures。
- 确定性主流程 Smoke Test。

也可分开执行：

```shell
make check-flutter
make check-go
make check-api
make check-smoke
```

### 11.3 数据库集成测试

部分 Go 集成测试只有设置 `TEST_DATABASE_URL` 才运行。请使用专门的可丢弃测试数据库，不要指向开发或生产数据：

```shell
export TEST_DATABASE_URL='postgres://<test-user>:<test-password>@127.0.0.1:<test-port>/<test-db>?sslmode=disable'
cd server
go test -count=1 ./...
```

## 12. 日常开发流程

每项改动都应：

1. 创建范围单一、验收清楚且关联当前 Milestone 的 Issue。
2. 保证主工作区没有需要保留的未提交改动；有并行工作时使用独立 worktree。
3. 从最新 `upstream/dev` 创建短分支：

   ```shell
   git fetch upstream
   git switch dev
   git merge --ff-only upstream/dev
   git switch -c codex/<issue-number>-<short-name> upstream/dev
   ```

4. 只修改当前 Issue 范围。
5. Commit 使用 `<type>(<scope>): <subject>`。
6. 推送个人 Fork 并向官方 `dev` 创建 PR。
7. PR 包含功能描述、实现思路、可复现测试和关联 Issue。
8. 检查 CI 与 Review；未全部通过或仍有未解决意见时不能宣称完成。

## 13. 数据、缓存与重置

### 查看 PostgreSQL 状态

```shell
docker compose -p xe3-esl -f compose.yaml ps
docker compose -p xe3-esl -f compose.yaml logs postgres
```

### 停止容器但保留数据

```shell
docker compose -p xe3-esl -f compose.yaml down
```

项目数据库保存在命名 volume 中，普通 `down` 不删除数据。

### 删除本地数据库数据

只有确认不需要本地数据时才执行：

```shell
docker compose -p xe3-esl -f compose.yaml down --volumes
```

此操作会删除本项目 Compose volume 中的本地 PostgreSQL 数据，无法从仓库恢复个人测试账号和练习记录。

### 常见构建缓存

- Flutter：`mobile/.dart_tool/`、`mobile/build/`
- iOS Pods：`mobile/ios/Pods/`
- Android/Gradle：用户 Gradle 缓存与 `mobile/build/`
- iOS 模拟器临时副本：`${TMPDIR}/xe3-ios-simulator-dev-cache`

这些均不应提交。遇到问题时优先查看错误并进行最小范围清理，不要直接删除整个工作区或模拟器数据。

## 14. 常见故障排查

### `缺少 .env，无法启动真实后端`

确认仓库根目录存在 `.env`：

```shell
pwd
ls -la .env .env.example
```

### Server 启动后立即退出

开发脚本的后端日志位于临时运行目录。最常见原因是：

- `DASHSCOPE_API_KEY` 为空或包含空格/控制字符。
- 文本模型、评估模型或口语反馈模型为空。
- `REVIEW_HISTORY_CURSOR_SIGNING_KEY` 格式不正确。
- ASR 模型名或超时不满足当前约束。
- 只填写了部分讯飞 ISE 凭证。
- 开启 OSS、OCR 或 SpatialWalk，但没有填写完整配置。
- PostgreSQL 端口被占用或 Docker 未启动。

可以手动运行后端直接查看错误：

```shell
cd server
SERVER_HOST=127.0.0.1 SERVER_PORT=18080 go run ./cmd/server
```

### `端口 18080 已被占用`

查看占用：

```shell
lsof -nP -iTCP:18080 -sTCP:LISTEN
```

如果是需要保留的其他任务，使用独立端口：

```shell
SPEAKUP_DEV_PORT=18081 make dev-ios-simulator
```

### 没有已启动的 iPhone Simulator

```shell
open -a Simulator
xcrun simctl list devices booted
```

等待 Simulator 完成启动后再运行 Make target。

### CocoaPods 或 Xcode 构建失败

先执行：

```shell
flutter doctor -v
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -runFirstLaunch
cd mobile
flutter pub get
cd ios
pod install --repo-update
```

不要提交自动生成的本地配置或无关 Pods 产物。

### Android 找不到设备或显示 unauthorized

```shell
adb kill-server
adb start-server
adb devices
```

解锁手机，撤销并重新授权 USB 调试。脚本只接受真实设备，不接受 `emulator-*`。

### Android 报 Java 不可用

项目需要 Java 17。使用 Android Studio 内置 JBR 或安装 JDK 17，并确认：

```shell
java -version
flutter doctor -v
```

### Android 设备 ABI 不支持

```shell
adb shell getprop ro.product.cpu.abilist
```

必须包含 `arm64-v8a`。

### iOS 模拟器无法访问 OSS

仅修改本机、当前工作区的 `.env`：

```dotenv
OSS_ENABLED=0
RESUME_OCR_ENABLED=0
```

不得提交 `.env`，也不得用 mock 替代真实后端。

### Flutter 依赖版本不一致

确认 Flutter 版本为 3.44.6，然后：

```shell
cd mobile
flutter pub get --enforce-lockfile
```

如果命令要求修改锁文件，先确认当前分支是否已同步最新 `upstream/dev`，不要直接提交意外的依赖升级。

### npm 契约检查失败

```shell
cd api
rm -rf node_modules
npm ci
npm run check
```

`node_modules` 是可再生成缓存，不得提交。

## 15. 密钥与生产安全

- `.env` 只能用于本地开发，不是生产密钥管理方案。
- 不要把密钥写进 Dart `--dart-define`；客户端编译参数可被提取。
- 云供应商密钥只由 Go Server 使用。
- 不要在日志、Issue、PR、截图、测试 Fixture 或 Commit 中暴露密钥。
- Live Test 可能产生费用并向外部服务发送文件或音频，必须显式选择启用。
- 测试音频和简历 PDF 必须是非敏感数据。
- 对象存储生产环境优先使用短期角色凭证，不使用长期 Access Key。
- 怀疑密钥泄漏时，先在供应商控制台撤销/轮换，再清理 Git 历史和相关日志；仅删除当前文件不足以消除泄漏。

## 16. 最终验收清单

新机器满足以下条件即可认为环境搭建完成：

- [ ] `origin` 指向个人 Fork，`upstream` 指向官方仓库。
- [ ] 当前 `dev` 与 `upstream/dev` 同步。
- [ ] Flutter 3.44.6 / Dart 3.12.2 可用。
- [ ] Go 1.26.5 可用。
- [ ] Node.js ≥22.12.0，且 `npm ci` 成功。
- [ ] Docker Engine 与 `docker compose` 可用。
- [ ] `.env` 已从模板创建，权限为 600，未被 Git 跟踪。
- [ ] 必要的 DashScope 与 Review 历史游标配置已填写。
- [ ] PostgreSQL 容器健康，迁移成功。
- [ ] `curl http://127.0.0.1:<端口>/readyz` 成功。
- [ ] iOS Simulator 或 arm64 Android 真机能打开 SpeakUp。
- [ ] 注册/登录、文字对话、语音输入、场景练习和 IELTS 页面按所配能力工作。
- [ ] `make check` 全部通过。

配置默认值或供应商约束发生变化时，应以仓库根目录的 `.env.example`、`Makefile`、`compose.yaml`、对应平台脚本和服务端配置校验代码为最终权威，并同步更新本文档。
