package logger

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultTailLines = 500
	MaxTailLines     = 5000

	logFilePattern = "k2-gateway-*.log"
)

var (
	ErrInvalidLogFilename = errors.New("invalid log filename")
	ErrLogFileNotFound    = errors.New("log file not found")
	ErrInvalidTail        = errors.New("invalid tail")
)

// LogFileInfo describes one gateway-managed log file.
type LogFileInfo struct {
	Name       string
	Size       int64
	ModifiedAt time.Time
	Current    bool
}

// LogTail contains a bounded tail read from a gateway-managed log file.
type LogTail struct {
	Name      string
	Current   bool
	Tail      int
	Lines     []string
	Truncated bool
}

// NormalizeTail validates and clamps a requested line count.
func NormalizeTail(tail int) (int, error) {
	if tail <= 0 {
		return 0, ErrInvalidTail
	}
	if tail > MaxTailLines {
		return MaxTailLines, nil
	}
	return tail, nil
}

// ListLogFiles returns gateway-managed log files in newest-first order.
func ListLogFiles(logsDir, currentLogFile string) ([]LogFileInfo, error) {
	pattern := filepath.Join(logsDir, logFilePattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	currentAbs := cleanAbs(currentLogFile)
	items := make([]LogFileInfo, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		items = append(items, LogFileInfo{
			Name:       filepath.Base(path),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
			Current:    cleanAbs(path) == currentAbs && currentAbs != "",
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ModifiedAt.Equal(items[j].ModifiedAt) {
			return items[i].Name > items[j].Name
		}
		return items[i].ModifiedAt.After(items[j].ModifiedAt)
	})
	return items, nil
}

// ListCurrentLogFiles returns gateway-managed log files from the active logger directory.
func ListCurrentLogFiles() ([]LogFileInfo, error) {
	return ListLogFiles(GetLogDir(), GetCurrentLogFile())
}

// ReadCurrentLogTail reads the tail of the current log file.
func ReadCurrentLogTail(tail int) (*LogTail, error) {
	current := GetCurrentLogFile()
	if current == "" {
		return nil, ErrLogFileNotFound
	}
	return readLogTailByPath(GetLogDir(), current, current, tail)
}

// ReadNamedLogTail reads the tail of a selected gateway-managed log file.
func ReadNamedLogTail(name string, tail int) (*LogTail, error) {
	path, err := ResolveLogFilePath(GetLogDir(), name)
	if err != nil {
		return nil, err
	}
	return readLogTailByPath(GetLogDir(), path, GetCurrentLogFile(), tail)
}

// ResolveLogFilePath validates a log filename and resolves it within logsDir.
func ResolveLogFilePath(logsDir, name string) (string, error) {
	if !isSafeLogFilename(name) {
		return "", ErrInvalidLogFilename
	}

	logsAbs, err := filepath.Abs(logsDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(logsAbs, name)
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Dir(pathAbs) != logsAbs {
		return "", ErrInvalidLogFilename
	}
	if _, err := os.Stat(pathAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrLogFileNotFound
		}
		return "", err
	}
	return pathAbs, nil
}

func readLogTailByPath(logsDir, path, currentLogFile string, tail int) (*LogTail, error) {
	normalizedTail, err := NormalizeTail(tail)
	if err != nil {
		return nil, err
	}

	logsAbs, err := filepath.Abs(logsDir)
	if err != nil {
		return nil, err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(pathAbs) != logsAbs || !isSafeLogFilename(filepath.Base(pathAbs)) {
		return nil, ErrInvalidLogFilename
	}

	lines, truncated, err := readLastLines(pathAbs, normalizedTail)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrLogFileNotFound
		}
		return nil, err
	}

	return &LogTail{
		Name:      filepath.Base(pathAbs),
		Current:   cleanAbs(pathAbs) == cleanAbs(currentLogFile) && currentLogFile != "",
		Tail:      normalizedTail,
		Lines:     lines,
		Truncated: truncated,
	}, nil
}

func isSafeLogFilename(name string) bool {
	if name == "" || name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	ok, err := filepath.Match(logFilePattern, name)
	return err == nil && ok
}

func readLastLines(path string, tail int) ([]string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() == 0 {
		return []string{}, false, nil
	}

	const chunkSize int64 = 4096
	var data []byte
	var pos = info.Size()
	newlines := 0
	truncated := false

	for pos > 0 {
		readSize := chunkSize
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize

		chunk := make([]byte, readSize)
		if _, err := file.ReadAt(chunk, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		data = append(chunk, data...)
		newlines += bytes.Count(chunk, []byte{'\n'})

		if newlines > tail {
			truncated = true
			break
		}
	}

	lines := splitLogLines(data)
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
		truncated = true
	}
	return lines, truncated, nil
}

func splitLogLines(data []byte) []string {
	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return []string{}
	}
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	return lines
}

func cleanAbs(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}
