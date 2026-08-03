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
