# Docker Compose 与 Actions

## 镜像发布

PR 执行 Go 测试/vet/race、前端测试/构建与 Compose 检查；master push 或 workflow_dispatch 检查通过后推送 Docker Hub 的 linux/amd64、linux/arm64 镜像。仓库 secret 为 DOCKERHUB_TOKEN。Actions 不 SSH 部署 NAS。

## 首次启动与引导

保存 docker-compose.yml 后即可启动，environment 仅保留 TZ=Asia/Shanghai，不需要 .env；.env.example 仅用于可选 IMAGE_TAG 覆盖。外部网络 wk 必须已经存在。

~~~sh
docker network inspect wk
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose exec hub hub setup-code
~~~

首次启动自动在独立 secrets 卷生成 /secrets/hub.key 和一次性 setup.code（0600），数据库位于 data 卷 /data/hub.db。最后一条命令只在初始化尚未完成时显示初始化码，不写到服务/Actions 日志。浏览器引导填写该码、管理员账号和至少 12 字符的密码，不再填写公网地址。完成后码失效并删除，管理员密码保存 Argon2id 哈希；重启或覆盖镜像不重新初始化。

容器以 UID 10001 运行，根文件系统只读，data/secrets 卷可写。不要删除卷来更新服务。已有数据库缺少原主密钥会拒绝启动，不能生成替代密钥覆盖；挂错密钥也会拒绝打开数据。

## 访问与订阅地址

服务不限制后台访问的域名、IP、端口或 HTTP/HTTPS；不设置固定地址白名单，不自动发送长期 HSTS。容器内部监听 8097/TCP，Compose 将宿主机 8097 映射到容器 8097，可通过 http://宿主机IP:8097 访问后台；wk 内服务仍可使用 http://hub:8097。HTTPS 入口可转发到上述任一可达地址，不改其他既有服务。

后台 API/资源采用相对地址，支持独立域名根路径和 /hub/ 等路径前缀；前缀可由反代保留或剥离，浏览器页面地址以 / 结尾。HTTPS 入口必须保留原 Host、Origin、Authorization 和 ETag。服务不从 X-Forwarded-Host/Forwarded 推导公开链接，也不因为转发头跳过 CSRF。

写操作校验请求 Host 与 Origin 的主机/端口匹配，并检查 CSRF Token；浏览器 HTTPS 使用独立的 __Host-hub_session Secure Cookie，HTTP 使用 hub_session_http，避免同域不同协议/端口的 Cookie 覆盖冲突。访问不同域名需分别登录。入口如更改 Host 而不保留原值，会导致同源校验失败，应修正代理请求头，不能通过固定公网地址环境变量修补。

创建或轮换订阅时，API 仅返回相对路径，浏览器用当前页面地址生成完整链接。因此应从手机能到达的 HTTPS 地址打开后台后复制。HTTP 或仅内网可达地址仍可管理，但生成的链接不能直接用于要求 HTTPS 的 Android；页面会提示，不阻止操作。复制后更换后台访问地址不会自动改变已经发给手机的链接。

## 更新、备份与恢复

~~~sh
docker compose pull
docker compose up -d
docker compose ps
docker compose exec hub hub backup --output /data/backup-new.db
~~~

备份输出文件须不存在。通过 SQLite VACUUM INTO 生成一致性快照，再从容器导出并保存到仓库外；主密钥单独从 secrets 卷保管，数据库与密钥不要放在同一个公开备份包。丢失密钥无法解密授权。

恢复时停止 Hub，在 data 卷还原快照为 hub.db，在 secrets 卷还原对应原 hub.key，并核对 UID 10001 读写权限；已初始化数据不再出现引导。恢复测试使用独立目录或卷，不删除其他服务数据。升级旧版独立 secret 文件部署时，必须先保留原密钥，迁移至 /secrets/hub.key，或显式用 HUB_MASTER_KEY_FILE 指定旧只读挂载路径；已有管理员无需重新创建。

生产可通过 IMAGE_TAG 固定已发布 sha 镜像。回退前保留一致性备份及原密钥；不要重新初始化。

## 本地开发

先在 web 执行 npm ci、npm test 和 npm run build。Go 可选环境变量仅供路径/监听覆盖：HUB_LISTEN=127.0.0.1:8097、HUB_DATA_DIR 指向临时数据库目录、HUB_MASTER_KEY_FILE 指向独立临时密钥路径。执行 go run ./cmd/hub 即可进入引导；用相同路径执行 go run ./cmd/hub setup-code 获取一次性码。无需 HUB_PUBLIC_URL、HUB_ADMIN_USER 或 HUB_ADMIN_PASSWORD_FILE。

本机无 Docker 时只验证本地二进制和 YAML，不能宣称容器或镜像发布已经通过。

## 令牌恢复与服务重建

固定订阅链接为 /api/v1/subscription/config#令牌，不含订阅 ID。创建时可指定原 43 位令牌；已有订阅可设置/恢复指定令牌或随机轮换。不同订阅不能绑定同一令牌，Hub 只存摘要，无法找回未备份的令牌。

灾后流程：保持原手机可达 HTTPS 域名、端口和路径前缀；使用新数据目录/卷初始化 Hub，重建代理/服务/授权/策略；新建订阅时填原令牌，预览并发布。新库有新的 serverInstanceId，设备按既有刷新周期重新验证代理与登录，成功后接收新版本 1，无需重新输入链接；可在设备点立即同步加快检查。现有 Android 升级到 0.6.43 可自动迁移已有可信缓存中的旧链接。

完整数据库备份保留原实例标识；如果还原的是落后于设备的旧快照，同实例防回退仍生效。此时应重新发布足够高的版本，或按新数据库+原令牌流程恢复；不能通过关闭版本保护强行接受旧配置。该功能不恢复已丢失的媒体代理/Jellyfin 密码，必须重建正确授权。
