<p align="center">
  <img src="assets/LOGO-Bar.png" alt="Wercture — 轻量、易用、可管理的图床" width="880" />
</p>

<h1 align="center">Wercture</h1>

<p align="center">
  <strong>轻量自托管图床 · Go 原生客户端 · Docker 服务端 · 嵌入式 Web</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Version-0.1.0-0969DA" alt="Version 0.1.0" />
  <img src="https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.27.1" />
  <img src="https://img.shields.io/badge/Vue-3-4FC08D?logo=vuedotjs&amp;logoColor=white" alt="Vue 3" />
  <img src="https://img.shields.io/badge/Storage-SQLite-003B57?logo=sqlite&amp;logoColor=white" alt="SQLite storage" />
  <img src="https://img.shields.io/badge/Server-Docker-2496ED?logo=docker&amp;logoColor=white" alt="Docker deployment" />
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>简体中文</strong> · <a href="#server-安装">Server 安装</a> · <a href="#client-安装">Client 安装</a> · <a href="#项目结构">项目结构</a>
</p>

---

Wercture 是一个轻量化自托管图床：仓库私有管理，文件通过直链分享。Go 服务端直接提供嵌入的 Vue 页面，SQLite 保存元数据；客户端只有一个可执行文件，同时提供终端菜单和脚本可用的 CLI，支持在本地压缩、转 WebP 后上传。

支持 JPEG、PNG、GIF、WebP，也能上传 PDF 和 Markdown，并生成文档预览图。

## 项目特性

| | 能力 |
| :--- | :--- |
| **轻量部署** | 一个 Docker 服务、一个 HTTP 端口和独立数据目录，Go 直接提供管理页面。 |
| **私有管理** | admin 创建账号，仓库独立归属，每个仓支持多个可吊销的上传 token。 |
| **稳定直链** | 随机 ID 加实际格式尾缀，重命名、移动、回收后恢复均保留原链接。 |
| **图库操作** | 单击预览、滚轮缩放图片、右键菜单、勾选后显示批量操作条。 |
| **文档支持** | PDF 阅读器、支持表格和数学公式的 Markdown 排版，图库显示预览图。 |
| **本地处理** | 压缩与转 WebP 独立开关，纯 Go 实现，不依赖外部图像工具。 |
| **回收机制** | admin 独立回收站，可恢复、移入其他仓，满 3 天自动彻底删除。 |
| **全站外观** | English / 简体中文，以及跟随系统 / 浅色 / 深色，由 admin 统一设置。 |

### 目录

[安装包](#安装包) · [Server 安装](#server-安装) · [Client 安装](#client-安装) · [CLI 使用](#cli-使用) · [网页与权限](#网页与权限) · [直链与回收站](#直链与回收站) · [源码构建与测试](#源码构建与测试) · [项目结构](#项目结构) · [致谢](#致谢)

## 安装包

| 组件 | 交付内容 | 架构 |
| :--- | :--- | :--- |
| **WerctureServer** | Docker 镜像归档、`manage`、部署文件及校验清单 | Linux amd64 |
| **WerctureClient** | 单个静态可执行文件，同时提供 TUI 与 CLI | Linux amd64 / arm64 |

[GitHub 仓库](https://github.com/XLYing23333/Wercture) · [v0.1.0 Release](https://github.com/XLYing23333/Wercture/releases/tag/v0.1.0)

| 下载 | 平台 |
| :--- | :--- |
| [Server](https://github.com/XLYing23333/Wercture/releases/download/v0.1.0/wercture-server_0.1.0_linux_amd64.release.tar.gz) | Linux amd64 |
| [Client amd64](https://github.com/XLYing23333/Wercture/releases/download/v0.1.0/wercture-linux-amd64) | Linux amd64 |
| [Client arm64](https://github.com/XLYing23333/Wercture/releases/download/v0.1.0/wercture-linux-arm64) | Linux arm64 |
| [SHA-256](https://github.com/XLYing23333/Wercture/releases/download/v0.1.0/SHA256SUMS) | 全部发布资产 |

同一个 Release 使用一份 `SHA256SUMS`，同时包含 Server 压缩包和两种 Client 可执行文件的校验值。

以下命令从 GitHub Release 安装 **v0.1.0**；源码构建可参考[源码构建与测试](#源码构建与测试)。Server 包内已包含 Docker 镜像，安装无需克隆源码或安装 Go、Node.js。Client 下载后即可使用，无需通过包管理器安装，也不依赖外部图像工具。

## Server 安装

面向 Debian 11 / 12 / 13 和 Ubuntu 20 / 22 / 24 / 26。Server 运行在 Docker 中；宿主机管理程序需要 Python 3.9+、Docker Engine、Compose 插件和 systemd。部署前安装这些组件，并确保 **9416/tcp** 可以访问。

### 1. 下载、校验并打开管理菜单

预编译 Server 镜像为 **Linux amd64**。下载发布包，校验压缩包与解压后的文件，再打开终端管理菜单：

```sh
set -eu
case "$(uname -m)" in
  x86_64) ;;
  *) echo "Server release supports Linux amd64 only"; exit 1 ;;
esac
release=https://github.com/XLYing23333/Wercture/releases/download/v0.1.0
work=$(mktemp -d)
cd "$work"
pkg=wercture-server_0.1.0_linux_amd64.release.tar.gz
wget "$release/$pkg"
wget "$release/SHA256SUMS"
awk -v name="$pkg" '$2 == name { print; found = 1 } END { if (!found) exit 1 }' SHA256SUMS > SHA256SUMS.server
sha256sum -c SHA256SUMS.server
sudo install -d -m 700 /opt/wercture
sudo tar -xzf "$pkg" -C /opt/wercture
sudo sh -c 'cd /opt/wercture && sha256sum -c SHA256SUMS'
sudo /opt/wercture/manage
```

选择“安装”，输入存储目录的绝对路径，例如 `/var/lib/wercture`。管理程序会导入包内镜像、启动容器、安装 `wercture.service`，并检查 `/health`。

不使用菜单时，明确指定存储目录：

```sh
sudo /opt/wercture/manage install --data /var/lib/wercture
sudo /opt/wercture/manage status
```

安装时可以加载已有的 Wercture 数据目录。请将 `/opt/wercture` 保留在固定位置，systemd 开机项会指向该目录。安装不会自动启用开机自启，需要另选“启用”。客户端以普通用户运行，只有服务端管理和安装需要提权。

> [!IMPORTANT]
> 安装会移除旧部署容器和 `wercture-server` 镜像，并执行 `docker builder prune -af`。这会清理整个 Docker daemon 的未使用构建缓存，包括其他项目的缓存。选定的数据目录会保留。

### 2. 初始化管理员

运行 `sudo /opt/wercture/manage status`，查看随机初始化地址：

```text
http://127.0.0.1:9416/init<64位十六进制随机值>
```

远程访问时，将 `127.0.0.1` 换成服务器地址。打开后设置第一个 admin 的用户名和密码。没有默认账号密码，用户名设置后不可修改，密码长度为 6–1024 字节。初始化成功后，该地址失效；之后从 `http://<服务器IP>:9416/` 正常登录。

### 3. 建仓与上传

1. 登录后创建一个仓库。
2. 在仓库页面直接上传图片、PDF 或 Markdown。
3. 需要客户端或脚本上传时，创建上传 token，复制对应的 `wercture://import?...` 导入链接。
4. 在“用户管理”中创建其他用户，将激活链接交给对应用户，由其自行设置密码。

删除用户时可选择“并入回收站”或“立即删除”；后者会彻底删除其仓库和文件。

没有公开注册入口。每个仓只有一个所有者，每个 token 只向它所属的仓上传。

### 管理命令

菜单与 CLI 使用相同逻辑。方向键选择、Enter 执行、Esc 返回、Q 退出。下表按安装到 `/opt/wercture` 编写；如果已在解压目录中，也可以使用 `sudo ./manage`。

| 操作 | 命令 |
| :--- | :--- |
| 安装 / 加载已有数据目录 | `sudo /opt/wercture/manage install --data /var/lib/wercture` |
| 启动 | `sudo /opt/wercture/manage start` |
| 重启 | `sudo /opt/wercture/manage restart` |
| 状态与初始化地址 | `sudo /opt/wercture/manage status` |
| 启用开机自启 | `sudo /opt/wercture/manage enable` |
| 关闭开机自启 | `sudo /opt/wercture/manage disable` |
| 重置 admin 密码 | `sudo /opt/wercture/manage config reset-password` |
| 设置全站语言 | `sudo /opt/wercture/manage config language zh-CN` |
| 检查 GitHub 稳定版并更新 | `sudo /opt/wercture/manage update` |
| 卸载并保留数据 | `sudo /opt/wercture/manage uninstall` |

英文使用 `en`。启动 / 重启只改变当前运行状态；启用 / 关闭开机自启只改变开机项。重置密码不改 admin 用户名。卸载移除本部署的容器、systemd 开机项和 `wercture-server` 镜像，上传文件和设置保留。

### 网络与存储

管理页、上传 API 和公开直链共用 **9416/tcp**，由 Go 直接提供 HTTP 服务。**如果需要 SSL/HTTPS，请自行在 Wercture 外部配置。** 网页复制操作建议使用 HTTPS 或 localhost。

存储目录挂载到容器内的 `/data`：

```text
/data/
├── wercture.db       # 账号、设置、仓库、token 和文件元数据
├── files/            # 上传的原文件及文档预览图
└── setup.secret      # 仅在管理员初始化前存在
```

容器以 UID/GID `10001:10001` 运行，根文件系统只读，并丢弃 capabilities。管理程序会准备数据权限，将选定路径保存到 `/opt/wercture/.env`。

### 更新与备份

发布版使用“更新”，或将校验后的新包解压到 `/opt/wercture`，用原存储目录执行“安装”；发布包安装会导入包内镜像。源码部署使用 `WerctureServer/deploy/docker/manage`，在本地 Server 或 Web 改动后，用原存储目录再次执行其“安装”操作，重新构建镜像。“重启”仍使用已有镜像，不会应用源码改动。

“更新”会检查 GitHub 稳定版，寻找匹配架构的发布包，校验 SHA-256，然后替换镜像和管理程序。此功能依赖已经发布的 Server 包和 `SHA256SUMS`，不会构建本地源码。更新期间使用 `.update-previous` 自动回滚，成功后会立即删除该目录；后续恢复需要独立备份。已校验的镜像归档和校验清单会保留在部署目录，供再次安装使用。

更新前停止部署，备份完整存储目录和部署 `.env`。只备份数据库不包含上传文件；同时保留旧源码或旧发布包，以便恢复。

## Client 安装

下载匹配架构的可执行文件，校验后安装为 `wercture`：

```sh
set -eu
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
esac
release=https://github.com/XLYing23333/Wercture/releases/download/v0.1.0
work=$(mktemp -d)
cd "$work"
pkg="wercture-linux-$arch"
wget "$release/$pkg"
wget "$release/SHA256SUMS"
awk -v name="$pkg" '$2 == name { print; found = 1 } END { if (!found) exit 1 }' SHA256SUMS > SHA256SUMS.client
sha256sum -c SHA256SUMS.client
sudo install -m 755 "$pkg" /usr/local/bin/wercture
wercture version
wercture
```

安装位置为 `/usr/local/bin/wercture`，只有一个可执行文件，无需 Go 工具链。不想安装到系统目录时，也可以在下载目录执行 `chmod +x "$pkg"`，然后用 `./"$pkg"` 直接运行。源码构建见[源码构建与测试](#源码构建与测试)。

在终端中不带参数执行即进入 TUI：方向键选择、Enter 执行、Esc 返回、Q 退出。不是终端时只打印用法，不进入菜单。

```text
导入 URL
切换配置
压缩                 开 / 关
转 WebP              开 / 关
上传文件
Status
删除当前配置
退出
```

在仓库的 token 面板复制导入链接，粘贴到“导入 URL”。Status 显示当前配置、首尾可见的 token、处理开关和服务器在线状态。TUI 读取当前服务器的全站语言，无法读取时使用英文。

多套配置保存在 `~/.config/wercture.db`，文件权限为 `0600`，包含服务器地址、上传 token、压缩 / WebP 开关及质量。以普通用户运行客户端，即使用该用户自己的配置。

## CLI 使用

以下示例假设 `wercture` 已加入 `PATH`，也可以替换为构建产物的路径。地址和 token 均为占位示例。

### 导入与管理配置

```sh
wercture import 'wercture://import?host=https%3A%2F%2Fimg.example.com&token=YOUR_TOKEN&name=personal'

# 也可以手动创建
wercture config add personal --host https://img.example.com --token YOUR_TOKEN
wercture config list
wercture config use personal
wercture config edit personal --compress --webp --quality 80
wercture config edit personal --none

# 删除本地配置
wercture config rm personal
```

导入后自动切换到该配置。导入同名配置会更新地址和 token，保留原有处理偏好。删除配置只移除本地记录。

### 上传图片

```sh
wercture upload photo.jpg screenshot.png
wercture upload --profile personal photo.jpg
wercture upload --compress --quality 80 photo.jpg
wercture upload --webp photo.png
wercture upload --compress --webp --quality 80 photo.png
wercture upload --none animation.gif

# 仅为本次上传指定连接信息
wercture upload --host https://img.example.com --token YOUR_TOKEN photo.jpg
```

显式处理参数会替换本次上传的整套处理模式；不传则沿用配置。`--none` 同时关闭两个开关，不能与 `--compress` 或 `--webp` 一起使用。质量默认 **80**，范围 **1–100**。

| 压缩 | 转 WebP | 静态图处理结果 |
| :---: | :---: | :--- |
| 关 | 关 | 原始字节直接上传。 |
| 开 | 关 | 格式和尺寸不变；JPEG / WebP 按质量重编码，PNG 无损重编码。 |
| 关 | 开 | 无损转换为 WebP。 |
| 开 | 开 | 按质量有损转换为 WebP。 |

重编码会去掉多余元数据，保留尺寸。GIF 和动画 WebP 始终原样上传。压缩不保证文件一定更小。处理在本地以 Go 完成，无需 ImageMagick、`cwebp` 或 FFmpeg；服务端按收到的结果保存，不再转码。

### 上传文档与链接输出

```sh
wercture upload --pdf document.pdf
wercture upload --md notes.md
wercture upload --link markdown photo.jpg
wercture upload --link html photo.jpg
```

PDF 必须带 `--pdf`，Markdown 必须带 `--md`。二者互斥，也不能和 `--compress`、`--webp`、`--none` 一起使用。文档会跳过配置中的图片处理偏好。网页选择文件上传时不需要这些参数。单文件上限为 **32 MiB**。

`--link` 支持 `url`（默认）、`typora`、`obsidian`、`markdown`、`html`。前三种输出裸链接，供调用方包装；Markdown 和 HTML 模式对图片输出图片标记，对文档输出普通链接。每成功上传一个文件，输出一行。

编辑器的自定义上传命令可设为“客户端绝对路径 + `upload --link typora`”或“客户端绝对路径 + `upload --link obsidian`”，由编辑器追加图片路径。

### 上传 API

脚本可通过仓库 token 上传，每个请求一个文件：

```sh
curl --fail-with-body https://img.example.com/api/v1/upload \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -F 'file=@photo.jpg'
```

JSON 响应包含 `url`、`markdown`、`html`，Markdown 上传还包含 `render_url`。token 只有上传权限，不能列出、重命名、移动或删除文件。

## 网页与权限

| 操作 | 仓主 | admin | 上传 token |
| :--- | :--- | :--- | :--- |
| 查看仓库与文件 | 自己的仓 | 全部仓 | — |
| 创建仓库 | 归创建者所有 | 归创建者所有 | — |
| 网页上传、重命名、复制、移动、删除 | 自己的仓 | 任意仓 | — |
| 创建、吊销 token | 自己的仓 | 任意仓 | — |
| token API 上传 | 携带 token | 携带 token | 仅所属仓 |
| 用户管理、回收站、全站语言和主题 | — | 可以 | — |
| 修改自己的密码 | 可以 | 可以 | — |

admin 操作他人的仓不会改变所有权。普通用户只能在自己的仓之间移动文件，admin 可以在任意仓之间移动。已登录的仓主和 admin 在网页里直接上传，无需填写 token。

单击图库项目打开预览，滚轮缩放图片；右键显示链接复制及文件操作菜单；勾选角落的复选框后，底部显示批量操作条，每次最多选择 100 个文件。PDF 使用阅读器打开，Markdown 按文档排版并支持表格和数学公式。

## 直链与回收站

仓库列表和图库需要登录。直链公开，拿到链接的人无需登录或 token 就能访问文件；没有全站公开目录。

```text
/i/<随机ID>.jpg          实际存储格式的图片
/i/<随机ID>.pdf          原始 PDF 文件
/i/<随机ID>.md           Markdown 排版页面
/i/<随机ID>.html         客户端使用的 Markdown 排版别名
/i/<随机ID>.md?raw=1     Markdown 源文件
```

无尾缀链接仍然可用。重命名只改显示名，移动不改 ID 和直链。复制会生成独立文件，获得新的 ID 和链接。

删除后文件进入仅 admin 可见的回收站，公开直链立即关闭。原仓仍在时可恢复到原仓，也可以移入其他仓，原链接随之恢复。删除仓库会将文件送入回收站，不再提供“恢复到原仓”。回收站文件满 **72 小时**后自动彻底删除，清理任务在服务启动时及之后每分钟运行一次；admin 也可提前彻底删除。

## 源码构建与测试

使用 Go **1.27.1**、Node.js **22.13+** 和 npm。未特别注明时，从项目根目录开始执行。

```sh
# 构建服务端嵌入的 Web 文件
cd WerctureServer/web
npm ci
npm run build

# 构建 Server
cd ..
CGO_ENABLED=0 go build -trimpath -o wercture-server ./cmd/wercture-server

# 构建 Client
cd ../WerctureClient
CGO_ENABLED=0 go build -trimpath -o wercture ./cmd/wercture
```

在对应目录执行检查：

| 目录 | 命令 |
| :--- | :--- |
| `WerctureServer` | `go test ./...` |
| `WerctureClient` | `go test ./...` |
| `WerctureServer/web` | `npm run typecheck` |
| `WerctureServer/web` | `node --experimental-strip-types --test src/markdown.test.ts src/selection.test.ts` |
| 项目根目录 | `python3 -m unittest discover -s WerctureServer/deploy/docker -p 'test_*.py'` |

在项目根目录单独构建 Docker 镜像：

```sh
docker build -t wercture-server:0.1.0 WerctureServer
```

生成 Server 分发包前，先在 `WerctureServer/web` 执行 `npm ci`，再从项目根目录运行：

```sh
python3 WerctureServer/deploy/package.py --version 0.1.0
```

此命令在宿主机构建 Web 和 Go，打包当前主机架构的 Docker 镜像，将 `wercture-server_0.1.0_linux_<arch>.release.tar.gz` 和 `SHA256SUMS` 写入 `dist/server/0.1.0/`。打包支持 amd64、arm64。解压到固定目录后，使用包内的 `sudo ./manage` 管理部署。分发或下载发布包时，解压前先用随包提供的 `SHA256SUMS` 校验压缩包。

Client 也可设置 `GOOS=linux`、`GOARCH=amd64` 或 `GOARCH=arm64`，配合 `CGO_ENABLED=0` 交叉编译为单个 Linux 可执行文件。

## 项目结构

```text
Wercture/
├── assets/                        # README 头图与项目 Logo
├── WerctureServer/
│   ├── cmd/wercture-server/        # HTTP 服务与维护命令
│   ├── internal/
│   │   ├── auth/                  # 密码哈希与校验
│   │   ├── preview/               # PDF、Markdown 预览图生成
│   │   ├── server/                # HTTP API、权限与公开直链
│   │   ├── store/                 # SQLite、仓库、文件和回收站
│   │   └── webui/                 # 嵌入式 Web 资源，包含 dist/
│   ├── migrations/                # 内嵌 SQL 数据库迁移
│   ├── web/                       # Vue 3、TypeScript、Vite 源码
│   ├── deploy/
│   │   ├── docker/                # manage、TUI、Compose、systemd、更新
│   │   └── package.py             # Server 分发包构建
│   └── Dockerfile                 # Web / Go 构建与 scratch 运行镜像
├── WerctureClient/
│   ├── cmd/wercture/              # CLI 与终端菜单
│   └── internal/
│       ├── config/                # 多配置、导入链接与参数覆盖
│       ├── filekind/              # 图片、PDF、Markdown 类型识别
│       └── image/                 # 纯 Go 压缩与 WebP 转换
├── README.md                      # English guide
└── README-CN.md                    # 简体中文说明
```

Server 与 Client 是两个独立 Go module。Web 构建产物从 `WerctureServer/internal/webui/dist/` 嵌入，修改前端后需要重新构建。运行时不需要 Node.js 或外部数据库。本地 `development/` 参考源码及生成的 `dist/` 发布包不进入 Git。

## 致谢

感谢以下开源项目及贡献者：

- [Go](https://go.dev/)、[Vue](https://vuejs.org/)、[Element Plus](https://element-plus.org/)、[SQLite](https://sqlite.org/)：语言、界面与存储基础。
- [go-webp](https://github.com/mayahiro/go-webp)：本地 WebP 编码。
- [PDF.js](https://github.com/mozilla/pdf.js)、[go-pdfium](https://github.com/klippa-app/go-pdfium)：PDF 阅读与预览渲染。
- [markdown-it](https://github.com/markdown-it/markdown-it)、[MathJax](https://github.com/mathjax/MathJax)：Markdown 与数学公式排版。

---

<p align="center">
  <a href="README.md">English</a> · <a href="#wercture">返回顶部</a>
</p>
