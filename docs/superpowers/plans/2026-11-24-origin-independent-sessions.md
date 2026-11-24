# 移除 Origin 限制实施计划

> **For agentic workers:** 使用 superpowers:executing-plans 按步骤执行，当前 master，不新建分支，不提交、推送或部署。

**Goal:** 不修改 Nginx，允许 Host 被反代改写，保留会话、CSRF 和 HTTP/HTTPS Cookie 行为。

**Architecture:** 初始化与登录不再校验 Origin/Fetch Metadata；管理写操作仅依赖有效会话与 CSRF Token。HTTPS Cookie 判断与授权解耦：直连 TLS 优先，其次合法 HTTP(S) Origin 的协议，缺失或无效 Origin 时可根据收到的 HTTPS 会话 Cookie 保持 Secure。Origin 仅提示 Cookie 属性，不能证明 TLS；不新增代理信任或 CORS 放行配置。

**Tech Stack:** Go net/http、httptest、现有 Vue 管理后台。

- [x] 添加回归：改写 Host、任意/缺失 Origin、cross-site 标头不阻止合法认证；错误密码、会话/CSRF 缺失或错误仍拒绝；初始化码必需且不能重复初始化。
- [x] 添加 HTTP/HTTPS Cookie、退出与改密清理回归，以及真实 TLS 终止反代测试；先运行确认失败。
- [x] 移除 sameOrigin 授权条件，独立实现 Cookie 判断，运行回归。
- [x] 更新当前规范、协议和部署说明，历史验收记录保留历史含义。
- [x] 完成 Go test/vet、前端测试/类型检查/构建，核对 Compose、Docker 构建路径和差异。本机无 Docker，不宣称执行了容器或镜像验证。
