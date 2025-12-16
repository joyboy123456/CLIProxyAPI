# 本地开发环境设置

## Go 环境安装

### macOS (使用 Homebrew)

```bash
# 安装 Go
brew install go

# 验证安装
go version
# 应该显示: go version go1.24.x darwin/amd64
```

### 验证项目构建

```bash
# 进入项目目录
cd source_code

# 下载依赖
go mod download

# 构建项目
go build ./cmd/server

# 运行项目 (需要配置文件)
cp config.example.yaml config.yaml
# 编辑 config.yaml 配置你的 API keys
./server
```

## 本地开发流程

### 1. 编辑代码
使用你喜欢的编辑器 (VS Code, GoLand 等) 编辑代码

### 2. 本地测试
```bash
# 快速运行 (不生成二进制)
go run ./cmd/server

# 或者编译后运行
go build -o CLIProxyAPI ./cmd/server
./CLIProxyAPI
```

### 3. 推送部署
```bash
git add .
git commit -m "your changes"
git push origin main
# GitHub Actions 会自动部署到 VPS
```

## 配置文件说明

本地开发时，复制 `config.example.yaml` 为 `config.yaml` 并根据需要修改：

```yaml
# 基本配置
host: "127.0.0.1"  # 本地开发用 localhost
port: 8317

# API Keys (根据你的需求配置)
api-keys:
  - "your-local-test-key"
```

## 常用开发命令

```bash
# 格式化代码
go fmt ./...

# 运行测试
go test ./...

# 检查依赖
go mod tidy

# 交叉编译 (生成 Linux 版本)
GOOS=linux GOARCH=amd64 go build -o CLIProxyAPI ./cmd/server
```
