// Package event implements event dispatching and handling mechanisms
package event

import (
	"context"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// CallbackFunc 定义回调函数类型
type CallbackFunc func()

// CallbackWithDataFunc 定义带数据的回调函数类型
type CallbackWithDataFunc[T any] func(T)

// CallbackTask 表示一个回调任务
type CallbackTask struct {
	ID       string
	Callback CallbackFunc
	Delay    time.Duration
	Source   string
	Metadata map[string]interface{}
}

// EventDispatcher 统一的事件派发器
// 用于异步处理回调函数，避免在持锁期间调用回调
type EventDispatcher struct { //nolint:revive
	mu          sync.RWMutex
	workerCount int
	taskQueue   chan CallbackTask
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	running     bool

	// 统计信息
	totalTasks     int64
	completedTasks int64
	failedTasks    int64
}

// NewEventDispatcher 创建新的事件派发器
func NewEventDispatcher(workerCount int) *EventDispatcher {
	if workerCount <= 0 {
		workerCount = 4 // 默认4个工作协程
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &EventDispatcher{
		workerCount: workerCount,
		taskQueue:   make(chan CallbackTask, 1000), // 缓冲队列
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start 启动事件派发器
func (ed *EventDispatcher) Start() {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	if ed.running {
		return
	}

	ed.running = true

	// 启动工作协程
	for i := 0; i < ed.workerCount; i++ {
		ed.wg.Add(1)
		go ed.worker(i)
	}

	logger.Info("事件派发器已启动，工作协程数: %d", ed.workerCount)
}

// Stop 停止事件派发器
func (ed *EventDispatcher) Stop() {
	ed.mu.Lock()
	if !ed.running {
		ed.mu.Unlock()
		return
	}
	ed.running = false
	ed.mu.Unlock()

	// 取消上下文
	ed.cancel()

	// 关闭任务队列
	close(ed.taskQueue)

	// 等待所有工作协程完成
	ed.wg.Wait()

	logger.Info("事件派发器已停止")
}

// worker 工作协程
func (ed *EventDispatcher) worker(id int) { //nolint:revive
	defer ed.wg.Done()

	for {
		select {
		case <-ed.ctx.Done():
			return
		case task, ok := <-ed.taskQueue:
			if !ok {
				return
			}
			ed.executeTask(task)
		}
	}
}

// executeTask 执行回调任务
func (ed *EventDispatcher) executeTask(task CallbackTask) {
	defer func() {
		if r := recover(); r != nil {
			ed.mu.Lock()
			ed.failedTasks++
			ed.mu.Unlock()
			logger.Error("回调任务执行失败 [%s]: %v", task.ID, r)
		}
	}()

	// 如果有延迟，先等待
	if task.Delay > 0 {
		select {
		case <-ed.ctx.Done():
			return
		case <-time.After(task.Delay):
		}
	}

	// 执行回调
	start := time.Now()
	task.Callback()
	duration := time.Since(start)

	// 更新统计信息
	ed.mu.Lock()
	ed.completedTasks++
	ed.mu.Unlock()

	logger.Debug("回调任务完成 [%s] 来源: %s 耗时: %v", task.ID, task.Source, duration)
}

// Dispatch 派发回调任务
func (ed *EventDispatcher) Dispatch(id, source string, callback CallbackFunc) bool {
	return ed.DispatchWithDelay(id, source, callback, 0, nil)
}

// DispatchWithDelay 派发带延迟的回调任务
func (ed *EventDispatcher) DispatchWithDelay(id, source string, callback CallbackFunc, delay time.Duration, metadata map[string]interface{}) bool {
	ed.mu.RLock()
	running := ed.running
	ed.mu.RUnlock()

	if !running {
		logger.Warn("事件派发器未运行，忽略回调任务: %s", id)
		return false
	}

	task := CallbackTask{
		ID:       id,
		Callback: callback,
		Delay:    delay,
		Source:   source,
		Metadata: metadata,
	}

	select {
	case ed.taskQueue <- task:
		ed.mu.Lock()
		ed.totalTasks++
		ed.mu.Unlock()
		return true
	case <-ed.ctx.Done():
		return false
	default:
		logger.Warn("事件派发器队列已满，丢弃回调任务: %s", id)
		return false
	}
}

// DispatchWithData 派发带数据的回调任务
func DispatchWithData[T any](ed *EventDispatcher, id, source string, callback CallbackWithDataFunc[T], data T) bool {
	return ed.Dispatch(id, source, func() {
		callback(data)
	})
}

// GetStats 获取统计信息
func (ed *EventDispatcher) GetStats() (total, completed, failed int64, queueSize int) {
	ed.mu.RLock()
	defer ed.mu.RUnlock()
	return ed.totalTasks, ed.completedTasks, ed.failedTasks, len(ed.taskQueue)
}

// IsRunning 检查派发器是否正在运行
func (ed *EventDispatcher) IsRunning() bool {
	ed.mu.RLock()
	defer ed.mu.RUnlock()
	return ed.running
}

// 全局事件派发器实例
var (
	globalDispatcher *EventDispatcher
	globalOnce       sync.Once
)

// GetGlobalDispatcher 获取全局事件派发器
func GetGlobalDispatcher() *EventDispatcher {
	globalOnce.Do(func() {
		globalDispatcher = NewEventDispatcher(4)
		globalDispatcher.Start()
	})
	return globalDispatcher
}

// StopGlobalDispatcher 停止全局事件派发器
func StopGlobalDispatcher() {
	if globalDispatcher != nil {
		globalDispatcher.Stop()
	}
}
