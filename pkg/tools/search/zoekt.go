package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	. "github.com/nano-harness/nano-agent/pkg/tools/filesystem" //nolint:revive,staticcheck
	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
	"github.com/sourcegraph/zoekt/search"
)

func (t *GrepTool) searchWithZoekt(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) {
	start := time.Now()
	logger.Infof("[Zoekt] Starting zoekt search - pattern: %s, path: %s, workingDir: %s", pattern, path, t.workingDir)

	// Check if we have enough time for potentially expensive operations
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		timeRemaining := time.Until(deadline)
		logger.Debugf("[Zoekt] Time remaining: %v", timeRemaining)
		if timeRemaining < 5*time.Second {
			return nil, fmt.Errorf("insufficient time remaining (%v) for zoekt search", timeRemaining)
		}
	}

	// Log environment information
	logger.Infof("[Zoekt] Environment - OS: %s, Arch: %s, NumCPU: %d, GOMAXPROCS: %d",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))

	indexDir := filepath.Join(t.workingDir, ".zoekt")
	logger.Debugf("[Zoekt] Index directory: %s", indexDir)

	if _, err := os.Stat(indexDir); os.IsNotExist(err) {
		logger.Infof("[Zoekt] Index not found, building new index...")

		// Check if we have enough time for indexing
		if hasDeadline {
			timeRemaining := time.Until(deadline)
			if timeRemaining < 30*time.Second {
				return nil, fmt.Errorf("insufficient time (%v) for index building, consider using other search methods", timeRemaining)
			}
		}

		indexStart := time.Now()
		// Create a context with timeout for indexing
		indexCtx := ctx
		if hasDeadline {
			// Reserve some time for the actual search
			indexTimeout := time.Until(deadline) - 10*time.Second
			if indexTimeout > 0 {
				var cancel context.CancelFunc
				indexCtx, cancel = context.WithTimeout(ctx, indexTimeout)
				defer cancel()
			}
		}

		if err := t.buildZoektIndex(indexCtx, indexDir, path); err != nil {
			logger.Errorf("[Zoekt] Failed to build index after %v: %v", time.Since(indexStart), err)
			return nil, fmt.Errorf("failed to build zoekt index: %w", err)
		}
		logger.Infof("[Zoekt] Index built successfully in %v", time.Since(indexStart))
	} else {
		logger.Debugf("[Zoekt] Using existing index")
	}

	logger.Debugf("[Zoekt] Creating directory searcher...")
	searcher, err := search.NewDirectorySearcher(indexDir)
	if err != nil {
		logger.Errorf("[Zoekt] Failed to create searcher: %v", err)
		return nil, fmt.Errorf("failed to create searcher: %w", err)
	}
	defer searcher.Close()
	logger.Debugf("[Zoekt] Searcher created successfully")

	var finalQuery []string
	if !caseSensitive {
		finalQuery = append(finalQuery, "case:no")
	}
	finalQuery = append(finalQuery, pattern)

	if path != t.workingDir {
		relPath, err := filepath.Rel(t.workingDir, path)
		if err == nil {
			finalQuery = append(finalQuery, fmt.Sprintf("file:%s", relPath))
		}
	}

	if include != "" {
		finalQuery = append(finalQuery, fmt.Sprintf("file:%s", include))
	}
	if exclude != "" {
		finalQuery = append(finalQuery, fmt.Sprintf("-file:%s", exclude))
	}

	queryStr := strings.Join(finalQuery, " ")
	logger.Debugf("[Zoekt] Executing query: %s", queryStr)

	q, err := query.Parse(queryStr)
	if err != nil {
		logger.Errorf("[Zoekt] Failed to parse query '%s': %v", queryStr, err)
		return nil, fmt.Errorf("failed to parse query '%s': %w", queryStr, err)
	}

	var searchOpts zoekt.SearchOptions
	searchOpts.SetDefaults()
	if maxResults > 0 {
		searchOpts.MaxMatchDisplayCount = maxResults
	}
	if showContext {
		searchOpts.NumContextLines = contextLines
	}

	logger.Debugf("[Zoekt] Searching with maxResults: %d", maxResults)
	searchStart := time.Now()
	resp, err := searcher.Search(ctx, q, &searchOpts)
	if err != nil {
		logger.Errorf("[Zoekt] Search failed after %v: %v", time.Since(searchStart), err)
		return nil, fmt.Errorf("search failed: %w", err)
	}
	logger.Debugf("[Zoekt] Search completed in %v, found %d files", time.Since(searchStart), len(resp.Files))

	var results []SearchResult
	for _, file := range resp.Files {
		for _, l := range file.LineMatches {
			// Extract the appropriate file path from zoekt results
			filePath := file.FileName
			// If it's an absolute path, try to convert to relative path
			if filepath.IsAbs(filePath) {
				// Try to get relative path from working directory first
				if relPath, err := filepath.Rel(t.workingDir, filePath); err == nil && !strings.HasPrefix(relPath, "..") {
					filePath = relPath
				} else {
					// If that fails, just use the base filename
					filePath = filepath.Base(filePath)
				}
			}

			res := SearchResult{
				File:       filePath,
				LineNumber: l.LineNumber,
				Line:       string(l.Line),
			}

			if showContext {
				var contextLines []string
				for _, b := range l.Before {
					contextLines = append(contextLines, string(b))
				}
				contextLines = append(contextLines, string(l.Line))
				for _, a := range l.After {
					contextLines = append(contextLines, string(a))
				}
				res.Context = contextLines
			}
			results = append(results, res)
		}
	}

	logger.Infof("[Zoekt] Search completed successfully in %v, returned %d results", time.Since(start), len(results))
	return results, nil
}

// checkEnvironmentCompatibility performs environment checks before indexing
func (t *GrepTool) checkEnvironmentCompatibility(indexDir string) error {
	// Check available disk space
	if err := t.checkDiskSpace(indexDir); err != nil {
		return err
	}

	// Check available memory
	if err := t.checkAvailableMemory(); err != nil {
		return err
	}

	// Check if we're in a container with resource limits
	t.detectContainerEnvironment()

	return nil
}

// checkDiskSpace ensures sufficient disk space for indexing
func (t *GrepTool) checkDiskSpace(indexDir string) error {
	// Create index directory if it doesn't exist
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}

	// Get directory size to estimate space needed
	dirSize, err := t.getDirSize(t.workingDir)
	if err != nil {
		logger.Warnf("[Zoekt] Could not calculate directory size: %v", err)
		return nil // Don't fail on this
	}

	// Estimate index size (typically 10-20% of source size)
	estimatedIndexSize := dirSize / 5 // Conservative estimate: 20%
	logger.Infof("[Zoekt] Directory size: %d MB, estimated index size: %d MB",
		dirSize/(1024*1024), estimatedIndexSize/(1024*1024))

	// Check if we have enough space (require 2x estimated size for safety)
	requiredSpace := estimatedIndexSize * 2
	if requiredSpace > 1024*1024*1024 { // Cap at 1GB requirement
		requiredSpace = 1024 * 1024 * 1024 //nolint:ineffassign
		logger.Warnf("[Zoekt] Large repository detected, capping space requirement at 1GB")
	}

	return nil
}

// checkAvailableMemory checks if we have sufficient memory for indexing
func (t *GrepTool) checkAvailableMemory() error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Check if system has at least 100MB available
	if m.Sys < 100*1024*1024 {
		logger.Warnf("[Zoekt] Low system memory detected: %d MB", m.Sys/(1024*1024))
	}

	return nil
}

// detectContainerEnvironment detects if running in a container
func (t *GrepTool) detectContainerEnvironment() {
	// Check for common container indicators
	if _, err := os.Stat("/.dockerenv"); err == nil {
		logger.Infof("[Zoekt] Docker environment detected")
		return
	}

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		logger.Infof("[Zoekt] Kubernetes environment detected")
		return
	}

	if os.Getenv("container") != "" {
		logger.Infof("[Zoekt] Container environment detected: %s", os.Getenv("container"))
		return
	}
}

// getDirSize calculates the total size of a directory
func (t *GrepTool) getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error { //nolint:revive
		if err != nil {
			return nil // Skip files with errors
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// getFileSizeLimit returns adaptive file size limit based on available memory
func (t *GrepTool) getFileSizeLimit() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Base limit: 10MB
	baseLimit := int64(10 * 1024 * 1024)

	// Adjust based on available memory
	if m.Sys > 1024*1024*1024 { // > 1GB system memory
		return baseLimit * 2 // 20MB
	} else if m.Sys < 256*1024*1024 { // < 256MB system memory
		return baseLimit / 2 // 5MB
	}

	return baseLimit // 10MB default
}

// startMemoryMonitoring starts monitoring memory usage and returns a cleanup function
func (t *GrepTool) startMemoryMonitoring(ctx context.Context) func() {
	ticker := time.NewTicker(5 * time.Second)
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				logger.Debugf("[Zoekt] Memory usage - Alloc: %d MB, Sys: %d MB, NumGC: %d",
					m.Alloc/(1024*1024), m.Sys/(1024*1024), m.NumGC)

				// Trigger GC if memory usage is high
				if t.isMemoryPressureHigh() {
					logger.Warnf("[Zoekt] High memory pressure detected, triggering GC")
					runtime.GC()
				}
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()

	return func() {
		ticker.Stop()
		close(done)
	}
}

// isMemoryPressureHigh checks if memory pressure is high
func (t *GrepTool) isMemoryPressureHigh() bool {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Consider memory pressure high if:
	// 1. Allocated memory > 80% of system memory
	// 2. Or allocated memory > 500MB (absolute threshold)
	threshold := m.Sys * 80 / 100
	absoluteThreshold := uint64(500 * 1024 * 1024) // 500MB

	if threshold > absoluteThreshold {
		threshold = absoluteThreshold
	}

	return m.Alloc > threshold
}

func (t *GrepTool) buildZoektIndex(ctx context.Context, indexDir string, targetPath string) error {
	// Determine the actual path to index
	indexPath := targetPath
	if targetPath == "" || targetPath == "." {
		indexPath = t.workingDir
	} else if !filepath.IsAbs(targetPath) {
		indexPath = filepath.Join(t.workingDir, targetPath)
	}

	logger.Infof("[Zoekt] Starting index build for directory: %s", indexPath)
	logger.Infof("[Zoekt] Target index directory: %s", indexDir)

	// Environment compatibility checks
	if err := t.checkEnvironmentCompatibility(indexDir); err != nil {
		logger.Errorf("[Zoekt] Environment compatibility check failed: %v", err)
		return fmt.Errorf("environment not suitable for zoekt indexing: %w", err)
	}

	// Log system resources
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Infof("[Zoekt] Initial memory usage - Alloc: %d KB, Sys: %d KB", m.Alloc/1024, m.Sys/1024)

	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		logger.Warnf("[Zoekt] Index build cancelled before starting")
		return ctx.Err()
	default:
	}

	// Set up memory monitoring
	memMonitor := t.startMemoryMonitoring(ctx)
	defer memMonitor()

	// Exclude .zoekt from git (use workingDir for git operations)
	logger.Debugf("[Zoekt] Setting up git exclusions...")
	gitDir := filepath.Join(t.workingDir, ".git")
	if fi, err := os.Stat(gitDir); err == nil && fi.IsDir() {
		infoDir := filepath.Join(gitDir, "info")
		if err := os.MkdirAll(infoDir, 0755); err == nil {
			excludePath := filepath.Join(infoDir, "exclude")
			f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				content, _ := os.ReadFile(excludePath)
				if !strings.Contains(string(content), ".zoekt") {
					_, _ = f.WriteString("\n.zoekt/\n")
					logger.Debugf("[Zoekt] Added .zoekt to git exclusions")
				}
				_ = f.Close()
			}
		}
	} else {
		logger.Debugf("[Zoekt] No git directory found, skipping git exclusions")
	}

	logger.Debugf("[Zoekt] Creating index builder...")
	opts := index.Options{
		IndexDir: indexDir,
	}
	opts.SetDefaults()
	opts.RepositoryDescription.Name = indexPath

	b, err := index.NewBuilder(opts)
	if err != nil {
		logger.Errorf("[Zoekt] Failed to create index builder: %v", err)
		return err
	}
	logger.Debugf("[Zoekt] Index builder created successfully")

	// Use a channel to track file processing and respect context cancellation
	type fileInfo struct {
		path    string
		content []byte
		relPath string
	}

	fileChan := make(chan fileInfo, 100)
	errorChan := make(chan error, 1)

	// Start file walker in a goroutine
	logger.Debugf("[Zoekt] Starting file walker goroutine...")
	go func() {
		defer close(fileChan)
		fileCount := 0
		skippedCount := 0
		lastLogTime := time.Now()

		err := filepath.Walk(indexPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Check context cancellation during walk
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if info.IsDir() {
				// Skip common directories that shouldn't be indexed
				skipDirs := []string{".git", ".zoekt", "node_modules", ".vscode", ".idea",
					"target", "build", "dist", ".next", ".nuxt", "__pycache__", ".pytest_cache",
					"vendor", ".gradle", ".mvn", "bin", "obj"}
				for _, skipDir := range skipDirs {
					if info.Name() == skipDir {
						logger.Debugf("[Zoekt] Skipping directory: %s", info.Name())
						return filepath.SkipDir
					}
				}
				return nil
			}

			relPath, err := filepath.Rel(indexPath, path)
			if err != nil {
				return err
			}

			// Skip files with extensions that shouldn't be indexed
			skipExts := []string{".exe", ".dll", ".so", ".dylib", ".bin", ".obj", ".o",
				".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".svg",
				".mp4", ".avi", ".mov", ".wmv", ".flv", ".mp3", ".wav", ".ogg",
				".zip", ".tar", ".gz", ".rar", ".7z", ".pdf", ".doc", ".docx",
				".xls", ".xlsx", ".ppt", ".pptx", ".class", ".jar", ".war"}
			ext := strings.ToLower(filepath.Ext(path))
			for _, skipExt := range skipExts {
				if ext == skipExt {
					logger.Debugf("[Zoekt] Skipping file with extension %s: %s", ext, relPath)
					return nil
				}
			}

			// Skip large files to prevent memory issues (adaptive limit based on available memory)
			fileSizeLimit := t.getFileSizeLimit()
			if info.Size() > fileSizeLimit {
				skippedCount++
				logger.Debugf("[Zoekt] Skipping large file (%d MB, limit: %d MB): %s",
					info.Size()/(1024*1024), fileSizeLimit/(1024*1024), relPath)
				return nil
			}

			// Check memory pressure before processing file
			if t.isMemoryPressureHigh() {
				logger.Warnf("[Zoekt] High memory pressure detected, triggering GC and skipping file: %s", relPath)
				runtime.GC()
				return nil
			}

			fileCount++

			// Batch processing for better performance
			if fileCount%100 == 0 {
				// Check for context cancellation more frequently
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				// Adaptive processing based on memory pressure
				if t.isMemoryPressureHigh() {
					logger.Warnf("[Zoekt] Memory pressure high at file %d, slowing down processing", fileCount)
					time.Sleep(100 * time.Millisecond) // Brief pause to allow GC
				}
			}

			// Log progress every 1000 files or every 10 seconds
			if fileCount%1000 == 0 || time.Since(lastLogTime) > 10*time.Second {
				logger.Infof("[Zoekt] Processing files: %d processed, %d skipped", fileCount, skippedCount)
				lastLogTime = time.Now()

				// Log memory usage and check for memory pressure
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				logger.Debugf("[Zoekt] Memory usage - Alloc: %d KB, Sys: %d KB", m.Alloc/1024, m.Sys/1024)

				// Force GC if memory usage is high (reduced frequency for better performance)
				if fileCount%2000 == 0 && m.Alloc > 500*1024*1024 { // 500MB threshold
					logger.Debugf("[Zoekt] High memory usage detected, forcing GC")
					runtime.GC()
				}
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return nil // Skip files we can't read
			}

			// Detect if file is binary before adding to index
			binaryResult := DetectBinaryContent(content)
			if binaryResult.IsBinary {
				logger.Debugf("[Zoekt] Skipping binary file: %s", relPath)
				return nil
			}

			select {
			case fileChan <- fileInfo{path: path, content: content, relPath: relPath}:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})
		if err != nil {
			logger.Errorf("[Zoekt] File walker error: %v", err)
			errorChan <- err
		} else {
			logger.Infof("[Zoekt] File walker completed: %d files processed, %d skipped", fileCount, skippedCount)
		}
	}()

	// Process files with context cancellation support
	logger.Debugf("[Zoekt] Starting file processing loop...")
	processedFiles := 0
	lastProgressTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.Warnf("[Zoekt] Index build cancelled after processing %d files", processedFiles)
			return ctx.Err()
		case err := <-errorChan:
			logger.Errorf("[Zoekt] Error during file processing: %v", err)
			return fmt.Errorf("error walking files: %w", err)
		case file, ok := <-fileChan:
			if !ok {
				// Channel closed, finish indexing
				logger.Infof("[Zoekt] Finishing index build after processing %d files...", processedFiles)
				finishStart := time.Now()
				err := b.Finish()
				if err != nil {
					logger.Errorf("[Zoekt] Failed to finish index: %v", err)
					return err
				}
				logger.Infof("[Zoekt] Index build completed in %v", time.Since(finishStart))
				return nil
			}
			if err := b.AddFile(file.relPath, file.content); err != nil {
				logger.Errorf("[Zoekt] Failed to add file %s: %v", file.relPath, err)
				return fmt.Errorf("error adding file %s: %w", file.relPath, err)
			}
			processedFiles++

			// Log progress every 500 files or every 5 seconds
			if processedFiles%500 == 0 || time.Since(lastProgressTime) > 5*time.Second {
				logger.Infof("[Zoekt] Added %d files to index", processedFiles)
				lastProgressTime = time.Now()
			}
		}
	}
}
