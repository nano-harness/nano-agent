# TView UI 图片粘贴与文件输入集成

[English](./tview_multimodal_integration.md)

## 现状

TView UI 目前使用 `github.com/atotto/clipboard` 包进行基本的文本剪贴板操作。与 Bubbletea 直接处理粘贴事件不同，TView 的剪贴板集成更为简单，且以文本为中心。

## 集成方案

### 阶段 1：接入 Attachment Manager（建议立即实现）

1. **在 Integration 结构体中添加 attachment manager**，位于 `pkg/ui/tview/integration.go`：
```go
type Integration struct {
    // ... existing fields
    attachmentMgr  *attachment.Manager
    pendingImages  []llm.MultimodalImage
}
```

2. **在工厂方法中初始化**，位于 `pkg/ui/factory.go`：
```go
func (f *Factory) newTViewAdapter() *TViewAdapter {
    integration := tview.NewIntegration()
    attachMgr, err := attachment.NewManager(f.cfg.WorkingDir)
    if err != nil {
        logger.Warnf("Failed to create attachment manager: %v", err)
    } else {
        integration.SetAttachmentManager(attachMgr)
    }
    return &TViewAdapter{integration: integration}
}
```

3. **为 Integration 添加 SetAttachmentManager 方法**：
```go
func (i *Integration) SetAttachmentManager(mgr *attachment.Manager) {
    i.attachmentMgr = mgr
}
```

### 阶段 2：@file 引用支持（现在即可实现）

在 `integration.go` 的 `forwardOutboundInput()` 方法中，添加 @file 解析：

```go
func (i *Integration) forwardOutboundInput(input string) {
    trimmed := strings.TrimSpace(input)
    if trimmed == "" {
        return
    }

    // Parse @file references for images
    if i.attachmentMgr != nil {
        fileRefs := attachment.ParseFileReference(trimmed)
        for _, ref := range fileRefs {
            if attachment.IsImageFile(ref) {
                img, err := i.attachmentMgr.ToMultimodalImage(ref)
                if err == nil {
                    i.pendingImages = append(i.pendingImages, img)
                }
            }
        }
    }

    // Submit with images if any
    if len(i.pendingImages) > 0 {
        _ = i.SendOutbound(eventsource.Outbound{
            Kind: "submit",
            Text: trimmed,
            Images: i.pendingImages,
        })
        i.pendingImages = nil
    } else {
        _ = i.SendOutbound(eventsource.Outbound{Kind: "submit", Text: trimmed})
    }
}
```

### 阶段 3：剪贴板图片检测（未来增强）

TView 的剪贴板集成比 Bubbletea 更受限制。要添加图片粘贴检测：

**挑战**：TView 使用 `atotto/clipboard`，它只能处理文本。我们新的 `pkg/clipboard` 包可以检测图片，但 TView 的粘贴处理发生在 `tview.TextArea.SetClipboard()` 内部，它只接收文本。

**方案**：
1. 在更高的层级挂接 TView 的输入捕获
2. 在粘贴事件到达 TextArea 之前，检查剪贴板内容类型
3. 如果检测到图片，保存它并添加指示标记
4. 允许文本粘贴正常继续

**实现**：
```go
// In components.go, modify NewInputField:
func NewInputField() *InputField {
    input := tview.NewTextArea()
    // ... existing setup

    // Custom input capture to intercept paste
    input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
        if event.Key() == tcell.KeyCtrlV {
            // Check if clipboard has image
            contentType := clipboard.DetectContentType()
            if contentType == clipboard.ContentImage {
                // Handle image paste
                // Note: This requires passing attachmentMgr to InputField
                // which requires refactoring the component initialization
                return nil // Consume event
            }
        }
        return event
    })

    return inputField
}
```

## 挑战

1. **架构差异**：与 Bubbletea 相比，TView 的事件流程不同
2. **组件耦合**：InputField 无法直接访问 Integration 的 attachment manager
3. **剪贴板库**：`atotto/clipboard` 仅支持文本，需要并行使用 `pkg/clipboard`
4. **状态管理**：需要将待发送的图片从 InputField 传递到 Integration

## 推荐的推进路径

为了与 Bubbletea 完全对齐：

1. ✅ **已完成**：核心包（`clipboard`、`attachment`）已支持跨 UI
2. ✅ **已完成**：`eventsource.Outbound` 已支持 Images 字段
3. **阶段 2**：添加 @file 引用支持（工作量小，价值高）
4. **阶段 3**：添加剪贴板图片检测（中等工作量，需要重构）

## 状态

- **Bubbletea UI**：✅ 已完整集成图片粘贴与 @file 支持
- **TView UI**：⏸️ 等待阶段 2-3 的实现
- **核心基础设施**：✅ 已完成并经过测试
