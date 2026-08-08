# AGENTS.md

- `upstream` 必须指向官方主仓 `https://github.com/1024XEngineer/XE3-ESL.git`，`origin` 指向个人 Fork；禁止直接推送官方 `dev` 或 `main`。
- 每项改动先创建范围单一、验收清楚且关联当前 Milestone 的 Issue；一个 Issue 对应一个从最新 `upstream/dev` 创建的短分支和一个 PR。
- 保留用户未提交的代码；主工作区不干净或存在并行任务时使用独立 worktree。
- 涉及移动端交互或视觉验证时，启动前将当前任务分支更新到最新 `upstream/dev` 并保留当前修改，然后从同一 worktree 运行 `make dev-ios-simulator`；使用真实后端，模拟器禁用数字人。
- iOS 模拟器因代理无法访问 OSS 时，仅在当前 worktree 的本地 `.env` 中设置 `OSS_ENABLED=0` 和 `RESUME_OCR_ENABLED=0`，不得提交该配置或改用 mock。
- 只修改当前 Issue 范围，优先复用现有组件及官方或主流实现，不提交密钥、`.env`、缓存、构建产物或无关文件。
- Commit 使用 `<type>(<scope>): <subject>`；PR 必须包含功能描述、实现思路、可复现测试和关联 Issue，默认邀请 `gangcaiyoule`、`jyqin0203`、`zhiwilliam1-cell`；合入前，相关分析、测试、构建和契约校验通过，Reviewer 可复现，Review 意见已解决且范围一致，AI 修改经人工检查且提交者能够解释。
- 提 PR 后必须检查 CI 和 Review；CI 未全部通过或 Review 尚有未解决意见时，只能报告实际状态，不得宣称完成。
- 视觉改动除非已明确要求直接提交，否则先启动并截图确认；PR 合并后关闭对应 Issue、同步个人 Fork，并仅删除当前任务创建且确认干净、已同步的分支和 worktree。
