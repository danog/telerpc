package main

import (
	"crypto/subtle"
	"log"
	"net/http"
	"slices"
	"unicode/utf8"
)

const usage = "API for reporting Telegram RPC errors. For localized errors see https://rpc.madelineproto.xyz, to report a new error use the `code`, `method` and `error` GET/POST parameters. Source code at https://github.com/danog/telerpc."

// Handler is the reporting API: one endpoint, any path and method.
type Handler struct {
	store *Store
	auth  string
}

// params reads the parameters as the PHP version did: from the query string, else from the
// body (urlencoded or multipart form). A parameter present but empty is "" (not missing).
type params struct {
	r *http.Request
}

func (p params) get(name string) (string, bool) {
	if q := p.r.URL.Query(); q.Has(name) {
		return q.Get(name), true
	}
	if p.r.PostForm != nil && p.r.PostForm.Has(name) {
		return p.r.PostForm.Get(name), true
	}
	if p.r.MultipartForm != nil {
		if v, ok := p.r.MultipartForm.Value[name]; ok && len(v) > 0 {
			return v[0], true
		}
	}
	return "", false
}

// truthy is PHP's truthiness of a string parameter: missing, "" and "0" are false.
func truthy(v string, ok bool) bool { return ok && v != "" && v != "0" }

func (h *Handler) authorized(auth string, ok bool) bool {
	return ok && subtle.ConstantTimeCompare([]byte(auth), []byte(h.auth)) == 1
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil && err != http.ErrNotMultipart {
		reply(w, http.StatusBadRequest, false, "Invalid request body")
		return
	}
	p := params{r}
	errName, errOK := p.get("error")
	code, codeOK := p.get("code")
	method, methodOK := p.get("method")
	auth, authOK := p.get("auth")
	ctx := r.Context()

	if setLayer, ok := p.get("setLayer"); ok {
		if !h.authorized(auth, authOK) {
			reply(w, http.StatusForbidden, false, "Invalid auth token")
			return
		}
		if !isNumeric(setLayer) {
			reply(w, http.StatusBadRequest, false, "Layer must be a number")
			return
		}
		layer := phpInt(setLayer)
		if layer <= 0 {
			reply(w, http.StatusBadRequest, false, "Invalid layer number")
			return
		}
		if err := h.store.SetLayer(ctx, layer); err != nil {
			serverError(w, err)
			return
		}
		a := newArray()
		a.Set("layer", layer)
		reply(w, http.StatusOK, true, a)
		return
	}
	if _, ok := p.get("cleanup"); ok {
		if !h.authorized(auth, authOK) {
			reply(w, http.StatusForbidden, false, "Invalid auth token")
			return
		}
		if err := h.store.Cleanup(ctx); err != nil {
			serverError(w, err)
			return
		}
		reply(w, http.StatusOK, true, true)
		return
	}
	if !truthy(errName, errOK) || !truthy(code, codeOK) || !truthy(method, methodOK) ||
		!isNumeric(code) || isBad(errName, phpInt(code), method) {
		reply(w, http.StatusBadRequest, false, usage)
		return
	}
	// MariaDB refused what isn't UTF-8 (utf8mb4 columns).
	if !utf8.ValidString(errName) || !utf8.ValidString(method) {
		reply(w, http.StatusBadRequest, false, "Invalid UTF-8")
		return
	}
	if slices.Contains(errorsRequireAuth, errName) && !h.authorized(auth, authOK) {
		h.description(w, r, errName)
		return
	}
	errName = sanitize(errName)
	var err error
	switch {
	case errName == code:
		err = h.store.RecordCode(ctx, mysqlInt(code))
	case errName == "BOT_METHOD_INVALID":
		err = h.store.RecordBotMethodInvalid(ctx, method)
	default:
		if err = h.store.Record(ctx, errName, method, mysqlInt(code)); err == nil {
			h.description(w, r, errName)
			return
		}
	}
	if err != nil {
		serverError(w, err)
		return
	}
	reply(w, http.StatusOK, true, true)
}

// description answers with an error's description (null if recorded as NULL), or 404.
func (h *Handler) description(w http.ResponseWriter, r *http.Request, errName string) {
	desc, found, err := h.store.Description(r.Context(), errName)
	switch {
	case err != nil:
		serverError(w, err)
	case !found:
		reply(w, http.StatusNotFound, false, "No description")
	case !desc.Valid:
		reply(w, http.StatusOK, true, nil)
	default:
		reply(w, http.StatusOK, true, desc.String)
	}
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("database: %v", err)
	reply(w, http.StatusInternalServerError, false, err.Error())
}

// reply writes {"ok": true, "result": v} or {"ok": false, "description": v}, as json_encode does.
func reply(w http.ResponseWriter, status int, ok bool, v any) {
	a := newArray()
	a.Set("ok", ok)
	if ok {
		a.Set("result", v)
	} else {
		a.Set("description", v)
	}
	body, err := phpJSON(a, false)
	if err != nil {
		// Only invalid UTF-8 can fail, and it's checked before anything is echoed.
		status, body = http.StatusInternalServerError, []byte(`{"ok":false,"description":"Internal error"}`)
	}
	hd := w.Header()
	hd.Set("Content-Type", "application/json")
	hd.Set("Access-Control-Allow-Origin", "*")
	hd.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	hd.Set("Access-Control-Expose-Headers", "Content-Length,Content-Type,Date,Server,Connection")
	w.WriteHeader(status)
	w.Write(body)
}
