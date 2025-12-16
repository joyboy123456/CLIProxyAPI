# Docker 到直接运行迁移指南

## 迁移概述

将 CLIProxyAPI 从 Docker 容器迁移到直接运行二进制文件，释放内存资源，避免 VPS 频繁宕机。

## 迁移前准备

### 1. 备份现有配置
```bash
# 备份 Docker 配置文件
cp config.yaml config.yaml.backup
cp -r auths auths.backup
cp -r logs logs.backup

# 查看当前 Docker 状态
docker ps
docker-compose ps
```

### 2. 记录当前端口配置
确认以下端口正在使用：
- 8317 (主端口)
- 8085, 1455, 54545, 51121, 11451

## 迁移步骤

### 第一步：停止 Docker 服务
```bash
# 停止 Docker Compose 服务
docker-compose down

# 确认容器已停止
docker ps
```

### 第二步：创建新的目录结构
```bash
# 创建应用目录
sudo mkdir -p /opt/cliproxy
sudo mkdir -p /opt/cliproxy/logs

# 设置权限
sudo useradd --system --home /opt/cliproxy --shell /bin/false cliproxy
sudo chown -R cliproxy:cliproxy /opt/cliproxy
```

### 第三步：迁移配置文件和数据
```bash
# 复制配置文件
sudo cp config.yaml /opt/cliproxy/config.yaml

# 复制认证数据
sudo cp -r auths /opt/cliproxy/auths

# 复制日志（可选）
sudo cp -r logs/* /opt/cliproxy/logs/ 2>/dev/null || true

# 设置权限
sudo chown -R cliproxy:cliproxy /opt/cliproxy
```

### 第四步：创建 systemd 服务
```bash
# 创建服务文件
sudo tee /etc/systemd/system/cliproxy.service > /dev/null << 'EOF'
[Unit]
Description=CLI Proxy API Server
After=network.target
Wants=network.target

[Service]
Type=simple
User=cliproxy
Group=cliproxy
WorkingDirectory=/opt/cliproxy
ExecStart=/opt/cliproxy/CLIProxyAPI
Environment=HOME=/opt/cliproxy
Environment=TZ=Asia/Shanghai
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 重载 systemd 配置
sudo systemctl daemon-reload

# 启用开机自启
sudo systemctl enable cliproxy
```

### 第五步：部署二进制文件
```bash
# 创建部署脚本
sudo tee /opt/cliproxy/deploy.sh > /dev/null << 'EOF'
#!/bin/bash
set -e

echo "开始部署 CLIProxyAPI..."

# 停止服务
sudo systemctl stop cliproxy || true

# 备份旧版本
if [ -f "/opt/cliproxy/CLIProxyAPI" ]; then
    sudo mv /opt/cliproxy/CLIProxyAPI /opt/cliproxy/CLIProxyAPI.bak
    echo "已备份旧版本"
fi

# 移动新版本
if [ -f "/tmp/CLIProxyAPI" ]; then
    sudo mv /tmp/CLIProxyAPI /opt/cliproxy/CLIProxyAPI
    sudo chmod +x /opt/cliproxy/CLIProxyAPI
    sudo chown cliproxy:cliproxy /opt/cliproxy/CLIProxyAPI
    echo "已部署新版本"
fi

# 确保配置文件存在
if [ ! -f "/opt/cliproxy/config.yaml" ]; then
    if [ -f "/opt/cliproxy/config.example.yaml" ]; then
        sudo cp /opt/cliproxy/config.example.yaml /opt/cliproxy/config.yaml
        sudo chown cliproxy:cliproxy /opt/cliproxy/config.yaml
        echo "已创建默认配置文件"
    fi
fi

# 启动服务
sudo systemctl start cliproxy

# 等待启动
sleep 3

# 检查状态
if sudo systemctl is-active --quiet cliproxy; then
    echo "✅ 部署成功！服务正在运行"
    sudo systemctl status cliproxy --no-pager -l
else
    echo "❌ 部署失败！检查日志："
    sudo journalctl -u cliproxy --no-pager -l -n 20
    exit 1
fi
EOF

# 设置执行权限
sudo chmod +x /opt/cliproxy/deploy.sh
```

### 第六步：首次手动部署
```bash
# 编译二进制文件
cd /path/to/your/project
go build -o CLIProxyAPI ./cmd/server

# 复制到临时位置
sudo cp CLIProxyAPI /tmp/CLIProxyAPI

# 执行部署
sudo /opt/cliproxy/deploy.sh
```

## 验证迁移结果

### 检查服务状态
```bash
# 查看服务状态
sudo systemctl status cliproxy

# 查看日志
sudo journalctl -u cliproxy -f

# 检查端口监听
sudo netstat -tlnp | grep -E "(8317|8085|1455|54545|51121|11451)"
```

### 测试 API 功能
```bash
# 测试主端口
curl -I http://localhost:8317

# 如果配置了 API key，测试具体接口
curl -H "Authorization: Bearer your-api-key" http://localhost:8317/v1/models
```

## 清理 Docker 资源（可选）

迁移成功后，可以清理 Docker 资源：
```bash
# 删除容器和镜像
docker-compose down --rmi all --volumes

# 删除 Docker 相关文件
rm docker-compose.yml
rm Dockerfile

# 释放 Docker 占用的空间
docker system prune -a
```

## 回滚方案

如果迁移出现问题，可以快速回滚：
```bash
# 停止新服务
sudo systemctl stop cliproxy
sudo systemctl disable cliproxy

# 恢复 Docker 配置
cp config.yaml.backup config.yaml
cp -r auths.backup auths
cp -r logs.backup logs

# 启动 Docker 服务
docker-compose up -d
```

## 故障排除

### 服务无法启动
```bash
# 查看详细日志
sudo journalctl -u cliproxy --no-pager -l

# 检查配置文件
sudo -u cliproxy /opt/cliproxy/CLIProxyAPI --help

# 检查权限
ls -la /opt/cliproxy/
```

### 端口冲突
```bash
# 检查端口占用
sudo netstat -tlnp | grep 8317

# 如果有其他进程占用，停止它们
sudo kill -9 <PID>
```

## 完成迁移

迁移完成后，你的 VPS 内存占用将显著降低，不再频繁宕机。后续的代码更新将通过 GitHub Actions 自动部署。