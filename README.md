# 敏感词检测系统（Vue3 + Vite + Go + Gin）

一个可直接落地的敏感词检测系统，支持多策略命中（精确/归一化/模糊/拼音）、风险分级、命中定位跳转、词组映射、可选大模型辅助鉴别、前端打码与复制导出。

- 前端 + 后端仓库（当前项目）：[https://github.com/aureate7/sensitive-word-checker](https://github.com/aureate7/sensitive-word-checker)
- 后端仓库（后端独立版）：[https://github.com/aureate7/go-sensitive-checker-backend](https://github.com/aureate7/go-sensitive-checker-backend)

---

## 目录

1. [功能总览](#功能总览)
2. [系统架构](#系统架构)
3. [项目结构](#项目结构)
4. [快速开始](#快速开始)
5. [核心功能说明](#核心功能说明)
6. [API 说明](#api-说明)
7. [词组映射文件格式](#词组映射文件格式)
8. [环境变量配置（后端）](#环境变量配置后端)
9. [构建与部署](#构建与部署)
10. [常见问题](#常见问题)
11. [后续优化预案](#后续优化预案)

---

## 功能总览

### 检测能力

- 多类别检测：政治、暴恐、涉黄、辱骂、广告等。
- 多策略命中：
  - 精确匹配（原文）
  - 去符号匹配（`exact_no_symbol`）
  - 归一化匹配（`exact_normalized`）
  - 模糊别名匹配（`fuzzy`）
  - 拼音别名匹配（`pinyin`）
- 结果解释：返回每条命中的证据（`hit_evidences`），包含命中词、类别、命中方式、原文片段、起止位置、风险等级。

### 风险评估与可视化

- 风险等级：`high / medium / low / safe`。
- 敏感词率面板：
  - 总敏感词率 = 敏感字符数 / 文本总字符数
  - 高/中/低风险敏感词率 = 对应风险敏感字符数 / 文本总字符数
- 环图 + 指标卡联动：点击右侧指标卡，左侧环图聚焦显示对应风险占比；再次点击恢复全量视图。

### 命中词交互

- 按类别展示命中词，支持数量与风险标记。
- 命中词悬浮提示：显示“原文命中词”及“词库命中词（可能多个）”。
- 点击命中词可跳转到下方“原文高亮”对应位置。
- 同一词多次命中时，连续点击会在多个位置循环定位。
- 跳转后目标词会进行显著高亮（强化颜色 + 外圈 + 动效）。

### 词组映射（可选开关）

- 映射开关：可启用/关闭词组映射能力。
- 映射模式：
  - `incremental`（增量）：系统映射 + 用户映射
  - `override`（覆盖）：仅用户映射
- 支持导入用户映射文件（`.txt/.csv/.tsv/.map`），并自动去重、忽略注释行。

### 打码能力（前端执行）

- 勾选后打码：从“原文命中词”下拉中选择目标词后打码。
- 一键打码：按风险级别（高/中/低）批量打码，默认全选。
- 打码结果可交互：在“打码文本”中点击片段可切换“打码/取消打码”。
- 一键复制：复制当前打码文本（图标按钮）。

### 大模型辅助鉴别（可选开关）

- 前端可开启 `enable_llm_assist`，后端会在规则检测完成后调用大模型 API 进行补充判断。
- 输出辅助风险等级、是否建议人工复核、简要原因与可疑词建议。
- 辅助结论不会替代规则引擎命中结果，适用于边界文本复核场景。

---

## 系统架构

```text
Vue3 + Element Plus 前端
        |
        | HTTP (/api/*)
        v
Go + Gin 后端
        |
        | 载入词库（temp 目录）
        v
AC 自动机 + 归一化 + 模糊/拼音别名索引
```

前端开发环境通过 Vite 代理将 `/api` 转发到 `http://127.0.0.1:8008`。

---

## 项目结构

```text
.
├── src/                         # 前端源码
│   ├── pages/Home.vue           # 页面编排（输入区 + 结果区）
│   ├── components/
│   │   ├── CategoryPanel.vue    # 文本输入、类别选择、词组映射导入
│   │   ├── ResultPanel.vue      # 风险可视化、命中词、打码、高亮
│   │   ├── HighlightText.vue    # 原文高亮与焦点样式
│   │   └── StatisticsCard.vue   # 左侧词库概览卡片
│   ├── router/index.js          # 路由（当前仅首页）
│   └── main.js                  # 应用入口
├── go-sensitive-checker/        # Go 后端
│   ├── main.go                  # API 服务入口（默认 :8008）
│   ├── detector.go              # 检测主流程
│   ├── normalize.go             # 归一化与词组映射
│   ├── fuzzy_matcher.go         # 模糊别名索引
│   ├── pinyin_matcher.go        # 拼音别名索引
│   ├── detect_options.go        # 检测选项与环境开关
│   └── temp/                    # 词库目录
├── vite.config.js               # 前端代理配置
└── README.md
```

---

## 快速开始

### 环境要求

- Node.js：`^20.19.0 || >=22.12.0`
- Go：`1.23+`

### 0) 配置 DeepSeek（可选，默认关闭）

在启动后端前，先在终端配置大模型环境变量：

```bash
export SENSITIVE_LLM_API_BASE_URL="https://api.deepseek.com"
export SENSITIVE_LLM_API_KEY="你的DeepSeekKey"
export SENSITIVE_LLM_MODEL="deepseek-v4-flash"
export SENSITIVE_ENABLE_LLM_ASSIST="true"
```

> 提示：启用后，用户选择“大模型辅助鉴别”会把最多 `SENSITIVE_LLM_MAX_TEXT_RUNES` 个字符发送到所配置的第三方服务。请先完成隐私告知与授权，并勿将真实 `API Key` 提交到 Git 仓库。
> 如果你使用自建网关或代理地址，改 `SENSITIVE_LLM_API_BASE_URL` 即可。

### 1) 启动后端（Go）

```bash
cd go-sensitive-checker
go mod tidy
go run .
```

启动后默认地址：

- API 服务：[http://localhost:8008](http://localhost:8008)
- 存活检查：[http://localhost:8008/health/live](http://localhost:8008/health/live)
- 就绪检查：[http://localhost:8008/health/ready](http://localhost:8008/health/ready)（词库为空或读取失败时返回 503）

### 2) 启动前端（Vue）

```bash
# 在项目根目录（fronted）
npm install
npm run dev
```

访问：

- 前端页面：[http://localhost:5173](http://localhost:5173)

> 开发时前端会自动代理 `/api` 到 `http://127.0.0.1:8008`。

### 3) 可选：运行后端测试

```bash
cd go-sensitive-checker
go test ./...
```

### 4) 两终端最小运行流程

- 终端 A（后端）：
  - `cd go-sensitive-checker`
  - 配置 `SENSITIVE_LLM_*` 环境变量
  - `go run .`
- 终端 B（前端）：
  - 在项目根目录执行 `npm run dev`
- 浏览器访问：
  - [http://localhost:5173](http://localhost:5173)
- 页面操作：
  - 输入文本 -> 勾选类别 -> 打开“大模型辅助鉴别” -> 点击“开始检测”

---

## 核心功能说明

### 1. 文本检测与类别选择

- 输入检测文本。
- 选择一个或多个类别（初始默认不选）。
- 点击“开始检测”后请求 `/api/detect`。

### 2. 风险统计与敏感词率

结果面板包含：

- 检测总览（命中数量 + 总风险级别）。
- 敏感词率环图与指标卡。
- 类别统计（每类高/中/低/总数）。

统计口径（前端展示）：

- 总敏感词率 = 敏感字符总数 / 文本总字符数
- 高风险敏感词率 = 高风险敏感字符数 / 文本总字符数
- 中风险敏感词率 = 中风险敏感字符数 / 文本总字符数
- 低风险敏感词率 = 低风险敏感字符数 / 文本总字符数

### 3. 命中敏感词模块

- 命中词按类别卡片化展示。
- 鼠标移入词条可查看：
  - 原文命中词
  - 命中词库词（可能多个候选）
- 点击词条：
  - 跳转到原文对应位置
  - 同词多次命中时循环跳转
  - 跳转目标词显著聚焦高亮

### 4. 词组映射模块

- 可通过开关启用/停用映射。
- 支持用户导入映射文件。
- 映射模式：
  - 增量映射：系统内置映射 + 用户导入映射
  - 覆盖映射：仅用户导入映射

典型场景：将 `sh@bi`、`s-b`、`s b` 等映射/归一化后识别为同类敏感表达。

### 5. 打码模块（前端）

- 勾选后打码：对选中的“原文命中词”进行局部打码。
- 一键打码：按风险级别批量打码。
- 打码文本支持交互反选（点击片段恢复明文，再点再次打码）。
- 支持一键复制打码结果。

---

## API 说明

后端基础地址：`http://localhost:8008`

### 1) 获取检测类别

`GET /api/categories`

响应示例：

```json
{
  "political_high": "政治高敏感",
  "political_low": "政治低敏感",
  "abusive_high": "辱骂高敏感"
}
```

### 2) 文本检测

`POST /api/detect`

请求体示例：

```json
{
  "text": "你这个 sh@bi",
  "categories": ["abusive_high", "abusive_low"],
  "options": {
    "exact_match": true,
    "normalize_match": true,
    "fuzzy_match": true,
    "pinyin_match": true,
    "enable_term_mapping": true,
    "enable_llm_assist": true,
    "mapping_mode": "incremental",
    "custom_mappings": [
      { "from": "@", "to": "a" },
      { "from": "vv", "to": "w" }
    ]
  }
}
```

响应关键字段：

| 字段 | 说明 |
|---|---|
| `has_sensitive` | 是否命中敏感词 |
| `risk_level` | 总体风险等级：`safe/high/medium/low` |
| `detected_words` | 聚合后的命中词结果 |
| `categories` | 按类别聚合的命中词 |
| `hit_evidences` | 命中证据列表（含 start/end/match_type/matched_text） |
| `mask_suggestions` | 打码建议（词库词与原文命中片段映射） |
| `applied_options` | 实际生效的检测选项 |
| `llm_assist` | 大模型辅助鉴别结果（开启时返回） |
| `normalized_text` | 常规归一化文本 |
| `normalized_aggressive_text` | 强归一化文本 |

`hit_evidences` 字段说明：

| 字段 | 说明 |
|---|---|
| `word` | 命中的词库词 |
| `category` | 命中类别 |
| `match_type` | 命中方式（`exact_raw/exact_no_symbol/exact_normalized/fuzzy/pinyin`） |
| `matched_text` | 原文命中片段 |
| `start` / `end` | 原文中的 rune 下标区间（`[start,end)`） |
| `risk_level` | 该条证据的风险等级 |

`llm_assist` 字段说明：

| 字段 | 说明 |
|---|---|
| `enabled` | 本次是否开启了辅助鉴别 |
| `used` | 是否成功调用了大模型 |
| `model` | 使用的模型名 |
| `risk_level` | 辅助风险等级（`safe/low/medium/high`） |
| `should_review` | 是否建议人工复核 |
| `reason` | 辅助判断说明 |
| `suspected_terms` | 建议关注的可疑词/短语 |
| `confidence` | 置信度（0~1） |
| `latency_ms` | 大模型辅助鉴别耗时（毫秒） |
| `error` | 调用失败时的错误信息 |

`llm_assist` 响应示例：

```json
{
  "enabled": true,
  "used": true,
  "model": "deepseek-v4-flash",
  "risk_level": "medium",
  "should_review": true,
  "reason": "文本存在规避表达，建议人工复核上下文语义。",
  "suspected_terms": ["sh@bi", "sha-bi"],
  "confidence": 0.86,
  "latency_ms": 732
}
```

### 3) 词库统计

`GET /api/statistics`

返回各大类与子类词库规模统计。

### 4) 健康检查

- `GET /health/live`：进程存活检查。
- `GET /health/ready`：返回词库路径、加载文件数、缺失文件数、总词数和分类词数；未就绪时返回 HTTP 503。
- `GET /ping` -> `pong`：保留用于兼容旧版探针。

### 5) 服务状态

`GET /api/status` 返回当前词库、服务能力、请求限制与进程内运行计数。前端使用该接口展示真实词库数量，并在词库未就绪时禁止提交检测。

### 6) 热重载词库

仅在配置 `SENSITIVE_RELOAD_TOKEN` 后启用：

```bash
curl -X POST http://localhost:8008/api/admin/wordlist/reload \
  -H "Authorization: Bearer $SENSITIVE_RELOAD_TOKEN"
```

后端会先在内存中完整构建并校验新检测器，成功后原子切换；新词库无效时返回 422，继续使用旧词库，不中断正在执行的请求。

### 7) 词库管理控制台

设置 `SENSITIVE_ADMIN_TOKEN` 后访问前端 `/admin`。管理控制台提供：

- 按类别搜索和分页查看当前有效词条。
- 新增、删除词条并立即重建检测索引。
- 批量文本预检，区分新增、重复和无效词条后再确认导入。
- 每次变更前自动创建文件快照，支持查看版本和回滚。
- 查看不包含检测原文的管理审计日志。

### 8) 白名单（豁免误报）

词库根目录下可放置 `白名单.txt`，命中的词库词条在指定范围内不再计入检测结果：

```text
# 全类别豁免（每行一个词条）
普通名称XYZ
# 仅对指定类别豁免（TAB 分隔，逗号列出类别键）
免费领取\tadvertising_high
```

被白名单豁免的命中不会出现在 `hit_evidences`、统计和打码建议中。也可通过管理接口维护（需要 `SENSITIVE_ADMIN_TOKEN`）：

- `GET /api/admin/whitelist`：查看当前全量与分类别条目。
- `POST /api/admin/whitelist`：body 为 `{"word": "...", "categories": ["..."], "reason": "..."}`，categories 省略表示全类别；写入后自动重建索引并记录审计日志。
- `DELETE /api/admin/whitelist`：body 为 `{"word": "..."}`。

复核工作台（前端 `/reviews`）结案为"误报"时会产生白名单候选（`GET /api/platform/feedback-candidates`），并在页面上提供一键落地：

- `POST /api/platform/feedback-candidates/:id/apply`：将 whitelist 类型候选写入白名单文件、热重载索引，候选状态置为 applied；wordlist 类型候选走管理端快照→原子写入→校验重载的正式发布流程加入对应类别词库（版本号随响应返回）。
- `POST /api/platform/feedback-candidates/:id/dismiss`：忽略该候选。

管理控制台（`/admin`）提供白名单可视化卡片：查看全部与分类别豁免词条、多选类别添加、一键移除，变更即时生效并记入审计日志。

### 10) 复核指标看板

`GET /api/platform/review-stats` 聚合返回：待处理/处理中/已结案数量、误报占已结案比例、候选待处理/已应用数，以及按审核员的结案量、结论分布与平均处理时长。前端复核工作台（`/reviews`）顶部直接展示。

### 11) Webhook 通知

配置 `SENSITIVE_WEBHOOK_URL` 后启用（未配置则完全关闭）：

- `high_risk_hit`：检测结果风险达到 `SENSITIVE_WEBHOOK_MIN_RISK`（默认 `high`）时推送；内容只含统计摘要（风险级别、命中分布、类别），**不包含检测原文**。
- `review_backlog`：复核队列 pending 达到 `SENSITIVE_WEBHOOK_BACKLOG_THRESHOLD`（默认 20）时告警，回落后允许再次告警。
- `SENSITIVE_WEBHOOK_MAX_PER_DAY`（默认 50）限制每日推送上限，防止通知风暴。

### 12) LLM 逐命中语境复核

在全文辅助鉴别（`enable_llm_assist`）之外，可开启逐命中复核（`SENSITIVE_ENABLE_LLM_HIT_REVIEW=true`）：检测完成后，对非高风险命中的证据截取前后 60 字上下文，批量询问 LLM 该词在语境中是否为真实违规，返回三级结论：

- `confirm`：确认违规，维持命中。
- `demote`：疑似误报（如新闻转述、正常词形巧合），证据带 `llm_verdict=demote` 标注。
- `review`：语境不足，建议人工复核。

成本护栏：

- 结果按"上下文+词+类别+匹配方式"哈希缓存，相同命中不再重复调用。
- 每次检测最多 20 条命中一批，单日调用上限 `SENSITIVE_LLM_HIT_REVIEW_DAILY_LIMIT`（默认 1000），耗尽后自动熔断降级为纯规则模式，降级原因记录在 `llm_assist.error`。
- 高风险命中（`risk_level=high`）不走 LLM，直接信任规则引擎。
- 前端命中词悬浮提示中展示 LLM 复核结论与原因。

仓库自带示例词库与白名单样例（`go-sensitive-checker/temp/`），克隆后即可直接启动验证；生产环境请替换为正式词库。

### 9) 评测集

`go-sensitive-checker/evalset/samples.jsonl` 提供脱敏标注语料，格式为 JSONL（`id` / `text` / `hits`）。运行离线评测：

```bash
cd go-sensitive-checker
go run . -eval evalset/samples.jsonl
```

输出各类别 TP/FP/FN 与 Precision/Recall/F1；总体 F1 低于 0.8 时进程以退出码 2 结束，可直接接入 CI 作为词库变更门禁。

管理令牌仅保存在浏览器 `sessionStorage`，关闭标签页后自动清除。生产环境必须通过 HTTPS 使用管理控制台，并设置高强度随机令牌。

### 8) 检测策略与批量任务

访问 `/tasks` 可按已启用策略创建批量检测任务。输入内容每行作为一条独立文本，任务在后台并行处理，支持进度刷新、取消以及 CSV/JSONL 导出。

- `GET /api/policies`：获取启用的检测策略。
- `GET/PUT/DELETE /api/platform/policies/*`：管理策略。
- `POST /api/platform/tasks`：创建批量任务。
- `GET /api/platform/tasks`：查看任务列表和进度。
- `POST /api/platform/tasks/:id/cancel`：请求取消。
- `POST /api/platform/tasks/:id/retry`：使用原始输入和策略创建重试任务。
- `DELETE /api/platform/tasks/:id`：删除终态任务及其全部文件。
- `GET /api/platform/storage`：查看任务存储使用量和保留策略。
- `POST /api/platform/tasks/cleanup`：立即清理过期终态任务。
- `GET /api/platform/tasks/:id/results?format=csv`：导出结果。

策略、任务元数据和结果保存在 `SENSITIVE_DATA_PATH`。服务异常重启后，未完成任务会标记为 `interrupted`，已完成结果仍可下载。

### 9) 白名单、组合规则与质量评测

策略支持白名单短语和组合规则。白名单会在不改变 rune 位置的情况下屏蔽完整豁免片段；组合规则可以要求多个词在指定距离内共同出现，并设置风险等级与处置动作。

策略每次保存自动递增版本。批量任务会保存完整策略快照和版本，任务重试继续使用原策略版本。

访问 `/evaluations` 可提交标注样本并计算：

- TP、FP、TN、FN
- Precision
- Recall
- F1
- 误报和漏报样本索引

管理 API：

- `POST /api/platform/detect`：使用指定策略检测单条文本。
- `POST /api/platform/evaluations`：运行最多 5000 条质量评测样本。

### 10) 可解释评分与人工复核

按策略检测会返回 `risk_score`、`recommended_action` 和 `score_breakdown`。策略可配置人工复核与阻断阈值，推荐动作包括 `allow/mask/review/block`。

访问 `/reviews` 可提交、领取、释放和结案复核任务。同一任务只能由一个审核员领取；误报可生成白名单候选，漏报可生成词库候选，候选不会直接影响线上检测。

### API 错误格式

参数、容量和服务状态错误使用稳定错误码，并在响应头及响应体中返回请求 ID：

```json
{
  "error": {
    "code": "INVALID_CATEGORY",
    "message": "包含未知检测类别",
    "request_id": "4f37d9c64f2646fb9a0d73e9",
    "details": { "category": "unknown" }
  }
}
```

---

## 词组映射文件格式

支持扩展名：`.txt`, `.csv`, `.tsv`, `.map`

每行一条映射，支持以下分隔符：

- `=>`
- `->`
- `=`
- `,`
- `\t`（制表符）

示例：

```text
# 注释行会被忽略
@ => a
vv -> w
s b = sb
```

规则说明：

- 空行、注释行（`#` 或 `//` 开头）会被忽略。
- 重复映射会自动去重。
- `mapping_mode=incremental`：在系统默认映射基础上追加用户映射。
- `mapping_mode=override`：只使用用户映射。

---

## 环境变量配置（后端）

| 变量名 | 默认值 | 作用 |
|---|---|---|
| `SENSITIVE_ENABLE_NORMALIZE` | `true` | 启用归一化匹配 |
| `SENSITIVE_ENABLE_FUZZY` | `true` | 启用模糊匹配 |
| `SENSITIVE_ENABLE_PINYIN` | `true` | 启用拼音匹配 |
| `SENSITIVE_ENABLE_AUTO_PINYIN` | `true` | 自动为汉字词生成拼音别名 |
| `SENSITIVE_ENABLE_PINYIN_INITIALS` | `false` | 启用拼音首字母别名 |
| `SENSITIVE_PINYIN_ALIAS_FILE` | `temp/拼音混淆词/拼音映射.txt` | 自定义拼音别名文件路径 |
| `SENSITIVE_ENABLE_LLM_ASSIST` | `false` | 是否允许启用 LLM 辅助鉴别；开启后可能向第三方发送文本 |
| `SENSITIVE_LLM_API_BASE_URL` | `https://api.deepseek.com` | 大模型 API 基础地址（DeepSeek/OpenAI 兼容） |
| `SENSITIVE_LLM_API_KEY` | 空 | 大模型 API Key |
| `SENSITIVE_LLM_MODEL` | `deepseek-v4-flash` | 大模型名称 |
| `SENSITIVE_LLM_TIMEOUT_MS` | `10000` | 辅助鉴别请求超时（毫秒） |
| `SENSITIVE_LLM_MAX_TEXT_RUNES` | `1200` | 发送给大模型的最大字符数（rune） |
| `SENSITIVE_SERVER_ADDRESS` | `:8008` | 后端监听地址 |
| `SENSITIVE_WORDLIST_PATH` | `temp` | 词库目录 |
| `SENSITIVE_ALLOWED_ORIGINS` | 本地 Vite 地址 | 允许跨域的来源，多个值用逗号分隔 |
| `SENSITIVE_MAX_BODY_BYTES` | `1048576` | 检测请求体最大字节数 |
| `SENSITIVE_MAX_TEXT_RUNES` | `20000` | 单次检测文本最大字符数 |
| `SENSITIVE_MAX_CONCURRENT` | `8` | 同时执行的检测请求数上限 |
| `SENSITIVE_READ_TIMEOUT_SECONDS` | `10` | HTTP 读取超时 |
| `SENSITIVE_WRITE_TIMEOUT_SECONDS` | `30` | HTTP 写入超时 |
| `SENSITIVE_IDLE_TIMEOUT_SECONDS` | `60` | HTTP 空闲连接超时 |
| `SENSITIVE_SHUTDOWN_TIMEOUT_SECONDS` | `10` | 优雅停机等待时间 |
| `SENSITIVE_RELOAD_TOKEN` | 空 | 词库热重载管理令牌；为空时不注册管理接口 |
| `SENSITIVE_MAX_CUSTOM_MAPPINGS` | `500` | 单次请求允许的自定义映射数量 |
| `SENSITIVE_MAX_MAPPING_RUNES` | `128` | 单个映射源或目标的最大字符数 |
| `SENSITIVE_ADMIN_TOKEN` | 空 | 词库管理接口令牌；为空时不注册管理 API |
| `SENSITIVE_DATA_PATH` | `data` | 词库版本快照和审计日志目录 |
| `SENSITIVE_MAX_BATCH_LINES` | `10000` | 单个批量任务最大文本行数 |
| `SENSITIVE_BATCH_WORKERS` | `4` | 单个任务并行检测工作数，最大 32 |
| `SENSITIVE_MAX_CONCURRENT_TASKS` | `2` | 同时运行的批量任务数，其他任务保持排队 |
| `SENSITIVE_TASK_RETENTION_HOURS` | `168` | 终态任务及结果默认保留小时数 |
| `SENSITIVE_TASK_MAX_STORAGE_BYTES` | `10737418240` | 任务数据目录容量上限，超限时拒绝新任务 |

---

## 构建与部署

### Docker Compose

确保 `go-sensitive-checker/temp` 已放置词库，然后执行：

```bash
export SENSITIVE_ADMIN_TOKEN="请替换为高强度随机令牌"
docker compose up --build -d
```

访问 `http://localhost:8080`。后端和前端均以非 root/只读容器运行，策略、任务、审计和版本数据保存在命名卷 `sensitive-data`。

### 前端构建

```bash
npm run build
```

生成目录：`dist/`

可用任意静态服务器部署（Nginx、Vercel、Netlify 等）。

### 后端构建

```bash
cd go-sensitive-checker
go build -o sensitive-checker
./sensitive-checker
```

生产环境建议：

- 使用反向代理（Nginx）统一域名与 HTTPS。
- 对 `/api/detect` 增加请求体大小限制。
- 结合业务场景设置 CORS 白名单（当前代码为 `cors.Default()`）。

---

## 常见问题

### 1) 前端请求报错 / 404

检查是否已启动后端，并确认后端监听 `8008` 端口。

### 2) 映射导入后没生效

检查：

- 映射开关是否开启。
- 是否选择了正确模式（增量/覆盖）。
- 文件格式是否为“每行一条有效映射”。

### 3) 命中词跳转不到原文位置

通常由命中数据与原文不一致导致。请确认检测结果对应的是当前文本，重新检测后再试。

### 4) 词库修改后无变化

后端启动时加载词库，修改 `go-sensitive-checker/temp` 下文件后需要重启后端。

### 5) 大模型辅助鉴别未生效 / 报错

优先检查以下项：

- `SENSITIVE_LLM_API_KEY` 是否为空或拼写错误。
- `SENSITIVE_LLM_API_BASE_URL` 是否可访问（默认 `https://api.deepseek.com`）。
- `SENSITIVE_LLM_MODEL` 是否为可用模型（当前默认 `deepseek-v4-flash`）。
- 前端是否开启“大模型辅助鉴别”开关（对应 `enable_llm_assist=true`）。
- 响应中的 `llm_assist.error` 具体错误信息（网络、鉴权、限流等）。

---

## 后续优化预案

- 将 `StatisticsCard` 改为实时调用 `/api/statistics`（当前为静态展示值）。
- 增加公网部署的接口鉴权与限流。
- 增加前端自动化测试与后端基准测试。
- 增加 Docker / docker-compose 一键部署方案。
