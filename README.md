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

## Technology Choices

CGME is mostly a data extraction and document-formatting tool, not a traditional frontend app.

Main technical areas:
- Go CLI: command parsing, config loading, batch control, retry-friendly reports, and file output
- Browser automation: Chrome DevTools Protocol is used to open ChatGPT pages and read rendered page content
- Web page extraction: the tool inspects the ChatGPT DOM, filters UI noise, extracts conversation text, tables, code blocks, math, and image nodes
- Markdown generation: conversations are rendered into GitHub-oriented Markdown files
- Math formatting: LaTeX-like content is normalized conservatively so GitHub Markdown can render formulas more reliably
- HTML / Markdown / asset handling: image links are downloaded, rewritten to local relative paths, and stored next to the exported Markdown
- JSON reports: `warnings.json` and `export-report.json` make batch runs auditable and resumable

The project intentionally avoids a server-side database, web UI, or heavy crawler framework. The current goal is a small local tool that turns ChatGPT project pages into stable Markdown folders.

## How It Works

For official ChatGPT export bundles, CGME reads local JSON files and renders the matched conversations directly.

For live ChatGPT project URLs, CGME needs a real browser because ChatGPT pages are dynamic and direct HTTP access to internal APIs is often blocked or challenged by Cloudflare. The live export path works roughly like this:

1. Read the cookie header from `--cookie-file`.
2. Start or reuse a dedicated Chrome process with remote debugging enabled.
3. Open the ChatGPT project or conversation page in that Chrome session.
4. Use Chrome DevTools Protocol to inspect the rendered DOM after the page loads.
5. Extract conversation content, math blocks, tables, code blocks, and images from the page.
6. Download image assets when possible and rewrite Markdown links to local files.
7. Write one Markdown file per conversation, plus warnings and an export report.

This means CGME may automatically open a Chrome window. That is expected behavior, not malware or a system infection. The Chrome process is used as a controlled rendering engine so the tool can see the same ChatGPT content that a logged-in user sees in the browser.

The browser profile used by CGME is separate from your normal Chrome profile by default. The default location is under the user's application support directory, and it can be overridden with `CGME_CHROME_PROFILE_ROOT`.

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

## macOS Release Packages

Release archives are built as separate packages for Intel and Apple Silicon Macs:

```text
cgme-macos-amd64-<version>.tar.gz   # Intel Mac
cgme-macos-arm64-<version>.tar.gz   # Apple Silicon Mac
```

After downloading or unpacking one package:

```bash
tar -xzf cgme-macos-arm64-<version>.tar.gz
cd cgme-macos-arm64-<version>
./cgme --help
```

If macOS Gatekeeper blocks the binary because it is unsigned, remove the quarantine attribute:

```bash
xattr -d com.apple.quarantine ./cgme
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

## Command Reference

### `cgme export`

Common options:
- `--bundle <dir>`: path to a ChatGPT official export directory
- `--project <name>`: project name inside the official export bundle
- `--project-url <url>`: one live ChatGPT project conversation URL
- `--url-list <path>`: text file with one live ChatGPT conversation URL per line
- `--cookie-file <path>`: file containing the full ChatGPT cookie header
- `--output <dir>`: output directory
- `--assets-dir <dir>`: optional assets directory override
- `--config <path>`: optional YAML-like config file
- `--write-readme`: write output `README.md`, default `true`
- `--write-warnings`: write `warnings.json`, default `true`
- `--preserve-links`: preserve external links in Markdown, default `true`
- `--overwrite`: re-export successful batch items instead of skipping them

Source selection:
- use `--bundle` for official export files
- use `--project-url` for one live conversation
- use `--url-list` for batch live export
- use `--config` when you want repeatable settings in a file

### `cgme discover`

Options:
- `--project-page-url <url>`: ChatGPT project page URL, usually ending in `/project`
- `--cookie-file <path>`: file containing the full ChatGPT cookie header
- `--output-list <path>`: output text file for discovered conversation URLs

Typical live-project flow:

```bash
cgme discover \
  --project-page-url "https://chatgpt.com/g/.../project" \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --output-list ./math-sessions.txt

cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out
```

## Environment Variables

Most users only need `--cookie-file`. Environment variables are mainly for debugging or advanced runs.

- `CGME_CHATGPT_SESSION_COOKIE`: cookie header fallback when `--cookie-file` is not provided
- `CGME_BROWSER_WAIT_SECONDS`: seconds to wait after a ChatGPT page opens before extracting DOM content
- `CGME_CHROME_DEBUG_PORT`: Chrome DevTools port, default is `9223`
- `CGME_CHROME_PROFILE_ROOT`: dedicated Chrome profile directory for CGME

Examples:

```bash
CGME_BROWSER_WAIT_SECONDS=20 cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./retry.txt \
  --output ./out \
  --overwrite
```

## Error Recovery

For `--url-list` batch exports, CGME writes `export-report.json` after each item. This makes reruns safe.

Common recovery flow:

1. Check `export-report.json` and `warnings.json`.
2. Rerun the same command. Successful items are skipped by default.
3. If a page was partially wrong or an image needs to be fetched again, rerun with `--overwrite`.
4. If ChatGPT rate limits or Cloudflare blocks some pages, wait a few minutes and rerun the same command.
5. If pages load slowly, increase `CGME_BROWSER_WAIT_SECONDS`.

Useful commands:

```bash
jq '.summary' ./out/export-report.json

jq -r '.entries[] | select(.status == "failed") | .url' \
  ./out/export-report.json > retry.txt

CGME_BROWSER_WAIT_SECONDS=20 cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list retry.txt \
  --output ./out \
  --overwrite
```

Typical error meanings:
- `browser_dom_empty`: ChatGPT page opened, but conversation content was not ready or not visible yet; rerun or increase wait time
- `cloudflare_challenge`: ChatGPT / Cloudflare challenged the request; wait and rerun with browser-backed export
- `request_failed`: network or backend request failed; rerun later
- `browser_session_lost`: the reusable Chrome session exited; rerun the command

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

### 技术选型

CGME 本质上是一个“数据抽取 + 文档格式转换”工具，不是传统意义上的前端应用。

主要涉及这些技术方向：
- Go 命令行程序：负责参数解析、配置读取、批量控制、可恢复报告和文件输出
- 浏览器自动化：通过 Chrome DevTools Protocol 打开 ChatGPT 页面并读取渲染后的页面内容
- 网页内容抽取：从 ChatGPT DOM 中过滤界面噪声，抽取对话文本、表格、代码块、数学公式和图片节点
- Markdown 生成：把对话转换成更适合 GitHub 展示的 Markdown 文件
- 数学公式处理：保守地规范 LaTeX 风格内容，尽量提高 GitHub Markdown 中公式渲染的稳定性
- HTML / Markdown / 资源处理：下载图片，改写为本地相对路径，并放到 Markdown 同级的 `assets/` 目录
- JSON 报告：通过 `warnings.json` 和 `export-report.json` 支持审计、排错和失败重跑

项目目前刻意不引入数据库、Web UI 或大型爬虫框架。目标是保持为一个小型本地工具，把 ChatGPT 项目页稳定转换成 Markdown 目录。

### 运行原理

如果输入是 ChatGPT 官方导出包，CGME 会直接读取本地 JSON 文件，然后把匹配到的对话渲染成 Markdown。

如果输入是在线 ChatGPT 项目 URL，CGME 需要使用真实浏览器。原因是 ChatGPT 页面是动态渲染的，而且直接访问内部 API 经常会遇到 Cloudflare 或权限限制。在线导出大致流程是：

1. 从 `--cookie-file` 读取用户提供的 cookie header。
2. 启动或复用一个带远程调试端口的 Chrome 进程。
3. 在这个 Chrome 会话里打开 ChatGPT 项目页或会话页。
4. 通过 Chrome DevTools Protocol 等页面加载完成后读取渲染后的 DOM。
5. 从页面里提取对话内容、数学公式、表格、代码块和图片。
6. 尽量下载图片资源，并把 Markdown 链接改写成本地相对路径。
7. 每条会话写成一个 Markdown 文件，同时写入 warnings 和导出报告。

所以运行在线导出时，CGME 可能会自动拉起一个 Chrome 窗口。这是预期行为，不是电脑中毒，也不是恶意程序。这个 Chrome 只是作为受控的页面渲染器，让工具看到登录用户在 ChatGPT 网页中能看到的内容。

CGME 默认使用独立的 Chrome profile，不直接使用你的日常 Chrome profile。默认位置在用户的 application support 目录下，也可以通过 `CGME_CHROME_PROFILE_ROOT` 覆盖。

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

## macOS Release 包

release 包按 CPU 架构分开：

```text
cgme-macos-amd64-<version>.tar.gz   # Intel Mac
cgme-macos-arm64-<version>.tar.gz   # Apple Silicon Mac
```

解压后直接运行：

```bash
tar -xzf cgme-macos-arm64-<version>.tar.gz
cd cgme-macos-arm64-<version>
./cgme --help
```

如果 macOS 因为未签名阻止运行，可以去掉 quarantine 属性：

```bash
xattr -d com.apple.quarantine ./cgme
```

## 中文命令行参数说明

### `cgme export`

常用参数：
- `--bundle <dir>`：ChatGPT 官方导出目录
- `--project <name>`：官方导出包里的项目名
- `--project-url <url>`：单条在线 ChatGPT 项目会话 URL
- `--url-list <path>`：URL 列表文件，一行一个在线会话 URL
- `--cookie-file <path>`：包含完整 ChatGPT cookie header 的文本文件
- `--output <dir>`：输出目录
- `--assets-dir <dir>`：图片资源目录覆盖，一般不用设置
- `--config <path>`：配置文件
- `--write-readme`：输出目录中写 README，默认 `true`
- `--write-warnings`：写 `warnings.json`，默认 `true`
- `--preserve-links`：保留外部链接，默认 `true`
- `--overwrite`：覆盖重抓已经成功的条目，默认不覆盖

来源选择：
- 官方导出包用 `--bundle`
- 单条在线会话用 `--project-url`
- 批量在线导出用 `--url-list`
- 想固定一套配置时用 `--config`

### `cgme discover`

参数：
- `--project-page-url <url>`：ChatGPT 项目页 URL，通常以 `/project` 结尾
- `--cookie-file <path>`：包含完整 ChatGPT cookie header 的文本文件
- `--output-list <path>`：发现到的会话 URL 输出文件

典型在线项目导出流程：

```bash
cgme discover \
  --project-page-url "https://chatgpt.com/g/.../project" \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --output-list ./math-sessions.txt

cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./math-sessions.txt \
  --output ./out
```

## 中文环境变量

大多数用户只需要 `--cookie-file`。环境变量主要用于调试或特殊场景。

- `CGME_CHATGPT_SESSION_COOKIE`：没有传 `--cookie-file` 时的 cookie header 兜底
- `CGME_BROWSER_WAIT_SECONDS`：打开 ChatGPT 页面后等待多少秒再抽取 DOM
- `CGME_CHROME_DEBUG_PORT`：Chrome DevTools 端口，默认 `9223`
- `CGME_CHROME_PROFILE_ROOT`：CGME 专用 Chrome profile 目录

示例：

```bash
CGME_BROWSER_WAIT_SECONDS=20 cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list ./retry.txt \
  --output ./out \
  --overwrite
```

## 中文报错与重跑

批量导出 `--url-list` 时，CGME 每处理一条都会更新 `export-report.json`，所以可以安全重跑。

常见恢复流程：

1. 先看 `export-report.json` 和 `warnings.json`。
2. 原命令直接重跑，已经成功的条目默认会跳过。
3. 如果某个页面内容不完整，或图片需要重新下载，用 `--overwrite`。
4. 如果遇到 ChatGPT 限流或 Cloudflare，等几分钟后再重跑。
5. 如果页面加载慢，调大 `CGME_BROWSER_WAIT_SECONDS`。

常用命令：

```bash
jq '.summary' ./out/export-report.json

jq -r '.entries[] | select(.status == "failed") | .url' \
  ./out/export-report.json > retry.txt

CGME_BROWSER_WAIT_SECONDS=20 cgme export \
  --cookie-file ~/Desktop/gpt-cookie.txt \
  --url-list retry.txt \
  --output ./out \
  --overwrite
```

常见错误含义：
- `browser_dom_empty`：页面打开了，但对话内容还没加载出来；可以重跑或增加等待时间
- `cloudflare_challenge`：ChatGPT / Cloudflare 拦截了请求；等一会儿后用浏览器导出路径重跑
- `request_failed`：网络或后端请求失败；稍后重跑
- `browser_session_lost`：复用的 Chrome 会话退出了；重新运行命令

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
