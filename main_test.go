package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T, seed bool) *Store {
	t.Helper()
	st, err := CreateStore(filepath.Join(t.TempDir(), "telerpc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if seed {
		sql, err := os.ReadFile("testdata/fixture.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// The files written from testdata/fixture.sql are byte for byte the PHP version's
// (testdata/export, written by its v2, v3 and v4 from the same rows in MariaDB).
func TestExportMatchesPHP(t *testing.T) {
	st := newStore(t, true)
	schemas, err := loadSchemas(context.Background(), "testdata", 233)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var warnings bytes.Buffer
	if err := export(context.Background(), st, schemas, dir, &warnings); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/export/warnings.txt")
	if err != nil {
		t.Fatal(err)
	}
	if warnings.String() != string(want) {
		t.Errorf("warnings:\n%s\nwant:\n%s", warnings.String(), want)
	}
	golden, err := filepath.Glob("testdata/export/*.json")
	if err != nil || len(golden) != 10 {
		t.Fatalf("golden files: %v %v", golden, err)
	}
	for _, g := range golden {
		want, _ := os.ReadFile(g)
		got, err := os.ReadFile(filepath.Join(dir, filepath.Base(g)))
		if err != nil {
			t.Error(err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs:\ngot  %s\nwant %s", filepath.Base(g), got, want)
		}
	}
}

func TestExportLayerMismatch(t *testing.T) {
	st := newStore(t, true)
	if err := st.SetLayer(context.Background(), 234); err != nil {
		t.Fatal(err)
	}
	schemas, err := loadSchemas(context.Background(), "testdata", 233)
	if err != nil {
		t.Fatal(err)
	}
	if err := export(context.Background(), st, schemas, t.TempDir(), io.Discard); err == nil || !strings.Contains(err.Error(), "Layer mismatch") {
		t.Errorf("got %v, want a layer mismatch", err)
	}
	if _, err := loadSchemas(context.Background(), "testdata", 234); err == nil {
		t.Error("no schema for layer 234: want an error")
	}
}

func TestPHPJSON(t *testing.T) {
	obj := newArray()
	obj.Set("400", list([]string{"a/b", "é😀", "\"\\\n\x01\x7f"}))
	obj.Set("x", newArray())
	obj.Set(-503, nil)
	gaps := newArray()
	gaps.Set(1, true)
	for _, c := range []struct {
		v      any
		pretty bool
		want   string
	}{
		{obj, false, `{"400":["a\/b","\u00e9\ud83d\ude00","\"\\\n\u0001` + "\x7f" + `"],"x":[],"-503":null}`},
		{obj, true, "{\n    \"400\": [\n        \"a\\/b\",\n        \"\\u00e9\\ud83d\\ude00\",\n        \"\\\"\\\\\\n\\u0001\x7f\"\n    ],\n    \"x\": [],\n    \"-503\": null\n}"},
		{gaps, false, `{"1":true}`},
		{list(nil), true, `[]`},
		{int64(-5), false, `-5`},
	} {
		got, err := phpJSON(c.v, c.pretty)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
	if _, err := phpJSON("\xff", false); err == nil {
		t.Error("invalid UTF-8: want an error, like json_encode")
	}
	// A canonical integer string is an integer key: a PHP list.
	a := newArray()
	a.Set("0", "x")
	a.Append("y")
	if got, _ := phpJSON(a, false); string(got) != `["x","y"]` {
		t.Errorf("got %s", got)
	}
}

func TestSanitize(t *testing.T) {
	for in, want := range map[string]string{
		"FLOOD_WAIT_5":            "FLOOD_WAIT_%d",
		"FLOOD_WAIT_X":            "FLOOD_WAIT_%d",
		"SLOWMODE_WAIT_X":         "SLOWMODE_WAIT_%d",
		"FILE_PART_12_MISSING":    "FILE_PART_%d_MISSING",
		"FILE_PART_X_MISSING":     "FILE_PART_%d_MISSING",
		"TAKEOUT_INIT_DELAY_5MIN": "TAKEOUT_INIT_DELAY_%dMIN",
		"WAIT_XMIN":               "WAIT_%dMIN",
		"AUTH_XYZ":                "AUTH_%dYZ",
		"PEER_ID_INVALID":         "PEER_ID_INVALID",
		`QUOTE_12"`:               `QUOTE_%d"`,
		"INTERDC_2_CALL_ERROR":    "INTERDC_%d_CALL_ERROR",
	} {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsBad(t *testing.T) {
	for _, c := range []struct {
		err, method string
		bad         bool
	}{
		{"PEER_ID_INVALID", "messages.sendMessage", false},
		{"FLOOD_WAIT_5", "messages.sendMessage", true},
		{"PHONE_MIGRATE_2", "auth.sendCode", true},
		{"SESSION_REVOKED", "x.y", true},
		{"FILE_PART_12_MISSING", "upload.saveFilePart", true},
		{"PEER_ID_INVALID", "messages.x\n", false},
		{"PEER_ID_INVALID", "messages.x\n\n", true},
		{"PEER_ID_INVALID", "messages.x y", true},
		{"PEER_ID_INVALID", "123", true},
		{"PEER_ID_INVALID", "1e5", true},
		{"Timeout", "messages.getBotCallbackAnswer", false},
		{"Timeout", "Messages.GetInlineBotResults", false},
		{"Timeout", "messages.getHistory", true},
		{"BOT_MISSING", "messages.uploadMedia", true},
		{"BOT_MISSING", "messages.sendMessage", false},
	} {
		if got := isBad(c.err, 400, c.method); got != c.bad {
			t.Errorf("isBad(%q, %q) = %v, want %v", c.err, c.method, got, c.bad)
		}
	}
}

func TestNumbers(t *testing.T) {
	for _, c := range []struct {
		s          string
		numeric    bool
		php, mysql int64
	}{
		{"400", true, 400, 400},
		{" 400 ", true, 400, 400},
		{"-503", true, -503, -503},
		{"1e3", true, 1000, 1000},
		{"4.7", true, 4, 5},
		{"-4.5", true, -4, -5},
		{".5", true, 0, 1},
		{"abc", false, 0, 0},
		{"0x1A", false, 0, 0},
		{"", false, 0, 0},
		{"1 2", false, 0, 0},
	} {
		if got := isNumeric(c.s); got != c.numeric {
			t.Errorf("isNumeric(%q) = %v", c.s, got)
		}
		if !c.numeric {
			continue
		}
		if got := phpInt(c.s); got != c.php {
			t.Errorf("phpInt(%q) = %d, want %d", c.s, got, c.php)
		}
		if got := mysqlInt(c.s); got != c.mysql {
			t.Errorf("mysqlInt(%q) = %d, want %d", c.s, got, c.mysql)
		}
	}
}

func TestParseTL(t *testing.T) {
	m := map[string]bool{}
	layer, err := parseTL(strings.NewReader("a#1 = A;\n---functions---\n// comment\nfoo.bar#12 x:int = A;\n---types---\nb#2 = B;\n---functions---\nbaz {X:Type} = X;\n// LAYER 42\n"), m)
	if err != nil {
		t.Fatal(err)
	}
	if layer != 42 || len(m) != 2 || !m["foo.bar"] || !m["baz"] {
		t.Errorf("layer %d, methods %v", layer, m)
	}
}

// The answers are the PHP version's, for the same requests on the same rows.
func TestHandler(t *testing.T) {
	st := newStore(t, true)
	h := &Handler{store: st, auth: "secret_tok"}
	usageBody := `{"ok":false,"description":"API for reporting Telegram RPC errors. For localized errors see https:\/\/rpc.madelineproto.xyz, to report a new error use the ` + "`code`, `method` and `error`" + ` GET\/POST parameters. Source code at https:\/\/github.com\/danog\/telerpc."}`
	get := func(q string) *http.Request { return httptest.NewRequest(http.MethodGet, "/"+q, nil) }
	form := func(q string, v url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/"+q, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	multi := func(v map[string]string) *http.Request {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for k, x := range v {
			w.WriteField(k, x)
		}
		w.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &b)
		r.Header.Set("Content-Type", w.FormDataContentType())
		return r
	}
	for _, c := range []struct {
		name   string
		r      *http.Request
		status int
		body   string
	}{
		{"usage", get(""), 400, usageBody},
		{"bad error", get("anything?error=FLOOD_WAIT_5&code=420&method=messages.sendMessage"), 400, usageBody},
		{"known", get("?error=CHAT_ID_INVALID&code=400&method=messages.getChats"), 200, `{"ok":true,"result":"The provided chat id is invalid."}`},
		{"other case", get("?error=chat_id_invalid&code=400&method=messages.getChats"), 200, `{"ok":true,"result":"The provided chat id is invalid."}`},
		{"new", get("?error=NEW_ERROR_42&code=400&method=messages.sendMessage"), 404, `{"ok":false,"description":"No description"}`},
		{"null description", get("?error=INTERNAL_THING&code=500&method=messages.sendMessage"), 200, `{"ok":true,"result":null}`},
		{"auth needed: description only", get("?error=AUTH_KEY_UNREGISTERED&code=401&method=messages.newThing"), 200, `{"ok":true,"result":"The specified authorization key is not registered in the system."}`},
		{"auth needed: wrong token", get("?error=USER_BOT_INVALID&code=400&method=bots.x&auth=wrong"), 404, `{"ok":false,"description":"No description"}`},
		{"auth needed: with token", get("?error=AUTH_KEY_UNREGISTERED&code=401&method=messages.newThing&auth=secret_tok"), 200, `{"ok":true,"result":"The specified authorization key is not registered in the system."}`},
		{"bot method invalid", get("?error=BOT_METHOD_INVALID&code=400&method=messages.userThing&auth=secret_tok"), 200, `{"ok":true,"result":true}`},
		{"code as error", get("?error=400&code=400&method=foo.bar"), 200, `{"ok":true,"result":true}`},
		{"setLayer without token", get("?setLayer=234"), 403, `{"ok":false,"description":"Invalid auth token"}`},
		{"setLayer not a number", get("?setLayer=abc&auth=secret_tok"), 400, `{"ok":false,"description":"Layer must be a number"}`},
		{"setLayer negative", get("?setLayer=-3&auth=secret_tok"), 400, `{"ok":false,"description":"Invalid layer number"}`},
		{"setLayer", get("?setLayer=233.9&auth=secret_tok"), 200, `{"ok":true,"result":{"layer":233}}`},
		{"cleanup wrong token", get("?cleanup&auth=wrong"), 403, `{"ok":false,"description":"Invalid auth token"}`},
		{"form", form("", url.Values{"error": {"FORM_ERROR"}, "code": {"400"}, "method": {"messages.sendMessage"}}), 404, `{"ok":false,"description":"No description"}`},
		{"empty query wins over form", form("?error=", url.Values{"error": {"FORM_ERROR"}, "code": {"400"}, "method": {"a.b"}}), 400, usageBody},
		{"multipart", multi(map[string]string{"error": "MULTI_ERROR", "code": "403", "method": "channels.joinChannel"}), 404, `{"ok":false,"description":"No description"}`},
		{"code 0", get("?error=X&code=0&method=a.b"), 400, usageBody},
		{"error 0", get("?error=0&code=400&method=a.b"), 400, usageBody},
		{"code not numeric", get("?error=X&code=abc&method=a.b"), 400, usageBody},
		{"numeric method", get("?error=X&code=400&method=123"), 400, usageBody},
		{"method with a final newline", get("?error=NEWLINE_ERR&code=400&method=messages.x%0A"), 404, `{"ok":false,"description":"No description"}`},
		{"timeout", get("?error=Timeout&code=-503&method=messages.getBotCallbackAnswer"), 200, `{"ok":true,"result":"Timeout while fetching data"}`},
		{"too long", get("?error=" + strings.Repeat("A", 300) + "&code=400&method=messages.a"), 500, ""},
		{"options", httptest.NewRequest(http.MethodOptions, "/", nil), 400, usageBody},
		{"cleanup", get("?cleanup=&auth=secret_tok"), 200, `{"ok":true,"result":true}`},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, c.r)
		if w.Code != c.status || (c.body != "" && w.Body.String() != c.body) {
			t.Errorf("%s: %d %s\nwant %d %s", c.name, w.Code, w.Body, c.status, c.body)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: headers %v", c.name, w.Header())
		}
	}

	ctx := context.Background()
	rows, err := st.Errors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	have := map[errorRow]bool{}
	for _, r := range rows {
		have[r] = true
	}
	for _, r := range []errorRow{
		{"NEW_ERROR_%d", "messages.sendMessage", 400},
		{"FORM_ERROR", "messages.sendMessage", 400},
		{"MULTI_ERROR", "channels.joinChannel", 403},
		{"NEWLINE_ERR", "messages.x\n", 400},
		{"chat_id_invalid", "messages.getChats", 400}, // REPLACE took the new case
	} {
		if !have[r] {
			t.Errorf("missing %v", r)
		}
	}
	for _, r := range []errorRow{
		{"CHAT_ID_INVALID", "messages.getChats", 400},
		{"AUTH_KEY_UNREGISTERED", "messages.newThing", 401}, // cleanup
		{"AUTH_KEY_UNREGISTERED", "messages.getHistory", 401},
	} {
		if have[r] {
			t.Errorf("unexpected %v", r)
		}
	}
	if m, _ := st.BotMethodsInvalid(ctx); len(m) != 0 {
		t.Errorf("bot_method_invalid after cleanup: %v", m)
	}
	if l, _ := st.Layer(ctx); l != 233 {
		t.Errorf("layer %d", l)
	}
}

func TestMaintain(t *testing.T) {
	st := newStore(t, true)
	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO errors VALUES ('USERNAME_X_TAKEN', 'account.updateUsername', 400)",
		"INSERT INTO errors VALUES ('STRANGE_%d', 'STRANGE_5', 400)",
		"INSERT INTO errors VALUES ('RPC_CALL_FAIL', 'messages.sendMessage', 400)",
		"INSERT INTO errors VALUES ('NO_DESC', 'messages.sendMessage', 400)",
		"INSERT INTO errors VALUES ('DROP_ME', 'messages.sendMessage', 400)",
		"INSERT INTO errors VALUES ('KEEP_UNDESCRIBED', 'messages.sendMessage', 400)",
		"UPDATE error_descriptions SET description = 'See [this](/api/x).' WHERE error = 'CHAT_ID_INVALID'",
	} {
		if err := st.exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	schemas, err := loadSchemas(ctx, "testdata", 233)
	if err != nil {
		t.Fatal(err)
	}
	// Asked in key order, each a description then "OK?": DROP_ME (dropped), INTERNAL_THING
	// (NULL description; skipped), KEEP_UNDESCRIBED (refused), NO_DESC (described); then
	// USER_BOT_INVALID, USERNAME_%d_TAKEN and the global errors without a description get
	// the end of the input, and are skipped.
	in := strings.NewReader("d\ny\n\n\nnope\nn\nNo description yet.\ny\n")
	var out bytes.Buffer
	if err := maintain(ctx, st, schemas, t.TempDir(), in, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"USERNAME_X_TAKEN => USERNAME_%d_TAKEN\n",
		"Delete strange STRANGE_%d\n",
		"Delete bad RPC_CALL_FAIL for messages.sendMessage\n",
		"Insert description for DROP_ME (messages.sendMessage): ",
		"Delete DROP_ME\n",
		"Delete disallowed ORPHAN_ERROR\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	rows, _ := st.Errors(ctx)
	errs := map[string]bool{}
	for _, r := range rows {
		errs[r.Error] = true
	}
	for e, want := range map[string]bool{"USERNAME_%d_TAKEN": true, "USERNAME_X_TAKEN": false, "STRANGE_%d": false, "RPC_CALL_FAIL": false, "DROP_ME": false, "KEEP_UNDESCRIBED": true, "NO_DESC": true} {
		if errs[e] != want {
			t.Errorf("errors has %s: %v, want %v", e, errs[e], want)
		}
	}
	if d, _, _ := st.Description(ctx, "NO_DESC"); d.String != "No description yet." {
		t.Errorf("NO_DESC: %v", d)
	}
	if _, found, _ := st.Description(ctx, "KEEP_UNDESCRIBED"); found {
		t.Error("KEEP_UNDESCRIBED: refused, should have no description")
	}
	if d, _, _ := st.Description(ctx, "CHAT_ID_INVALID"); d.String != "See [this](https://core.telegram.org/api/x)." {
		t.Errorf("CHAT_ID_INVALID: %v", d)
	}
	if _, found, _ := st.Description(ctx, "ORPHAN_ERROR"); found {
		t.Error("ORPHAN_ERROR: not allowed, should be deleted")
	}
}

// verify's hashes are those of `mariadb -N -B` output: tab-separated, NULL as NULL.
func TestSums(t *testing.T) {
	st := newStore(t, false)
	ctx := context.Background()
	if err := st.exec(ctx, "INSERT INTO error_descriptions VALUES ('B', NULL), ('A', 'tab\there'), ('a2', 'x')"); err != nil {
		t.Fatal(err)
	}
	sums, err := st.Sums(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("A\ttab\\there\nB\tNULL\na2\tx\n"))
	for _, s := range sums {
		if s.Table == "error_descriptions" && (s.Rows != 3 || s.Hash != hex.EncodeToString(h[:])) {
			t.Errorf("%v", s)
		}
	}
	if err := st.exec(ctx, "INSERT INTO error_descriptions VALUES ('b', 'case')"); err == nil {
		t.Error("'b' and 'B' are the same key, as in MariaDB")
	}
}

func TestOpenStoreRefusesMissing(t *testing.T) {
	if _, err := OpenStore(filepath.Join(t.TempDir(), "telerpc.db")); err == nil {
		t.Error("want an error for a missing database")
	}
}
