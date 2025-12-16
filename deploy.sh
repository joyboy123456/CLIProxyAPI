#!/bin/bash
set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
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

# 检查是否以 root 权限运行
if [ "$EUID" -ne 0 ]; then
    log_error "请使用 sudo 运行此脚本"
    exit 1
fi

log_info "开始部署 CLIProxyAPI..."

# 定义路径
APP_DIR="/opt/cliproxy"
BINARY_NAME="CLIProxyAPI"
SERVICE_NAME="cliproxy"
TEMP_BINARY="/tmp/CLIProxyAPI"

# 检查临时二进制文件是否存在
if [ ! -f "$TEMP_BINARY" ]; then
    log_error "临时二进制文件 $TEMP_BINARY 不存在"
    log_info "请确保 GitHub Actions 已正确上传文件"
    exit 1
fi

# 停止现有服务
log_info "停止现有服务..."
systemctl stop $SERVICE_NAME || log_warn "服务可能未运行"

# 备份旧版本
if [ -f "$APP_DIR/$BINARY_NAME" ]; then
    log_info "备份旧版本..."
    mv "$APP_DIR/$BINARY_NAME" "$APP_DIR/${BINARY_NAME}.bak"
    log_info "已备份到 ${BINARY_NAME}.bak"
fi

# 移动新版本
log_info "部署新版本..."
mv "$TEMP_BINARY" "$APP_DIR/$BINARY_NAME"
chmod +x "$APP_DIR/$BINARY_NAME"
chown cliproxy:cliproxy "$APP_DIR/$BINARY_NAME"

# 检查并创建配置文件
if [ ! -f "$APP_DIR/config.yaml" ]; then
    if [ -f "$APP_DIR/config.example.yaml" ]; then
        log_info "创建默认配置文件..."
        cp "$APP_DIR/config.example.yaml" "$APP_DIR/config.yaml"
        chown cliproxy:cliproxy "$APP_DIR/config.yaml"
        log_warn "请编辑 $APP_DIR/config.yaml 配置文件"
    else
        log_warn "配置文件不存在，服务可能无法正常启动"
    fi
else
    log_info "配置文件已存在，保持不变"
fi

# 确保目录权限正确
log_info "设置目录权限..."
chown -R cliproxy:cliproxy "$APP_DIR"
chmod 755 "$APP_DIR"

# 启动服务
log_info "启动服务..."
systemctl start $SERVICE_NAME

# 等待服务启动
log_info "等待服务启动..."
sleep 3

# 检查服务状态
if systemctl is-active --quiet $SERVICE_NAME; then
    log_info "✅ 部署成功！服务正在运行"
    
    # 显示服务状态
    echo "=== 服务状态 ==="
    systemctl status $SERVICE_NAME --no-pager -l
    
    # 显示最近日志
    echo ""
    echo "=== 最近日志 ==="
    journalctl -u $SERVICE_NAME --no-pager -l -n 10
    
    # 检查端口监听
    echo ""
    echo "=== 端口监听状态 ==="
    netstat -tlnp | grep -E "(8317|8085|1455|54545|51121|11451)" || log_warn "未检测到预期端口"
    
    log_info "🎉 部署完成！"
else
    log_error "❌ 部署失败！服务未能启动"
    echo "=== 错误日志 ==="
    journalctl -u $SERVICE_NAME --no-pager -l -n 20
    
    # 尝试恢复旧版本
    if [ -f "$APP_DIR/${BINARY_NAME}.bak" ]; then
        log_info "尝试恢复旧版本..."
        mv "$APP_DIR/${BINARY_NAME}.bak" "$APP_DIR/$BINARY_NAME"
        systemctl start $SERVICE_NAME
        
        if systemctl is-active --quiet $SERVICE_NAME; then
            log_info "✅ 已恢复到旧版本"
        else
            log_error "❌ 恢复失败，请手动检查"
        fi
    fi
    
    exit 1
fi