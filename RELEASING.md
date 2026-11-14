# 发布与更新 | Releases and updates

本 fork 的仓库为 `suprelory/octopus`。程序更新、GitHub Release 和 GHCR 镜像都使用本仓库。上游 `bestruirui/octopus` 仅作为来源说明及 Go 模块标识保留。

This fork publishes its own releases and images. The retained upstream Go module path does not control the update source.

## 使用者更新

- 尚无正式 Release 时，设置页显示“暂无正式发布版本”，不会提供在线更新。
- 正式安装只接受版本号更高、且包含当前平台安装包的正式 Release；相同版本和旧版本不会触发更新。开发版、未知版本和预览版需手动安装正式版本。
- “立即更新”提交页面显示的版本标签，后端重新校验该 Release，再下载该标签下的安装包。后续发布的新版本不会改变本次更新目标。
- 普通源码构建默认为 `dev`，前后端版本一致。`OCTOPUS_GITHUB_PAT` 仍可用于 GitHub API 访问，属于可选配置。
- 二进制更新前备份 `data`，使用默认可执行文件名 `octopus` / `octopus.exe`。容器通过拉取并重建更新，保留数据卷；不要依赖容器内部的二进制替换来更新镜像。

```bash
docker compose pull
docker compose up -d
```

The settings page offers only newer stable releases with an archive for the current platform. Development and preview builds require manual installation. Container users should pull and recreate their containers while retaining the data volume.

## 维护者发布

1. 完成适用测试、人工审查并将 PR 合并到 `master`。
2. 为合并后的提交创建未使用的正式版本标签，格式为 `vMAJOR.MINOR.PATCH`。首次 fork 发布建议使用 `v1.0.0`，后续版本递增；不要复用上游标签或将开发构建标为正式版本。
3. 推送该标签触发 `release` 工作流。仅修改 `main.go` 或合并普通提交不会自动发布。

```bash
git switch master
git pull --ff-only origin master
git tag v1.0.0
git push origin v1.0.0
```

工作流校验正式版本格式及提交属于 `master`，构建前端、更新价格数据、执行 Go 测试和静态检查，然后生成全部平台安装包与 `SHA256SUMS`。镜像推送成功后发布 GitHub Release，发布说明由 GitHub 按本仓库变更生成。

- **GHCR**：默认发布 `ghcr.io/suprelory/octopus:latest` 和版本标签，使用自动提供的 `GITHUB_TOKEN`，需要工作流中的 `packages: write` 权限。首次发布后检查包可见性，公开部署示例需要将包设为 Public。
- **Docker Hub**：可选。仅在仓库同时配置 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN` Secrets 时登录并发布；未配置时只发布 GHCR。
- **版本**：Git 标签同时作为前端的 `VITE_APP_VERSION` 和后端的 `BUILD_VERSION`。稳定更新暂不发布预览标签。
- **产物**：保持 `octopus-<os>-<arch>.zip` 命名；压缩包根目录包含 `octopus`，Windows 包包含 `octopus.exe`。更改产物名称时须同步更新器。

After review and merge, push a new stable `vMAJOR.MINOR.PATCH` tag to publish. GHCR is always enabled; Docker Hub is optional and needs both repository secrets. Ensure the GHCR package is public for unauthenticated pulls.

## 构建配置

`internal/conf/version.go` 定义默认仓库和构建者。发布流程使用当前 GitHub 仓库及所有者覆盖它们，设置页和更新器均读取后端配置。

完整跨平台构建脚本面向 Ubuntu runner，要求已安装 Go、zip、Android NDK 并设置 `ANDROID_NDK_HOME`，且已构建前端：

```bash
export BUILD_VERSION=v1.0.0
export VITE_APP_VERSION="$BUILD_VERSION"
export BUILD_REPOSITORY=suprelory/octopus
export BUILD_AUTHOR=suprelory
cd web
pnpm install --frozen-lockfile
pnpm build
cd ..
bash scripts/build.sh
```

未设置 `BUILD_VERSION` 时后端版本为 `dev`，不会通过 `git describe` 继承本地的上游标签。Go 模块路径由 `go list -m` 获取，用于链接器注入及许可证报告，与发布仓库名称独立。
