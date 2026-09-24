# Sub2API Native（下游版）

本仓库是 `qiuham/sub2api` 的生产下游版本，基于上游 `Wei-Shaw/sub2api` 持续合并维护。

## Native 改造

- Claude / Codex 支持账号级 Native 链路切换
- 尽可能透传原生请求体、请求头和响应流
- Claude 遥测感知：检测到遥测时阻止发送到上游
- 保留 OAuth、账号池、调度、sticky、计费、用量记录和 TLS Profile
- 更新检测同时显示本仓库更新与上游更新；自动更新只执行本仓库 Release

## 分支策略

```text
main          本仓库生产分支
upstream-main 上游同步基线
```

上游更新先进入 `upstream-main`，审核后再合并到 `main`，不会直接覆盖本仓库 README 或 Native 改动。

## 镜像

```bash
docker pull ghcr.io/qiuham/sub2api:latest
```

镜像由 GitHub Actions 自动构建发布。

## 上游项目

通用配置和完整功能文档：<https://github.com/Wei-Shaw/sub2api>
