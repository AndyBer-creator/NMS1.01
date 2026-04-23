package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"go.uber.org/zap"
)

const mibUploadMaxBytes = 10 << 20 // 10 MiB

type mibFileRow struct {
	Name      string
	SizeHuman string
}

type mibPanelView struct {
	Dir       string
	Files     []mibFileRow
	Error     string
	OK        string
	CSRFToken string
}

func safeMibFilename(name string) (string, bool) {
	base := filepath.Base(name)
	if base == "." || base == "/" || base == "" {
		return "", false
	}
	if strings.Contains(base, "..") {
		return "", false
	}
	if len(base) > 200 {
		return "", false
	}
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".mib") && !strings.HasSuffix(lower, ".my") && !strings.HasSuffix(lower, ".txt") {
		return "", false
	}
	for _, r := range base {
		if r == '.' || r == '-' || r == '_' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return "", false
	}
	return base, true
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}

func (h *Handlers) listMibFiles() ([]mibFileRow, error) {
	entries, err := os.ReadDir(h.mibUploadDir)
	if err != nil {
		return nil, err
	}
	var rows []mibFileRow
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == ".gitkeep" || strings.HasPrefix(name, ".") {
			continue
		}
		if _, ok := safeMibFilename(name); !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		rows = append(rows, mibFileRow{Name: name, SizeHuman: formatSize(info.Size())})
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows, nil
}

func (h *Handlers) renderMibPanel(w http.ResponseWriter, vm mibPanelView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.mibPanelTmpl.Execute(w, vm); err != nil {
		h.logger.Error("mib panel template", zap.Error(err))
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// MibPanel renders dashboard MIB upload/list block.
func (h *Handlers) MibPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	if u != nil && u.role != roleAdmin {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<div class="text-sm text-gray-500">Загрузка MIB доступна только администратору.</div>`)
		return
	}

	files, err := h.listMibFiles()
	if err != nil {
		h.logger.Error("mib list", zap.Error(err))
		http.Error(w, "Не удалось прочитать каталог MIB", http.StatusInternalServerError)
		return
	}
	h.renderMibPanel(w, mibPanelView{
		Dir:       h.mibUploadDir,
		Files:     files,
		CSRFToken: csrfTokenFromContext(r),
	})
}

// MibUpload handles single multipart MIB file upload.
func (h *Handlers) MibUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(mibUploadMaxBytes); err != nil {
		h.mibUploadError(w, r, "Слишком большой файл или ошибка формы")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		h.mibUploadError(w, r, "Файл не выбран")
		return
	}
	defer func() { _ = file.Close() }()

	safe, ok := safeMibFilename(hdr.Filename)
	if !ok {
		h.mibUploadError(w, r, "Допустимы только .mib, .my, .txt и безопасное имя файла")
		return
	}

	dest := filepath.Join(h.mibUploadDir, safe)
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		h.logger.Error("mib create tmp", zap.Error(err))
		h.mibUploadError(w, r, "Не удалось сохранить файл")
		return
	}
	defer func() { _ = out.Close() }()

	n, err := io.Copy(out, io.LimitReader(file, mibUploadMaxBytes+1))
	if err != nil {
		_ = os.Remove(tmp)
		h.mibUploadError(w, r, "Ошибка записи")
		return
	}
	if n > mibUploadMaxBytes {
		_ = os.Remove(tmp)
		h.mibUploadError(w, r, "Файл больше 10 MiB")
		return
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		h.mibUploadError(w, r, "Не удалось завершить загрузку")
		return
	}

	h.logger.Info("MIB uploaded", zap.String("name", safe), zap.Int64("bytes", n))

	if r.Header.Get("HX-Request") == "true" {
		files, err := h.listMibFiles()
		if err != nil {
			h.mibUploadError(w, r, "Файл сохранён, но список не обновлён")
			return
		}
		h.renderMibPanel(w, mibPanelView{
			Dir:       h.mibUploadDir,
			Files:     files,
			OK:        "Файл загружен: " + safe,
			CSRFToken: csrfTokenFromContext(r),
		})
		return
	}
	http.Redirect(w, r, "/?mib=ok", http.StatusSeeOther)
}

func (h *Handlers) mibUploadError(w http.ResponseWriter, r *http.Request, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		files, _ := h.listMibFiles()
		h.renderMibPanel(w, mibPanelView{
			Dir:       h.mibUploadDir,
			Files:     files,
			Error:     msg,
			CSRFToken: csrfTokenFromContext(r),
		})
		return
	}
	http.Redirect(w, r, "/?mib_err="+url.QueryEscape(truncateQuery(msg, 200)), http.StatusSeeOther)
}

func truncateQuery(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// MibDelete removes uploaded MIB file by form field name.
func (h *Handlers) MibDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.mibUploadError(w, r, "Ошибка формы")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	safe, ok := safeMibFilename(name)
	if !ok {
		h.mibUploadError(w, r, "Некорректное имя файла")
		return
	}
	path := filepath.Join(h.mibUploadDir, safe)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		h.logger.Error("mib delete", zap.String("path", path), zap.Error(err))
		h.mibUploadError(w, r, "Не удалось удалить")
		return
	}
	h.logger.Info("MIB deleted", zap.String("name", safe))

	if r.Header.Get("HX-Request") == "true" {
		files, err := h.listMibFiles()
		if err != nil {
			h.mibUploadError(w, r, "Удалено, но список не обновлён")
			return
		}
		h.renderMibPanel(w, mibPanelView{
			Dir:       h.mibUploadDir,
			Files:     files,
			OK:        "Удалено: " + safe,
			CSRFToken: csrfTokenFromContext(r),
		})
		return
	}
	http.Redirect(w, r, "/?mib=del", http.StatusSeeOther)
}

// MibResolve — POST JSON { "symbol": "IF-MIB::sysDescr.0" } → { "oid": "1.3.6.1.2.1.1.1.0" }.
func (h *Handlers) MibResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Symbol string `json:"symbol"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Symbol) == "" {
		http.Error(w, "symbol required", http.StatusBadRequest)
		return
	}
	oid, err := h.resolveOIDInput(body.Symbol)
	if err != nil {
		http.Error(w, "invalid MIB symbol", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"oid":    oid,
		"symbol": strings.TrimSpace(body.Symbol),
	})
}
