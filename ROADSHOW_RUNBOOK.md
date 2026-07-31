# SpeakUp 路演保底启动说明

这份说明用于在 Android 真机上恢复并启动 2026-08-01 路演保底版本。仓库只保存配置模板；真实密钥继续放在根目录 `.env`，不得提交。

## 固定版本

- 分支：`codex/roadshow-backup-2026-08-01`
- 远端：个人 Fork 的 `origin`
- 查看当前精确提交：`git rev-parse HEAD`

恢复版本：

```shell
git fetch origin
git switch codex/roadshow-backup-2026-08-01
git pull --ff-only origin codex/roadshow-backup-2026-08-01
```

## 路演前检查

1. 根目录存在真实 `.env`，并已配置数据库、千问、讯飞、OSS 和 SpatialReal。
2. Docker Desktop 已启动。
3. Android 手机已解锁并允许 USB 调试，`adb devices` 显示状态为 `device`。
4. 保持网络稳定；关闭手机 VPN、系统省电和会限制后台网络的设置。

## 一条命令启动

在仓库根目录执行：

```shell
ANDROID_DEVICE_ID=10AG4Y1XHR007JT make dev-android
```

脚本会依次完成：启动 PostgreSQL、执行数据库迁移、构建并启动真实后端、建立 USB 端口映射、构建并安装 Android App。看到 App 首页后保持该终端运行；按 `q` 才会退出并关闭本地后端。

如果更换手机，先执行 `adb devices`，将命令中的设备 ID 换成新设备 ID。

## 手动保底启动

自动脚本异常时，分别打开两个终端。

终端 A：

```shell
docker compose -p xe3-esl -f compose.yaml up -d --wait postgres
set -a
source .env
set +a
cd server
go run ./cmd/migrate up
go run ./cmd/server
```

终端 B：

```shell
adb reverse tcp:8080 tcp:8080
cd mobile
flutter pub get --enforce-lockfile
flutter build apk --debug --target-platform android-arm64
adb install -r build/app/outputs/flutter-apk/app-debug.apk
adb shell am start -S -n com.xengineer.speakup/.MainActivity
```

后端就绪检查：

```shell
curl --fail http://127.0.0.1:8080/readyz
```

## 语音链路的正确表现

1. 停止录音后先显示“正在处理语音”。
2. ASR 完成并持久化后，用户的正式语音消息立即显示。
3. 页面随后显示“SpeakUp 正在回复”，不会等待 Agent 完成后再一起出现。
4. Agent 回复出现并自动朗读；评分与纠错继续异步生成。

如遇外部服务余额不足或网络异常，不要临时更换代码分支；先在对应控制台确认额度，再重新启动本分支。
