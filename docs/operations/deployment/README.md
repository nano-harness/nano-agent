# Nano Agent Deployment Guide

[中文](./README.zh-CN.md)

This document describes how to deploy the nano agent daemon mode, Cloud Gateway, and Connector on an AWS EC2 instance.

## Table of contents
1. [Quick start](#quick-start)
2. [Deployment scripts](#deployment-scripts)
3. [Cloud Gateway deployment](#cloud-gateway-deployment)
4. [GitHub Actions automation](#github-actions-automation)
5. [Secrets configuration](#secrets-configuration)

## Quick start

### Deploy the Daemon and Connector
Use the `unified-deploy.sh` script to deploy the Daemon and Connector in one step.

```bash
# Edit the EC2_HOST and PEM_FILE variables in the script
./deployment/unified-deploy.sh deploy
```

### Deploy the Cloud Gateway
Use the `deploy-gateway.sh` script to deploy the Gateway service.

```bash
./deployment/deploy-gateway.sh deploy
```

## Cloud Gateway deployment

The Gateway service listens on port `8081` by default.

**Deployment steps:**
1. Make sure the variables in `deployment/deploy-gateway.sh` are configured correctly.
2. Run `./deployment/deploy-gateway.sh deploy`.
3. Verify the service status: `curl http://<EC2_IP>:8081/console`.

## GitHub Actions automation

The project includes an automated deployment workflow at `.github/workflows/deploy-gateway.yml`.
Deployment is triggered automatically when the following paths change on the `main` branch:
* `cloud/cmd/gateway/**`
* `cloud/pkg/**`
* `deployment/deploy-gateway.sh`

## Secrets configuration

For GitHub Actions to work, configure the following secrets in the GitHub repository under Settings -> Secrets and variables -> Actions:

| Secret Name | Description | Example |
|-------------|-------------|---------|
| `EC2_HOST` | Public IP or domain of the EC2 instance | `ec2-xx-xx-xx-xx.compute.amazonaws.com` |
| `EC2_USER` | SSH login username | `ubuntu` |
| `SSH_PRIVATE_KEY` | SSH private key content (PEM format) | `-----BEGIN RSA PRIVATE KEY-----...` |
| `NANO_GATEWAY_TOKEN` | (Optional) Gateway auth token; a default is used if not set | `your-strong-token` |

**Note**: `SSH_PRIVATE_KEY` must contain the complete private key, including the BEGIN and END lines.

## Client connections

### Java client
See the `nano-agent/cloud/JAVA_CLIENT_PROTOCOL.md` document for Java client integration.

### Test client
You can use `cloud/cmd/test-client` to test the connection:
```bash
go run cloud/cmd/test-client/main.go -gateway ws://<EC2_IP>:8081
```
