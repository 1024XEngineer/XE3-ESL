# SpeakUp 门户网站

这是从 `Lq0412/ai-en-coach` 最新 `main` 分支中独立抽取的 SpeakUp 门户应用。
目录只包含门户页面、首批体验报名、访问事件、运营后台、数据存储、部署配置、运行时图片和对应测试；完整 SpeakUp 产品原型未包含在内。

## 环境要求

- Node.js >= 22.16.0
- npm

## 本地运行

```bash
npm install
npm run dev
```

默认首页为 `/`，Android 发布页为 `/download/android`，面向用户的更新日志为
`/changelog`，运营后台为 `/admin`。

Android 发布页只从同源 `/downloads/android/release.json` 读取当前正式版本。
`404` 显示“准备中”；网络、HTTP、JSON 或严格协议校验失败时显示“暂不可用”；只有
元数据完整有效时才展示其中的版本化 APK 地址与真实版本、大小、时间、兼容范围及
SHA-256。页面不会猜测下载地址，也不使用 `latest.apk`。APK 与发布元数据由宿主机
Nginx 提供，不放入 Portal Docker 镜像；发布操作见
[`deploy/android-download/README.md`](../deploy/android-download/README.md)。

更新日志同样先以 `/downloads/android/release.json` 为当前正式版本的唯一依据，再读取
`/release-notes/android/index.zh-CN.json` 中从当前版本开始的版本索引，并行加载对应的
`/release-notes/android/v<version>.zh-CN.json`。索引必须按版本从新到旧排列且不能重复；
当前版本说明的版本号和发布时间必须与正式版本完全一致，所有可见说明也必须按发布时间
递减。文件缺失、协议无效或信息不匹配时，页面会明确显示暂不可用，不会展示猜测或不完整
的版本历史。索引可以提前包含下一候选版本，但在对应 APK 正式激活前不会展示该版本。

索引是实际公开到 Production 的版本白名单；Git Tag、候选制品或 Staging 部署都不能单独
成为收录依据。可以为下一候选预置说明和索引项，但候选未激活或被跳过时，必须在激活后续
正式版本前移除。每次正式激活版本时，新增一份只描述该版本的中文说明文件，并把版本号
插入索引首位。内容只写用户可以感知的变化：新功能、体验优化、问题修复、兼容性变化和
确实需要告知的已知限制。不要写提交记录、重构、依赖升级、测试或部署过程；小修复也应
说明解决了什么用户问题，不使用“修复已知 bug”这类无法判断影响的占位文案。

如需在本地持久化报名和事件数据：

```bash
cp .env.example .env.local
```

然后在 `.env.local` 中设置安全的 `PORTAL_ADMIN_PASSWORD`。`PORTAL_SQLITE_PATH` 默认指向项目内被忽略的 `.wrangler/portal.sqlite`。

## 验证

```bash
npm test
npm run lint
```

`npm test` 会先完成生产构建，再验证门户渲染、报名和事件接口、后台鉴权、SQLite 持久化与反向代理来源处理。

## 来源

具体来源版本和抽取范围见 [SOURCE_VERSION.md](SOURCE_VERSION.md)。
