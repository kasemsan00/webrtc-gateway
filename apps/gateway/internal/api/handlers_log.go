package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/logger"
)

type LogFileResponse struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	Current    bool   `json:"current"`
}

type LogFileListResponse struct {
	Items []LogFileResponse `json:"items"`
}

type LogTailResponse struct {
	Name      string   `json:"name"`
	Current   bool     `json:"current"`
	Tail      int      `json:"tail"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

var (
	listGatewayLogFiles = logger.ListCurrentLogFiles
	readCurrentLogTail  = logger.ReadCurrentLogTail
	readNamedLogTail    = logger.ReadNamedLogTail
)

func (s *Server) handleListLogFiles(w http.ResponseWriter, r *http.Request) {
	files, err := listGatewayLogFiles()
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list log files: %v", err))
		return
	}

	items := make([]LogFileResponse, 0, len(files))
	for _, file := range files {
		items = append(items, LogFileResponse{
			Name:       file.Name,
			Size:       file.Size,
			ModifiedAt: file.ModifiedAt.Format(time.RFC3339Nano),
			Current:    file.Current,
		})
	}
	s.respondJSON(w, http.StatusOK, LogFileListResponse{Items: items})
}

func (s *Server) handleGetCurrentLog(w http.ResponseWriter, r *http.Request) {
	tail, ok := s.parseLogTailQuery(w, r)
	if !ok {
		return
	}

	result, err := readCurrentLogTail(tail)
	if err != nil {
		s.respondLogFileError(w, err)
		return
	}
	s.respondJSON(w, http.StatusOK, logTailResponse(result))
}

func (s *Server) handleGetLogFile(w http.ResponseWriter, r *http.Request) {
	tail, ok := s.parseLogTailQuery(w, r)
	if !ok {
		return
	}

	name := mux.Vars(r)["name"]
	result, err := readNamedLogTail(name, tail)
	if err != nil {
		s.respondLogFileError(w, err)
		return
	}
	s.respondJSON(w, http.StatusOK, logTailResponse(result))
}

func (s *Server) parseLogTailQuery(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tail"))
	tail := logger.DefaultTailLines
	if raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, "tail must be a positive integer")
			return 0, false
		}
		tail = parsed
	}

	normalized, err := logger.NormalizeTail(tail)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "tail must be a positive integer")
		return 0, false
	}
	return normalized, true
}

func (s *Server) respondLogFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, logger.ErrInvalidLogFilename), errors.Is(err, logger.ErrInvalidTail):
		s.respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, logger.ErrLogFileNotFound):
		s.respondError(w, http.StatusNotFound, err.Error())
	default:
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to read log file: %v", err))
	}
}

func logTailResponse(result *logger.LogTail) LogTailResponse {
	return LogTailResponse{
		Name:      result.Name,
		Current:   result.Current,
		Tail:      result.Tail,
		Lines:     result.Lines,
		Truncated: result.Truncated,
	}
}
