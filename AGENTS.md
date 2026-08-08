# AGENTS.md

- `upstream` 必须指向官方主仓 `https://github.com/1024XEngineer/XE3-ESL.git`，`origin` 指向个人 Fork；禁止直接推送官方 `dev` 或 `main`。
- 每项改动先创建范围单一、验收清楚且关联当前 Milestone 的 Issue；一个 Issue 对应一个从最新 `upstream/dev` 创建的短分支和一个 PR。
- 保留用户未提交的代码；主工作区不干净或存在并行任务时使用独立 worktree。
- 启动前将当前任务分支更新到最新 `upstream/dev` 并保留当前修改，然后从同一 worktree 运行 `make dev-ios-simulator`；使用真实后端，模拟器禁用数字人。
- 只修改当前 Issue 范围，优先复用现有组件及官方或主流实现，不提交密钥、`.env`、缓存、构建产物或无关文件。
- Commit 使用 `<type>(<scope>): <subject>`；PR 必须包含功能描述、实现思路、可复现测试和关联 Issue，默认邀请 `gangcaiyoule`、`jyqin0203`、`zhiwilliam1-cell`；相关检查通过、Review 意见解决且范围一致后方可合并。
- 视觉改动除非已明确要求直接提交，否则先启动并截图确认；PR 合并后同步个人 Fork，并删除已完成的远程分支、本地分支和 worktree。
