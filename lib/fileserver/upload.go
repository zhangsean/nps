package fileserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	uploadStateDirectory  = ".nps-upload-state"
	maxUploadChunkSize    = int64(16 << 20)
	uploadStateTTL        = 7 * 24 * time.Hour
	uploadCleanupInterval = time.Hour
)

var uploadIDPattern = regexp.MustCompile(`^[a-f0-9]{16,64}$`)

type uploadManager struct {
	root        string
	locks       sync.Map
	cleanupMu   sync.Mutex
	lastCleanup time.Time
}

type uploadRequestInfo struct {
	ID           string
	Name         string
	Size         int64
	LastModified int64
	Directory    string
	stateKey     string
}

type uploadMetadata struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	LastModified int64     `json:"last_modified"`
	Directory    string    `json:"directory"`
	Complete     bool      `json:"complete"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type uploadResponse struct {
	Offset   int64  `json:"offset"`
	Complete bool   `json:"complete"`
	Error    string `json:"error,omitempty"`
}

func newUploadManager(root string) *uploadManager {
	return &uploadManager{root: root}
}

func isUploadStateEntry(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), uploadStateDirectory)
}

func isUploadStatePath(cleanPath string) bool {
	cleanPath = strings.TrimPrefix(filepath.ToSlash(cleanPath), "/")
	if cleanPath == "" {
		return false
	}
	first := strings.SplitN(cleanPath, "/", 2)[0]
	return isUploadStateEntry(first)
}

func (b *Browser) handleUploadStatus(w http.ResponseWriter, r *http.Request, dirPath string) {
	info, err := b.uploads.parseRequest(r, dirPath)
	if err != nil {
		writeUploadError(w, http.StatusBadRequest, err)
		return
	}
	b.uploads.maybeCleanup()
	lock := b.uploads.lockFor(info.stateKey)
	lock.Lock()
	defer lock.Unlock()

	metadata, offset, complete, err := b.uploads.ensureState(info, dirPath)
	if err != nil {
		writeUploadError(w, http.StatusConflict, err)
		return
	}
	if !complete && offset == info.Size {
		if err := b.uploads.finalize(info, metadata, dirPath); err != nil {
			writeUploadError(w, http.StatusInternalServerError, err)
			return
		}
		complete = true
	}
	writeUploadJSON(w, http.StatusOK, uploadResponse{Offset: info.SizeIf(complete, offset), Complete: complete})
}

func (b *Browser) handleUploadChunk(w http.ResponseWriter, r *http.Request, dirPath string) {
	info, err := b.uploads.parseRequest(r, dirPath)
	if err != nil {
		writeUploadError(w, http.StatusBadRequest, err)
		return
	}
	requestedOffset, err := parseUploadInt(r, "offset")
	if err != nil || requestedOffset < 0 || requestedOffset > info.Size {
		writeUploadError(w, http.StatusBadRequest, fmt.Errorf("invalid upload offset"))
		return
	}
	if r.ContentLength > maxUploadChunkSize {
		writeUploadError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("upload chunk exceeds %d bytes", maxUploadChunkSize))
		return
	}

	b.uploads.maybeCleanup()
	lock := b.uploads.lockFor(info.stateKey)
	lock.Lock()
	defer lock.Unlock()

	metadata, offset, complete, err := b.uploads.ensureState(info, dirPath)
	if err != nil {
		writeUploadError(w, http.StatusConflict, err)
		return
	}
	if complete {
		writeUploadJSON(w, http.StatusOK, uploadResponse{Offset: info.Size, Complete: true})
		return
	}
	if requestedOffset != offset {
		writeUploadJSON(w, http.StatusConflict, uploadResponse{
			Offset: offset,
			Error:  "upload offset changed; resume from the returned offset",
		})
		return
	}

	_, partPath := b.uploads.statePaths(info.stateKey)
	out, err := os.OpenFile(partPath, os.O_WRONLY, 0600)
	if err != nil {
		writeUploadError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err = out.Seek(offset, io.SeekStart); err != nil {
		_ = out.Close()
		writeUploadError(w, http.StatusInternalServerError, err)
		return
	}

	limited := io.LimitReader(r.Body, maxUploadChunkSize+1)
	written, copyErr := io.Copy(out, limited)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil || written <= 0 || written > maxUploadChunkSize || offset+written > info.Size || (r.ContentLength >= 0 && written != r.ContentLength) {
		_ = os.Truncate(partPath, offset)
		if copyErr == nil {
			copyErr = fmt.Errorf("incomplete or oversized upload chunk")
		}
		writeUploadError(w, http.StatusBadRequest, copyErr)
		return
	}

	offset += written
	metadata.UpdatedAt = time.Now().UTC()
	if err := b.uploads.writeMetadata(info.stateKey, metadata); err != nil {
		writeUploadError(w, http.StatusInternalServerError, err)
		return
	}
	if offset == info.Size {
		if err := b.uploads.finalize(info, metadata, dirPath); err != nil {
			writeUploadError(w, http.StatusInternalServerError, err)
			return
		}
		writeUploadJSON(w, http.StatusOK, uploadResponse{Offset: info.Size, Complete: true})
		return
	}
	writeUploadJSON(w, http.StatusOK, uploadResponse{Offset: offset})
}

func (m *uploadManager) parseRequest(r *http.Request, dirPath string) (*uploadRequestInfo, error) {
	id := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("upload_id")))
	if !uploadIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid upload id")
	}
	name, ok := safeEntryName(r.URL.Query().Get("name"))
	if !ok || isUploadStateEntry(name) {
		return nil, fmt.Errorf("invalid file name")
	}
	size, err := parseUploadInt(r, "size")
	if err != nil || size < 0 {
		return nil, fmt.Errorf("invalid file size")
	}
	lastModified, err := parseUploadInt(r, "last_modified")
	if err != nil || lastModified < 0 {
		return nil, fmt.Errorf("invalid last modified time")
	}
	relative, err := filepath.Rel(m.root, dirPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("upload directory is outside root")
	}
	if relative == "." {
		relative = ""
	}
	directory := filepath.ToSlash(relative)
	keyHash := sha256.Sum256([]byte(directory + "\x00" + id))
	return &uploadRequestInfo{
		ID:           id,
		Name:         name,
		Size:         size,
		LastModified: lastModified,
		Directory:    directory,
		stateKey:     hex.EncodeToString(keyHash[:]),
	}, nil
}

func parseUploadInt(r *http.Request, name string) (int64, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return 0, fmt.Errorf("missing %s", name)
	}
	return strconv.ParseInt(value, 10, 64)
}

func (info *uploadRequestInfo) SizeIf(complete bool, offset int64) int64 {
	if complete {
		return info.Size
	}
	return offset
}

func (m *uploadManager) lockFor(key string) *sync.Mutex {
	lock, _ := m.locks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (m *uploadManager) statePaths(key string) (string, string) {
	stateDir := filepath.Join(m.root, uploadStateDirectory)
	return filepath.Join(stateDir, key+".json"), filepath.Join(stateDir, key+".part")
}

func (m *uploadManager) ensureState(info *uploadRequestInfo, dirPath string) (*uploadMetadata, int64, bool, error) {
	stateDir := filepath.Join(m.root, uploadStateDirectory)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, 0, false, err
	}
	metadataPath, partPath := m.statePaths(info.stateKey)
	metadata, err := m.readMetadata(metadataPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, false, fmt.Errorf("cannot read upload state: %w", err)
	}
	if os.IsNotExist(err) {
		metadata = &uploadMetadata{
			ID:           info.ID,
			Name:         info.Name,
			Size:         info.Size,
			LastModified: info.LastModified,
			Directory:    info.Directory,
			UpdatedAt:    time.Now().UTC(),
		}
		part, createErr := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil && !os.IsExist(createErr) {
			return nil, 0, false, createErr
		}
		if part != nil {
			_ = part.Close()
		}
		if err := m.writeMetadata(info.stateKey, metadata); err != nil {
			return nil, 0, false, err
		}
	} else if metadata.ID != info.ID || metadata.Name != info.Name || metadata.Size != info.Size || metadata.LastModified != info.LastModified || metadata.Directory != info.Directory {
		return nil, 0, false, fmt.Errorf("upload id belongs to different file metadata")
	}

	if metadata.Complete {
		if completeUploadExists(dirPath, info) {
			return metadata, info.Size, true, nil
		}
		metadata.Complete = false
		metadata.UpdatedAt = time.Now().UTC()
		if err := os.WriteFile(partPath, nil, 0600); err != nil {
			return nil, 0, false, err
		}
		if err := m.writeMetadata(info.stateKey, metadata); err != nil {
			return nil, 0, false, err
		}
	}

	partInfo, err := os.Stat(partPath)
	if os.IsNotExist(err) && completeUploadExists(dirPath, info) {
		metadata.Complete = true
		metadata.UpdatedAt = time.Now().UTC()
		if err := m.writeMetadata(info.stateKey, metadata); err != nil {
			return nil, 0, false, err
		}
		return metadata, info.Size, true, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if partInfo.IsDir() || partInfo.Size() > info.Size {
		return nil, 0, false, fmt.Errorf("invalid upload state")
	}
	return metadata, partInfo.Size(), false, nil
}

func completeUploadExists(dirPath string, info *uploadRequestInfo) bool {
	fileInfo, err := os.Stat(filepath.Join(dirPath, info.Name))
	if err != nil || fileInfo.IsDir() || fileInfo.Size() != info.Size {
		return false
	}
	if info.LastModified == 0 {
		return true
	}
	difference := fileInfo.ModTime().UnixMilli() - info.LastModified
	if difference < 0 {
		difference = -difference
	}
	return difference <= 2000
}

func (m *uploadManager) finalize(info *uploadRequestInfo, metadata *uploadMetadata, dirPath string) error {
	metadataPath, partPath := m.statePaths(info.stateKey)
	partInfo, err := os.Stat(partPath)
	if err != nil || partInfo.IsDir() || partInfo.Size() != info.Size {
		return fmt.Errorf("upload is incomplete")
	}
	destination := filepath.Join(dirPath, info.Name)
	relative, err := filepath.Rel(m.root, destination)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("upload destination is outside root")
	}
	destinationExists := false
	if destinationInfo, statErr := os.Stat(destination); statErr == nil {
		if destinationInfo.IsDir() {
			return fmt.Errorf("a directory with the same name already exists")
		}
		destinationExists = true
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.Rename(partPath, destination); err != nil {
		if !destinationExists {
			return err
		}
		if removeErr := os.Remove(destination); removeErr != nil {
			return removeErr
		}
		if retryErr := os.Rename(partPath, destination); retryErr != nil {
			return retryErr
		}
	}
	if info.LastModified > 0 {
		modifiedAt := time.UnixMilli(info.LastModified)
		_ = os.Chtimes(destination, time.Now(), modifiedAt)
	}
	metadata.Complete = true
	metadata.UpdatedAt = time.Now().UTC()
	if err := m.writeMetadata(info.stateKey, metadata); err != nil {
		return err
	}
	_ = os.Chtimes(metadataPath, time.Now(), time.Now())
	return nil
}

func (m *uploadManager) readMetadata(path string) (*uploadMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	metadata := &uploadMetadata{}
	if err := json.Unmarshal(data, metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func (m *uploadManager) writeMetadata(key string, metadata *uploadMetadata) error {
	metadataPath, _ := m.statePaths(key)
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(metadataPath), key+"-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, metadataPath); err == nil {
		return nil
	}
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, metadataPath)
}

func (m *uploadManager) maybeCleanup() {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	if !m.lastCleanup.IsZero() && time.Since(m.lastCleanup) < uploadCleanupInterval {
		return
	}
	m.lastCleanup = time.Now()
	stateDir := filepath.Join(m.root, uploadStateDirectory)
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-uploadStateTTL)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(stateDir, entry.Name()))
		}
	}
}

func writeUploadError(w http.ResponseWriter, status int, err error) {
	writeUploadJSON(w, status, uploadResponse{Error: err.Error()})
}

func writeUploadJSON(w http.ResponseWriter, status int, response uploadResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
