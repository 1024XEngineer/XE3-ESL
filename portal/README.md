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

默认首页为 `/`，Android 发布页为 `/download/android`，运营后台为 `/admin`。

Android 发布页只从同源 `/downloads/android/release.json` 读取当前正式版本。
`404` 显示“准备中”；网络、HTTP、JSON 或严格协议校验失败时显示“暂不可用”；只有
元数据完整有效时才展示其中的版本化 APK 地址与真实版本、大小、时间、兼容范围及
SHA-256。页面不会猜测下载地址，也不使用 `latest.apk`。APK 与发布元数据由宿主机
Nginx 提供，不放入 Portal Docker 镜像；发布操作见
[`deploy/android-download/README.md`](../deploy/android-download/README.md)。

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
