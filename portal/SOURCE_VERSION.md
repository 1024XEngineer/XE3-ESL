# 来源版本

- 源仓库：https://github.com/Lq0412/ai-en-coach
- 源分支：`main`
- 源 HEAD：`bc5f338f397e82d68eae780d0342f3e53e57cb0f`
- 抽取日期：`2026-08-21`
- 门户在源仓库中的位置：`prototype/`
- 最近一批门户实现提交：`337d2dd`（`feat(prototype): 优化门户响应式叙事布局`）

## 已保留

- `app/`：门户首页、报名弹窗、运营后台和 API 路由
- `lib/`、`db/`、`drizzle/`：报名、埋点、鉴权和数据持久化
- `worker/`、`build/`、`.openai/`：vinext、Cloudflare 和 Sites 运行配置
- `deploy/`、`Dockerfile`：独立部署配置
- `public/assets/portal-shots/`：门户运行时实际引用的图片
- `tests/`：门户专属构建和接口测试

## 已排除

- `speakup-premium/pages/prototype.html` 及完整产品原型脚本、样式和流程测试
- `portal-shots/` 中仅用于设计验收的历史截图
- 源仓库的团队文档、调研资料和工作流 skills
