# Docker Compose 与 Actions

## 镜像发布

参照 z-sub-convert，PR 执行检查，master push 或 workflow_dispatch 在检查通过后构建 linux/amd64、linux/arm64 并推送 Docker Hub。仓库配置 secret `DOCKERHUB_TOKEN`，账号为 `jiuwusan`。不保存服务器密码，不 SSH 自动部署 NAS。

## 首次启动

在新部署目录保存 docker-compose.yml，将 .env.example 复制为 .env 并设置真实 HTTPS 入口。不要提交 .env、secrets 或数据卷。

```sh
docker pull jiuwusan/z-jellyfin-hub:latest
mkdir -p secrets
docker run --rm --user 0 -v "$PWD/secrets:/secrets" jiuwusan/z-jellyfin-hub:latest init --secrets-dir /secrets
docker network inspect wk
docker compose config --quiet
docker compose up -d
```

初始化只创建新的随机密钥和管理员密码文件；已有文件时拒绝覆盖。文件归属运行 UID 10001，权限 0600。管理员首次密码在 secrets/admin.password，需通过有权限的本地操作查看，不输出到 Actions 或服务日志。

容器仅暴露 wk 内部 8080 端口。HTTPS 入口转发到 `http://hub:8080`；不要公开额外 HTTP 端口。入口需保留 Host/Origin、认证头和订阅响应的 ETag，禁止订阅路径共享缓存与认证头日志。建议使用独立域名。新增 HTTPS 入口由部署者执行，不修改已有 Jellyfin/mihomo 服务。

可设置包含路径前缀的 HUB_PUBLIC_URL；入口应统一保留或剥离该前缀，不能重复添加。后台 API 基址从公开配置生成，订阅完整 URL 保留前缀。

## 更新与回退

```sh
docker compose pull
docker compose up -d
docker compose ps
```

默认 latest 适合日常更新；生产可将 IMAGE_TAG 固定为 `sha-提交SHA`。回退镜像前保留数据库一致性备份及原密钥；不删除数据卷或重新初始化密钥。

## 备份与恢复

```sh
docker compose exec hub hub backup --output /data/backup-new.db
```

输出文件必须不存在。通过 SQLite VACUUM INTO 生成一致性快照，不直接复制正在写入的 hub.db。将快照导出保管，主密钥另行保管，不放在同一个公开备份包。

恢复时停止新 Hub 实例，在空数据卷恢复快照为 hub.db，挂载对应原密钥并启动。只备份数据库而丢失密钥无法解密凭据。不要删除现有实例或其他服务的数据进行恢复测试。

## 本地开发

先执行 web/npm ci 和 npm run build，再 go run ./cmd/hub init --secrets-dir 本地私有目录。将密钥/密码文件路径设置为 HUB_MASTER_KEY_FILE/HUB_ADMIN_PASSWORD_FILE，HUB_DATA_DIR 指向临时目录。

仅本机浏览器开发可同时设置 HUB_DEV_ALLOW_HTTP=true、HUB_LISTEN=127.0.0.1:8080、HUB_PUBLIC_URL=http://127.0.0.1:8080。该例外不能监听公网，也不能用于 Android 正式订阅。Android 订阅始终要求 HTTPS。
