package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/files"
)

const (
	CodeFileOutside   = "path_outside_server"
	CodeFileProtected = "path_protected"
	CodeFileExists    = "file_exists"
	CodeFileTooLarge  = "file_too_large"
	CodeFileIsDir     = "is_directory"
	CodeFileNotDir    = "not_directory"
	CodeFileBadName   = "invalid_name"
)

func (s *Server) filesFor(r *http.Request) *files.Manager {
	return files.New(s.servers.Dir(serverFromContext(r.Context()).ID))
}

// writeFileError maps file manager errors to the API envelope.
func (s *Server) writeFileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, files.ErrOutsideRoot):
		writeError(w, http.StatusBadRequest, CodeFileOutside, "That path is outside the server directory.")
	case errors.Is(err, files.ErrProtected):
		writeError(w, http.StatusForbidden, CodeFileProtected, "This path is managed by the panel and cannot be changed here.")
	case errors.Is(err, files.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "File or folder not found.")
	case errors.Is(err, files.ErrExists):
		writeError(w, http.StatusConflict, CodeFileExists, "A file or folder with that name already exists.")
	case errors.Is(err, files.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, CodeFileTooLarge, "The file is too large.")
	case errors.Is(err, files.ErrIsDir):
		writeError(w, http.StatusBadRequest, CodeFileIsDir, "That path is a folder.")
	case errors.Is(err, files.ErrNotDir):
		writeError(w, http.StatusBadRequest, CodeFileNotDir, "That path is not a folder.")
	case errors.Is(err, files.ErrBadName):
		writeError(w, http.StatusBadRequest, CodeFileBadName, "Invalid file name.")
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) auditFile(r *http.Request, action, target string, details map[string]any) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	if details == nil {
		details = map[string]any{}
	}
	details["path"] = target
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: action, Target: srv.ID, IP: clientIP(r), Details: details})
}

type listResponse struct {
	Path    string        `json:"path"`
	Entries []files.Entry `json:"entries"`
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	entries, err := s.filesFor(r).List(p)
	if err != nil {
		s.writeFileError(w, r, err)
		return
	}
	clean, _ := files.Clean(p)
	writeJSON(w, http.StatusOK, listResponse{Path: clean, Entries: entries})
}

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	data, err := s.filesFor(r).ReadText(p)
	if err != nil {
		s.writeFileError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	data, err := io.ReadAll(io.LimitReader(r.Body, files.MaxEditSize+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Could not read the request body.")
		return
	}
	if len(data) > files.MaxEditSize {
		writeError(w, http.StatusRequestEntityTooLarge, CodeFileTooLarge, "Files over 2 MB cannot be edited in the browser; upload them instead.")
		return
	}
	if err := s.filesFor(r).WriteText(p, data); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	s.auditFile(r, "files.write", p, map[string]any{"bytes": len(data)})
	entry, _ := s.filesFor(r).Stat(p)
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	f, size, err := s.filesFor(r).Open(p)
	if err != nil {
		s.writeFileError(w, r, err)
		return
	}
	defer f.Close()
	name := path.Base("/" + p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, f)
}

type pathRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var req pathRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.filesFor(r).Mkdir(req.Path); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	s.auditFile(r, "files.mkdir", req.Path, nil)
	w.WriteHeader(http.StatusNoContent)
}

type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) handleFilesRename(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.filesFor(r).Rename(req.From, req.To); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	s.auditFile(r, "files.rename", req.From, map[string]any{"to": req.To})
	w.WriteHeader(http.StatusNoContent)
}

type deleteRequest struct {
	Paths []string `json:"paths"`
}

func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	var req deleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > 500 {
		writeError(w, http.StatusBadRequest, CodeValidation, "Provide between 1 and 500 paths.")
		return
	}
	m := s.filesFor(r)
	for _, p := range req.Paths {
		if err := m.Delete(p); err != nil {
			s.writeFileError(w, r, fmt.Errorf("%s: %w", p, err))
			return
		}
		s.auditFile(r, "files.delete", p, nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

type extractRequest struct {
	Path string `json:"path"`
	To   string `json:"to"`
}

func (s *Server) handleFilesExtract(w http.ResponseWriter, r *http.Request) {
	var req extractRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.To == "" {
		req.To = path.Dir("/" + req.Path)
	}
	if err := s.filesFor(r).Extract(req.Path, req.To); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	s.auditFile(r, "files.extract", req.Path, map[string]any{"to": req.To})
	w.WriteHeader(http.StatusNoContent)
}

// handleFilesUpload accepts multipart/form-data with one or more "files"
// parts into ?path=<dir>. Each part is capped at max_upload_mb.
func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	maxMB := s.cfg.MaxUploadMB
	if maxMB <= 0 {
		maxMB = 1024
	}
	maxBytes := int64(maxMB) << 20
	// Several files per request are allowed; cap the whole body generously.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes*4+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Expected a multipart/form-data upload.")
		return
	}
	m := s.filesFor(r)
	var uploaded []files.Entry
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, CodeFileTooLarge, "Upload exceeds the size limit.")
				return
			}
			writeError(w, http.StatusBadRequest, CodeBadRequest, "Malformed upload: "+err.Error())
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		n, err := m.Create(dir, name, part, maxBytes)
		part.Close()
		if err != nil {
			s.writeFileError(w, r, fmt.Errorf("%s: %w", name, err))
			return
		}
		s.auditFile(r, "files.upload", path.Join(dir, name), map[string]any{"bytes": n})
		if e, err := m.Stat(path.Join(dir, name)); err == nil {
			uploaded = append(uploaded, e)
		}
	}
	if uploaded == nil {
		uploaded = []files.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}
