# SpeakUp

SpeakUp 是一个面向英语表达训练的 AI 陪练项目。首个验证场景聚焦程序员英文面试，并为后续角色和训练场景扩展保留清晰边界。

项目当前处于 **MS1：战略决策** 阶段，代码骨架将通过独立 Issue 和 Pull Request 渐进加入。

## 项目入口

- [产品范围与功能边界](https://github.com/1024XEngineer/XE3-ESL/issues/9)
- [已接受的 MS1 系统架构](https://github.com/1024XEngineer/XE3-ESL/issues/15)
- [场景与角色扩展模型](https://github.com/1024XEngineer/XE3-ESL/issues/24)
- [MS1 Issues](https://github.com/1024XEngineer/XE3-ESL/milestone/1)

工程设计与架构决策以对应 GitHub Issue 为权威来源，不在仓库中重复维护。

## 参与协作

所有变更遵循 Issue → Fork → Pull Request → Review 流程。开始开发前请先选择或创建范围单一的 Issue，并关联当前 Milestone；不要在主仓直接创建开发分支。

Commit 信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/v1.0.0/)。

## 本地质量检查

根目录统一入口会执行 Flutter、Go、API 契约和确定性 Mock 主链检查：

```shell
make check
```

也可以分别运行 `make check-flutter`、`make check-go`、`make check-api` 和 `make check-smoke`。PostgreSQL 迁移、Readiness 和数据库集成测试继续由独立的 Database Workflow 验证。

## License

[MIT](LICENSE)
