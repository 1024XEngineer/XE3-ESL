# AGENTS.md

本文件适用于整个仓库，约束自动化 Agent 在本项目中的执行方式。

## 开始工作前

- 每项变更必须有一个范围单一、验收清楚且关联当前 Milestone 的 GitHub Issue。
- 架构和产品决策以对应 Issue 为权威来源；已标记 `Proposal-Accepted` 的正文保持只读。
- 开始编码前确认目标目录、依赖边界和验收命令，不根据相邻任务扩大范围。

## 仓库与资源边界

- 正式开发工作区为 `/Users/mac/Projects/XE3-ESL`，前端、后端、API 契约、CI 和正式用户文档只在该仓库修改。
- 本地仓库的 `origin` 为个人 Fork `https://github.com/Lq0412/XE3-ESL.git`，用于推送个人任务分支。
- `upstream` 为官方主仓 `https://github.com/1024XEngineer/XE3-ESL.git`；`dev` 是日常开发与联调集成分支，`main` 只接收经过验证的正式发布。
- 常规任务从最新 `upstream/dev` 创建短期分支，推送到 `origin` 后向 `upstream/dev` 发起 Pull Request。
- 正式发布通过 `upstream/dev` 向 `upstream/main` 的发布 Pull Request 完成；禁止把常规功能分支直接合入 `main`。
- 禁止直接推送官方主仓的 `dev` 或 `main`，也不在官方主仓创建个人开发分支。
- `/Users/mac/Projects/ai-en-coach` 是前期共享开发与验证仓，包含 App 原型、已上线门户源码、Agent Demo 后端、资源文件和历史文档，仅允许读取和选择性迁移。
- 不在 `ai-en-coach` 中继续同步维护正式代码，也不把 `XE3-ESL` 的正式实现反向复制回共享仓。
- 从 `ai-en-coach` 迁移内容前，必须先检查正式仓的目录结构、已接受 Issue、接口契约和其他成员的在审分支或 Pull Request，避免覆盖或重复实现。
- `https://speak-up.top` 是已经上线、独立运行的 Web 门户。门户不迁入 Flutter App；其源码、品牌、文案和视觉资源可以作为迁移参考。
- Flutter 的迁移对象是共享仓中的 App 交互原型，不是门户的 React、HTML、CSS 或 JavaScript 源码；正式移动端功能必须按照 Flutter 工程结构重新实现。
- 共享仓中的历史文档和原型不是正式决策来源。发生冲突时，权威顺序为：官方主仓已接受的 GitHub Issue、当前接口契约和代码、在审 Pull Request、线上门户实际表现、共享仓历史资料。
- 未经明确授权，不创建、修改或关闭官方主仓的 Issue、Milestone、Pull Request、Tag 或 Release。

## Git 工作流

- 所有代码变更必须通过个人 Fork 的短分支向主仓提交 Pull Request。
- 禁止在主仓直接创建个人开发分支或直接推送 `dev`、`main`。
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
