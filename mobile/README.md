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
