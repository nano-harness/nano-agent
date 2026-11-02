# Nano Agent 部署指南

[English](./README.md)

本文档介绍如何在AWS EC2实例上部署nano agent的daemon模式、Cloud Gateway和Connector。

## 目录
1. [快速开始](#快速开始)
2. [部署脚本说明](#部署脚本说明)
3. [Cloud Gateway部署](#cloud-gateway部署)
4. [GitHub Actions自动化](#github-actions自动化)
5. [Secrets配置](#secrets配置)

## 快速开始

### 部署 Daemon 和 Connector
使用 `unified-deploy.sh` 脚本可以一键部署 Daemon 和 Connector。

```bash
# 修改脚本中的 EC2_HOST 和 PEM_FILE 变量
./deployment/unified-deploy.sh deploy
```

### 部署 Cloud Gateway
使用 `deploy-gateway.sh` 脚本部署 Gateway 服务。

```bash
./deployment/deploy-gateway.sh deploy
```

## Cloud Gateway部署

Gateway服务默认监听 `8081` 端口。

**部署步骤：**
1. 确保 `deployment/deploy-gateway.sh` 中的变量配置正确。
2. 运行 `./deployment/deploy-gateway.sh deploy`。
3. 验证服务状态：`curl http://<EC2_IP>:8081/console`。

## GitHub Actions自动化

项目包含自动化部署流程，位于 `.github/workflows/deploy-gateway.yml`。
当 `main` 分支的以下路径发生变化时，会自动触发部署：
* `cloud/cmd/gateway/**`
* `cloud/pkg/**`
* `deployment/deploy-gateway.sh`

## Secrets配置

为了使 GitHub Actions 正常工作，需要在 GitHub 仓库的 Settings -> Secrets and variables -> Actions 中配置以下 Secrets：

| Secret Name | Description | Example |
|-------------|-------------|---------|
| `EC2_HOST` | EC2实例的公网IP或域名 | `ec2-xx-xx-xx-xx.compute.amazonaws.com` |
| `EC2_USER` | SSH登录用户名 | `ubuntu` |
| `SSH_PRIVATE_KEY` | SSH私钥内容 (PEM格式) | `-----BEGIN RSA PRIVATE KEY-----...` |
| `NANO_GATEWAY_TOKEN` | (可选) Gateway鉴权Token，不配则使用默认值 | `your-strong-token` |

**注意**：`SSH_PRIVATE_KEY` 必须包含完整的私钥内容，包括 BEGIN 和 END 行。

## 客户端连接

### Java客户端
参考 `nano-agent/cloud/JAVA_CLIENT_PROTOCOL.md` 文档集成 Java 客户端。

### 测试客户端
可以使用 `cloud/cmd/test-client` 进行连接测试：
```bash
go run cloud/cmd/test-client/main.go -gateway ws://<EC2_IP>:8081
```
