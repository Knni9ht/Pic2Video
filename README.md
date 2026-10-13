# Pic2Video

上传一张商品图片和商品描述，生成、预览并下载商品展示视频。第一版采用 Go + Vue 3 + SQLite，供内部团队使用共享密码访问，所有成员共享任务历史。

默认运行 **模拟模式**，无需 Seedance API Key。模拟模式返回项目自带的固定演示短片，画面带有 `DEMO` 标识；它不根据上传内容生成视频，也不会调用收费接口。样片固定为 5 秒、360×640，表单里的时长、比例和 720p 用于验证真实模式的请求参数。

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
- 参数：5 或 10 秒，9:16、16:9 或 1:1，720p。
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
| `ARK_MODEL_ID` | 真实模式必填，使用账号实际开通的模型 ID |
| `ARK_BASE_URL` | `https://ark.cn-beijing.volces.com/api/v3` |

接入真实 API 时，将 `VIDEO_PROVIDER` 改为 `seedance`，填写 API Key 和支持单图首帧生成的模型 ID，再重启服务。模型必须支持选择的时长、比例和 720p。无效配置会阻止启动，不会自动切换到模拟模式。

后端将图片编码为 Base64，调用创建任务接口，保存远程任务 ID 并轮询查询；完成后将视频保存到本地。查询和下载采用有限重试；创建请求结果不明时不自动重新创建任务。不同 Provider 的未完成任务应在原 Provider 配置下处理。

接口依据：[创建视频生成任务](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)、[查询视频生成任务](https://docs.volcengine.com/docs/ark/get-video-generation-task-api?lang=zh)。真实生成需要你的有效凭据和已开通模型；使用本地替身服务的接口测试不能替代真实生成验收。

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
