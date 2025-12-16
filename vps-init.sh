#!/bin/bash
set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# 检查是否以 root 权限运行
if [ "$EUID" -ne 0 ]; then
    log_error "请使用 sudo 运行此脚本"
    exit 1
fi

log_info "开始初始化 VPS 环境..."

# 定义变量
APP_DIR="/opt/cliproxy"
SERVICE_NAME="cliproxy"
USER_NAME="cliproxy"

# 步骤 1: 创建系统用户
log_step "1. 创建系统用户"
if id "$USER_NAME" &>/dev/null; then
    log_info "用户 $USER_NAME 已存在"
else
    useradd --system --home "$APP_DIR" --shell /bin/false --create-home "$USER_NAME"
    log_info "已创建系统用户 $USER_NAME"
fi

# 步骤 2: 创建目录结构
log_step "2. 创建目录结构"
mkdir -p "$APP_DIR"/{logs,auths}
log_info "已创建目录结构"

# 步骤 3: 设置目录权限
log_step "3. 设置目录权限"
chown -R "$USER_NAME:$USER_NAME" "$APP_DIR"
chmod 755 "$APP_DIR"
chmod 755 "$APP_DIR/logs"
chmod 700 "$APP_DIR/auths"  # 认证目录需要更严格的权限
log_info "已设置目录权限"

# 步骤 4: 复制 systemd 服务文件
log_step "4. 安装 systemd 服务"
if [ -f "cliproxy.service" ]; then
    cp cliproxy.service /etc/systemd/system/
    log_info "已复制服务文件到 /etc/systemd/system/"
else
    log_error "cliproxy.service 文件不存在"
    log_info "请确保在包含 cliproxy.service 的目录中运行此脚本"
    exit 1
fi

# 步骤 5: 复制部署脚本
log_step "5. 安装部署脚本"
if [ -f "deploy.sh" ]; then
    cp deploy.sh "$APP_DIR/"
    chmod +x "$APP_DIR/deploy.sh"
    chown "$USER_NAME:$USER_NAME" "$APP_DIR/deploy.sh"
    log_info "已安装部署脚本到 $APP_DIR/deploy.sh"
else
    log_error "deploy.sh 文件不存在"
    log_info "请确保在包含 deploy.sh 的目录中运行此脚本"
    exit 1
fi

# 步骤 6: 重载 systemd 配置
log_step "6. 重载 systemd 配置"
systemctl daemon-reload
log_info "已重载 systemd 配置"

# 步骤 7: 启用服务开机自启
log_step "7. 启用服务开机自启"
systemctl enable "$SERVICE_NAME"
log_info "已启用 $SERVICE_NAME 服务开机自启"

# 步骤 8: 检查防火墙设置（如果使用 ufw）
log_step "8. 检查防火墙设置"
if command -v ufw &> /dev/null; then
    log_info "检测到 ufw 防火墙"
    echo "需要开放的端口: 8317, 8085, 1455, 54545, 51121, 11451"
    read -p "是否自动配置防火墙规则? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        ufw allow 8317/tcp
        ufw allow 8085/tcp
        ufw allow 1455/tcp
        ufw allow 54545/tcp
        ufw allow 51121/tcp
        ufw allow 11451/tcp
        log_info "已配置防火墙规则"
    else
        log_warn "请手动配置防火墙规则"
    fi
else
    log_info "未检测到 ufw 防火墙，请手动检查防火墙设置"
fi

# 步骤 9: 显示初始化结果
log_step "9. 初始化完成"
echo ""
echo "=== 初始化结果 ==="
echo "应用目录: $APP_DIR"
echo "系统用户: $USER_NAME"
echo "服务名称: $SERVICE_NAME"
echo "部署脚本: $APP_DIR/deploy.sh"
echo ""

log_info "✅ VPS 初始化完成！"
echo ""
echo "=== 下一步操作 ==="
echo "1. 配置 GitHub Secrets (VPS_HOST, VPS_USER, VPS_SSH_KEY, VPS_PORT)"
echo "2. 推送代码到 main 分支触发自动部署"
echo "3. 或者手动编译并部署:"
echo "   go build -o CLIProxyAPI ./cmd/server"
echo "   sudo cp CLIProxyAPI /tmp/"
echo "   sudo $APP_DIR/deploy.sh"
echo ""

# 步骤 10: 检查系统资源
log_step "10. 系统资源检查"
echo "=== 系统资源状态 ==="
echo "内存使用:"
free -h
echo ""
echo "磁盘使用:"
df -h /
echo ""

log_info "🎉 初始化脚本执行完成！"