package hours

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsafeReferenceIsRejectedThroughHTTPWithoutInternalDetails(t *testing.T) {
	python := os.Getenv("OFFICE_PYTHON")
	if python == "" {
		python = "python3"
	}
	if err := exec.Command(python, "-c", "import docx, pypdf").Run(); err != nil {
		t.Skip("Office Python dependencies are required")
	}
	var archive bytes.Buffer
	zipper := zip.NewWriter(&archive)
	entry, err := zipper.Create("word/vbaProject.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("test macro")); err != nil {
		t.Fatal(err)
	}
	if err = zipper.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "reference.docx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/references", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	app := App{Token: "test-token", Exporter: Exporter{Python: python, Script: filepath.Join("office", "export.py")}}
	app.Handler().ServeHTTP(response, request)
	text := response.Body.String()
	if response.Code != http.StatusBadRequest || !strings.Contains(text, "макросы") || strings.Contains(text, "Traceback") || strings.Contains(text, "reference-") || strings.Contains(text, os.TempDir()) {
		t.Fatalf("unsafe file response: %d %s", response.Code, text)
	}
}
