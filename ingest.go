package hours

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ReferenceExtract struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Text   string `json:"text"`
}

func (a App) references(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: "file is required"})
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	result, err := a.extractReference(ctx, file, header)
	if err != nil {
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

func (a App) extractReference(ctx context.Context, file multipart.File, header *multipart.FileHeader) (ReferenceExtract, error) {
	suffix := strings.ToLower(filepath.Ext(header.Filename))
	if suffix != ".docx" && suffix != ".pdf" && suffix != ".txt" {
		return ReferenceExtract{}, errors.New("supported reference formats: DOCX, PDF, TXT")
	}
	temp, err := os.CreateTemp("", "reference-*"+suffix)
	if err != nil {
		return ReferenceExtract{}, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(file, 10<<20))
	if err != nil {
		return ReferenceExtract{}, err
	}
	python := a.Exporter.Python
	if python == "" {
		python = "python3"
	}
	command := exec.CommandContext(ctx, python, filepath.Join(filepath.Dir(a.Exporter.Script), "ingest.py"), temp.Name())
	output, err := command.Output()
	if err != nil {
		return ReferenceExtract{}, fmt.Errorf("reference extraction failed: %w", err)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return ReferenceExtract{}, err
	}
	if strings.TrimSpace(result.Text) == "" {
		return ReferenceExtract{}, errors.New("no text found; scanned PDFs require text recognition before upload")
	}
	return ReferenceExtract{Name: filepath.Base(header.Filename), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Bytes: size, Text: result.Text}, nil
}
