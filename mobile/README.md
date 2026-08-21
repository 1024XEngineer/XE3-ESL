# SpeakUp Mobile

SpeakUp 的 Flutter Android/iOS 正式客户端。

## 本地检查

```shell
flutter pub get
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test
```

## Android 真机调试

在仓库根目录执行：

```shell
make dev-android
```

命令会启动 PostgreSQL 和本地后端、配置 USB 端口映射，并在已授权的
Android 真机上运行 App。连接多台设备时，可通过
`ANDROID_DEVICE_ID=<设备 ID> make dev-android` 指定设备。
本地调试后端默认使用 `18080` 端口，可通过
`SPEAKUP_DEV_PORT=<端口> make dev-android` 覆盖。

## Android 网站分发

首发网站分发基线为：

- applicationId：`com.xengineer.speakup`
- versionName/versionCode：`0.1.0` / `1`（Release Tag：`v0.1.0`）
- ABI：仅 `arm64-v8a`
- staging API 发布契约：`https://staging-api.speak-up.top`
- production API 发布契约：`https://api.speak-up.top`

两个域名是首发构建契约；当前仓库没有 DNS 或后端部署就绪证据，解析与服务可用性
仍待发布 Owner 在分发前单独验收。本仓库只校验构建注入值与契约一致，不声称服务
已经上线。

Android 使用 `staging`、`production` 两个 product flavor。release 构建根据
flavor 注入固定 API；传入不一致的 `SPEAKUP_API_BASE_URL` 会在 App 启动前失败，
未知 Android release flavor 也不会回落到 localhost。未使用 Android flavor 的 iOS
release 必须显式注入经校验的 HTTPS、非 loopback API；开发构建仍可显式覆盖本地
API。

### 正式签名边界

仓库不生成、保存或传递生产私钥。Owner 需要在批准的密码管理器中创建并备份
网站分发使用的 app signing key，记录其公开证书 SHA-256，并在本地受控会话或
CI Secret 中提供以下环境变量：

- `SPEAKUP_ANDROID_KEYSTORE_PATH`
- `SPEAKUP_ANDROID_KEY_ALIAS`
- `SPEAKUP_ANDROID_STORE_PASSWORD`
- `SPEAKUP_ANDROID_KEY_PASSWORD`
- `SPEAKUP_ANDROID_CERT_SHA256`（公开证书指纹，仅用于产物校验）

前四项有任一缺失、空值或 keystore 文件不可读时，release Gradle 配置立即失败；
release 不再使用 debug signing。不要把 keystore、密码、`key.properties`、终端
输出或 CI Secret 提交到仓库。首次网站发布后必须长期保管同一 app signing key，
否则后续 APK 无法作为更新安装。

### 构建与校验

在已经安全注入上述环境变量的会话中，从仓库根目录执行：

```shell
make build-android-release-staging

make build-android-release-production
```

两个 build 入口都会在构建后立即执行完整校验；如果只需重新检查现有产物：

```shell
make verify-android-release-staging
make verify-android-release-production
```

校验入口使用 Android SDK 的 `aapt` 和 `apksigner`，从 `mobile/pubspec.yaml` 读取
唯一版本来源，并检查包名、版本、唯一 ABI、APK 签名有效性、拒绝 Android Debug
证书，以及签名证书 SHA-256 是否与 Owner 批准值一致，同时输出 APK 文件
SHA-256。缺少正式私钥时只能运行 fail-closed 守卫，不能用 debug key 生成
“正式”APK：

```shell
make check-android-release-guard
```

参考：[Flutter Android flavors](https://docs.flutter.dev/deployment/flavors)、
[Flutter Android 发布](https://docs.flutter.dev/deployment/android)、
[Android App Signing](https://developer.android.com/studio/publish/app-signing)、
[apksigner](https://developer.android.com/tools/apksigner)。

## iOS 模拟器调试

AvatarKit 当前不提供 Intel Mac 所需的 x86_64 模拟器二进制。先启动一个
iPhone Simulator，再在仓库根目录执行：

```shell
make dev-ios-simulator
```

该命令会把当前移动端源码复制到临时构建目录、启动 PostgreSQL 与本地后端，
并仅在临时副本中把数字人明确标记为不支持，使情景对话沿用纯语音/文字
降级链路。正式工作区与真实 AvatarKit 依赖始终不变；Android 与 iOS 真机构建
不受影响。连接多个模拟器时，可通过
`IOS_SIMULATOR_ID=<设备 ID> make dev-ios-simulator` 指定设备。
