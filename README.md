# ChatGPT Math Exporter (CGME)

🌍 Language / 语言:

* 🇺🇸 English
* 🇨🇳 中文（下方）

---

## ✨ What is this?

**ChatGPT Math Exporter (CGME)** is a local-first tool that converts ChatGPT conversations — especially math-heavy ones — into clean, structured, GitHub-ready Markdown.

It is designed for people who use ChatGPT to solve math problems and want to turn those conversations into publishable notes.

ChatGPT is the first supported platform; support for other AI assistants such as Gemini and Claude is planned later.

---

## 🚀 Features

### 📦 Export

* Export from:

  * ChatGPT official export (`conversations.json`)
  * ChatGPT Project URL (with a valid ChatGPT session cookie)
* Batch export by project

---

### 🧠 Math-aware processing (Core Feature)

* Detect LaTeX expressions automatically
* Normalize math syntax
* Convert Unicode symbols:

| Symbol | LaTeX  |
| ------ | ------ |
| ∞      | \infty |
| ≤      | \le    |
| ≥      | \ge    |
| →      | \to    |
| ∈      | \in    |

* Auto-wrap formulas:

  * Inline → `$...$`
  * Block → ```math

* Avoid breaking:

  * code blocks
  * links
  * normal text

* Output warnings for uncertain cases

---

### 🖼 Planned asset handling

* Download all images locally
* Replace external links with local paths
* Deduplicate via hash
* Preserve original formats

---

### 🧾 Markdown output

* One conversation = one `.md` file
* Clear Q/A separation:

```md
## 🧠 Question

...

## 🤖 Answer

...
```

* GitHub-friendly rendering
* Math supported

---

### 🔗 Current link handling

* Preserve links that already appear in exported text
* Reference indexing is not implemented yet

---

### 📁 Output structure

```text
output/
├── project-name/
│   ├── 001_problem.md
│   ├── 002_problem.md
├── assets/
│   ├── conv_xxx/
├── README.md
├── warnings.json
```

---

## 🧑‍💻 Usage

### From official export

```bash
cgme export \
  --bundle ./chatgpt-export \
  --project "经典数学题100例 6" \
  --output ./out
```

---

### From Project URL

```bash
cgme export \
  --project-url "https://chatgpt.com/..." \
  --output ./out
```

---

## 🛠 Development Status

This repository is now in an **early working stage**.

Implemented today:

* runnable Go CLI
* config file loading (`json` / simple `yaml`)
* official ChatGPT export parsing from `conversations.json`
* Markdown rendering with basic Q/A structure
* conservative math normalization for common Unicode symbols
* conservative ` ```math ` block wrapping for standalone formula lines
* `warnings.json` output for heuristic transforms
* ChatGPT Project URL parsing
* macOS + Chrome Project URL fetching with a valid ChatGPT session cookie

Still incomplete:

* image and asset downloading
* richer ChatGPT content node support
* stronger formula detection and inline math wrapping
* support beyond the current macOS + Chrome + ChatGPT scope

---

## 🧪 Go Development

Local commands:

```bash
go run ./cmd/cgme --help
go run ./cmd/cgme export --bundle ./chatgpt-export --output ./out
go build -o ./bin/cgme ./cmd/cgme
```

Current implementation status:

* `bundle` export is usable
* `project_url` export works on the current target path: macOS + Chrome + valid ChatGPT session cookie
* the HTTP-only fetch path is kept as a fallback, but Cloudflare can block it

---

## 🧭 Product Direction

CGME is intended to be a practical desktop-side tool, not a hosted service.

Product constraints:

* Run locally on the user's machine
* Require no programming knowledge for normal use
* Work with a single executable whenever possible
* Prefer sensible defaults over mandatory configuration
* Produce a clean folder structure that users can review and push to GitHub directly

This means the UX should support both:

* simple one-command usage for ordinary users
* optional config-file workflows for repeatable batch export

---

## ⚙️ Runtime Model

The expected release form is:

* `cgme` executable
* command-line flags for direct use
* optional config file for reusable settings
* no required database
* no required web backend

Recommended usage tiers:

### Simple mode

For users who do not want to learn configuration:

```bash
cgme export --bundle ./chatgpt-export --output ./my-notes
```

For a live ChatGPT conversation URL on macOS with Chrome and a valid session cookie:

```bash
export CGME_CHATGPT_SESSION_COOKIE='__Secure-next-auth.session-token=...'
cgme export --project-url "https://chatgpt.com/..." --output ./my-notes
```

### Repeatable mode

For users who export regularly:

```bash
cgme export --config ./cgme.yaml
```

Example config shape:

```yaml
source:
  type: bundle
  path: ./chatgpt-export
  project: 经典数学题100例 6

output:
  dir: ./my-notes
  assets_dir: ./my-notes/assets

options:
  write_readme: true
  write_warnings: true
  preserve_links: true
```

## 🌐 Browser Runtime Notes

`project_url` export is currently scoped to:

* macOS
* Google Chrome
* ChatGPT
* a valid session cookie supplied by the user

Useful environment variables:

* `CGME_CHATGPT_SESSION_COOKIE`
  ChatGPT session cookie header used for live `project_url` export.
* `CGME_BROWSER_WAIT_SECONDS`
  Extra wait time after page navigation before DOM extraction
* `CGME_CHROME_DEBUG_PORT`
  Override the temporary Chrome DevTools port

Known limitation:

* direct HTTP requests to `chatgpt.com/backend-api/...` may be blocked by Cloudflare even with valid cookies, so browser-backed export remains the primary live URL path

---

## 🧱 Current Repository Layout

Current layout:

```text
.
├── cmd/
│   └── cgme/
├── README.md
├── internal/
│   ├── cli/
│   ├── config/
│   └── exporters/
└── go.mod
```

Module intent:

* `cli/`: command-line entry points and argument parsing
* `exporters/`: bundle loading, live project URL fetching, Markdown rendering, and math-aware transforms
* `config/`: config file loading, defaults, and validation

---

## 🧪 Early Development Principles

Before optimizing for speed or framework choice, the first implementation should preserve these contracts:

* Input should remain reproducible from local files
* Export output should be deterministic for the same source data
* Math transformations should be traceable and conservative
* Uncertain conversions should emit warnings instead of silently mutating content
* Asset downloads and rewrites should be isolated from text processing
* The default CLI path should be understandable by non-technical users

For the first milestone, correctness matters more than automation breadth.

---

## 👤 Usability Requirements

Because the target user may not be a programmer, the executable should behave like a tool, not like a framework.

Minimum UX requirements:

* Clear help text from `cgme --help`
* Human-readable error messages with next-step guidance
* Safe defaults for output paths and file naming
* Minimal required arguments for common export flows
* Config file support, but never config-file-only
* Warnings should be understandable without reading source code

If a feature makes the tool harder to explain, it should justify its complexity.

---

## 🗺 MVP Roadmap

Recommended order for implementation:

1. Read `conversations.json` and extract a single conversation into a stable internal structure
2. Render plain Markdown with clean question/answer sections
3. Add math-aware normalization with warnings for ambiguous cases
4. Add project-level batch export
5. Add local asset downloading and link rewriting
6. Add Project URL import as a separate adapter layer

The next priority is export quality: improving message extraction, math rendering, and publish-ready Markdown while staying inside the current macOS + Chrome + ChatGPT scope.

---

## ✅ Definition Of Done For v0

A first usable version should be considered done only if it can:

* Export at least one real ChatGPT official bundle end-to-end
* Produce readable Markdown without breaking code blocks or links
* Convert common Unicode math symbols into LaTeX safely
* Emit a machine-readable `warnings.json`
* Save remote images locally and rewrite references
* Generate a folder users can inspect and push to GitHub directly
* Support both direct CLI flags and config-driven execution
* Pass fixture-based regression tests on representative math conversations

---

## 🤝 Contribution Notes

Because this is an early-stage repository, contributors should avoid unnecessary abstraction and preserve the single-binary goal.

Preferred working style:

* Add sample inputs before adding transformation logic
* Keep parsing, math normalization, and rendering as separate layers
* Prefer snapshot or fixture-based tests for exporter output
* Document any heuristic with at least one failing and one passing example
* Avoid silent formatting changes that make diffs hard to review
* Keep dependencies justified, especially anything that complicates binary distribution

---

## 🧠 Design Philosophy

* Local-first (your data stays local)
* Math-first (not generic export)
* GitHub-friendly output
* Safe transformation
* Extensible architecture

---

## ⚠️ Limitations

* Formula detection is heuristic-based
* Some expressions may need manual review
* Project crawling may break if UI changes

---

## 📜 License & Content

This tool does **not claim ownership** of exported content.

Content may originate from:

* User inputs
* ChatGPT outputs
* Public knowledge

You are free to use exported results.

---

## 🔥 Future Plans

* Better math parsing
* Static site generation (MkDocs / Docusaurus)
* PDF export
* Obsidian support

---

## 🧭 Vision

> Turn ChatGPT math conversations into publishable knowledge.

This is not just an exporter —
this is a **math knowledge pipeline**.

---

# 🇨🇳 中文说明

---

## ✨ 这是什么？

**ChatGPT 数学导出工具（CGME）** 是一个本地工具，用于将 ChatGPT 中的数学对话导出为结构清晰、支持 LaTeX、适合在 GitHub 展示的 Markdown 文档。

当前优先支持 ChatGPT，后续会逐步支持 Gemini、Claude 等其他 AI 大模型平台。

---

## 🚀 功能特点

### 📦 导出能力

* 支持：

  * ChatGPT 官方导出数据（conversations.json）
  * ChatGPT Project URL（提供有效 ChatGPT session cookie）
* 支持按项目批量导出

---

### 🧠 数学公式处理（核心）

* 自动识别 LaTeX
* 标准化公式格式
* Unicode 转 LaTeX：

| 符号 | LaTeX  |
| -- | ------ |
| ∞  | \infty |
| ≤  | \le    |
| ≥  | \ge    |
| →  | \to    |
| ∈  | \in    |

* 自动补全：

  * 行内：`$...$`
  * 块级：```math

* 避免破坏：

  * 代码块
  * 链接
  * 普通文本

* 对不确定公式输出 warning

---

### 🖼 规划中的图片处理

* 所有图片本地保存
* 替换为相对路径
* 自动去重
* 保留原始格式

---

### 🧾 Markdown 输出

* 一个会话一个 Markdown 文件
* 问答结构清晰：

```md
## 🧠 问题

...

## 🤖 解答

...
```

* GitHub 可直接展示

---

### 🔗 当前链接处理

* 保留导出文本中原本存在的论文 / 网站链接
* 参考索引尚未实现

---

### 📁 输出结构

```text
output/
├── 项目名称/
├── assets/
├── README.md
├── warnings.json
```

---

## 🧑‍💻 使用方法

```bash
cgme export --bundle ./chatgpt-export --output ./out
```

或：

```bash
cgme export --project-url "<你的项目URL>" --output ./out
```

---

## 🛠 当前开发状态

这个仓库目前处于**早期可用阶段**。

当前已经具备这些能力：

* Go CLI 可直接运行
* 支持从官方导出包 `conversations.json` 读取会话
* 支持将会话导出为基础问答结构的 Markdown
* 支持保守的数学符号标准化和 `warnings.json`
* 支持在当前限定范围内做 Project URL 导出：
  `macOS + Chrome + 有效 ChatGPT session cookie`

仍未完成的部分：

* 图片与其他资产下载
* 更丰富的 ChatGPT 内容节点支持
* 更稳健的公式识别与正文排版
* 超出当前限定范围之外的平台支持

---

## 🧪 Go 开发方式

本地开发命令：

```bash
go run ./cmd/cgme --help
go run ./cmd/cgme export --bundle ./chatgpt-export --output ./out
go build -o ./bin/cgme ./cmd/cgme
```

当前实现状态：

* `bundle` 导出已经可用
* `project_url` 导出在当前目标路径下可用：`macOS + Chrome + 有效 ChatGPT session cookie`
* 纯 HTTP 抓取链路仍保留为 fallback，但可能被 Cloudflare 拦截

---

## 🧭 产品方向

CGME 的目标是一个运行在用户本地机器上的工具，而不是在线服务。

产品约束如下：

* 本地运行
* 普通用户也能正常使用
* 尽量以单个可执行文件交付
* 优先提供合理默认值，而不是强依赖复杂配置
* 输出结果应是一个结构清晰、可直接推送到 GitHub 的目录

这意味着交互层应同时支持：

* 普通用户的一条命令直接导出
* 高频用户通过配置文件复用导出规则

---

## ⚙️ 运行模型

预期发布形态：

* `cgme` 可执行文件
* 支持命令行参数直接运行
* 支持配置文件复用设置
* 不依赖数据库
* 不依赖 Web 后端

建议提供两种使用层级：

### 简单模式

给不想研究配置的用户：

```bash
cgme export --bundle ./chatgpt-export --output ./my-notes
```

如果是实时 Project URL：

```bash
export CGME_CHATGPT_SESSION_COOKIE='__Secure-next-auth.session-token=...'
cgme export --project-url "https://chatgpt.com/..." --output ./my-notes
```

### 可复用模式

给需要重复导出的用户：

```bash
cgme export --config ./cgme.yaml
```

配置文件示例结构：

```yaml
source:
  type: bundle
  path: ./chatgpt-export
  project: 经典数学题100例 6

output:
  dir: ./my-notes
  assets_dir: ./my-notes/assets

options:
  write_readme: true
  write_warnings: true
  preserve_links: true
```

---

## 🧱 当前仓库结构

当前仓库的高层结构：

```text
.
├── cmd/
│   └── cgme/
├── README.md
├── internal/
│   ├── cli/
│   ├── config/
│   └── exporters/
└── go.mod
```

各目录当前职责如下：

* `cli/`：命令行入口和参数解析
* `exporters/`：官方导出解析、Project URL 抓取、Markdown 渲染与数学处理
* `config/`：配置文件加载、默认值与校验

---

## 🧪 早期开发原则

在考虑性能和框架之前，第一版实现应该优先守住这些约束：

* 相同输入应得到可复现的输出
* 数学转换应尽量保守、可追踪
* 不确定的公式处理必须输出 warning，而不是静默修改
* 图片处理逻辑应与文本处理逻辑解耦
* 本地文件导出链路应先于网页抓取链路完成
* 默认命令行体验应让非技术用户也能理解

第一阶段应优先保证正确性，而不是功能铺得很宽。

---

## 👤 易用性要求

由于目标用户不一定懂编程，可执行文件必须更像“工具”，而不是“开发框架”。

最低要求应包括：

* `cgme --help` 输出清晰、可读
* 报错信息是人能看懂的，并给出下一步提示
* 常见导出流程只需要极少参数
* 支持配置文件，但不能强迫用户先学配置文件
* warning 内容应让普通用户也能理解
* 默认输出路径和文件命名应尽量安全、稳定

如果一个功能让工具明显更难解释，它就必须有足够高的价值。

---

## 🗺 MVP 开发顺序

建议按这个顺序实现：

1. 读取 `conversations.json`，抽取单个会话为内部结构
2. 输出基础 Markdown，保证问答结构稳定
3. 加入数学公式识别、标准化和 warning 机制
4. 支持按项目批量导出
5. 加入图片本地化和引用替换
6. 将 Project URL 导入作为单独适配层接入

接下来的重点不再是扩浏览器登录方式，而是把导出质量做扎实：消息抽取、数学排版、Markdown 成品感。

---

## ✅ v0 完成标准

一个可用的首版，至少应满足：

* 能完整处理至少一个真实的 ChatGPT 官方导出包
* 输出 Markdown 时不破坏代码块和链接
* 能安全转换常见 Unicode 数学符号为 LaTeX
* 生成可机器读取的 `warnings.json`
* 能把远程图片保存到本地并重写引用
* 生成的目录可由用户直接检查并 push 到 GitHub
* 同时支持命令行直传参数和配置文件运行
* 对代表性的数学对话样例通过回归测试

---

## 🤝 贡献说明

由于项目还在早期，建议避免不必要的抽象，并保持“单二进制可分发”这个目标不被破坏。

更推荐的开发方式：

* 先补样例输入，再写转换逻辑
* 解析、数学处理、渲染三层尽量分离
* 优先使用 fixture / snapshot 测试验证导出结果
* 每条启发式规则都应至少附一个成功样例和一个失败样例
* 避免无说明的格式调整，降低 diff 审查成本
* 每引入一个依赖，都要说明它是否影响最终二进制分发

---

## 🧠 设计理念

* 本地优先（不上传数据）
* 面向数学（不是普通导出工具）
* 输出即发布
* 可扩展

---

## ⚠️ 注意事项

* 数学公式识别非 100% 完美
* 少量情况需要人工检查
* 页面抓取可能随版本变化

---

## 🧭 项目目标

> 将 ChatGPT 中的数学对话，转化为可发布的知识。

这不仅是导出工具，而是：

👉 一个数学知识整理系统

---
