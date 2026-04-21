# ChatGPT Math Exporter (CGME)

Language:
- English
- 中文在下方

CGME is a local Go CLI that exports ChatGPT math conversations into Markdown folders that are easy to inspect, archive, and publish to GitHub.

Current scope is intentionally narrow:
- macOS
- Google Chrome
- ChatGPT
- a valid ChatGPT session cookie provided by the user

The project is optimized for correctness and reproducibility, not speed.

## What It Can Do

- Export from an official ChatGPT export bundle (`conversations.json`)
- Export from a live ChatGPT project conversation URL
- Discover all conversation URLs from a ChatGPT project page
- Batch export from a text file with one URL per line
- Save image questions locally and rewrite Markdown links to local files
- Write `warnings.json` and `export-report.json` for audit and recovery
- Skip already completed batch items by default

## What It Does Not Try To Do

- It does not automate ChatGPT login
- It does not support other browsers or operating systems yet
- It does not try to aggressively rewrite your original question text
- It does not optimize for high throughput or parallel crawling

## Install / Build

```bash
git clone git@github.com:lenovobenben/chatgpt-math-exporter.git
cd chatgpt-math-exporter
go build -o ./bin/cgme ./cmd/cgme
```

Or run directly:

```bash
env GOCACHE=/tmp/cgme-gocache go run ./cmd/cgme --help
```

## Core Commands

```bash
cgme export --help
cgme discover --help
```

### `export`

Exports from one of these sources:
- `--bundle`
- `--project-url`
- `--url-list`

### `discover`

Crawls a ChatGPT project page and writes conversation URLs into a text file.

## Recommended Real-World Workflow

For the current implementation, the most practical workflow is:

1. Copy your current ChatGPT cookie header into a local text file
2. Run `discover` on a project page
3. Run `export` on the generated URL list
4. Review the exported Markdown and commit it to GitHub

## Usage

### 1. Export from official ChatGPT bundle

```bash
cgme export \
  --bundle ./chatgpt-export \
  --project "经典数学题100例 6" \
  --output ./out
```

### 2. Export one live ChatGPT conversation URL

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --project-url "https://chatgpt.com/g/..." \
  --output ./out
```

### 3. Discover all conversation URLs from a project page

```bash
cgme discover \
  --project-page-url "https://chatgpt.com/g/.../project" \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --output-list ./math-sessions.txt
```

### 4. Batch export from a URL list

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out
```

### 5. Re-run a batch without redoing finished items

Default behavior is to skip already successful exports:

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out
```

### 6. Force overwrite existing successful exports

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out \
  --overwrite
```

## Cookie File

`--cookie-file` should point to a plain text file that contains the full ChatGPT cookie header on one line.

Example:

```text
oai-did=...; __Secure-next-auth.session-token.0=...; __Secure-next-auth.session-token.1=...; ...
```

This file is local-only input. It is not meant to be committed into Git.

## Output Structure

Typical batch output looks like this:

```text
out/
├── README.md
├── warnings.json
├── export-report.json
├── 经典数学题100例-6-数学证明/
│   ├── 001_经典数学题100例-6-数学证明.md
│   └── assets/
│       └── image-001.png
├── 经典数学题100例-6-中线长度估计/
│   ├── 001_经典数学题100例-6-中线长度估计.md
│   └── assets/
│       └── image-001.png
```

Notes:
- one conversation becomes one Markdown file
- image assets are stored next to that Markdown file in a local `assets/` folder
- Markdown uses relative local paths such as `assets/image-001.png`

## Batch Recovery And Reports

When exporting from `--url-list`, CGME writes `export-report.json`.

This report records:
- original URL
- line number in the input list
- success / failed / skipped status
- output path
- project name
- error message for failed items
- last update time

Behavior:
- failed items do not stop the batch
- failed items do not write placeholder Markdown
- successful items are recorded immediately
- rerunning the same batch will skip already successful items by default

## Markdown Behavior

Current rendering goals:
- clear `Question` / `Answer` sections
- conservative math normalization
- avoid breaking code blocks and links
- preserve original text as much as possible

Important boundary:
- user question text is exported conservatively
- CGME does not currently try to “fix” mixed or messy user LaTeX input aggressively
- if a formula-like line is ambiguous, the project prefers caution over smart rewriting

## Images

CGME now supports image questions from live ChatGPT project URLs.

Behavior:
- detects image nodes in the ChatGPT conversation DOM
- downloads the image locally
- rewrites Markdown to a local relative path
- records asset actions in `warnings.json`

Current expectation:
- GitHub-style Markdown rendering should display these relative image paths correctly
- local preview behavior may vary between editors

## Warnings

CGME writes `warnings.json` when `--write-warnings` is enabled.

Warnings are used for traceability, for example:
- discovery or fetch status
- skipped existing items
- asset saved or asset download fallback
- conservative math transformation notices

The tool prefers warning + preservation over silent mutation.

## Config File

You can run the tool with a config file:

```bash
cgme export --config ./cgme.yaml
```

Minimal example:

```yaml
source:
  type: project_url_list
  url_list: ./math-sessions.txt
  cookie_file: /Users/you/Desktop/gpt-cookie.txt

output:
  dir: ./out

options:
  write_readme: true
  write_warnings: true
  preserve_links: true
  overwrite_existing: false
```

Supported `source.type` values:
- `bundle`
- `project_url`
- `project_url_list`

## Runtime Notes

Current live URL export uses browser-backed crawling on macOS + Chrome.

Relevant environment variables:
- `CGME_CHATGPT_SESSION_COOKIE`
- `CGME_BROWSER_WAIT_SECONDS`
- `CGME_CHROME_DEBUG_PORT`

In practice, `--cookie-file` is the preferred user-facing input.

Direct HTTP-only access to `chatgpt.com/backend-api/...` may still be blocked by Cloudflare, so browser-backed export remains the main path for live URLs.

## Design Principles

- local-first
- GitHub-oriented output
- conservative text handling
- explicit warnings
- resumable batch export
- low-speed, low-risk automation

## Current Limitations

- project discovery depends on the current ChatGPT web UI and may break if the page changes
- math handling is heuristic-based
- some editor previews may differ from GitHub rendering
- the project currently focuses on ChatGPT only

## Development

Useful commands:

```bash
env GOCACHE=/tmp/cgme-gocache go run ./cmd/cgme --help
env GOCACHE=/tmp/cgme-gocache go test ./...
```

Repository layout:

```text
.
├── cmd/
│   └── cgme/
├── internal/
│   ├── cli/
│   ├── config/
│   └── exporters/
├── README.md
└── go.mod
```

## 中文说明

CGME 是一个本地 Go 命令行工具，用来把 ChatGPT 数学对话导出为适合整理和上传 GitHub 的 Markdown 目录。

当前范围刻意收窄，只做这一条链路：
- macOS
- Chrome
- ChatGPT
- 用户自己提供有效 cookie

当前重点是“稳”和“可恢复”，不是“快”。

### 当前能做什么

- 从 ChatGPT 官方导出包 `conversations.json` 导出
- 从单条 ChatGPT 项目会话 URL 导出
- 从 ChatGPT 项目页自动发现全部会话 URL
- 从 URL 列表批量导出
- 把题目图片下载到本地并改写为相对路径
- 输出 `warnings.json` 和 `export-report.json`
- 批量导出默认跳过已经成功的条目

### 当前不做什么

- 不自动登录 ChatGPT
- 不支持其他浏览器或其他操作系统
- 不激进改写你的原始提问文字
- 不追求高并发或高吞吐

## 中文使用方式

### 1. 从官方导出包导出

```bash
cgme export \
  --bundle ./chatgpt-export \
  --project "经典数学题100例 6" \
  --output ./out
```

### 2. 导出单条在线会话

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --project-url "https://chatgpt.com/g/..." \
  --output ./out
```

### 3. 从项目页发现全部题目 URL

```bash
cgme discover \
  --project-page-url "https://chatgpt.com/g/.../project" \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --output-list ./math-sessions.txt
```

### 4. 从 URL 列表批量导出

```bash
cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out
```

### 5. 失败恢复与跳过策略

批量导出时会写 `export-report.json`。

它会记录：
- 原始 URL
- 输入文件中的行号
- 成功 / 失败 / 跳过
- 输出文件路径
- 项目名
- 错误信息
- 更新时间

行为约定：
- 某一条失败，不会中断整个批次
- 失败条目不写 placeholder Markdown
- 成功条目会立刻记入报告
- 下次重跑默认跳过已经成功的条目
- 只有显式传 `--overwrite` 才会重抓

### 6. 图片保存

当前已经支持在线题目中的图片导出：
- 会在页面里识别图片节点
- 把图片下载到当前 Markdown 同级目录下的 `assets/`
- 把 Markdown 链接改成相对路径，如 `assets/image-001.png`

目录一般类似这样：

```text
out/
├── README.md
├── warnings.json
├── export-report.json
├── 经典数学题100例-6-数学证明/
│   ├── 001_经典数学题100例-6-数学证明.md
│   └── assets/
│       └── image-001.png
```

### 7. cookie 文件格式

`--cookie-file` 指向一个本地文本文件。文件内容是一整行完整的 ChatGPT cookie header。

例如：

```text
oai-did=...; __Secure-next-auth.session-token.0=...; __Secure-next-auth.session-token.1=...; ...
```

这个文件只作为本地输入，不应提交到 Git 仓库。

## 中文配置文件示例

```yaml
source:
  type: project_url_list
  url_list: ./math-sessions.txt
  cookie_file: /Users/you/Desktop/gpt-cookie.txt

output:
  dir: ./out

options:
  write_readme: true
  write_warnings: true
  preserve_links: true
  overwrite_existing: false
```

支持的 `source.type`：
- `bundle`
- `project_url`
- `project_url_list`

## 设计原则

- 本地优先
- 输出适合 GitHub
- 文本处理保守
- 所有启发式行为尽量写 warning
- 批量导出可恢复
- 宁可慢一点，也不要冒进

## 当前限制

- 项目页发现逻辑依赖当前 ChatGPT 网页结构，网页改版后可能失效
- 数学格式处理仍然是启发式，不是完整 TeX 解析器
- 不同本地编辑器的预览效果可能和 GitHub 不完全一致
- 当前只专注 ChatGPT，不考虑其他平台

## 开发命令

```bash
env GOCACHE=/tmp/cgme-gocache go run ./cmd/cgme --help
env GOCACHE=/tmp/cgme-gocache go test ./...
```
