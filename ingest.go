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
	size, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(file, (10<<20)+1))
	if err != nil {
		return ReferenceExtract{}, err
	}
	if size > 10<<20 {
		return ReferenceExtract{}, errors.New("Файл превышает допустимый размер 10 МиБ")
	}
	python := a.Exporter.Python
	if python == "" {
		python = "python3"
	}
	output, err := runProcess(ctx, 30*time.Second, 2<<20, 64<<10, python, filepath.Join(filepath.Dir(a.Exporter.Script), "ingest.py"), temp.Name())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ReferenceExtract{}, errors.New("Обработка файла заняла больше 30 секунд; загрузите документ меньшего размера")
		}
		return ReferenceExtract{}, errors.New("Не удалось обработать файл; проверьте формат и содержимое документа")
	}
	var result struct {
		Text      string `json:"text"`
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return ReferenceExtract{}, err
	}
	if result.ErrorCode != "" {
		message := "Не удалось прочитать документ; проверьте формат и содержимое"
		switch result.ErrorCode {
		case "unsafe_document":
			message = "Файл отклонён: небезопасное или неподдерживаемое содержимое (макросы, вложения, активные действия или шифрование)"
		case "reference_too_large":
			message = "Документ превышает ограничения на объём или сложность содержимого"
		case "no_text":
			message = "В документе нет извлекаемого текста; для сканированного PDF сначала выполните распознавание"
		}
		return ReferenceExtract{}, errors.New(message)
	}
	if strings.TrimSpace(result.Text) == "" {
		return ReferenceExtract{}, errors.New("no text found; scanned PDFs require text recognition before upload")
	}
	return ReferenceExtract{Name: filepath.Base(header.Filename), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Bytes: size, Text: result.Text}, nil
}
