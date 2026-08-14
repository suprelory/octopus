# Octopus

Octopus 是一个面向多模型服务的 LLM API 聚合、协议转换和负载均衡服务。它提供统一的管理面板、API Key、渠道与分组管理、故障切换、熔断、统计和运行日志，并把不同上游的请求与响应转换为客户端熟悉的协议。

## 功能概览

- 统一代理 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 和 Gemini 等协议。
- 按渠道、模型和 API Key 进行负载均衡，支持轮询、随机、故障切换和加权策略。
- 支持渠道亲和、熔断、健康检查、空响应检测和流式请求。
- 提供 Web 管理面板，用于管理用户、API Key、渠道、分组、站点同步、备份和运行设置。
- 支持 SSE、WebSocket、工具调用、推理内容和多模态请求的协议适配。
- 支持 SQLite、MySQL 和 PostgreSQL。
- 发布包使用 SHA-256 校验和与 Ed25519 签名保护自更新流程。

## 技术栈

| 层次 | 技术 |
| --- | --- |
| 后端语言 | Go 1.25 |
| HTTP 服务 | Gin 1.12、SSE、WebSocket |
| 数据访问 | GORM；SQLite（默认）、MySQL、PostgreSQL |
| 配置与 CLI | Viper、Cobra |
| 日志与缓存 | Zap 结构化日志、分片内存缓存、xxhash |
| 协议适配 | OpenAI、Anthropic、Gemini 等 inbound/outbound transformer |
| 前端框架 | Next.js 16、React 19、TypeScript |
| 前端样式与组件 | Tailwind CSS 4、Radix UI、shadcn/ui、Lucide |
| 前端状态 | Zustand、TanStack React Query |
| 国际化 | next-intl；简体中文、繁体中文、英文 |
| 构建与发布 | pnpm、Python、GitHub Actions、多架构 Docker、Ed25519 |

## 架构

```text
客户端
  │
  ▼
Gin Router / Auth / CORS / Logger
  │
  ▼
Inbound Transformer
  │
  ▼
Relay + Balancer + Circuit Breaker
  │
  ▼
上游 LLM 服务
  │
  ▼
Outbound Transformer
  │
  ▼
客户端响应
```

后端入口为 `main.go`，启动流程依次初始化配置、数据库、缓存、HTTP 服务和后台任务。前端使用 Next.js 静态导出，生产构建后嵌入 Go 二进制的 `static/out` 目录。

## 快速部署

### Docker Compose

仓库中的 `compose.yaml` 使用 `suprelory/octopus` 镜像，并把宿主机目录挂载到容器的 `/app/data`。

```yaml
services:
  octopus:
    image: suprelory/octopus
    environment:
      - OCTOPUS_ADMIN_PASSWORD=${OCTOPUS_ADMIN_PASSWORD:?set OCTOPUS_ADMIN_PASSWORD}
    ports:
      - "8080:8080"
    volumes:
      - "./data:/app/data"
    restart: unless-stopped
```

启动服务：

```sh
export OCTOPUS_ADMIN_PASSWORD='至少 12 个字符的强密码'
docker compose up -d
```

访问 `http://服务器地址:8080`。查看日志和状态：

```sh
docker compose logs -f octopus
docker compose ps
```

升级镜像前备份挂载的数据目录，然后执行：

```sh
docker compose pull
docker compose up -d
```

生产环境建议在 Octopus 前配置 HTTPS 反向代理，并限制容器端口只允许来自反向代理的流量。

### 发布二进制

从 Release 下载对应平台的压缩包，解压后在可持久化的数据目录旁运行：

```sh
mkdir -p data
OCTOPUS_ADMIN_PASSWORD='至少 12 个字符的强密码' ./octopus start
```

默认监听 `0.0.0.0:8080`，默认配置文件为 `data/config.json`。可以使用自定义配置文件：

```sh
./octopus start --config /etc/octopus/config.json
```

### 从源码构建

开发环境需要 Go 1.25、Node.js 22、pnpm 和 Python 3。前端开发服务器与后端分开运行：

```sh
cd web
pnpm install --frozen-lockfile
NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" pnpm dev
```

另开终端启动后端：

```sh
go run main.go start
```

构建可运行的生产二进制时，需要先生成并嵌入前端静态文件：

```sh
cd web
pnpm install --frozen-lockfile
pnpm build
cd ..
rm -rf static/out
mv web/out static/out
go build -tags=jsoniter -o build/octopus .
```

也可以使用项目构建脚本：

```sh
./scripts/build.sh build linux x86_64
```

构建脚本会把产物写入 `build/bin/`。完整的多平台发布流程使用 `./scripts/build.sh release`，见下方的发布签名说明。

## 管理员首次初始化

新安装不会创建固定的默认密码。管理员密码至少需要 12 个 Unicode 字符，同时不能超过 72 个字节。

### 容器部署

首次启动前通过 secret 注入初始密码：

```sh
OCTOPUS_ADMIN_PASSWORD='<strong password>' docker compose up -d
```

`OCTOPUS_ADMIN_USERNAME` 可选，默认值为 `admin`。管理员写入数据库后，后续重启不会覆盖已有凭据。Compose 配置会在每次执行时检查密码变量是否存在，这是部署安全措施；请持续配置该 secret，或在完成初始化后按部署策略移除 Compose 中的变量插值检查。

### 裸机部署

如果 `OCTOPUS_ADMIN_PASSWORD` 未设置或为空，启动日志会输出一个加密随机的一次性 bootstrap token。打开 Web 界面，输入该 token 并创建管理员账户。token 在初始化成功后立即失效；服务在尚未初始化时每次重启都会轮换 token。

不要把启动日志公开给不可信人员。初始化完成前，所有需要认证的管理接口都不可用。

## 配置

首次启动会自动创建 `data/config.json`。配置项也可以使用 `OCTOPUS_` 前缀的环境变量覆盖，例如：

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080,
    "max_request_body_mb": 32
  },
  "database": {
    "type": "sqlite",
    "path": "data/data.db"
  },
  "log": {
    "level": "info",
    "format": "console"
  }
}
```

常用环境变量包括：

```sh
OCTOPUS_SERVER_HOST=0.0.0.0
OCTOPUS_SERVER_PORT=8080
OCTOPUS_DATABASE_TYPE=sqlite
OCTOPUS_DATABASE_PATH=data/data.db
OCTOPUS_LOG_LEVEL=info
```

数据库类型支持 `sqlite`、`mysql` 和 `postgres`。SQLite 是默认选项；使用 MySQL 或 PostgreSQL 时，把 `OCTOPUS_DATABASE_PATH` 设置为对应驱动所需的 DSN，并确保数据库已创建且应用用户拥有读写权限。

### 可信代理

如果应用位于 Nginx、云负载均衡器或 Docker 反向代理之后，请在管理面板的“网络与服务”中配置实际代理的 IP 或 CIDR，例如 `172.24.0.1` 或 `172.24.0.0/16`。设置保存后立即生效，不需要重启。

只有 TCP 对端命中可信代理列表时，应用才会读取 `X-Forwarded-For` 和 `X-Real-IP`。反向代理也必须正确转发这些请求头：

```nginx
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

只信任实际反向代理的地址。不要在应用端口仍可被公网直连时盲目信任整个地址空间，否则客户端可能伪造转发头。

## 目录结构

```text
cmd/                  CLI 与启动流程
internal/conf/        Viper 配置与构建信息
internal/db/          GORM 初始化和数据库迁移
internal/model/       数据模型
internal/op/          业务逻辑与缓存
internal/relay/       API 代理、负载均衡、熔断和流处理
internal/transformer/ 协议转换 inbound/outbound
internal/server/      路由、处理器、中间件和认证
internal/task/        后台定时任务
scripts/              构建、价格同步、发布签名脚本
web/src/              Next.js 管理面板
static/out/           前端生产静态文件
```

## 测试与质量检查

运行后端全部测试：

```sh
go test ./...
```

运行前端检查和构建：

```sh
cd web
pnpm lint
pnpm build
```

价格生成器的 Python 测试：

```sh
python -m unittest discover -s scripts -p 'test_*.py'
```

## 发布签名与自更新

自更新只接受同时满足以下条件的发布包：归档文件被列在 `checksums.sha256` 中，并且该清单被 Ed25519 签名覆盖。

首次生成签名 seed：

```sh
go run scripts/release_sign.go generate
```

命令会输出 `OCTOPUS_RELEASE_SIGNING_KEY` 和对应的公钥。只把 `OCTOPUS_RELEASE_SIGNING_KEY` 保存为 GitHub Actions secret，secret 名称必须保持一致。签名 seed 应保留离线备份；丢失或轮换该密钥后，必须先发布内置新公钥的受信任二进制，之后新密钥签名的更新包才能被安装。

本地或 CI 环境生成公钥：

```sh
OCTOPUS_RELEASE_SIGNING_KEY='<secret>' go run scripts/release_sign.go public-key
```

发布构建会派生公钥并嵌入每个平台的二进制，对 ZIP 归档生成 SHA-256 清单，同时上传 `checksums.sha256` 和 `checksums.sha256.sig`：

```sh
export OCTOPUS_RELEASE_SIGNING_KEY='<secret>'
./scripts/build.sh release
```

生产签名 key 不得打印、提交到仓库或写入构建日志。没有签名 secret 的本地构建和普通 CI 构建不会嵌入更新公钥，自更新接口会在下载归档前直接失败。

## 致谢

- [looplj/axonhub](https://github.com/looplj/axonhub)
- [bestruirui/octopus](https://github.com/bestruirui/octopus)
