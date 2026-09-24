# Sub2API Native

这是 `qiuham/sub2api` 的下游 Native 版本，基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 构建。

## 本仓库改动

- Claude / Codex 原生链路模式（账号级 `native_wire_mode`）
- Native 模式保留客户端请求体和关键请求头
- Claude 遥测感知与阻断
- 复用上游账号池、OAuth、调度、计费、用量记录和 TLS Profile
- 更新检测区分本仓库 Release 与上游 Release

## 分支

- `main`：本仓库生产分支
- 远端仅保留 `main`；本地通过 `origin/main` 跟踪原始上游，用于人工合并（`downstream` 指向本仓库）。

## 镜像

```bash
docker pull ghcr.io/qiuham/sub2api:latest
```


## 上游文档

通用安装和配置说明请参考上游文档：<https://github.com/Wei-Shaw/sub2api>


## 版本与发布

版本格式为 `上游基线-N.修订号`，首个正式版本计划为 `0.2.8-N.1`。
同一基线修复递增修订号；合并新的上游基线后从 `N.1` 开始。
开发测试只提交代码，使用 Release 工作流的 `dry_run` 构建，不创建 Tag 或 Release。
验收通过后才创建 `v0.2.8-N.1` 形式的 Tag；镜像标签不带 `v`。
正式 Release 不标记为 GitHub 预发布；更新与回滚只使用本仓库正式 Release。
旧的 `native.*` 实验版本不作为新版本序列的更新起点，需要手动安装首个正式版本。

CI 规则：
- main 推送或以 main 为目标的 PR：一套 CI；普通测试分支推送不额外启动。
- 同一分支的新提交取消旧的 CI，减少重复运行。
- 安全扫描：每周定时或手动触发，不随每次推送重复运行。
- Release：仅正式 `v*-N.*` Tag 或手动触发；手动验证选择 `dry_run`。
