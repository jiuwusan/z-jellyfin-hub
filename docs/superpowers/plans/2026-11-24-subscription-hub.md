# Hub 与 Android 订阅实现计划

> 在当前分支使用 executing-plans 实施；用户已确认方案和 wk/Actions 镜像发布方式。禁止自动提交、推送、修改既有服务。

Spec: docs/requirements/技术方案.md。Go + Vue/TS + SQLite，Hub 不代理媒体，订阅仅 HTTPS，手机保留字幕音轨行为。

## 任务与验收

- [x] 任务 1：服务端协议、加密与持久化。先测试加密篡改/错上下文、草稿发布隔离、订阅身份、ETag 撤权先于 304，再实现 domain、secret、storage、service/httpapi。
- [x] 任务 2：管理后台。完成安全登录、代理/服务/授权/策略、个人订阅、预览发布、历史回滚、令牌轮换、状态反馈。类型检查、构建并进行浏览器真实交互。
- [x] 任务 3：Docker/Actions。参考 z-sub-convert，检查后推双架构镜像；Compose wk、独立 secret、数据卷、健康检查。初始化/备份/恢复和部署文档配套。
- [x] 任务 4：Android。严格订阅解析，直连 HTTPS/ETag、Keystore 保存、候选验证后替换、离线缓存、配置变更安全应用、订阅设置与旧手动入口移除；构建及受控集成测试。
- [x] 任务 5：归档与交付。核对 Hub/Web、Android、iOS 交接范围、协议样例哈希与引用；最终安全/并发评审，完成可执行检查，不伪称容器发布或 NAS 部署。

## 执行记录

2026-11-24：Hub 仓库已 clone 且 master 干净；Player 存在前轮未提交变更，全部保留。参考项目仅授权为构建/部署约定，不沿用其 Node-only 业务工程限制。当前本机有 Go/Node/Android SDK，没有 Docker/gh CLI；容器运行与镜像发布需在具备环境时验证，不能假称已完成。

2026-11-24：实现与本地可执行验收完成。Go 测试/vet、Vue 类型检查/生产构建、Linux 双架构无 CGO 交叉构建、隔离浏览器管理操作和荣耀三组订阅集成测试通过。retained 与同链接撤销并发问题均有红绿测试。Compose/Actions YAML 静态检查通过；容器运行、Linux race、云端镜像发布和正式 HTTPS 联调仍待部署环境验证，任务勾选仅表示代码与交付资料完成。最终评审未发现剩余 P1/P2。
