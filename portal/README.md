# SpeakUp 门户网站

这是从 `Lq0412/ai-en-coach` 最新 `main` 分支中独立抽取的 SpeakUp 门户应用。
目录只包含门户页面、首批体验报名、访问事件、运营后台、数据存储、部署配置、运行时图片和对应测试；完整 SpeakUp 产品原型未包含在内。

## 环境要求

- Node.js >= 22.13.0
- npm

## 本地运行

```bash
npm install
npm run dev
```

默认首页为 `/`，运营后台为 `/admin`。

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
