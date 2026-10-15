# Pic2Video

上传一张商品图片和商品描述，生成、预览并下载商品展示视频。第一版采用 Go + Vue 3 + SQLite，供内部团队使用共享密码访问，所有成员共享任务历史。

默认运行 **模拟模式**，无需 Seedance API Key。模拟模式返回项目自带的固定演示短片，画面带有 `DEMO` 标识；它不根据上传内容生成视频，也不会调用收费接口。样片固定为 5 秒、360×640，表单里的时长、比例和 720p 用于验证真实模式的请求参数。

真实生成型号默认采用 **Seedance 2.0 mini：`doubao-seedance-2-0-mini-260615`**。为保留模型额度，后续开发和完整流程回归默认使用 Mock 或本地 HTTP 替身；只有用户明确要求本轮真实调用时，才运行收费生成测试。已有 Key 不代表允许自动使用额度。

## Windows 快速启动

需要 Go 1.25+、Node.js 22.12+ 和 npm。在仓库根目录使用 PowerShell：

```powershell
Copy-Item .env.example .env
notepad .env
```

把 `APP_PASSWORD` 改成自己的访问密码，保留 `VIDEO_PROVIDER=mock`，然后构建并启动：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
powershell -ExecutionPolicy Bypass -File scripts/start.ps1
```

打开 <http://127.0.0.1:8080>，输入配置的密码，上传商品图片并填写描述。模拟任务数秒后完成，可以在历史记录中预览、下载或删除。

构建会安装前端依赖、进行 TypeScript 检查、构建前端、运行 Go 测试并生成 `bin/Pic2Video.exe`。初次构建需要联网下载依赖。可执行文件包含前端和演示视频，运行时不需要 Node.js、Go、FFmpeg 或 Python。

## 使用规则

- 图片：一张 JPG、PNG 或 WebP，不超过 10 MB；每边 300–6000 像素，宽高比为 0.4–2.5。
- 描述：说明商品外观和希望展示的细节；无需自己编写视频提示词。
- 参数：5 或 10 秒，720p、静音 MP4。本项目的 Seedance 模式采用跟随原图比例；演示模式保留 9:16、16:9、1:1 选项供本地验证。
- 状态：排队、提交、生成、保存、完成或失败。关闭网页不影响后台任务。
- 历史：团队共享，保留到手动删除。运行中的任务不能删除。
- 模拟失败：开发测试时在描述中加入 `[模拟失败]`，可验证失败状态和错误提示。
- 提交结果不明：显示提示要求先核实远程控制台，避免再次创建收费任务。

## 配置与真实 Seedance 模式

程序读取当前工作目录的 `.env`，已有进程环境变量优先。启动脚本会将工作目录设为仓库根目录。

| 配置项 | 默认值或用途 |
| --- | --- |
| `APP_PASSWORD` | 必填，共享访问密码 |
| `VIDEO_PROVIDER` | `mock` 或 `seedance`，默认 `mock` |
| `HOST` | `127.0.0.1`，监听地址 |
| `PORT` | `8080` |
| `DATA_DIR` | `./data`，数据库、图片及视频存储目录 |
| `COOKIE_SECURE` | `false`；通过 HTTPS 访问时设为 `true` |
| `ARK_API_KEY` | 真实模式必填，仅保存在后端 |
| `ARK_MODEL_ID` | 默认 doubao-seedance-2-0-mini-260615，可用环境变量覆盖 |
| `ARK_BASE_URL` | `https://ark.cn-beijing.volces.com/api/v3` |
| `ALLOW_REAL_MODEL_TESTS` | 默认禁用，仅用户授权后临时设为 `1`，供真实验收脚本使用 |

接入真实 API 时，将 `VIDEO_PROVIDER` 改为 `seedance`，填写 API Key 并确认账号已开通 Seedance 2.0 mini，再重启服务。默认 Model ID 为 `doubao-seedance-2-0-mini-260615`，可通过 `ARK_MODEL_ID` 覆盖；更换型号时须重新核对参数能力。无效本地配置会阻止启动；模型权限、额度和上游参数须在用户授权的真实调用中确认，不会自动切换到模拟模式。

后端将图片编码为 Base64，以 `first_frame` 调用创建任务接口，设置 `ratio=adaptive`、`generate_audio=false`，使用模型默认 MP4 输出（不发送 2.5 专用的 `output_format` 字段），保存远程任务 ID 并轮询查询；完成后将视频保存到本地。查询和下载采用有限重试；创建请求结果不明时不自动重新创建任务。不同 Provider 的未完成任务应在原 Provider 配置下处理。

`POST /api/jobs` 在 Seedance 模式下省略比例时默认 `adaptive`，显式传入 9:16、16:9 或 1:1 返回 HTTP 400 和 `invalid_parameters`，不会静默替换。任务对象的 `ratio=adaptive` 在页面显示为“跟随原图”。历史记录保留原参数，复用旧素材生成新视频时会提示画幅跟随原图；无需数据库迁移。

接口依据：[创建视频生成任务](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)、[查询视频生成任务](https://docs.volcengine.com/docs/ark/get-video-generation-task-api?lang=zh)。真实生成需要你的有效凭据和已开通模型；使用本地替身服务的接口测试不能替代真实生成验收。

### 账号准备和调用步骤

1. 登录火山方舟，按控制台要求完成账号准备并开通 Seedance 2.0 mini。当前开通条件包括账户余额大于 200 元、购买符合条件的节省计划，或拥有有效资源包，操作时再次核对[官方开通说明](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)。
2. 在正确项目空间创建生效的方舟 API Key，确认有目标模型权限；若设置 IP 白名单，需包含调用机器的出口 IP。使用 `Authorization: Bearer <API Key>`，详见 [API Key 管理](https://docs.volcengine.com/docs/ark/api-key?lang=zh)。
3. 在现有 `.env` 填写 `ARK_API_KEY`、`ARK_MODEL_ID`，设置 `VIDEO_PROVIDER=seedance`，然后重新构建并重启。不要覆盖已有密码和数据目录配置，密钥只保存在后端。
4. 提交一张商品图片和描述；后端调用 `POST /contents/generations/tasks`。收到任务 ID 仅代表创建成功，需要继续查询。
5. 使用 `GET /contents/generations/tasks/{id}` 轮询；`succeeded` 后读取 `content.video_url`，失败时根据中文提示和后台错误码处理。
6. 及时下载并保存 MP4。官方查询记录保留 7 天，视频 URL 有效期为 24 小时；本地保存成功后才显示完成。

### 真实生成验收

日常测试使用下方的 Mock 验证命令和本地替身测试。仅用户明确授权真实验收时，启动 Seedance 模式的后端服务，再在仓库根目录运行（Node.js 22.12+）：

```powershell
# 默认只检查登录与运行模式，不创建远程任务
node --env-file=.env scripts/seedance-smoke.mjs

# 仅在用户明确授权本轮真实调用后临时启用，可能产生费用
$env:ALLOW_REAL_MODEL_TESTS = '1'
node --env-file=.env scripts/seedance-smoke.mjs --generate --image "C:\素材\商品.png" --description "磨砂白色保温杯，以平稳环绕镜头展示商品外观。"
$env:ALLOW_REAL_MODEL_TESTS = '0'

# 继续检查已有本地任务；不会创建新任务，也不会恢复已停止处理的 unknown/failed 任务
node --env-file=.env scripts/seedance-smoke.mjs --job-id "上一次输出的本地任务ID"
```

可用 `SMOKE_URL` 指定服务根地址；进程环境变量优先于 `--env-file`。默认等待 15 分钟，可用 `--timeout-seconds` 设置 1–7200 秒。超时只结束脚本，后台任务继续；使用已有任务 ID 查询，避免再次运行 `--generate`。

脚本在 `.cache/seedance-smoke/<本轮目录>/` 保存 `record.json` 和下载的 `video.mp4`，保留服务上的任务和素材。同一轮以相同幂等键提交两次，确认返回同一个本地任务；工作器只创建一次远程任务。创建响应无法确认时，不自动重试，记录幂等键并提示核查服务任务列表与日志。

记录包含本地任务 ID、环境中的模型 ID、耗时、参数和 HTTP 验收结果。真实 Model ID 与远程任务 ID 以后台 `远程生成任务已创建` 日志中的 `model`、`remote_id` 核对；这些信息不通过网页 API 公开。默认检查通过只证明登录和运行模式正常，不证明 API Key、模型权限或额度可用。

HTTP 验收通过后，还需在网页或媒体工具中确认实际约 5 秒、720p、画幅跟随图片、静音和商品外观；刷新页面、重启服务后再次播放或下载。在 `record.json` 的 `manual_checks` 中填写人工验收结果和远程任务 ID。10 秒本轮只通过官方参数核对和本地合同测试验证。

### 错误核查

- 鉴权或权限失败：检查 Key 是否生效、模型是否开通、项目空间、模型权限和 IP 白名单。
- 余额或额度不足：检查充值、资源包和模型用量限制；限流时检查并发和请求频率。
- `SetLimitExceeded`：检查安心体验与模型用量设置。旧的失败任务不会自动恢复，不要通过反复提交任务排查。
- 参数或内容校验失败：检查图片、描述、模型 ID 和参数，本项目的商品首帧流程使用 `adaptive`，保留原图画幅。
- `unknown`：先核实控制台已有任务，不自动重新生成；下载失败时同样先查已有生成结果。
- 后台失败日志包含 `stage`、`http_status`、`upstream_code`、`job` 和 `remote_id`，不输出密钥、图片 Base64、完整上游响应或原始错误文本。

当前真实 API 验收状态：2026-10-15 已按用户授权完成 mini 的独立多素材、有声、11 秒 API 生成及下载校验。网页单图、静音流程尚未做真实付费验收；后续测试默认使用 Mock 和本地替身，保留模型额度。

## 开发与验证

第一次启动后端开发服务：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1
```

另开终端启动 Vue 热更新：

```powershell
npm --prefix web run dev
```

访问终端显示的 Vite 地址。`/api` 默认代理到 `http://127.0.0.1:8080`；如修改后端端口，需要同步调整 Vite 代理。前端热更新模式使用 HTTP 时保持 `COOKIE_SECURE=false`。

```powershell
go test ./...
npm --prefix web run build
node --test scripts/seedance-smoke.test.mjs
```

对运行中的 **mock** 服务进行端到端 HTTP 验证：

```powershell
$env:APP_PASSWORD = '与服务一致的密码'
node scripts/smoke.mjs
```

可通过 `SMOKE_URL` 指定其他地址。脚本验证登录、图片校验、防重复提交、异步成功与失败、媒体鉴权、Range 播放、下载、历史和删除；测试任务结束后会被删除。它会在 `.cache/smoke-product.png` 生成一张可供手动上传的测试图片。

## 部署与数据

- 生产环境使用一个服务实例。将可执行文件与 `.env` 放到固定工作目录，配置稳定的 `DATA_DIR`；双击或从其他目录启动时请注意工作目录。
- 默认只监听本机。内部网络直接访问时可设置 `HOST=0.0.0.0`；公网部署使用 HTTPS 反向代理，并设置 `COOKIE_SECURE=true`。
- 程序升级只替换可执行文件，不覆盖 `.env` 和数据目录。备份时先停止服务，再复制完整数据目录，包括 SQLite 文件及其附属文件。
- 任务媒体经登录鉴权提供，不应将数据目录映射为公开静态目录。
- `.env`、数据库、上传文件、生成结果、构建产物和本地验证文件均已加入 Git 忽略规则。

## 开发文件

`agent.md` 记录开发约定；`cmd/server` 是服务入口；`internal` 包含配置、存储、HTTP 服务、任务工作器及生成 Provider；`web` 是 Vue 页面和静态资源嵌入入口；`scripts` 包含构建、启动和验证工具。

固定演示短片由本项目的 `scripts/generate_demo.py` 程序绘制并编码，没有外部素材。重新制作样片才需要 Pillow 和 FFmpeg；这些工具不是应用运行依赖。
