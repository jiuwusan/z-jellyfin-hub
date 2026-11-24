# z-jellyfin-hub

家庭影音配置与个人订阅管理服务。Go + Vue 3/TypeScript + SQLite，管理代理、Jellyfin 授权和画质策略；手机只需订阅链接。Hub 不中转视频，不覆盖字幕、音轨偏好。

- [技术方案](docs/requirements/技术方案.md)
- [开发规范](docs/standards/开发规范.md)
- [Docker 与 Actions](docs/deployment/Docker与Actions.md)
- [订阅协议](docs/api/订阅协议.md)

## 开发检查

```sh
cd web
npm ci
npm run build
cd ..
go test ./...
go vet ./...
go build ./cmd/hub
```

生产使用 docker-compose.yml，外部网络为 wk。Actions 通过检查后发布 jiuwusan/z-jellyfin-hub 的 latest 与 sha 镜像；需配置 DOCKERHUB_TOKEN。发布镜像不等于已在 NAS 部署。

首次启动自动生成独立密钥，并通过一次性初始化码引导创建管理员；Compose 仅配置 TZ=Asia/Shanghai。后台不限制域名、IP、端口或 HTTP/HTTPS，订阅链接由浏览器当前地址生成。密码与发布快照加密保存，订阅令牌只存摘要，后台写操作有 CSRF 防护。Android 订阅使用 HTTPS，备份数据库与独立密钥，并保护完整订阅链接。
