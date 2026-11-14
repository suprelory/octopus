# Octopus Web

前端使用 React、TypeScript 和 Vite。

开发环境启动：

```bash
pnpm install
pnpm dev
```

Vite 默认监听 `http://localhost:5173`，并将 `/api` 请求代理到 `http://127.0.0.1:8080`。如需连接其他后端地址，可在启动时设置 `VITE_PROXY_TARGET`：

```bash
VITE_PROXY_TARGET="http://127.0.0.1:8080" pnpm dev
```

生产构建：

```bash
pnpm build
```

构建产物直接输出到 `static/out`，供 Go 二进制文件嵌入。

普通开发构建的前后端版本均为 `dev`。发布构建通过 `VITE_APP_VERSION` 注入版本，须与后端 `BUILD_VERSION` 一致；正式发布流程会统一设置这两个值。设置页的仓库链接来自后端 `/api/v1/update/metadata`，与更新源共享配置，无需设置 `VITE_GITHUB_REPO`。
