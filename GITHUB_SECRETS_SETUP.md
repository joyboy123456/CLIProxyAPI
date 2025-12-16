# GitHub Secrets 配置指南

## 概述

为了让 GitHub Actions 能够自动部署到你的 VPS，需要配置以下 Secrets 来存储 VPS 连接信息。

## 必需的 Secrets

### 1. VPS_HOST
- **说明**: VPS 的 IP 地址或域名
- **示例**: `192.168.1.100` 或 `your-domain.com`
- **获取方式**: 
  ```bash
  # 在 VPS 上查看公网 IP
  curl ifconfig.me
  ```

### 2. VPS_USER
- **说明**: SSH 登录用户名
- **示例**: `root` 或 `ubuntu`
- **注意**: 该用户需要有 sudo 权限

### 3. VPS_SSH_KEY
- **说明**: SSH 私钥内容
- **格式**: 完整的私钥文件内容，包括 `-----BEGIN` 和 `-----END` 行

### 4. VPS_PORT
- **说明**: SSH 端口号
- **默认值**: `22`
- **示例**: `22` 或 `2222`

## SSH 密钥生成和配置

### 步骤 1: 生成 SSH 密钥对

在你的本地机器上运行：

```bash
# 生成新的 SSH 密钥对
ssh-keygen -t rsa -b 4096 -C "github-actions@your-project" -f ~/.ssh/cliproxy_deploy

# 这会生成两个文件:
# ~/.ssh/cliproxy_deploy (私钥)
# ~/.ssh/cliproxy_deploy.pub (公钥)
```

### 步骤 2: 将公钥添加到 VPS

```bash
# 方法 1: 使用 ssh-copy-id (推荐)
ssh-copy-id -i ~/.ssh/cliproxy_deploy.pub user@your-vps-ip

# 方法 2: 手动复制
cat ~/.ssh/cliproxy_deploy.pub
# 复制输出内容，然后在 VPS 上执行:
# echo "粘贴的公钥内容" >> ~/.ssh/authorized_keys
```

### 步骤 3: 测试 SSH 连接

```bash
# 测试连接
ssh -i ~/.ssh/cliproxy_deploy user@your-vps-ip

# 如果能正常连接，说明配置成功
```

### 步骤 4: 获取私钥内容

```bash
# 显示私钥内容
cat ~/.ssh/cliproxy_deploy

# 复制完整输出，包括 -----BEGIN 和 -----END 行
```

## 在 GitHub 中配置 Secrets

### 步骤 1: 进入仓库设置

1. 打开你的 GitHub 仓库
2. 点击 **Settings** 标签
3. 在左侧菜单中点击 **Secrets and variables** → **Actions**

### 步骤 2: 添加 Secrets

点击 **New repository secret** 按钮，依次添加以下 secrets：

#### VPS_HOST
- **Name**: `VPS_HOST`
- **Secret**: 你的 VPS IP 地址或域名

#### VPS_USER  
- **Name**: `VPS_USER`
- **Secret**: SSH 用户名

#### VPS_SSH_KEY
- **Name**: `VPS_SSH_KEY`
- **Secret**: 完整的私钥内容
```
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABFwAAAAdzc2gtcn
... (私钥内容) ...
-----END OPENSSH PRIVATE KEY-----
```

#### VPS_PORT
- **Name**: `VPS_PORT`
- **Secret**: `22` (或你的自定义端口)

## 安全建议

### 1. 使用专用密钥
- 为部署创建专用的 SSH 密钥
- 不要使用你的个人 SSH 密钥

### 2. 限制密钥权限
在 VPS 上可以限制该密钥只能执行特定命令：

```bash
# 编辑 ~/.ssh/authorized_keys
# 在公钥前添加限制:
command="/opt/cliproxy/deploy.sh",no-port-forwarding,no-X11-forwarding,no-agent-forwarding ssh-rsa AAAAB3...
```

### 3. 定期轮换密钥
- 建议每 3-6 个月更换一次部署密钥
- 删除不再使用的旧密钥

## 验证配置

### 方法 1: 手动触发 Workflow

1. 进入 GitHub 仓库的 **Actions** 标签
2. 选择 **Deploy to VPS** workflow
3. 点击 **Run workflow** 按钮
4. 观察执行日志

### 方法 2: 推送代码触发

```bash
# 推送到 main 分支
git add .
git commit -m "test: trigger deployment"
git push origin main
```

## 故障排除

### SSH 连接失败

**错误**: `Permission denied (publickey)`

**解决方案**:
1. 检查公钥是否正确添加到 VPS
2. 检查 VPS 上的 SSH 配置
3. 确认私钥格式正确

### 权限不足

**错误**: `sudo: no tty present and no askpass program specified`

**解决方案**:
```bash
# 在 VPS 上为部署用户配置免密 sudo
echo "your-user ALL=(ALL) NOPASSWD: /opt/cliproxy/deploy.sh, /bin/systemctl" | sudo tee /etc/sudoers.d/cliproxy-deploy
```

### 端口连接问题

**错误**: `Connection refused`

**解决方案**:
1. 检查 VPS 防火墙设置
2. 确认 SSH 端口配置
3. 检查 VPS_PORT secret 是否正确

## 测试清单

部署前请确认：

- [ ] SSH 密钥已生成并配置
- [ ] 可以使用私钥 SSH 连接到 VPS
- [ ] 所有 4 个 Secrets 已在 GitHub 中配置
- [ ] VPS 上已运行初始化脚本 `vps-init.sh`
- [ ] 部署用户有执行部署脚本的权限

完成以上配置后，推送代码到 main 分支即可触发自动部署！
