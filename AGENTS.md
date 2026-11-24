# 项目协作规范

- 当前分支工作，不创建分支或 worktree；未明确要求时不提交、推送或发布。
- 先读 docs/requirements/技术方案.md、docs/standards/开发规范.md 和 docs/api/订阅协议.md。
- Go 后端、Vue 3/TypeScript 管理后台、SQLite 单实例。Docker Compose 外部网络固定 wk，Actions 参照 z-sub-convert 的检查与 Docker Hub 多架构镜像发布流程。
- 无真实密码、订阅链接、令牌、服务器地址、签名或数据库密钥进入工程、日志、截图和测试夹具；只使用占位或测试期临时生成的值。
- 业务缺陷先复现再修复；协议、加密、权限或配置应用时机的变更必须测试。完成前运行 Go 测试、vet、前端类型检查及构建，并核对 Compose 和镜像构建路径。
- 不修改 Jellyfin、mihomo、nginx 或其他已有服务配置。不可用环境和未执行的检查如实记录。
