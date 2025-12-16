# VPS 迁移提示词

当你通过 antigravity SSH 连接到 VPS 准备迁移时，可以直接复制以下命令：

## 一键迁移脚本

```bash
# 1. 备份现有配置
echo "=== 备份现有配置 ==="
cp config.yaml config.yaml.backup
cp -r auths auths.backup
cp -r logs logs.backup
echo "✅ 备份完成"

# 2. 停止 Docker 服务
echo "=== 停止 Docker 服务 ==="
docker-compose down
echo "✅ Docker 服务已停止"

# 3. 创建新目录结构
echo "=== 创建新目录结构 ==="
sudo mkdir -p /opt/cliproxy/logs
sudo useradd --system --home /opt/cliproxy --shell /bin/false cliproxy 2>/dev/null || echo "用户已存在"
sudo chown -R cliproxy:cliproxy /opt/cliproxy
echo "✅ 目录结构创建完成"

# 4. 迁移数据
echo "=== 迁移配置和数据 ==="
sudo cp config.yaml /opt/cliproxy/config.yaml
sudo cp -r auths /opt/cliproxy/auths
sudo cp -r logs/* /opt/cliproxy/logs/ 2>/dev/null || true
sudo chown -R cliproxy:cliproxy /opt/cliproxy
echo "✅ 数据迁移完成"

# 5. 创建 systemd 服务
echo "=== 创建 systemd 服务 ==="
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

sudo systemctl daemon-reload
sudo systemctl enable cliproxy
echo "✅ systemd 服务创建完成"

# 6. 创建部署脚本
echo "=== 创建部署脚本 ==="
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

sudo chmod +x /opt/cliproxy/deploy.sh
echo "✅ 部署脚本创建完成"

# 7. 首次手动部署
echo "=== 首次手动部署 ==="
echo "请在项目目录执行以下命令："
echo "go build -o CLIProxyAPI ./cmd/server"
echo "sudo cp CLIProxyAPI /tmp/CLIProxyAPI"
echo "sudo /opt/cliproxy/deploy.sh"
```

## 验证命令

迁移完成后，使用以下命令验证：

```bash
# 检查服务状态
sudo systemctl status cliproxy

# 查看实时日志
sudo journalctl -u cliproxy -f

# 检查端口监听
sudo netstat -tlnp | grep -E "(8317|8085|1455|54545|51121|11451)"

# 测试 API
curl -I http://localhost:8317
```

## 如果出现问题

```bash
# 查看详细错误日志
sudo journalctl -u cliproxy --no-pager -l -n 50

# 检查文件权限
ls -la /opt/cliproxy/

# 手动测试二进制文件
sudo -u cliproxy /opt/cliproxy/CLIProxyAPI --help
```

## 回滚命令（如果需要）

```bash
# 停止新服务
sudo systemctl stop cliproxy
sudo systemctl disable cliproxy

# 恢复 Docker
cp config.yaml.backup config.yaml
cp -r auths.backup auths
docker-compose up -d
```