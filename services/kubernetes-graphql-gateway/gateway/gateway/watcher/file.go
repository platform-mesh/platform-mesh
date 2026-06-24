/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SchemaEventHandler handles schema change events from watchers.
type SchemaEventHandler interface {
	OnSchemaChanged(ctx context.Context, clusterName string, schema []byte)
	OnSchemaDeleted(ctx context.Context, clusterName string)
}

// FileWatcher watches a directory for schema files and notifies the handler.
type FileWatcher struct {
	watcher   *fsnotify.Watcher
	handler   SchemaEventHandler
	watchPath string
}

// NewFileWatcher creates a new file watcher that will notify the given handler
// when schema files change.
func NewFileWatcher(handler SchemaEventHandler) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &FileWatcher{
		watcher: watcher,
		handler: handler,
	}, nil
}

// Run starts the file watcher and blocks until the context is cancelled.
// It first loads all existing files, then watches for changes.
func (fw *FileWatcher) Run(ctx context.Context, watchPath string) error {
	logger := log.FromContext(ctx)
	fw.watchPath = watchPath

	// Process all existing files first
	if err := fw.loadAllFiles(ctx, watchPath); err != nil {
		return err
	}

	// Add directory and subdirectories recursively
	if err := fw.addWatchRecursively(watchPath); err != nil {
		return fmt.Errorf("failed to add watch paths: %w", err)
	}
	defer func() {
		if err := fw.watcher.Close(); err != nil {
			logger.Error(err, "Failed to close file watcher")
		}
	}()

	logger.WithValues("dirPath", watchPath).Info("started watching directory")

	return fw.watchLoop(ctx)
}

// watchLoop handles events immediately for directory watching
func (fw *FileWatcher) watchLoop(ctx context.Context) error {
	logger := log.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			logger.Info("stopping directory watcher gracefully")
			return nil // Graceful termination is not an error

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return fmt.Errorf("directory watcher events channel closed")
			}

			fw.handleEvent(ctx, event)

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return fmt.Errorf("directory watcher errors channel closed")
			}
			logger.Error(err, "directory watcher error")
		}
	}
}

// handleEvent processes file system events for directory watching
func (fw *FileWatcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	logger := log.FromContext(ctx)
	logger.V(4).WithValues("event", event.String()).Info("directory event")

	filePath := event.Name
	switch event.Op {
	case fsnotify.Create, fsnotify.Write:
		// Check if this is actually a file (not a directory)
		info, err := os.Stat(filePath)
		if err != nil {
			return
		}

		if info.IsDir() {
			// New directory created, add it to the watcher and process all files
			if err := fw.watcher.Add(filePath); err != nil {
				logger.WithValues("path", filePath).Error(err, "failed to add directory to watcher")
				return
			}
			if err := filepath.WalkDir(filePath, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				fw.onFileChanged(ctx, path)
				return nil
			}); err != nil {
				logger.WithValues("path", filePath).Error(err, "failed to walk directory")
			}
		} else {
			fw.onFileChanged(ctx, filePath)
		}

	case fsnotify.Rename, fsnotify.Remove:
		fw.onFileDeleted(ctx, filePath)

	default:
		logger.V(4).WithValues("filepath", filePath, "op", event.Op.String()).Info("unhandled file event")
	}
}

// onFileChanged reads the file and notifies the schema handler.
func (fw *FileWatcher) onFileChanged(ctx context.Context, filePath string) {
	logger := log.FromContext(ctx)

	// Check if this is actually a file (not a directory)
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return
	}

	// Read the file content
	data, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error(err, "Failed to read schema file", "path", filePath)
		return
	}

	// Extract cluster name from file path and notify handler
	clusterName := extractClusterName(filePath)
	fw.handler.OnSchemaChanged(ctx, clusterName, data)

	logger.Info("Successfully processed schema file change", "path", filePath, "cluster", clusterName)
}

// onFileDeleted notifies the schema handler that a schema was deleted.
func (fw *FileWatcher) onFileDeleted(ctx context.Context, filePath string) {
	logger := log.FromContext(ctx)

	// Extract cluster name from file path and notify handler
	clusterName := extractClusterName(filePath)
	fw.handler.OnSchemaDeleted(ctx, clusterName)

	logger.Info("Successfully processed schema file deletion", "path", filePath, "cluster", clusterName)
}

// loadAllFiles loads all files in the directory and subdirectories
func (fw *FileWatcher) loadAllFiles(ctx context.Context, dir string) error {
	logger := log.FromContext(ctx)

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Read and process the file
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Error(err, "Failed to read schema file", "file", path)
			return nil // Continue processing other files
		}

		clusterName := extractClusterName(path)
		fw.handler.OnSchemaChanged(ctx, clusterName, data)

		return nil
	})
}

// addWatchRecursively adds the directory and all subdirectories to the watcher
func (fw *FileWatcher) addWatchRecursively(dir string) error {
	if err := fw.watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to add watch path %s: %w", dir, err)
	}

	// Find subdirectories
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return fmt.Errorf("failed to glob directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if dirInfo, err := os.Stat(entry); err == nil && dirInfo.IsDir() {
			if err := fw.addWatchRecursively(entry); err != nil {
				return err
			}
		}
	}

	return nil
}

// extractClusterName extracts the cluster name from a file path.
// The file name (last component of the path) is used as the cluster name.
// For example: "_output/schemas/root:bob" -> "root:bob"
func extractClusterName(filePath string) string {
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return filePath
	}
	return filePath[lastSlash+1:]
}
