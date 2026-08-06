# SpeakUp

SpeakUp 是一个面向英语口语训练的 AI 陪练项目，通过自由对话、场景练习和练习复盘，帮助学习者持续开口并改进表达。

项目当前处于 **MS3：核心功能闭合** 阶段，正在准备首次预发布版本 `v0.1.0-alpha.1`。该版本用于建立可追溯的协作与验证基线，不代表正式生产版本。

## 当前能力

- **AI 口语陪练**：支持文字和语音输入、流式回复、语音朗读与对话记录。
- **四类场景练习**：英文面试、IELTS 口语、职场英语、生活与旅行。
- **专项训练**：英文面试支持模拟面试与轮次练习；IELTS 支持 Part 1、2、3 与完整模考。
- **逐句反馈**：在用户表达中提供轻量纠错与更自然的英文表达建议。
- **练习复盘**：保存练习结果，并按场景展示评分、反馈和历史记录。

## 项目结构

```text
mobile/   Flutter Android/iOS 客户端与 Feature 组合
server/   Go 业务模块、应用装配、数据与供应商实现
api/      OpenAPI、WebSocket Schema 与契约校验
tools/    本地开发、模拟器验证和专项检查脚本
```

代码依赖保持为“客户端页面 → Controller → Client Port → 传输契约 → 服务端入口 → 应用编排 → 领域能力 → PostgreSQL/供应商实现”。各模块职责、核心伪代码调用链和扩展位置统一记录在 GitHub Issue，不在 README 重复维护。

当前架构与发布：

- [当前代码模块与协作主链](https://github.com/1024XEngineer/XE3-ESL/issues/456)
- [MS3：核心功能闭合](https://github.com/1024XEngineer/XE3-ESL/milestone/3)
- [v0.1.0-alpha.1 首次预发布准备](https://github.com/1024XEngineer/XE3-ESL/issues/452)

## 本地运行

本地开发需要 Flutter 3.44.6、Go 1.26.5、Node.js 22.12 或更高版本，以及可用的 Docker 环境。

先创建本地配置并按照模板注释填写所需的服务端配置；不要提交 `.env`：

```shell
cp .env.example .env
```

启动一个 iPhone Simulator 后，可从仓库根目录运行：

```shell
make dev-ios-simulator
```

连接并授权一台 arm64 Android 真机后，可运行：

```shell
make dev-android
```

这两个入口会启动 PostgreSQL、执行数据库迁移、启动本地后端并运行 App。移动端的设备要求和模拟器限制见 [mobile/README.md](mobile/README.md)。

## 质量检查

在仓库根目录执行完整的 Flutter、Go、API 契约和确定性主流程检查：

```shell
make check
```

也可以分别运行：

```shell
make check-flutter
make check-go
make check-api
make check-smoke
```

PostgreSQL 迁移、数据库集成、Readiness 和可达 Go 漏洞检查由 Database Workflow 验证。所有合入 `dev` 或 `main` 的变更都必须通过对应的 Pull Request 检查。

## 协作与发布

- 日常开发从最新 `upstream/dev` 创建短期分支，通过个人 Fork 向官方仓 `dev` 提交 Pull Request。
- 正式发布通过 `dev → main` 的 Release Pull Request 完成，不直接向 `main` 推送功能提交。
- Tag 只在 Release Pull Request 合入后的 `main` 提交上创建；GitHub Release 以官方仓 Tag 为权威来源。
- Commit 信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/v1.0.0/)。

## License

[MIT](LICENSE)
