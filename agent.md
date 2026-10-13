# Pic2Video — 开发约定与实现状态（最小 Demo）

本文件记录 Pic2Video 第一版的产品目标、当前实现、开发规则和验收标准，作为后续实现与协作的依据。修改功能、接口、配置或启动方式时，应同步更新本文件和 README。

## 当前状态

截至 2026-10-13，最小 Demo 已实现并完成本地验收：

- 共享密码登录、退出、会话持久化和任务媒体鉴权。
- 单图上传与预览、商品描述、生成参数选择。
- 异步任务、状态展示、共享分页历史、视频播放与下载、确认删除。
- SQLite 和本地文件持久化、任务恢复、防重复提交及 Windows 删除回滚。
- `mock` 与 `seedance` 两种 Provider、Windows 构建和启动脚本。

默认使用 `mock`，返回固定演示短片。Seedance 调用代码及本地接口合同测试已完成，但尚未使用真实 API Key 验证真实视频生成。当前成果是可运行 Demo，Windows 服务器上的实际部署与真实 API 验收仍待完成。

## 1. 项目目标

构建供内部团队使用的商品展示视频生成工具：

1. 使用共享密码登录网页。
2. 上传一张商品图片，填写商品描述。
3. 选择视频参数，提交生成任务。
4. 后端调用火山引擎 Seedance API。
5. 查看任务状态，在完成后预览、下载视频。
6. 查看共享历史记录，手动删除不再需要的任务及文件。

第一版聚焦商品展示短片，不包含配音、字幕、背景音乐、视频剪辑、多图参考、批量生成和独立用户账号。

## 2. 技术与部署约定

- **后端**：Go 1.25+，使用标准库 `net/http`；模块为 `github.com/Knni9ht/Pic2Video`。
- **前端**：Vue 3、TypeScript、Vite，源代码放在 `web/`。
- **开发依赖**：Node.js 22.12+ 和 npm；前端依赖版本以 `package-lock.json` 为准。
- **数据库**：SQLite，使用 `modernc.org/sqlite` 纯 Go 驱动，保证 Windows 构建不依赖 CGO。
- **文件存储**：本地 `data/` 目录保存数据库、商品图片和生成的视频。
- **部署**：Windows 服务器单实例运行；生产构建将前端静态资源嵌入 Go 可执行文件。
- **开发**：前端使用 Vite 开发服务，通过代理访问 Go 后端。
- **界面**：中文、浅色、简洁，兼容桌面与手机访问。
- **持久化**：文件和任务记录保留到手动删除；升级程序不得覆盖数据目录。

第一版无需 Redis、对象存储或独立消息队列。

### 代码职责

| 位置 | 职责 |
| --- | --- |
| `cmd/server/main.go` | 配置加载、服务入口、工作器启动和关闭 |
| `internal/config` | 环境配置校验、`.env` 加载 |
| `internal/store` | SQLite 任务及会话持久化 |
| `internal/app` | HTTP 接口、鉴权、上传、工作器及删除恢复 |
| `internal/provider` | 生成接口、Mock、Seedance 和嵌入的演示 MP4 |
| `web/src` | Vue 页面、API 调用、状态轮询和样式 |
| `web/embed.go` | 将构建后的 `web/dist` 嵌入 Go 程序 |
| `scripts` | Windows 构建、启动、开发、HTTP 验证及样片制作 |

## 3. 功能与接口约定

### 登录与访问

- 团队使用一个共享密码，所有成员共享任务记录。
- 密码通过服务器配置设置。
- 登录使用 HttpOnly、SameSite=Lax 会话 Cookie，有效期为 7 天；会话摘要保存在 SQLite，服务重启后保留。
- 会话绑定当前共享密码，修改密码并重启后，旧会话失效。
- 登录按客户端连接 IP 限速，5 分钟内最多 5 次尝试；成功登录清除计数。不直接信任客户端提供的转发 IP 头。
- 网页业务接口、图片、视频及下载接口统一鉴权。
- 修改接口拒绝浏览器标识的跨站请求，保留 JSON 登录、Cookie 和请求幂等标识等现有防护。
- 公网访问通过 HTTPS 反向代理提供服务。

### 图片与视频制作

- 单次上传一张 JPG、PNG 或 WebP 图片，文件不超过 10 MB。
- 后端校验实际图片格式、尺寸和文件大小，不能只信任扩展名或浏览器声明。
- 图片每边须为 300–6000 像素，宽高比须为 0.4–2.5；后端完整解码以拒绝损坏图片。
- 商品描述必填，去除首尾空白后为 1–2000 个字符。
- 默认生成参数为 **5 秒、720p、9:16**。
- 时长可选 5 或 10 秒；比例可选 9:16、16:9、1:1。
- 后端使用固定提示词模板组织商品描述，要求保持商品外观、不编造卖点。
- 参数必须符合所配置模型的能力；不支持的参数应明确报错。

### HTTP 接口

统一使用 `/api` 前缀：

- `POST /api/login`、`POST /api/logout`、`GET /api/session`：会话管理。
- `POST /api/jobs`：通过 multipart 提交图片、描述和生成参数。
- `GET /api/jobs`：分页查询共享任务历史。
- `GET /api/jobs/{id}`：查询任务详情和状态。
- `DELETE /api/jobs/{id}`：删除已结束的任务及关联文件。
- `GET /api/jobs/{id}/image`：读取商品图片。
- `GET /api/jobs/{id}/video`：播放或下载已保存的视频，支持 Range 请求。

失败响应提供稳定错误码和中文提示，不暴露密钥、服务器路径或完整上游敏感响应。

接口合同：

- 会话查询返回 `{authenticated, provider}`；未登录时该接口仍返回 200，任务和媒体接口返回 401。
- 创建任务接收 `image`、`description`、`duration`、`ratio`、`resolution`，并要求 `Idempotency-Key` 请求头；成功返回 202 和任务对象。
- 幂等标识长度为 8–128 字符；相同标识和内容返回原任务，不同内容返回 409。
- 任务列表接收 `page`、`page_size`，默认 1、12，返回 `{items, total, page, page_size}`。
- 任务对象包含 `id`、`description`、`duration`、`ratio`、`resolution`、`status`、`provider`、`created_at`、`updated_at`、`error`、`image_url`、`video_url`；时间使用 RFC3339，服务器路径与远程任务信息不对前端公开。
- 错误格式为 `{error: {code, message}}`；删除和退出登录成功返回 204。
- 视频接口支持 Range 请求，添加 `?download=1` 返回下载附件。

## 4. 视频生成与任务处理

### Provider 分离

已实现统一的视频生成 Provider 接口，分别提供：

- `mock`：模拟异步生成，使用明确标注为演示的固定视频。
- `seedance`：调用火山引擎官方创建任务和查询任务接口。

Provider 通过 `Submit` 创建任务、`Check` 查询结果、`Download` 读取视频流。HTTP 层不直接拼接上游请求，任务工作器不依赖具体 Provider。

Mock 样片固定为 **5 秒、360×640、静音 MP4**，带有 `DEMO` 标识，与上传图片、描述和所选参数无关。页面必须明确说明模拟行为，不能将样片描述为 AI 实际生成的结果。描述包含 `[模拟失败]` 时触发模拟失败，供开发验证使用。

默认使用模拟模式，便于尚未申请 API 时开发。真实模式配置错误时直接报错，不自动降级为模拟模式。

程序读取当前工作目录中的 `.env`，已有进程环境变量优先；加载兼容 UTF-8 BOM。配置变更需要重启服务。

| 配置项 | 默认值或要求 |
| --- | --- |
| `APP_PASSWORD` | 必填，禁止在文档中记录真实密码 |
| `VIDEO_PROVIDER` | 默认 `mock`，可设为 `seedance` |
| `HOST` | 默认 `127.0.0.1`，按部署需求配置监听地址 |
| `PORT` | 默认 `8080` |
| `DATA_DIR` | 默认 `./data`，相对于工作目录 |
| `COOKIE_SECURE` | 默认 `false`，HTTPS 部署时设为 `true` |
| `ARK_API_KEY` | Seedance 模式必填，仅保存在后端 |
| `ARK_MODEL_ID` | Seedance 模式必填，使用账号已开通模型 |
| `ARK_BASE_URL` | 默认 `https://ark.cn-beijing.volces.com/api/v3`，真实模式要求有效 HTTPS 地址 |

API Key 仅由后端读取。模型 ID 使用账号实际开通的、支持单图首帧生成的模型，不猜测或硬编码不可用型号。

### 异步任务

- 先保存本地任务，再由后台工作器处理；默认一次处理一个生成任务。
- 状态为 `queued`、`submitting`、`running`、`downloading`、`succeeded`、`failed`、`unknown`。
- 创建任务接口快速返回本地任务 ID，不等待视频生成完成。
- 图片通过 Base64 传给 Seedance，避免要求服务器图片具有公网地址。
- 保存远程任务 ID，通过后台轮询更新状态，服务重启后恢复已有任务查询。
- 工作器仅处理当前 Provider 的待办任务；切换 Provider 时保留其他模式的任务状态，切回后可继续处理。
- 生成完成后立即下载视频到本地，保存成功后才标记为完成。
- 视频先写入临时文件并校验，再重命名到最终路径；当前下载大小上限为 512 MB。
- 前端显示排队、生成、保存、完成或失败等真实状态，不伪造百分比。
- 页面关闭或刷新不影响后台任务。

### 异常与重复请求

- 前端同一份素材复用幂等标识，成功提交后禁用原表单的再次提交；修改素材或主动重新使用失败任务素材时创建新标识。
- 后端以唯一幂等标识和内容摘要检查重复请求，不能只依赖前端按钮禁用。
- 查询间隔默认 2 秒，连续查询失败 3 次或轮询超过 2 小时后标记 `unknown`，要求人工核实；下载最多尝试 3 次。
- 创建远程任务结果不明时，不盲目重试收费请求；记录异常并提示人工处理。
- 服务在 `submitting` 阶段中断时，恢复后标记 `unknown`；已有远程任务 ID 的正常生成任务继续查询，不重新创建。
- 用户主动重新生成时创建新任务，保留原任务记录。
- 下载失败时优先重试保存已有视频，避免重新生成。
- 运行中的任务暂不允许删除；其他任务删除前需在页面确认。
- 删除先暂存重命名关联文件，再删除数据库记录，失败时回滚；启动时恢复或清理遗留 `.delete` 文件，避免 Windows 文件占用造成可见任务缺失部分素材。

官方接口以[创建任务文档](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)和[查询任务文档](https://docs.volcengine.com/docs/ark/get-video-generation-task-api?lang=zh)为准。

## 5. 开发规则与验收

- 使用中文沟通和编写项目说明；代码命名遵循 Go、TypeScript 常规风格。
- 后续开发应在现有可运行闭环上增量实现，保持模拟模式可以独立开发和验收。
- 前端展示逻辑、任务业务逻辑、Provider 调用和存储逻辑分离。
- 密钥、密码、数据库、上传文件、生成视频和构建产物加入忽略规则，不提交到 Git。
- 提供不含真实凭据的配置示例、Windows 开发与构建脚本、中文 README。
- 修改代码后执行与改动相关的验证；纯文档修改核对内容和格式即可。
- `.env`、数据目录、`.cache`、依赖目录、静态构建产物、可执行文件和 TypeScript 构建缓存均不提交到 Git。

### 运行与构建

在仓库根目录使用 PowerShell；首次使用从 `.env.example` 创建 `.env` 并设置自己的密码，已有配置不得覆盖。

```powershell
# 完整构建：前端锁定依赖安装、类型检查、Go 测试、无 CGO 打包
powershell -ExecutionPolicy Bypass -File scripts/build.ps1

# 启动已构建程序
powershell -ExecutionPolicy Bypass -File scripts/start.ps1

# Go 开发服务；前端热更新在另一个终端启动
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1
npm --prefix web run dev
```

默认访问 `http://127.0.0.1:8080`，构建产物为 `bin/Pic2Video.exe`。Windows 下替换正在运行的可执行文件前，应先停止对应服务；不得停止无法确认归属的进程。

前端资源必须先构建，再进行 Go 编译，因为 `web/embed.go` 嵌入 `web/dist`。运行已打包程序不需要 Node.js、Go、Python 或 FFmpeg；Python/Pillow/FFmpeg 仅用于重新制作固定样片。

Vite 默认将 `/api` 代理到 `http://127.0.0.1:8080`。修改后端端口时同步调整代理；HTTP 开发环境保持 `COOKIE_SECURE=false`。

### 验证命令

```powershell
go test ./...
npm --prefix web run build

# 仅对运行中的 mock 服务执行，不在文档中填写实际密码
$env:APP_PASSWORD = '与服务一致的密码'
node scripts/smoke.mjs
```

HTTP 验证脚本支持 `SMOKE_URL`，创建并删除测试任务，在 `.cache/smoke-product.png` 生成测试图片。共享的实际任务或用户文件不得用作可删除测试样本。

验收至少覆盖：

- 未登录时无法访问任务和媒体。
- 合法图片可提交，非法格式和超限文件被拒绝。
- 模拟模式完整跑通上传、状态更新、预览、下载和删除。
- 刷新网页后仍能查看任务；重启服务后恢复已提交任务。
- API 失败、超时和下载失败均有明确反馈。
- 重复提交不会创建重复任务。
- 前端构建、Go 测试及 Windows 可执行文件构建通过。

### 最近验收记录与后续工作

2026-10-13 已通过前端类型检查与构建、Go 测试、Windows 无 CGO 打包，以及 Mock HTTP 和浏览器联调。验证包含登录、上传、异步成功与失败、幂等提交、媒体鉴权、Range 播放、下载、历史、删除、页面刷新及服务重启后的会话保留。

回归测试覆盖提交结果不明时不重提、生成和下载恢复、Provider 切换后继续查询、Windows 文件锁导致的删除回滚，以及中断删除的启动恢复。

后续优先完成：

1. 使用有效 API Key 和已开通模型完成真实 Seedance 生成、视频保存及失败场景验收。
2. 根据实际模型能力核对时长、比例、分辨率及音频参数；当前上游参数不支持时返回明确错误，不静默替换用户选择。
3. 在目标 Windows 服务器验证 HTTPS 反向代理、Secure Cookie、监听地址、持久化目录和升级备份流程。

真实 API 验收需要有效 API Key 和已开通模型；报告中必须区分模拟验证与真实生成验证。
