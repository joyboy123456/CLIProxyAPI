# Implementation Plan

- [x] 1. 创建 GitHub Actions 部署 Workflow
  - [x] 1.1 创建 `.github/workflows/deploy.yml` 文件
    - 配置 push 到 main 分支触发
    - 设置 Go 1.24 环境
    - 编译 Linux amd64 二进制
    - 使用 appleboy/ssh-action 连接 VPS
    - 上传二进制并执行部署脚本
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 3.1, 3.2_

- [x] 2. 创建 VPS 端配置文件
  - [x] 2.1 创建 systemd service 文件 `cliproxy.service`
    - 配置服务类型为 simple
    - 设置工作目录 `/opt/cliproxy`
    - 配置 HOME 环境变量指向 /opt/cliproxy
    - 配置 TZ 环境变量为 Asia/Shanghai
    - 配置 Restart=on-failure 和 RestartSec=5
    - 设置开机自启 WantedBy=multi-user.target
    - _Requirements: 2.1, 2.2, 2.3, 2.4_
  - [x] 2.2 创建部署脚本 `deploy.sh`
    - 停止现有服务
    - 备份旧二进制文件
    - 移动新二进制到目标位置
    - 检查并保留 config.yaml
    - 重启服务并验证状态
    - _Requirements: 5.1, 5.2, 5.3_

- [x] 3. 创建 Docker 到直接运行的迁移指南
  - [x] 3.1 创建 `MIGRATION.md` 迁移指南文档
    - 备份现有 Docker 配置和数据
    - 停止 Docker 服务
    - 创建新的目录结构
    - 迁移配置文件和认证数据
    - 安装 systemd 服务
    - 验证迁移结果
    - _Requirements: 5.1, 5.2_

- [x] 4. 创建 VPS 初始化脚本和配置文档
  - [x] 4.1 创建 VPS 首次部署初始化脚本 `vps-init.sh`
    - 创建 /opt/cliproxy 目录
    - 复制 systemd service 文件
    - 启用服务开机自启
    - _Requirements: 2.1_
  - [x] 4.2 创建 GitHub Secrets 配置说明文档
    - 说明需要配置的 secrets
    - 提供 SSH 密钥生成指南
    - _Requirements: 3.1, 3.2, 3.3_

- [x] 5. 本地开发验证
  - [x] 5.1 验证本地 Go 构建
    - 确认 `go build ./cmd/server` 可以成功编译
    - _Requirements: 4.1, 4.2, 4.3_
