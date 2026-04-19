# Nano Agent Daemon Deployment Guide

本文档介绍如何在AWS EC2实例上部署nano agent的daemon模式，以及如何通过配置文件连接到远程daemon。

## 快速开始

### 服务器部署
1. 配置部署脚本中的EC2地址和密钥路径
2. 运行 `./deploy-daemon.sh` 部署daemon到EC2
3. 确认daemon运行状态
## 新增功能：可配置的图像生成器模型

最新版本支持通过环境变量配置OpenRouter和Seedream图像生成器模型：
- `OPENROUTER_IMAGE_MODEL`: OpenRouter图像模型名称（默认：google/gemini-2.5-flash-image）
- `SEEDREAM_IMAGE_MODEL`: Seedream图像模型名称（默认：doubao-seedream-4-0-250828）

这使得用户可以灵活切换不同的图像生成模型，无需修改代码。


### 客户端连接
1. 复制客户端配置模板：`cp deployment/client-config.yaml .nano.yaml`
2. 编辑配置文件，设置daemon服务器地址和端口
3. 使用 `nano "your prompt"` 自动连接到daemon

详细步骤请参考下面的章节。

## 部署脚本说明

### `unified-deploy.sh` - 统一部署脚本

这是统一部署脚本，整合了所有部署、监控、测试和故障恢复功能。

**主要功能：**
- 完整部署（构建+传输+配置+启动）
- 服务管理（启动/停止/重启）
- 健康检查和状态监控
- 故障诊断和自动修复
- 日志查看和分析
- SystemD配置修复

**支持的命令：**
```bash
./unified-deploy.sh deploy    # 完整部署
./unified-deploy.sh restart   # 重启服务
./unified-deploy.sh start     # 启动服务
./unified-deploy.sh stop      # 停止服务
./unified-deploy.sh status    # 检查状态
./unified-deploy.sh test      # 运行健康检查
./unified-deploy.sh logs      # 查看日志
./unified-deploy.sh fix       # 修复SystemD配置
./unified-deploy.sh monitor   # 启动监控模式
```

## 使用前准备

### 1. 修改脚本配置

在使用脚本前，需要修改以下变量：

```bash
# 在 deploy-daemon.sh 和 update-daemon.sh 中修改这些变量
EC2_HOST="your-ec2-instance.compute.amazonaws.com"  # 你的EC2实例地址
PEM_FILE="~/Downloads/your-key.pem"                # 你的PEM密钥文件路径
```

### 2. 配置API密钥

在 `deploy-daemon.sh` 中，找到配置文件部分并设置你的LLM API密钥：

```yaml
api_key: "your-llm-api-key"  # 替换为你的实际API密钥
```

### 3. 确保EC2实例配置

- EC2实例运行Ubuntu系统
- 安全组开放8080端口（或你配置的其他端口）
- 有足够的磁盘空间和内存

## 部署步骤

1. 给脚本添加执行权限：
```bash
chmod +x unified-deploy.sh
```

2. 运行完整部署：
```bash
./unified-deploy.sh deploy
```

3. 检查部署状态：
```bash
./unified-deploy.sh status
```

4. 运行健康检查：
```bash
./unified-deploy.sh test
```

## Daemon管理命令

### 推荐方式：使用统一脚本

```bash
# 查看daemon状态（包含详细健康检查）
./unified-deploy.sh status

# 查看daemon日志
./unified-deploy.sh logs

# 停止daemon
./unified-deploy.sh stop

# 启动daemon
./unified-deploy.sh start

# 重启daemon
./unified-deploy.sh restart

# 运行完整测试
./unified-deploy.sh test

# 修复SystemD配置问题
./unified-deploy.sh fix

# 启动监控模式
./unified-deploy.sh monitor
```

### 备选方式：直接使用nano命令

部署完成后，可以在EC2实例上使用以下命令管理daemon：

```bash
# 查看daemon状态
nano daemon status

# 查看daemon日志
nano daemon logs

# 停止daemon
nano daemon stop

# 启动daemon
nano daemon start

# 重启daemon
nano daemon restart
```

## 客户端使用

### 配置文件优先级

nano使用以下优先级顺序加载配置文件：
1. 命令行指定的配置文件（`--config` 参数）
2. 项目目录下的 `.nano.yaml`
3. 全局配置文件 `~/.config/nano/config.yaml`
4. 环境变量（最高优先级，覆盖文件配置）

### 方式一：通过配置文件连接到远程daemon（推荐）

1. **创建客户端配置文件**：
```bash
# 复制客户端配置模板到全局配置目录
cp deployment/client-config.yaml ~/.config/nano/config.yaml

# 或者复制到项目目录（优先级更高）
cp deployment/client-config.yaml .nano.yaml

# 或者使用自定义配置文件路径
cp deployment/client-config.yaml /path/to/my-config.yaml
```

2. **编辑配置文件**，修改daemon连接信息：
```yaml
# 在配置文件中设置daemon连接信息
daemon:
  port: 8080                           # 远程daemon的端口
  host: "your-ec2-instance.com"        # 替换为你的EC2实例地址
  api_key: "nano-agent-9527!"          # 如果daemon设置了认证密钥
```

3. **使用daemon模式**：
```bash
# 自动检测并使用daemon（如果daemon正在运行）
nano "your prompt here"

# 强制使用daemon模式
nano --daemon "your prompt here"
nano -d "your prompt here"

# 使用自定义配置文件
nano --config /path/to/my-config.yaml "your prompt here"
nano -c /path/to/my-config.yaml "your prompt here"

# 设置超时时间（默认300秒）
nano --daemon --timeout 600 "your prompt here"

# 强制使用TUI模式（即使daemon正在运行）
nano --tui "your prompt here"
nano -t "your prompt here"
```

### 方式二：使用client子命令

#### 执行命令
```bash
# 通过daemon执行命令
nano client exec "your prompt here"

# 设置超时时间
nano client exec --timeout 600 "your prompt here"
```

#### 查看daemon状态
```bash
# 查看daemon健康状态和基本信息
nano client status
```

#### MCP管理
```bash
# 查看MCP状态
nano client mcp status

# 列出可用的MCP工具
nano client mcp tools

# 获取MCP诊断信息
nano client mcp diagnostics
```

#### 内存管理
```bash
# 列出所有内存条目
nano client memory list

# 保存内存条目
nano client memory save "key" "content"

# 获取内存条目
nano client memory get "key"

# 删除内存条目
nano client memory delete "key"
```

### 方式三：通过环境变量配置

```bash
# 设置daemon连接相关环境变量
export NANO_DAEMON_HOST="your-ec2-instance.com"
export NANO_DAEMON_PORT="8080"
export NANO_DAEMON_API_KEY="nano-agent-9527!"

# 设置其他配置
export NANO_API_KEY="your-llm-api-key"
export NANO_BASE_URL="https://api.openai.com/v1"
export NANO_MODEL="gpt-4"
export NANO_VERBOSE="true"
# 图像生成器配置（新增）
export OPENROUTER_IMAGE_MODEL="google/gemini-2.5-flash-image"  # OpenRouter图像模型
export SEEDREAM_IMAGE_MODEL="doubao-seedream-4-0-250828"      # Seedream图像模型
export IMAGE_API_KEY: [REDACTED]                # OpenRouter API密钥
export SEEDREAM_API_KEY: [REDACTED]               # Seedream API密钥


# 使用daemon模式
nano --daemon "your prompt here"
```

### 配置文件查看

```bash
# 查看配置文件加载顺序和状态
nano config locations

# 使用自定义配置文件查看
nano --config /path/to/config.yaml config locations
```

### 配置文件位置

**服务器端（EC2实例）**：
- 配置文件：`~/.config/nano/config.yaml`
- PID文件：`~/.nano/daemon.pid`
- 日志文件：`~/.nano/daemon.log`

**客户端（本地机器）**：
- 全局配置：`~/.config/nano/config.yaml`
- 项目配置：`.nano.yaml`（项目根目录）
- 配置优先级：项目配置 > 全局配置 > 环境变量

### 客户端配置说明

**重要：daemon客户端配置限制**

daemon客户端只是一个HTTP客户端，所有AI处理都在daemon服务器端进行。因此，客户端配置文件中的大部分配置项都**不会生效**：

❌ **无效的配置项**（在daemon客户端模式下被忽略）：
- LLM配置（`api_key`, `base_url`, `model`等）
- 内存系统配置（`memory`部分）
- 工具配置（`enabled_tools`, `disabled_tools`等）
- MCP配置（`mcp`部分）
- 网络搜索API密钥（`web_search_api_keys`）
- 上下文管理配置（`context`部分）

✅ **有效的配置项**：
```yaml
# 客户端超时设置
response_timeout: 300s  # 等待daemon响应的超时时间
http_timeout: 60s       # HTTP请求超时时间

# 安全设置（客户端本地行为）
confirm_destructive: false

# Daemon连接配置（最重要）
daemon:
  host: "your-ec2-instance.com"    # 远程daemon服务器地址
  port: 8080                       # daemon监听端口
  api_key: "your-api-key"          # 可选：认证密钥
  tls_cert_file: ""                # 可选：HTTPS证书
  tls_key_file: ""                 # 可选：HTTPS私钥
```

**重要提示**：
- 客户端**只需要配置`daemon`部分**，其他配置由服务器端处理
- 如果daemon服务器设置了`api_key`认证，客户端必须配置相同的密钥
- 支持HTTPS连接，需要配置相应的证书文件

### 客户端配置最佳实践

1. **项目级配置**（推荐）：
```bash
# 在项目根目录创建配置文件
cp deployment/client-config.yaml .nano.yaml
# 编辑配置，只保留daemon部分
```

2. **全局配置**：
```bash
# 创建全局配置目录
mkdir -p ~/.config/nano
# 复制并编辑配置文件
cp deployment/client-config.yaml ~/.config/nano/config.yaml
```

3. **配置验证**：
```bash
# 检查daemon连接
nano client status

# 查看配置路径
nano config paths
```

4. **常见配置示例**：

**本地开发环境**：
```yaml
daemon:
  host: "127.0.0.1"
  port: 8080
  api_key: ""
```

**连接远程服务器**：
```yaml
daemon:
  host: "your-server.example.com"
  port: 8080
  api_key: "your-secure-api-key"
```

**HTTPS连接**：
```yaml
daemon:
  host: "your-server.example.com"
  port: 8443
  api_key: "your-secure-api-key"
  tls_cert_file: "/path/to/client.crt"
  tls_key_file: "/path/to/client.key"
```

## 安全注意事项

1. **API密钥安全**：确保不要将API密钥提交到版本控制系统
2. **网络安全**：考虑使用VPN或限制访问IP范围
3. **认证**：可以在daemon配置中设置API密钥进行认证
4. **HTTPS**：生产环境建议配置TLS证书

## 故障排除

### 使用统一脚本进行故障排除

1. **快速诊断**
   ```bash
   # 运行完整测试，包含所有健康检查
   ./unified-deploy.sh test

   # 查看详细状态信息
   ./unified-deploy.sh status
   ```

2. **自动修复常见问题**
   ```bash
   # 修复SystemD配置问题
   ./unified-deploy.sh fix

   # 重启服务解决临时问题
   ./unified-deploy.sh restart
   ```

3. **查看详细日志**
   ```bash
   # 查看daemon日志
   ./unified-deploy.sh logs

   # 启动监控模式，实时查看状态
   ./unified-deploy.sh monitor
   ```

### 常见问题

**服务器部署问题**：
1. **连接被拒绝**：检查EC2安全组是否开放了相应端口
2. **Go未找到**：脚本会自动安装Go，如果失败请手动安装
3. **权限问题**：确保PEM文件权限正确（600）
4. **端口冲突**：如果8080端口被占用，修改配置中的端口号
5. **SystemD服务问题**：使用 `./unified-deploy.sh fix` 自动修复配置

**客户端连接问题**：
1. **无法连接到daemon**：
   ```bash
   # 检查daemon状态
   nano client status
   # 检查配置
   nano config paths
   ```

2. **认证失败**：
   - 确保客户端和服务器的`api_key`一致
   - 检查配置文件中的`daemon.api_key`设置

3. **配置文件未生效**：
   ```bash
   # 检查配置加载顺序
   nano config paths
   # 确保配置文件在正确位置
   ```

4. **网络连接问题**：
   - 检查防火墙设置
   - 确认服务器地址和端口正确
   - 测试网络连通性：`telnet your-server 8080`
   - 使用 `./unified-deploy.sh status` 验证daemon运行状态

5. **HTTPS证书问题**：
   - 确保证书文件路径正确
   - 检查证书有效性
   - 验证证书与服务器域名匹配

### 查看日志

```bash
# 在EC2实例上查看daemon日志
nano daemon logs

# 或直接查看日志文件
tail -f ~/.nano/daemon.log
```

## 高级配置

### 自定义端口和主机

在 `deploy-daemon.sh` 中修改：
```bash
DAEMON_PORT="8080"     # 修改为你想要的端口
DAEMON_HOST="0.0.0.0"  # 0.0.0.0 监听所有接口，127.0.0.1 仅本地
```

### 启用HTTPS

在配置文件中添加TLS证书配置：
```yaml
daemon:
  tls_cert_file: "/path/to/cert.pem"
  tls_key_file: "/path/to/key.pem"
```

### 系统服务配置

如需将daemon配置为系统服务，可以创建systemd服务文件：

```bash
# 创建服务文件
sudo nano /etc/systemd/system/nano-daemon.service
```

服务文件内容：
```ini
[Unit]
Description=Nano Agent Daemon
After=network.target

[Service]
Type=forking
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/usr/local/bin/nano daemon start
ExecStop=/usr/local/bin/nano daemon stop
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

启用服务：
```bash
sudo systemctl enable nano-daemon
sudo systemctl start nano-daemon
```
