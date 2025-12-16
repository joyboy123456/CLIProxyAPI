# Requirements Document

## Introduction

本功能为 CLIProxyAPI 项目提供一套完整的本地开发 + 自动部署方案。目标是让开发者可以在本地 Mac 上进行 vibe coding，push 代码后自动编译并部署到低配 VPS（1核1G），无需手动 SSH 操作，同时避免使用 Docker 以节省 VPS 资源。

## Glossary

- **VPS**: Virtual Private Server，虚拟专用服务器
- **GitHub Actions**: GitHub 提供的 CI/CD 自动化服务
- **systemd**: Linux 系统服务管理器
- **Binary**: Go 编译后的可执行文件
- **Workflow**: GitHub Actions 的自动化流程配置

## Requirements

### Requirement 1

**User Story:** As a developer, I want to automatically deploy my code to VPS when I push to the main branch, so that I don't need to manually SSH and deploy.

#### Acceptance Criteria

1. WHEN a developer pushes code to the main branch THEN the GitHub_Actions SHALL trigger an automatic build and deploy workflow
2. WHEN the workflow starts THEN the GitHub_Actions SHALL compile the Go project into a Linux amd64 binary
3. WHEN the binary is compiled THEN the GitHub_Actions SHALL transfer the binary to the VPS via SSH
4. WHEN the binary is transferred THEN the GitHub_Actions SHALL restart the systemd service on the VPS
5. IF the deployment fails THEN the GitHub_Actions SHALL report the error in the workflow logs

### Requirement 2

**User Story:** As a VPS administrator, I want the application to run as a systemd service, so that it automatically starts on boot and can be easily managed.

#### Acceptance Criteria

1. WHEN the VPS boots THEN the systemd SHALL automatically start the CLIProxyAPI service
2. WHEN the service crashes THEN the systemd SHALL automatically restart the service within 5 seconds
3. WHEN an administrator runs `systemctl status cliproxy` THEN the systemd SHALL display the current service status
4. WHEN an administrator runs `systemctl stop cliproxy` THEN the systemd SHALL gracefully stop the service

### Requirement 3

**User Story:** As a developer, I want to configure deployment secrets securely, so that my VPS credentials are not exposed in the repository.

#### Acceptance Criteria

1. WHEN configuring the workflow THEN the GitHub_Actions SHALL read VPS credentials from GitHub Secrets
2. WHEN the workflow runs THEN the GitHub_Actions SHALL use SSH key authentication instead of password
3. WHEN secrets are accessed THEN the GitHub_Actions SHALL mask sensitive values in logs

### Requirement 4

**User Story:** As a developer, I want to verify my code locally before pushing, so that I can catch errors early without waiting for deployment.

#### Acceptance Criteria

1. WHEN a developer runs `go run ./cmd/server` THEN the Go_Runtime SHALL start the server locally
2. WHEN a developer runs `go build ./cmd/server` THEN the Go_Compiler SHALL produce a local executable
3. WHEN the local server starts THEN the Go_Runtime SHALL load configuration from config.yaml

### Requirement 5

**User Story:** As a VPS administrator, I want the deployment to preserve my configuration file, so that I don't lose my settings after each deployment.

#### Acceptance Criteria

1. WHEN deploying a new binary THEN the Deployment_Script SHALL preserve the existing config.yaml on VPS
2. WHEN config.yaml does not exist on VPS THEN the Deployment_Script SHALL copy config.example.yaml as a template
3. WHEN the service restarts THEN the CLIProxyAPI SHALL load the preserved configuration
