# Changelog

本项目的所有重要变更都记录在这个文件里。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/),
版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [1.0.0] - 2026-11-04

首个开源版本。

### 新增

- **server/**:Go 后端 —— Hertz + GORM + Casbin,49 个框架原语包
  (缓存 / 幂等 / 限流 / 熔断 / 灰度 / KMS / 审计 / 短信 / OSS / Swagger …),
  每包配有手册章节与 `go run` 即跑的示例
- **web/**:React 18 + Ant Design 5 管理端(用户 / 角色 / 资源 / 部门 / 字典 /
  配置中心 / 审计 / 文件 / 动态表单设计器 / 通用审批 / 站内信 …)
- **js-sdk/**:@rapid-admin/dyn-sdk —— 动态表单数据面 SDK,零运行时依赖
- **docs/**:权限模型 / 文件系统 / 通用审批等设计文档 + 56 页 GitBook 框架手册
  (`go:embed` 嵌入二进制,`/docs` 直接访问)
- **工程守卫**:框架原语可达性检查、文档断链扫描、手册产物漂移检测、
  ROADMAP 计数一致性校验
- **deploy/**:systemd unit、备份 / 恢复脚本、Prometheus 告警规则
