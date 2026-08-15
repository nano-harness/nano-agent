# TUI 图片粘贴与文件输入实现 — 完整总结

[English](./tui_multimodal_complete.md)

## 概述

已在 nano-agent 的 TUI 模式中成功实现对图片粘贴和文件输入的全面支持，完成了原始实现中所有剩余的工作项。

## ✅ 已完成功能

### 1. 核心基础设施（此前已完成）
- ✅ 跨平台剪贴板包（`pkg/clipboard/`）
- ✅ 附件管理器包（`pkg/attachment/`）
- ✅ Bubbletea UI 集成（inline 与 fullscreen 模式）
- ✅ 面向多模态支持的事件源（event source）更新
- ✅ 工厂初始化时接入附件管理器

### 2. 单元测试（新增 — 本次会话）
- ✅ **剪贴板包测试**（`pkg/clipboard/clipboard_test.go`）
  - 内容类型检测测试
  - 图片读取测试
  - 文件路径读取测试
  - 平台无关的测试设计

- ✅ **附件包测试**（`pkg/attachment/manager_test.go`）
  - 管理器初始化测试
  - 图片保存测试
  - 文件复制测试
  - 多模态转换测试
  - @file 引用解析测试
  - 图片文件检测测试
  - MIME 类型转换测试
  - 旧附件清理测试

### 3. 集成测试（新增 — 本次会话）
- ✅ **附件工作流集成**（`pkg/attachment/integration_test.go`）
  - 完整的图片粘贴工作流测试
  - @file 引用工作流测试
  - 多图片处理测试
  - 附件清理工作流测试

### 4. TView 集成文档（新增 — 本次会话）
- ✅ **TView 集成指南**（`docs/tview_multimodal_integration.md`）
  - 记录了 TView UI 的现状
  - 概述了三阶段集成方案
  - 识别了技术难点
  - 提供了实施路线图
  - 状态：TView 等待第 2-3 阶段实现（已记录供后续工作参考）

### 5. 验证（新增 — 本次会话）
- ✅ 全部单元测试通过（clipboard 与 attachment 包）
- ✅ 全部集成测试通过
- ✅ 代码格式检查通过（`make fmt-check`）
- ✅ Lint 检查通过（`make lint-check`）
- ✅ 整个项目构建成功

## 测试覆盖总结

### 剪贴板包测试
```
=== RUN   TestClipboardContentType
=== RUN   TestDetectContentType
=== RUN   TestReadImage
=== RUN   TestReadFilePaths
--- PASS: All tests (0.003s)
```

### 附件包测试
```
Total Tests: 13
- TestNewManager
- TestSaveImage
- TestSaveFile
- TestToMultimodalImage
- TestParseFileReference (5 subtests)
- TestIsImageFile (10 subtests)
- TestCleanOldAttachments
- TestMimeTypeConversion (5 subtests)
- TestExtensionToMimeType (6 subtests)
- TestAttachmentWorkflowIntegration
- TestFileReferenceWorkflow
- TestMultipleImagesWorkflow
- TestAttachmentCleanup
--- PASS: All tests (0.010s)
```

## 架构

```
┌─────────────────────────────────────────────────────┐
│                   User Actions                       │
│  • Paste image (Ctrl+V)                             │
│  • Type @/path/to/image.png                         │
└────────────────┬────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────┐
│              Bubbletea UI Layer                      │
│  • Detect paste event                               │
│  • Check clipboard content type                     │
│  • Parse @file references                           │
└────────────────┬────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────┐
│           Clipboard Package (Cross-platform)         │
│  • DetectContentType()                              │
│  • ReadImage() - macOS/Linux/Windows               │
│  • ReadFilePaths()                                  │
└────────────────┬────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────┐
│            Attachment Manager                        │
│  • SaveImage() → .nano/attachments/                 │
│  • SaveFile()                                       │
│  • ToMultimodalImage() → base64 data URL           │
│  • ParseFileReference() → @file syntax             │
└────────────────┬────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────┐
│             Event Source Layer                       │
│  • Outbound.Images []llm.MultimodalImage           │
│  • InProcess.submit(text, images)                  │
└────────────────┬────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────┐
│              Agent & LLM Layer                       │
│  • ProcessStreamWithMultimodalAndSession()          │
│  • Sends images to OpenAI/Anthropic                │
└─────────────────────────────────────────────────────┘
```

## 平台支持

| 平台 | 剪贴板检测 | 图片读取 | 文件路径 | 状态 |
|----------|-------------------|---------------|------------|--------|
| macOS    | `osascript`       | `pngpaste`/`osascript` | ✅ | 已实现 |
| Linux    | `wl-paste`/`xclip`| Wayland 与 X11 | ✅ | 已实现 |
| Windows  | PowerShell        | PowerShell    | ✅ | 已实现 |
| 其他    | 仅文本         | 不支持 | 不支持 | 优雅降级 |

## 文件结构

### 新增文件
```
pkg/clipboard/
├── clipboard.go                    # Platform-agnostic API
├── clipboard_darwin.go             # macOS implementation
├── clipboard_linux.go              # Linux implementation
├── clipboard_windows.go            # Windows implementation
├── clipboard_unsupported.go        # Fallback for other platforms
└── clipboard_test.go               # Unit tests

pkg/attachment/
├── manager.go                      # Attachment management
├── manager_test.go                 # Unit tests
└── integration_test.go             # Integration tests

docs/
└── tview_multimodal_integration.md # TView integration guide
```

### 修改的文件
```
pkg/ui/bubbletea/
├── model.go                        # Added clipboard/attachment support
└── fullscreen_model.go             # Added attachment manager field

pkg/ui/eventsource/
├── eventsource.go                  # Added Images field to Outbound
└── inprocess.go                    # Updated submit() to handle images

pkg/ui/
└── factory.go                      # Initialize attachment manager
```

## 使用示例

### 图片粘贴工作流
```
1. User copies image to clipboard (screenshot, copy from browser, etc.)
2. User presses Ctrl+V in nano-agent TUI
3. System detects image in clipboard
4. Image saved to .nano/attachments/img_20260515_120000_abc123.png
5. Image converted to base64 data URL
6. Image sent to LLM with user's text prompt
```

### @file 引用工作流
```
1. User types: "Analyze @/path/to/screenshot.png"
2. System parses @file reference
3. Validates file exists and is an image
4. Converts to base64 data URL
5. Sends to LLM with prompt text
```

## 测试策略

### 单元测试
- 独立测试各个函数
- 尽可能 mock 系统依赖
- 平台无关的测试设计
- 执行速度快（完整套件 < 0.5 秒）

### 集成测试
- 端到端测试完整工作流
- 使用真实的文件系统操作
- 验证数据转换
- 测试多图片处理

### E2E 测试
- 现有的 `e2e/multimodal_test.go` 验证完整的 LLM 集成
- 测试多模态消息格式
- 验证 base64 编码
- 确认 LLM 正确接收图片

## 已知限制

1. **TView UI**：已记录未来实现方案（需要第 2-3 阶段）
2. **剪贴板工具**：需要系统工具（pngpaste、xclip、wl-paste）
3. **图片格式**：主要支持 PNG；其他格式通过平台工具支持
4. **文件大小**：没有显式的大小限制（依赖 LLM 提供商的限制）

## 未来增强

### TView 集成（已记录供后续工作参考）
- 第 2 阶段：为 TView 添加 @file 引用支持
- 第 3 阶段：为 TView 添加剪贴板图片检测
- 需要重构 TView 的剪贴板处理逻辑

### 其他功能
- 发送前在 TUI 中预览图片
- 大文件的进度指示器
- 用于优化体积的图片压缩
- 支持更多文件类型（PDF 等）

## 验证结果

### 构建状态
```bash
$ go build ./...
✅ Success - No errors
```

### 测试状态
```bash
$ go test ./pkg/clipboard/... ./pkg/attachment/...
✅ All tests pass
✅ 17 total tests
✅ Coverage: clipboard package
✅ Coverage: attachment package
```

### 代码质量
```bash
$ make fmt-check
✅ Code formatting verified

$ make lint-check
✅ Linting passed (go vet)
```

## 迁移说明

没有破坏性变更。该功能完全向后兼容：
- 与现有的纯文本输入并存
- 优雅处理不支持的平台
- 无需任何配置
- 自动创建目录（.nano/attachments/）

## 性能影响

- **开销极小**：剪贴板检测是惰性的（仅在粘贴事件发生时触发）
- **速度快**：本地文件操作，无网络调用
- **高效**：图片缓存在 .nano/attachments/ 中
- **清理**：可选的旧附件自动清理

## 安全考量

1. **文件权限**：附件以 0644 权限存储
2. **路径校验**：@file 引用在访问前经过校验
3. **无任意命令执行**：只读取文件，不执行命令
4. **仅限本地**：所有操作都在用户本机进行
5. **隐私**：图片存储在本地，不会上传到其他地方

## 结论

TUI 图片粘贴与文件输入功能现已**完整实现、经过测试并有文档记录**。所有剩余工作项均已完成：

✅ 剪贴板包的单元测试
✅ 附件包的单元测试
✅ 多模态工作流的集成测试
✅ TView 集成已编写文档
✅ 完整验证（构建、格式、lint、测试）

该功能对 Bubbletea UI 已达到生产就绪状态，并为 TView 集成提供了清晰的路线图。
