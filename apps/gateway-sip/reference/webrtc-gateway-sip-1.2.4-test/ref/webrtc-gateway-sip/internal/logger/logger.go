// Package logger provides centralized logging with file rotation and console output.
// It redirects stdout/stderr so existing fmt.Printf and log.Printf calls are captured.
// Creates a new log file each time the service starts.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Config holds logger configuration
type Config struct {
	LogsDir    string // Directory for log files (default: "logs")
	MaxBackups int    // Max number of old log files to keep (default: 10)
	Console    bool   // Also write to console (default: true)
}

// DefaultConfig returns default logger configuration
func DefaultConfig() Config {
	return Config{
		LogsDir:    "logs",
		MaxBackups: 10,
		Console:    true,
	}
}

// Logger handles file and console logging
// Creates a new log file each time the service starts
type Logger struct {
	config      Config
	logFile     *os.File
	multiWriter io.Writer
	originalOut *os.File
	originalErr *os.File
	pipeReader  *os.File
	pipeWriter  *os.File
	mu          sync.Mutex
	logFilePath string
	done        chan struct{}
}

var (
	instance *Logger
	once     sync.Once
)

// Init initializes the global logger with the given configuration.
// It redirects os.Stdout, os.Stderr, and log output to the log file.
// Creates a NEW log file each time the service starts.
// Returns a cleanup function that should be deferred in main().
func Init(cfg Config) (cleanup func(), err error) {
	once.Do(func() {
		instance, err = newLogger(cfg)
	})
	if err != nil {
		return nil, err
	}
	return instance.Close, nil
}

// InitDefault initializes the logger with default configuration.
func InitDefault() (cleanup func(), err error) {
	return Init(DefaultConfig())
}

// newLogger creates a new Logger instance
func newLogger(cfg Config) (*Logger, error) {
	// Ensure logs directory exists
	if err := os.MkdirAll(cfg.LogsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	l := &Logger{
		config:      cfg,
		originalOut: os.Stdout,
		originalErr: os.Stderr,
		done:        make(chan struct{}),
	}

	// Generate unique log file path with timestamp
	l.logFilePath = l.generateLogFilePath()

	// Create new log file (always create new, never append)
	logFile, err := os.Create(l.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}
	l.logFile = logFile

	// Cleanup old log files
	l.cleanupOldLogs()

	// Create multi-writer (file + console if enabled)
	if cfg.Console {
		l.multiWriter = io.MultiWriter(l.logFile, l.originalOut)
	} else {
		l.multiWriter = l.logFile
	}

	// Redirect standard log package
	log.SetOutput(l.multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Create pipe to capture os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe: %w", err)
	}
	l.pipeReader = pr
	l.pipeWriter = pw

	// Redirect os.Stdout to our pipe
	os.Stdout = pw

	// Start goroutine to read from pipe and write to multi-writer
	go l.captureOutput()

	// Log initialization
	fmt.Fprintf(l.multiWriter, "\n")
	fmt.Fprintf(l.multiWriter, "================================================================================\n")
	fmt.Fprintf(l.multiWriter, "=== K2 Gateway Log Started: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(l.multiWriter, "=== Log File: %s ===\n", l.logFilePath)
	fmt.Fprintf(l.multiWriter, "================================================================================\n")
	fmt.Fprintf(l.multiWriter, "\n")

	return l, nil
}

// generateLogFilePath creates a unique log file path with timestamp
// Format: k2-gateway-YYYY-MM-DD_HH-MM-SS.log
func (l *Logger) generateLogFilePath() string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("k2-gateway-%s.log", timestamp)
	return filepath.Join(l.config.LogsDir, filename)
}

// cleanupOldLogs removes old log files keeping only MaxBackups most recent
func (l *Logger) cleanupOldLogs() {
	pattern := filepath.Join(l.config.LogsDir, "k2-gateway-*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	if len(files) <= l.config.MaxBackups {
		return
	}

	// Sort files by name (which includes timestamp, so oldest first)
	sort.Strings(files)

	// Remove oldest files, keeping MaxBackups
	filesToDelete := len(files) - l.config.MaxBackups
	for i := 0; i < filesToDelete; i++ {
		// Don't delete the current log file
		if files[i] != l.logFilePath {
			os.Remove(files[i])
		}
	}
}

// captureOutput reads from the pipe and writes to the multi-writer
func (l *Logger) captureOutput() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-l.done:
			return
		default:
			n, err := l.pipeReader.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				l.mu.Lock()
				l.multiWriter.Write(buf[:n])
				l.mu.Unlock()
			}
		}
	}
}

// Close flushes and closes the logger, restoring original stdout/stderr
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Signal goroutines to stop
	close(l.done)

	// Log shutdown
	fmt.Fprintf(l.multiWriter, "\n")
	fmt.Fprintf(l.multiWriter, "================================================================================\n")
	fmt.Fprintf(l.multiWriter, "=== K2 Gateway Log Ended: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(l.multiWriter, "================================================================================\n")

	// Close pipe
	if l.pipeWriter != nil {
		l.pipeWriter.Close()
	}
	if l.pipeReader != nil {
		l.pipeReader.Close()
	}

	// Restore original stdout/stderr
	os.Stdout = l.originalOut
	os.Stderr = l.originalErr

	// Close log file
	if l.logFile != nil {
		l.logFile.Close()
	}

	// Reset log output
	log.SetOutput(os.Stderr)
}

// GetLogDir returns the logs directory path
func GetLogDir() string {
	if instance != nil {
		return instance.config.LogsDir
	}
	return "logs"
}

// GetCurrentLogFile returns the current log file path
func GetCurrentLogFile() string {
	if instance != nil {
		return instance.logFilePath
	}
	return ""
}
