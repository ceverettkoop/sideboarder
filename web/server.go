package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ceverettkoop/sideboarder/web/internal/carddb"
	"github.com/ceverettkoop/sideboarder/web/internal/config"
	"github.com/ceverettkoop/sideboarder/web/internal/sbd"
)

type server struct {
	store  docStore // the *.sbd.json documents
	db     *carddb.DB
	static fs.FS

	mu sync.Mutex // serializes document read-modify-write cycles

	jobMu sync.Mutex
	job   cardJob
}

// cardJob is the state of the (single) background card-database update.
type cardJob struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func newServer(store docStore, db *carddb.DB, static fs.FS) *server {
	return &server{store: store, db: db, static: static}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/files", s.listFiles)
	mux.HandleFunc("POST /api/files", s.createFile)
	mux.HandleFunc("GET /api/files/{file}", s.getFile)
	mux.HandleFunc("POST /api/files/{file}/ops", s.applyOp)
	mux.HandleFunc("POST /api/files/{file}/copy", s.copyFile)
	mux.HandleFunc("GET /api/files/{file}/download", s.download)
	mux.HandleFunc("GET /api/files/{file}/report", s.report)
	mux.HandleFunc("GET /api/files/{file}/report.csv", s.reportCSV)
	mux.HandleFunc("GET /api/files/{file}/results.csv", s.resultsCSV)
	mux.HandleFunc("POST /api/parse-decklist", s.parseDecklist)
	mux.HandleFunc("GET /api/cards", s.cards)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("POST /api/settings", s.setSettings)
	mux.HandleFunc("POST /api/carddb/update", s.updateCardDB)
	mux.HandleFunc("GET /api/carddb/status", s.getSettings)
	mux.Handle("GET /", noCache(http.FileServerFS(s.static)))
	return securityHeaders(mux)
}

// ----- helpers ---------------------------------------------------------------

type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

func errStatus(status int, format string, args ...any) error {
	return httpError{status: status, msg: fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var he httpError
	if errors.As(err, &he) {
		status = he.status
	} else if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// readJSON decodes a JSON request body. Requiring the JSON content type also
// means a cross-site page can't submit writes with a plain HTML form.
func readJSON(r *http.Request, v any) error {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		return errStatus(http.StatusUnsupportedMediaType, "expected application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return errStatus(http.StatusBadRequest, "bad request body: %v", err)
	}
	return nil
}

func validName(name string) bool {
	return name != "" && len(name) <= 200 &&
		strings.HasSuffix(name, sbd.FileSuffix) &&
		!strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`+"\x00") &&
		filepath.Base(name) == name
}

func checkName(name string) error {
	if !validName(name) {
		return errStatus(http.StatusBadRequest, "invalid document name %q", name)
	}
	return nil
}

func (s *server) load(name string) (*sbd.Document, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	data, _, err := s.store.Read(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errStatus(http.StatusNotFound, "no document named %q", name)
	}
	if err != nil {
		return nil, errStatus(http.StatusInternalServerError, "open %s failed: %v", name, err)
	}
	doc, err := sbd.ParseDocument(data)
	if err != nil {
		return nil, errStatus(http.StatusUnprocessableEntity, "open %s failed: %v", name, err)
	}
	return doc, nil
}

func (s *server) save(name string, doc *sbd.Document) error {
	data, err := sbd.Marshal(doc)
	if err == nil {
		err = s.store.Write(name, data)
	}
	if err != nil {
		return errStatus(http.StatusInternalServerError, "save failed: %v", err)
	}
	return nil
}

type fileResponse struct {
	File     string        `json:"file"`
	Modified string        `json:"modified"`
	View     sbd.View      `json:"view"`
	Result   *sbd.OpResult `json:"result,omitempty"`
}

func (s *server) respondDoc(w http.ResponseWriter, name string, doc *sbd.Document, res *sbd.OpResult) {
	modified := ""
	if _, mod, err := s.store.Read(name); err == nil {
		modified = mod.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, fileResponse{File: name, Modified: modified, View: sbd.BuildView(doc), Result: res})
}

// ----- documents -------------------------------------------------------------

type fileInfo struct {
	File     string `json:"file"`
	DeckName string `json:"deck_name"`
	Modified string `json:"modified"`
	Error    string `json:"error,omitempty"`
}

func (s *server) listFiles(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.List()
	if err != nil {
		writeErr(w, err)
		return
	}
	files := []fileInfo{}
	for _, d := range docs {
		info := fileInfo{File: d.Name, Modified: d.Modified.Format(time.RFC3339)}
		if doc, err := s.load(d.Name); err == nil {
			info.DeckName = doc.Deck.Name
		} else {
			info.Error = err.Error()
		}
		files = append(files, info)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified > files[j].Modified })
	writeJSON(w, http.StatusOK, map[string]any{"dir": s.store.Location(), "files": files})
}

// freeName returns name, or name with a -2, -3 … suffix if that file exists.
func (s *server) freeName(name string) string {
	stem := strings.TrimSuffix(name, sbd.FileSuffix)
	for i := 1; ; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, sbd.FileSuffix)
		}
		if !s.store.Exists(candidate) {
			return candidate
		}
	}
}

type newFileRequest struct {
	DeckName string `json:"deck_name"`
	File     string `json:"file"`
}

func (req newFileRequest) filename(fallbackDeck string) (string, error) {
	name := strings.TrimSpace(req.File)
	if name == "" {
		deck := strings.TrimSpace(req.DeckName)
		if deck == "" {
			deck = fallbackDeck
		}
		return sbd.DefaultFilename(deck), nil
	}
	if !strings.HasSuffix(name, sbd.FileSuffix) {
		name = strings.TrimSuffix(name, ".json") + sbd.FileSuffix
	}
	if !validName(name) {
		return "", errStatus(http.StatusBadRequest, "invalid document name %q", name)
	}
	return name, nil
}

func (s *server) createFile(w http.ResponseWriter, r *http.Request) {
	var req newFileRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	name, err := req.filename("Untitled")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name = s.freeName(name)
	doc := sbd.NewDocument()
	if deck := strings.TrimSpace(req.DeckName); deck != "" {
		doc.Deck.Name = deck
	}
	if err := s.save(name, doc); err != nil {
		writeErr(w, err)
		return
	}
	s.respondDoc(w, name, doc, nil)
}

func (s *server) getFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	doc, err := s.load(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.respondDoc(w, name, doc, nil)
}

func (s *server) applyOp(w http.ResponseWriter, r *http.Request) {
	var op sbd.Op
	if err := readJSON(r, &op); err != nil {
		writeErr(w, err)
		return
	}
	name := r.PathValue("file")
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := sbd.Apply(doc, op)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.save(name, doc); err != nil {
		writeErr(w, err)
		return
	}
	s.respondDoc(w, name, doc, &res)
}

// copyFile is "save as": write the document under a new name.
func (s *server) copyFile(w http.ResponseWriter, r *http.Request) {
	var req newFileRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(r.PathValue("file"))
	if err != nil {
		writeErr(w, err)
		return
	}
	name, err := req.filename(doc.Deck.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	name = s.freeName(name)
	if err := s.save(name, doc); err != nil {
		writeErr(w, err)
		return
	}
	s.respondDoc(w, name, doc, nil)
}

func attachment(w http.ResponseWriter, filename, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	_, _ = w.Write(body)
}

func (s *server) download(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if err := checkName(name); err != nil {
		writeErr(w, err)
		return
	}
	data, _, err := s.store.Read(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	attachment(w, name, "application/json", data)
}

func reportMode(r *http.Request) (string, error) {
	switch mode := r.URL.Query().Get("mode"); mode {
	case "", sbd.ModeBase:
		return sbd.ModeBase, nil
	case sbd.ModePlay, sbd.ModeDraw:
		return mode, nil
	default:
		return "", errStatus(http.StatusBadRequest, "unknown report mode %q", mode)
	}
}

func (s *server) report(w http.ResponseWriter, r *http.Request) {
	mode, err := reportMode(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, err := s.load(r.PathValue("file"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "rows": sbd.BuildFrequency(doc.Archetypes, mode)})
}

func (s *server) reportCSV(w http.ResponseWriter, r *http.Request) {
	mode, err := reportMode(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, err := s.load(r.PathValue("file"))
	if err != nil {
		writeErr(w, err)
		return
	}
	csv := sbd.FrequencyCSV(sbd.BuildFrequency(doc.Archetypes, mode))
	attachment(w, "frequency-"+mode+".csv", "text/csv; charset=utf-8", []byte(csv))
}

func (s *server) resultsCSV(w http.ResponseWriter, r *http.Request) {
	doc, err := s.load(r.PathValue("file"))
	if err != nil {
		writeErr(w, err)
		return
	}
	attachment(w, "results.csv", "text/csv; charset=utf-8", []byte(sbd.ResultsCSV(doc.Results)))
}

// parseDecklist powers the import dialog's live preview.
func (s *server) parseDecklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	res := sbd.ParseDecklist(req.Text, req.Name)
	writeJSON(w, http.StatusOK, map[string]any{
		"main_count":  res.Deck.MainboardCount(),
		"main_unique": len(res.Deck.Mainboard),
		"side_count":  res.Deck.SideboardCount(),
		"side_unique": len(res.Deck.Sideboard),
		"unparsed":    res.Unparsed,
	})
}

// ----- card database & settings ----------------------------------------------

func (s *server) cards(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}
	writeJSON(w, http.StatusOK, s.db.Autocomplete(r.URL.Query().Get("q"), limit))
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	s.jobMu.Lock()
	job := s.job
	s.jobMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"card_source": config.LoadSettings().CardSource,
		"sources":     carddb.Sources,
		"db":          s.db.Status(),
		"job":         job,
		"dir":         s.store.Location(),
	})
}

func (s *server) setSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CardSource string `json:"card_source"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if _, ok := carddb.Sources[req.CardSource]; !ok {
		writeErr(w, errStatus(http.StatusBadRequest, "unknown card source %q", req.CardSource))
		return
	}
	if err := config.UpdateSettings(map[string]any{"card_source": req.CardSource}); err != nil {
		writeErr(w, errStatus(http.StatusInternalServerError, "save settings: %v", err))
		return
	}
	s.getSettings(w, r)
}

func (s *server) setJob(job cardJob) {
	s.jobMu.Lock()
	s.job = job
	s.jobMu.Unlock()
}

func (s *server) updateCardDB(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if _, ok := carddb.Sources[req.Source]; !ok {
		writeErr(w, errStatus(http.StatusBadRequest, "unknown card source %q", req.Source))
		return
	}
	s.jobMu.Lock()
	if s.job.Running {
		s.jobMu.Unlock()
		writeErr(w, errStatus(http.StatusConflict, "an update is already running"))
		return
	}
	s.job = cardJob{Running: true, Message: "Starting update…"}
	s.jobMu.Unlock()

	go func() {
		progress := func(msg string) { s.setJob(cardJob{Running: true, Message: msg}) }
		count, err := s.db.Update(req.Source, progress, nil)
		if err != nil {
			s.setJob(cardJob{Error: "Update failed: " + err.Error()})
			return
		}
		st := s.db.Status()
		_ = config.UpdateSettings(map[string]any{
			"card_source":       req.Source,
			"cardnames_updated": st.Updated,
			"cardnames_count":   count,
		})
		s.setJob(cardJob{Message: fmt.Sprintf("Card database updated: %d names.", count)})
	}()
	s.getSettings(w, r)
}

// ----- middleware --------------------------------------------------------------

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
