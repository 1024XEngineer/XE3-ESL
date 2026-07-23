# AGENTS.md

本文件适用于整个仓库，约束自动化 Agent 在本项目中的执行方式。

## 开始工作前

- 每项变更必须有一个范围单一、验收清楚且关联当前 Milestone 的 GitHub Issue。
- 架构和产品决策以对应 Issue 为权威来源；已标记 `Proposal-Accepted` 的正文保持只读。
- 开始编码前确认目标目录、依赖边界和验收命令，不根据相邻任务扩大范围。

## 仓库与资源边界

- `/Users/mac/Projects/XE3-ESL` 是正式开发工作区，正式前端、后端和 CI 只在本仓库维护。
- `origin`（`https://github.com/Lq0412/XE3-ESL.git`）是个人 Fork，只用于推送任务分支；`upstream`（`https://github.com/1024XEngineer/XE3-ESL.git`）是官方主仓，只通过 Pull Request 合入。
- `/Users/mac/Projects/ai-en-coach` 是前期共享开发与验证仓，只用于读取 Prototype、Agent Demo、门户资源和历史文档；不得在两个仓库重复维护同一份正式代码。
- 从参考仓迁移前，先检查正式仓现有结构、已接受 Issue、接口契约、在审 PR 和其他成员分支；只迁移当前验收所需内容，不整仓复制或覆盖已有实现。
- `https://speak-up.top` 是独立运行的线上门户，不属于 Flutter App 的迁移范围；Flutter 只选择性迁移当前 App 原型所需页面和流程，不搬运历史页面或门户实现。
- Agent Demo 只作为后端能力验证参考；正式后端架构、接口和依赖必须在对应 Issue 中评估后再迁移。
- 发生冲突时，依据优先级为：已接受的 Issue 决策、正式仓契约与代码、在审 PR、线上门户现状、历史参考文档。
- 未经明确授权，不新建、修改或关闭官方主仓的 Issue、Milestone、PR、Tag 或 Release。

## Git 工作流

- 所有代码变更必须通过个人 Fork 的短分支向主仓提交 Pull Request。
- 常规任务从最新 `upstream/dev` 创建短期分支，推送到 `origin` 后向 `upstream/dev` 发起 PR。
- `main` 只用于正式发布；发布通过 `upstream/dev` 向 `upstream/main` 的发布 PR 完成，常规功能分支不得直接合入 `main`。
- 禁止在主仓直接创建个人开发分支或直接推送 `dev`、`main`。
- 功能 Issue 在对应 PR 完成 Review 并合入 `upstream/dev` 后视为开发完成；正式发布状态由发布 PR 与 Milestone 单独管理。
- 一个 Issue 对应一个分支和一个 PR；分支合并后删除。
- Commit 使用 Conventional Commits，格式为 `<type>(<scope>): <subject>`。
- PR 必须包含功能描述、实现思路、可复现测试方式和关联 Issue。
- 不得提交密钥、`.env`、缓存、构建产物或与当前 Issue 无关的改动。

## 文档边界

- 产品设计、架构说明、架构决策和技术选型等工程文档保存在 GitHub Issue 描述区，不提交到仓库。
- 已接受的工程决策发生变化时，新建增量 Issue，不覆盖冻结正文。
- 用户使用说明可以入库；完成并通过 Review 后按团队流程更新 `Documented` 标签。
- 会议纪要不能替代 Issue 中的正式结论。

## 目录与代码

- 不使用 `.gitkeep` 批量创建空目录；目录随真实可编译代码或可校验契约进入仓库。
- 业务代码默认留在所属模块；只有存在两个以上真实调用方时才考虑提取公共代码。
- 公共层不得依赖业务模块，也不得成为存放临时代码的兜底目录。
- 修改跨模块接口前，先更新并评审对应 Issue；禁止在实现中私自增加协议字段。

## 合入条件

- 本地分析、测试、构建和契约校验全部通过。
- Reviewer 能按 PR 描述复现验证结果。
- Review 意见已解决，PR 与关联 Issue 的范围一致。
- AI 生成或修改的内容已经人工检查，并且提交者能够解释其逻辑。
