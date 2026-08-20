# IELTS Part 1 报告完整性验证小报告

测试时间：2026-08-20  
测试样本：正常回答 1 次、较短回答 1 次  
未采集：录屏、页面截图

## 1. 总体结论

**V1 判定：失败。**

发现两个独立问题：

| 测试轮次 | 结果 | 问题 |
| --- | --- | --- |
| 正常回答 | `FAILED` | 模型响应未通过报告格式校验，未生成报告 |
| 较短回答 | `READY` | 有分数、改进建议和行动，但所有原话证据为空 |

第二轮明确命中失败判定：报告进入 `READY`，但评分没有任何可追溯的用户原话证据。

本次只完成每种回答各 1 次，尚未达到“每种至少执行 3 次”的完整样本要求。但已有样本已经足以判定当前版本不通过。

## 2. 第一轮：正常回答

| 字段 | 结果 |
| --- | --- |
| Session ID | `ae6abb74-9f17-41ca-9eb3-e0eaca12f43f` |
| Evaluation ID | `084f0a9c-8076-457f-804c-2d8caa1a9d05` |
| Session status | `completed` |
| Evaluation status | `FAILED` |
| 有效回答数 | 7 |
| 结束原因 | `turn_limit_reached` |
| Evaluation attempt | 1 |
| 错误代码 | `PROVIDER_RESPONSE_INVALID` |
| 是否可重试 | `false` |
| 是否有结果 | 否 |

模型配置：

| 字段 | 值 |
| --- | --- |
| Provider | `qianwen` |
| Model | `qwen3.7-plus` |
| Prompt | `ielts-report/v2` |
| Pipeline | `session-evaluation/v1` |
| Result schema | `evaluation-report/v2` |

### 原始转写

1. “I mostly visit news sites and social media platforms to stay updated with current events. I also frequently check educational websites when I need to learn something new or find specific information for my studies.”
2. “My favorite website is probably YouTube because it offers such a wide variety of content. I really enjoyed watching educational videos and tutorials there, which helped me learn new skills in a fun and engaging way.”
3. “Yes, my habits have shifted quite a bit recently. I used to spend a lot of time on social media, but now I find myself visiting more professional networking sites and online learning platforms. This change helps me focus more on career development and acquiring new skills.”
4. “Social media platforms and short video apps are extremely popular here because they offer quick entertainment. E-commerce sites are also widely used since online shopping has become a major part of daily life for most people.”
5. “I generally prefer websites because they provide instant access to the latest information. It is much faster to search for specific answers online than to look through a physical book. However, I still enjoy reading books when I want to dive deep into a topic without distractions.”
6. “Honestly, yes, I would love to run my own personal website. Unlike social platforms where everything is controlled by algorithms, a website lets me organize my content exactly how I want. I could post my creative work, keep travel journals, or even share useful tips for other people. That said, I'm not good at coding, so building and maintaining it might take quite a lot of time. Still, if I had the chance, I'd definitely give it a go.”
7. “I have learned many practical skills from websites, such as cooking recipes and basic coding. These online resources help me solve daily problems quickly and improve my study efficiency. It is really convenient to find step-by-step guides whenever I need them.”

7 条单题反馈均进入 `READY`，声学数据也全部完成评估。但整轮报告响应没有通过严格校验，因此没有总分、评分依据、改进建议或下一步行动。

### 第一轮判定

**失败。**

失败原因不是内容不足，而是评分供应商返回的报告结构无效，并且系统将该错误标记为不可重试。

## 3. 第二轮：较短回答

| 字段 | 结果 |
| --- | --- |
| Session ID | `2ec5cb9d-cb6a-43c3-a276-04f81579d607` |
| Evaluation ID | `9d63be53-2993-44bd-a1e5-568a9435533b` |
| Session status | `completed` |
| Evaluation status | `READY` |
| 有效回答数 | 4 |
| 结束原因 | `turn_limit_reached` |
| Evaluation attempt | 1 |
| 是否有结果 | 是 |
| Scoreability | `PROVISIONAL` |

### 原始转写

| 问题 | 用户回答 |
| --- | --- |
| Do you like to keep things tidy? | “yes.” |
| Did you keep your room tidy as a child? | “Sure.” |
| How do you keep your work or study space tidy? | “I try to clear my desk at the end of each day, so I can start fresh the next morning.” |
| Do you think that it is necessary to be tidy? | “Yes, I think it is necessary.” |

前两条回答的声学评估失败，后两条声学评估成功。因此报告没有给出发音分数。

## 4. 第二轮报告完整性

| 维度 | 分数 | Strengths | Improvements | 示例 | 原话证据 | 优先行动 | 判定 |
| --- | ---: | ---: | ---: | ---: | ---: | --- | --- |
| Fluency & Coherence | 5.0 | 1 | 2 | 2 | 0 | 有 | 失败 |
| Lexical Resource | 5.0 | 1 | 2 | 2 | 0 | 有 | 失败 |
| Grammar Range & Accuracy | 5.5 | 1 | 2 | 2 | 0 | 有 | 失败 |
| Pronunciation | 未评分 | 0 | 0 | 0 | 0 | 无 | 不适用 |

报告具备：

- 非空总结
- 三个有效维度的分数
- 每个有效维度的改进建议
- 推荐表达
- 3 条优先行动

报告缺失：

- 所有 `strengths[].evidence`
- 所有 `improvements[].evidence`
- 所有 `recommended_examples[].evidence`
- 所有维度的 `evidence_ref_ids`

因此报告虽然有分数和建议，但无法证明这些结论来自哪一句用户原话。

## 5. 模型原始响应与 Repair

| 项目 | 第一轮 | 第二轮 |
| --- | --- | --- |
| 原始模型响应 | 未持久化 | 未持久化 |
| 规范化结果 | 无 | 已持久化 |
| Worker attempt | 1 | 1 |
| Repair 是否发生 | 无法确认 | 无法确认 |
| Repair 轮次 | 无法确认 | 无法确认 |

当前数据库只保存规范化结果或统一错误，不保存脱敏后的模型原始响应。日志中也没有 repair 事件或轮次记录。

因此，“原始响应”和“repair 轮次”目前无法按测试要求采集，属于可观测性缺口。`attempt_count=1` 只代表 Worker 执行一次，不能证明模型响应内部没有 repair。

## 6. 最终判定

**V1：失败。**

主要缺陷：

1. 正常回答可能因 `PROVIDER_RESPONSE_INVALID` 永久丢失整轮报告。
2. 较短回答会生成 `READY` 报告，但所有评分依据均为空。
3. 后端允许缺少原话证据的报告进入 `READY`。
4. 原始模型响应和 repair 过程不可观测。
5. 当前样本量尚未达到每种回答至少 3 次。

---

# IELTS Part 2 两轮测试记录

测试时间：2026-08-20  
测试样本：Part 2 长回答 2 次  
未采集：录屏、页面截图

## 7. Part 2 总体结论

两轮录音均成功生成转写，但都没有完成转写确认。对应 turn 停留在 `transcribed`，`confirmed_at` 为空；session 随后以 `user_ended` 提前结束，数据库中的 `effective_turns` 为 0。

因此两轮均未创建 Evaluation，也没有生成 Part 2 评分报告。

| 测试轮次 | Session status | Turn status | 有效回合 | Evaluation | 报告 |
| --- | --- | --- | ---: | --- | --- |
| 第一轮 | `ended_early` | `transcribed` | 0 | 未创建 | 未生成 |
| 第二轮 | `ended_early` | `transcribed` | 0 | 未创建 | 未生成 |

这两轮记录不能用于验证报告内容是否完整，但可以用于验证 Part 2 从转写到确认、再到评分触发的流程。

## 8. Part 2 第一轮

| 字段 | 结果 |
| --- | --- |
| Session ID | `587a4ad5-c665-44c6-98eb-94cb46a1185d` |
| Turn ID | `0cc6efa2-9f96-4239-835e-247c4399e242` |
| Question ID | `01adf1d4-d5a3-4d07-9b28-882b6ea3cc2d` |
| Session status | `ended_early` |
| End reason | `user_ended` |
| Turn status | `transcribed` |
| 有效回答数 | 0 |
| ASR attempt | 2 |
| ASR provider | `qianwen` |
| ASR model | `fun-asr-flash-2026-06-15` |
| confirmed_at | 空 |
| Evaluation ID | 未创建 |
| Evaluation status | 不适用 |

### 日志时间线

| 时间 | 事件 | 结果 |
| --- | --- | --- |
| 16:40:56 | 激活 Part 2 session | HTTP 200 |
| 16:44:22 | 第一次提交转写 | HTTP 503，耗时约 82 秒 |
| 16:45:43 | 第二次提交转写 | HTTP 201，转写成功 |
| 16:45:53 | 查询 interaction state | HTTP 200 |
| 16:45:53 | `end-early` | HTTP 200 |

### 原始转写

> My childhood friend that I'd like to talk about is my best mate Xiao Yu, who is practically my childhood buddy. We first met when we were about six years old, living in the same housing estate. Our parents were neighbors, so we hung out almost every day back then. We spent most of our free time together after school. We often rode bikes around the community, played outdoor games, and shared snacks. Sometimes we would do homework at each other's home on weekends. We told each other all our little secrets and dreamed about what we wanted to be when we grew up. As we got older, we went to different high schools and gradually saw each other less often. Even so, I still remember him. Really well, he was such a warm hearted kid. Those simple carefree moments with him are really precious to me. That's why he sticks in my memory.

### 第一轮判定

录音和 ASR 最终成功，但转写没有进入确认状态。由于没有确认 turn，系统没有增加有效回合，也没有触发单题反馈或整轮评分。

## 9. Part 2 第二轮

| 字段 | 结果 |
| --- | --- |
| Session ID | `43200f52-5c4a-44bc-ba08-ea9f0425d0da` |
| Turn ID | `a53c251f-ae18-4a2b-b791-3ae1483cfd66` |
| Question ID | `fe20a4f8-fb84-4162-9dc3-d52ad386ba1b` |
| Session status | `ended_early` |
| End reason | `user_ended` |
| Turn status | `transcribed` |
| 有效回答数 | 0 |
| ASR attempt | 1 |
| ASR provider | `qianwen` |
| ASR model | `fun-asr-flash-2026-06-15` |
| confirmed_at | 空 |
| Evaluation ID | 未创建 |
| Evaluation status | 不适用 |

### 日志时间线

| 时间 | 事件 | 结果 |
| --- | --- | --- |
| 16:45:53 | 激活 Part 2 session | HTTP 200 |
| 16:49:54 | 提交转写 | HTTP 201，转写成功 |
| 16:50:02 | 查询 interaction state | HTTP 200 |
| 16:50:02 | `end-early` | HTTP 200 |

### 原始转写

> I'm going to talk about a small local museum I visited last year with my family. It was located in the city center, and we went there on a public holiday. We spent roughly one and a half hours walking around the exhibition halls. Most of the displays were old objects behind glass. We just read the information boards and wandered from one room to another. There were almost no interactive activities and few visitors. To be honest, I found this place really dull. For one thing, all the exhibits looked quite familiar to me. There was nothing eye-catching or surprising. Besides, there was no audio guide, and the written introductions were long and boring. I didn't feel engaged at all. Time passed really slowly there. That's why I considered it such a boring spot.

### 第二轮判定

ASR 一次成功，但转写同样没有确认。Session 在转写成功后约 8 秒提前结束，因此未形成有效回合，也未触发评分。

## 10. Part 2 共同问题

1. 两轮均已生成可读的长转写，但 `confirmed_at` 为空。
2. 两轮 turn 均停留在 `transcribed`，没有进入 confirmed/effective 状态。
3. 两轮 session 均以 `user_ended` 结束，而不是正常完成。
4. 数据库中没有对应的 `PRACTICE_TURN_FEEDBACK` 或 `SESSION_REPORT` Evaluation。
5. 第一轮 ASR 首次请求返回 503，重试后成功；第二轮 ASR 一次成功，因此共同阻塞点不是 ASR 最终失败，而是转写后的确认流程。
6. 应用随后又创建了 Session `5001a3f2-8c04-40ca-aff4-e918fc0d8f99`。该 session 后续已结束，详细结果记录在下一批测试中。

## 11. Part 2 最终判定

**流程验证失败，报告完整性暂不可判定。**

两轮回答均未进入评分管线。需要先确认 Part 2 页面在转写完成后为何没有调用 confirmation，或者为何在确认前执行 `end-early`。在该流程修复前，无法继续验证 Part 2 报告是否包含评分依据、原话证据、改进建议和下一步行动。

---

# IELTS Part 2 新增三轮测试记录

测试时间：2026-08-20  
新增样本：Part 2 测试 3 次  
Session 范围：16:50:02–16:56:29

## 12. 新增批次总体结果

| 轮次 | Session ID | Session status | Turn status | 有效回合 | Evaluation | 结果 |
| --- | --- | --- | --- | ---: | --- | --- |
| 第三轮 | `5001a3f2-8c04-40ca-aff4-e918fc0d8f99` | `ended_early` | `transcribed` | 0 | 未创建 | 转写未确认 |
| 第四轮 | `5e0f6bf1-a05a-458e-be6d-21f7a9ea3f8a` | `ended_early` | `confirmed` | 1 | 单题反馈 `READY` | 确认成功，但无整轮报告 |
| 第五轮 | `9ce435b3-5dde-4c2c-bc38-1fe615e34c31` | `ended_early` | `failed` | 0 | 未创建 | ASR 两次失败 |

三轮均以 `user_ended` 提前结束，没有任何一轮创建 `SESSION_REPORT`。

## 13. Part 2 第三轮

| 字段 | 结果 |
| --- | --- |
| Session ID | `5001a3f2-8c04-40ca-aff4-e918fc0d8f99` |
| Turn ID | `db7d8cf9-3532-47d6-83f1-228886bc2b24` |
| Question ID | `be6b2caa-6a16-4d70-9176-4754ca2838bb` |
| Session status | `ended_early` |
| End reason | `user_ended` |
| Turn status | `transcribed` |
| ASR attempt | 1 |
| confirmed_at | 空 |
| 有效回合 | 0 |
| Evaluation | 未创建 |

### 日志时间线

| 时间 | 事件 | 结果 |
| --- | --- | --- |
| 16:50:02 | 激活 session | HTTP 200 |
| 16:50:48 | 提交转写 | HTTP 201，耗时约 7.2 秒 |
| 16:52:59 | 查询 interaction state | HTTP 200 |
| 16:52:59 | `end-early` | HTTP 200 |

### 原始转写

> 是。

### 第三轮判定

转写成功，但两分钟内没有确认。Session 最终提前结束，有效回合保持为 0，未触发任何 Evaluation。

## 14. Part 2 第四轮

| 字段 | 结果 |
| --- | --- |
| Session ID | `5e0f6bf1-a05a-458e-be6d-21f7a9ea3f8a` |
| Turn ID | `fc86cc94-1c55-48c9-b44b-78d4fcd20a72` |
| Question ID | `3f325b2b-6c16-4dab-91ce-46cde219d60e` |
| Session status | `ended_early` |
| End reason | `user_ended` |
| Turn status | `confirmed` |
| ASR attempt | 1 |
| confirmed_at | `2026-08-20 16:54:11.783 +08:00` |
| 有效回合 | 1 |
| Evaluation ID | `aaeed901-80c6-4793-8029-18fe042301d0` |
| Evaluation kind | `PRACTICE_TURN_FEEDBACK` |
| Evaluation status | `READY` |
| Evaluation attempt | 1 |
| SESSION_REPORT | 未创建 |

### 日志时间线

| 时间 | 事件 | 结果 |
| --- | --- | --- |
| 16:52:59 | 激活 session | HTTP 200 |
| 16:54:11 | 提交转写 | HTTP 201，耗时约 17.9 秒 |
| 16:54:11 | 转写确认 | 成功，有效回合变为 1 |
| 16:54:26 | 查询 interaction state | HTTP 200 |
| 16:54:26 | `end-early` | HTTP 200 |
| 16:54:35 | 单题反馈完成 | `READY` |

### 原始转写

> Hello hello hello, what's your name.

### 单题反馈结果

| 字段 | 结果 |
| --- | --- |
| Scoreability | `PROVISIONAL` |
| Summary | `Feedback is ready for this confirmed transcript.` |
| Acoustic status | `ASSESSED` |
| Fluency | `86.83872` |
| Pronunciation | `100` |
| Integrity | `100` |

### 第四轮判定

这是本批次唯一成功完成确认并进入评分管线的回合。单题语音反馈成功生成，但 session 仍以 `user_ended` 提前结束，系统没有创建整轮 `SESSION_REPORT`，因此无法验证 Part 2 报告完整性。

## 15. Part 2 第五轮

| 字段 | 结果 |
| --- | --- |
| Session ID | `9ce435b3-5dde-4c2c-bc38-1fe615e34c31` |
| Turn ID | `d6376340-0047-4bee-b7af-6ac6e220d97f` |
| Question ID | `4caa97ee-d65f-4919-b96c-5a887c38f36b` |
| Session status | `ended_early` |
| End reason | `user_ended` |
| Turn status | `failed` |
| ASR attempt | 2 |
| failure_code | `invalid_request` |
| 原始转写 | 无 |
| 有效回合 | 0 |
| Evaluation | 未创建 |

### 日志时间线

| 时间 | 事件 | 结果 |
| --- | --- | --- |
| 16:54:26 | 激活 session | HTTP 200 |
| 16:55:48 | 第一次提交转写 | HTTP 503，耗时约 31 秒 |
| 16:56:11 | 第二次提交转写 | HTTP 503，耗时约 21.9 秒 |
| 16:56:20 | 查询 interaction state | HTTP 200 |
| 16:56:29 | 再次查询 interaction state | HTTP 200 |
| 16:56:29 | `end-early` | HTTP 200 |

### 第五轮判定

两次 ASR 请求均失败，最终 turn 状态为 `failed`，错误代码为 `invalid_request`。本轮没有转写、确认、有效回合或 Evaluation。

## 16. 五轮 Part 2 汇总结论

结合前两轮和本次新增三轮，共记录 5 次 Part 2 测试：

| 结果类型 | 次数 |
| --- | ---: |
| 转写成功但未确认 | 3 |
| 转写确认成功 | 1 |
| ASR 最终失败 | 1 |
| 创建单题反馈 | 1 |
| 创建整轮报告 | 0 |

当前最主要的问题仍是 Part 2 完成链路不稳定：

1. 大多数成功转写没有进入 confirmation。
2. 所有 session 最终都以 `user_ended` 提前结束。
3. 唯一确认成功的回合只生成单题反馈，没有生成整轮报告。
4. ASR 还存在慢请求、503 和 `invalid_request`。
5. 目前 5 次测试均无法用于验证 Part 2 最终报告的证据、改进项和下一步行动是否完整。

## 17. IELTS 完整模考测试记录（前两轮）

记录时间：2026-08-20（Asia/Shanghai）

本节先记录前两轮完整模考；第三轮完成后的结果见第 22 节。

| 轮次 | Session ID | Plan ID | 最终状态 | 有效回合 | 结果 |
| --- | --- | --- | --- | ---: | --- |
| 第 1 轮 | `478dc3e3-c434-442a-8359-2a9a1f80c79e` | `4037257e-7dc5-407e-b201-50479b871d1d` | `completed` | 15 | 完成并生成报告，但发音维度和总分缺失 |
| 第 2 轮 | `bb75916d-f2cb-4118-81c7-ec93235fb6af` | `7fa168ce-1367-49a4-b6c1-fdf7e7ec53fd` | `ended_early` | 8 | Part 2 已转写但未确认，未进入 Part 3，也未生成整轮报告 |

## 18. 第 1 轮：完成模考但报告不完整

### 18.1 Session 结果

- Session ID：`478dc3e3-c434-442a-8359-2a9a1f80c79e`
- 开始时间：2026-08-20 16:56:29
- 结束时间：2026-08-20 17:03:05
- 最终 session status：`completed`
- end reason：`turn_limit_reached`
- 有效回合数：15
- 中间有 2 个转写回合失败，错误为 `invalid_response`，失败回合未计入最终 15 个有效回答。

### 18.2 报告结果

- Evaluation ID：`ccbeeccd-4592-4ea9-9eb0-3777d159a6ff`
- Evaluation kind：`SESSION_REPORT`
- 最终 evaluation status：`READY`
- attempt count：1
- 创建时间：2026-08-20 17:03:05
- 完成时间：2026-08-20 17:03:51
- scoreability status：`PROVISIONAL`
- 报告收录问题数：15
- priority actions 数量：3

各维度结果：

| 维度 | 分数 | Coverage | Confidence | 原话证据引用数 | 改进项数 | 状态 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Fluency and Coherence | 4.0 | 0.15 | 0.95 | 3 | 2 | 有分数、有证据、有改进项 |
| Lexical Resource | 4.0 | 0.20 | 0.90 | 3 | 2 | 有分数、有证据、有改进项 |
| Grammatical Range and Accuracy | 4.5 | 0.25 | 0.85 | 2 | 2 | 有分数、有证据、有改进项 |
| Pronunciation | 空 | 0 | 0 | 0 | 0 | `ACOUSTIC_ASSESSMENT_FAILED` |

### 18.3 问题判定

本轮报告虽然进入 `READY`，但不是完整的 IELTS 四维报告：

1. Pronunciation 的 `score` 为空。
2. Pronunciation 的 coverage 和 confidence 均为 0。
3. Pronunciation 没有原话或声学证据、改进建议和推荐示例。
4. 报告没有生成 overall score，整体只能标记为 `PROVISIONAL`。
5. 其余三个维度均有分数、原话证据和明确改进项，且报告提供了 3 个下一步行动。

判定：**失败**。存在 `READY` 报告的关键评分维度为空，且用户无法获得完整四维分数和总分。

## 19. 第 2 轮：Part 2 转写后未完成确认

### 19.1 Session 结果

- Session ID：`bb75916d-f2cb-4118-81c7-ec93235fb6af`
- 开始时间：2026-08-20 17:03:25
- 结束时间：2026-08-20 17:14:03
- 最终 session status：`ended_early`
- end reason：`user_ended`
- 有效回合数：8
- SESSION_REPORT Evaluation ID：无
- 最终 evaluation status：未创建

### 19.2 Part 2 卡点

Part 2 问题 ID：`18f73315-212a-4aa0-8169-4802c5a0d92d`

服务端已成功生成转写，原始转写开头为：

> I'd like to talk about my classmate Leo, who helped me out last Semester, the problem was that I struggled with a really difficult group assignment. I fell behind because I was un...

数据库中的该回合状态停留在 `transcribed`，没有 `confirmed_at`，因此没有计入新的有效回合，也没有触发从 Part 2 向 Part 3 的推进。

关键日志时间线：

1. 17:12:20 左右开始提交 Part 2 录音转写。
2. 17:13:42，`POST /transcription-candidates` 返回 `201`，单次请求耗时约 82.26 秒。
3. 转写成功后出现 interaction-state 查询，但没有该 candidate 对应的 confirmation 请求。
4. 17:14:03，客户端调用 `end-early`，session 以 `user_ended` 结束。
5. 因 session 未完成，没有创建 `SESSION_REPORT` Evaluation。

### 19.3 问题判定

判定：**失败**。Part 2 音频已经被服务端成功转写，但转写候选没有完成确认，状态机没有进入 Part 3。约 82 秒的转写等待也明显放大了前端“卡住”的体验。

## 20. 前两轮结论

1. 第 1 轮证明完整模考可以完成并生成 `READY` 报告，但声学评分失败会导致 Pronunciation 维度、总分、证据和建议缺失，当前仍会把报告发布给用户。
2. 第 2 轮暴露 Part 2 长音频链路问题：转写耗时过长，并在转写成功后没有完成 confirmation，导致无法进入 Part 3。
3. 两轮问题属于不同阶段：第 1 轮是报告完整性问题，第 2 轮是 Part 2 转写确认和状态推进问题。
4. 当前服务日志未持久化脱敏后的模型原始响应，也没有可用于确认 repair 是否发生及轮次的明确字段；本节仅记录数据库最终结果和可核对的 HTTP 日志。

## 21. 近期报告缺少发音评分的原因

### 21.1 不是所有逐回合声学评估都失败

第 1 轮完整模考的 15 个有效回答中：

- 8 个回合的声学状态为 `ASSESSED`，并成功取得讯飞 ISE 发音分。
- 7 个回合在重试 3 次后降级为 `NOT_ASSESSED / ACOUSTIC_ASSESSMENT_FAILED`。
- 失败主要集中在 `Yes.`、`No.`、`Sure.` 等极短回答。
- 成功回合的发音原始分约为 70.29、86.59、88.74、91.13、95.12、97.38、97.65 和 100。

因此，本轮不是 OSS 被禁用，也不是讯飞 ISE 完全不可用。对象存储、音频读取和 ISE 调用链路均已启用，并且部分请求成功。

### 21.2 报告聚合覆盖率不足

当前 IELTS 报告会按“成功声学评估回合数 / 全部有效回合数”计算覆盖率。近期已有完整报告的结果如下：

| 报告时间 | Evaluation ID | 成功声学评估 | 覆盖率 | Pronunciation |
| --- | --- | ---: | ---: | --- |
| 2026-08-20 17:03 | `ccbeeccd-4592-4ea9-9eb0-3777d159a6ff` | 8/15 | 53.3% | 空，`ACOUSTIC_ASSESSMENT_FAILED` |
| 2026-08-20 16:26 | `9d63be53-2993-44bd-a1e5-568a9435533b` | 2/4 | 50.0% | 空，`ACOUSTIC_ASSESSMENT_FAILED` |
| 2026-08-17 14:14 | `2540db27-f44a-497e-bf40-221b2080e7ce` | 4/4 | 100% | IELTS 8.0 |

前两份近期报告的覆盖率未达到生成 Pronunciation 分数的要求。报告生成器按设计禁止根据文字转写推测发音，因此即使已有部分有效 ISE 分数，也会把整个 Pronunciation 维度置空。

### 21.3 问题判断

1. 声学链路本身不是整体不可用，主要问题是极短回答的 ISE 成功率低。
2. 当前覆盖率分母包含所有有效回答，Part 1 中大量极短回答会显著拉低整轮覆盖率。
3. 已取得 8 个声学结果但最终完全不展示，造成有效评测信息损失。
4. 更合理的行为是排除明显不可进行声学评分的极短回答后计算覆盖率；已有足够有效样本时，应输出带 `PARTIAL_ACOUSTIC_COVERAGE` 的暂定发音分，而不是清空整个维度。

## 22. 第 3 轮：完成全流程但整轮报告生成失败

### 22.1 Session 结果

- Session ID：`70c3fc6d-1426-4868-af58-a92be733addb`
- Plan ID：`0963eee7-9ed7-49aa-8fa7-68c07990f4aa`
- 开始时间：2026-08-20 17:14:03
- 结束时间：2026-08-20 17:33:28
- 最终 session status：`completed`
- end reason：`turn_limit_reached`
- 有效回合数：15
- 逐回合 Evaluation：15 个，最终全部为 `READY`
- 逐回合 Evaluation attempt count：全部为 1

### 22.2 Part 2 和 Part 3 推进情况

本轮 Part 2 没有永久卡死，但存在明显的超长等待：

1. Part 2 长回答的 `POST /transcription-candidates` 请求耗时约 88.12 秒。
2. 17:25:37 转写请求返回 `201`，17:25:38 完成 confirmation。
3. 17:27:30 开始请求后续问题，说明状态机最终成功从 Part 2 推进到 Part 3。
4. Part 3 中仍有多次较长转写等待，部分请求约 40.80 秒、96.32 秒和 97.28 秒。
5. 最终 15 个有效回答全部完成，session 正常触发 `turn_limit_reached`。

判定：Part 2 到 Part 3 的功能推进最终成功，但长音频转写耗时严重影响体验，用户会在等待期间认为页面已经卡住。

### 22.3 声学评估结果

- 成功声学评估：15/15。
- 声学覆盖率：100%。
- 所有逐回合声学任务均在第 1 次尝试完成。
- 本轮不存在覆盖率不足或 `ACOUSTIC_ASSESSMENT_FAILED`。

这进一步证明近期报告缺少发音分不是因为 OSS 或讯飞 ISE 被整体禁用；第三轮声学链路工作正常。

### 22.4 整轮报告失败

- SESSION_REPORT Evaluation ID：`66a7c79f-7f0a-46a4-8f67-2a76b8dc2ea9`
- Evaluation kind：`SESSION_REPORT`
- 创建时间：2026-08-20 17:33:28
- 完成时间：2026-08-20 17:36:05
- attempt count：1
- 最终 evaluation status：`FAILED`
- error code：`PROVIDER_RESPONSE_INVALID`
- error message：`Evaluation processing failed.`
- retryable：`false`

报告失败后没有持久化 `result`，因此：

- 没有 overall score。
- 没有四个评分维度。
- 没有原话证据。
- 没有改进建议。
- 没有 priority actions。
- 页面无法展示本次完整模考报告。

服务日志只记录 `lane=session, error_kind=processing`，数据库只保留统一错误 `PROVIDER_RESPONSE_INVALID`。当前没有脱敏后的模型原始响应，也没有 repair 轮次记录，无法仅凭现有日志继续判断是模型 JSON 格式、字段约束、证据引用还是归一化校验失败。

### 22.5 第 3 轮判定

判定：**失败**。

1. 模考交互链路完成，成功进入并完成 Part 3。
2. 15 个逐回合反馈和 15 个声学评估均成功。
3. 最终整轮报告因 `PROVIDER_RESPONSE_INVALID` 失败，用户没有获得任何完整模考报告。
4. 该问题与第 1 轮不同：第 1 轮是 `READY` 报告内容不完整；第 3 轮是 SESSION_REPORT 直接进入 `FAILED`。

## 23. 三轮完整模考汇总结论

| 轮次 | Session 结果 | Part 2 → Part 3 | 声学覆盖率 | 最终报告 | 判定 |
| --- | --- | --- | ---: | --- | --- |
| 第 1 轮 | `completed`，15 回合 | 成功 | 8/15，53.3% | `READY`，但发音维度和总分为空 | 失败 |
| 第 2 轮 | `ended_early`，8 回合 | 失败，Part 2 转写后未确认 | 未形成整轮报告 | 未创建 | 失败 |
| 第 3 轮 | `completed`，15 回合 | 最终成功，但等待约 88 秒 | 15/15，100% | `FAILED / PROVIDER_RESPONSE_INVALID` | 失败 |

当前三个独立问题分别是：

1. Part 2 长音频转写耗时过长，且存在转写成功后未 confirmation 的情况。
2. 部分声学结果成功但覆盖率不足时，整项 Pronunciation 被清空。
3. SESSION_REPORT 的无效 provider response 不可重试，且缺少可诊断的原始响应和 repair 记录。
