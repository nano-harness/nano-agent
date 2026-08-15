# TView UI Integration for Image Paste & File Input

[中文](./tview_multimodal_integration.zh-CN.md)

## Current State

TView UI currently uses the `github.com/atotto/clipboard` package for basic text clipboard operations. Unlike Bubbletea which has direct event handling for paste events, TView's clipboard integration is simpler and text-focused.

## Integration Approach

### Phase 1: Setup Attachment Manager (Recommended for immediate implementation)

1. **Add attachment manager to Integration struct** in `pkg/ui/tview/integration.go`:
```go
type Integration struct {
    // ... existing fields
    attachmentMgr  *attachment.Manager
    pendingImages  []llm.MultimodalImage
}
```

2. **Initialize in factory** `pkg/ui/factory.go`:
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

3. **Add SetAttachmentManager method** to Integration:
```go
func (i *Integration) SetAttachmentManager(mgr *attachment.Manager) {
    i.attachmentMgr = mgr
}
```

### Phase 2: @file Reference Support (Can be implemented now)

In `forwardOutboundInput()` method of `integration.go`, add @file parsing:

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

### Phase 3: Clipboard Image Detection (Future Enhancement)

TView's clipboard integration is more limited than Bubbletea. To add image paste detection:

**Challenge**: TView uses `atotto/clipboard` which only handles text. Our new `pkg/clipboard` package can detect images, but TView's paste handling happens within `tview.TextArea.SetClipboard()` which only receives text.

**Approach**:
1. Hook into TView's input capture at a higher level
2. Before paste events reach the TextArea, check clipboard content type
3. If image detected, save it and add indicator
4. Allow text paste to continue normally

**Implementation**:
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

## Challenges

1. **Architecture Difference**: TView has a different event flow compared to Bubbletea
2. **Component Coupling**: InputField doesn't have direct access to Integration's attachment manager
3. **Clipboard Library**: `atotto/clipboard` is text-only, requiring parallel use of `pkg/clipboard`
4. **State Management**: Need to propagate pending images from InputField to Integration

## Recommended Path Forward

For complete parity with Bubbletea:

1. ✅ **Already Done**: Core packages (`clipboard`, `attachment`) are cross-UI
2. ✅ **Already Done**: `eventsource.Outbound` supports Images field
3. **Phase 2**: Add @file reference support (low effort, high value)
4. **Phase 3**: Add clipboard image detection (medium effort, requires refactoring)

## Status

- **Bubbletea UI**: ✅ Fully integrated with image paste and @file support
- **TView UI**: ⏸️ Awaiting Phase 2-3 implementation
- **Core Infrastructure**: ✅ Complete and tested
