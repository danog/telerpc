package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// export writes rpc-db's JSON files into dir, as the PHP version did (v2, v3, v4): the same
// shapes and json_encode formatting. The rows are read in key order. log gets the warnings
// (unknown methods, errors without a code).
func export(ctx context.Context, st *Store, schemas *tlMethods, dir string, log io.Writer) error {
	errs, err := st.Errors(ctx)
	if err != nil {
		return err
	}
	descs, err := st.Descriptions(ctx)
	if err != nil {
		return err
	}
	botInvalid, err := st.BotMethodsInvalid(ctx)
	if err != nil {
		return err
	}
	layer, err := st.Layer(ctx)
	if err != nil {
		return err
	}
	if layer != schemas.layer {
		return fmt.Errorf("Layer mismatch, I have %d, but have errors for layer %d!", schemas.layer, layer)
	}
	w := func(name string, v any, pretty bool) error {
		b, err := phpJSON(v, pretty)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return os.WriteFile(filepath.Join(dir, name), b, 0o644)
	}
	okResult := func(result any, more ...any) *phpArray {
		a := newArray()
		a.Set("ok", true)
		a.Set("result", result)
		for i := 0; i < len(more); i += 2 {
			a.Set(more[i], more[i+1])
		}
		return a
	}

	// v2: per method, its errors with their descriptions (no 500s).
	desc := newArray()
	for _, d := range descs {
		desc.Set(d.Error, d.Description.String)
	}
	v2 := newArray()
	for _, e := range errs {
		if e.Code == 500 {
			continue
		}
		item := newArray()
		item.Set("error_code", e.Code)
		item.Set("error_message", e.Error)
		d, _ := desc.Get(e.Error)
		if d == nil {
			d = ""
		}
		item.Set("error_description", d)
		v2.Sub(e.Method).Append(item)
	}
	if err := w("v2.json", okResult(v2), false); err != nil {
		return err
	}

	// v3: per code and method, the sanitized errors; their descriptions.
	v3 := newArray()
	for _, e := range errs {
		s := sanitize(e.Error)
		v3.Sub(e.Code).Sub(e.Method).Set(s, s)
	}
	hr3 := newArray()
	for _, d := range descs {
		hr3.Set(sanitize(d.Error), strings.ReplaceAll(d.Description.String, " X ", " %d "))
	}
	if err := w("v3.json", okResult(v3, "human_result", hr3), false); err != nil {
		return err
	}

	// v4: per code and error, the methods; and the lists of methods by kind.
	r := newArray()
	seen := newArray()
	var botOnly, business []string
	unauthedDisallowed := map[string]bool{}
	methods := newArray()
	for _, e := range errs {
		s := sanitize(e.Error)
		m := r.Sub(e.Code).Sub(s)
		if !slices.Contains(arrayStrings(m), e.Method) {
			m.Append(e.Method)
		}
		seen.Set(s, true)
		if (s == "USER_BOT_REQUIRED" || s == "USER_BOT_INVALID") &&
			!slices.Contains(botOnly, e.Method) &&
			e.Method != "bots.setBotInfo" && e.Method != "bots.getBotInfo" {
			botOnly = append(botOnly, e.Method)
		}
		if s == "BUSINESS_CONNECTION_INVALID" {
			business = append(business, e.Method)
		}
		if s == "AUTH_KEY_UNREGISTERED" {
			unauthedDisallowed[e.Method] = true
		}
		if schemas.methods[e.Method] {
			methods.Set(e.Method, true)
		} else {
			fmt.Fprintf(log, "Unknown method %s!\n", e.Method)
		}
	}
	var unauthed []string
	for _, k := range methods.Keys() {
		if m := fmt.Sprint(k); !unauthedDisallowed[m] {
			unauthed = append(unauthed, m)
		}
	}
	for _, m := range []string{"auth.signUp", "auth.checkPassword", "account.getPassword"} {
		if !slices.Contains(unauthed, m) {
			unauthed = append(unauthed, m)
		}
	}
	slices.Sort(unauthed)
	slices.Sort(business)
	slices.Sort(botOnly)

	hr := newArray()
	for _, d := range descs {
		s := strings.ReplaceAll(d.Description.String, " X ", " %d ")
		if s != "" && !strings.ContainsAny(s[len(s)-1:], "?.!") {
			s += "."
		}
		hr.Set(sanitize(d.Error), s)
	}
	hr.Set("FLOOD_WAIT_%d", "Please wait %d seconds before repeating the action.")
	for _, k := range hr.Keys() {
		if seen.Has(k) {
			continue
		}
		code, ok := globalCodes.Get(k)
		if !ok {
			fmt.Fprintf(log, "Missing code for %v!\n", k)
			continue
		}
		r.Sub(code).Set(k, newArray())
	}

	userOnly := append(botInvalid, "account.resetPassword", "account.deleteAccount", "account.resetAuthorization", "auth.resetAuthorizations", "account.updateUsername")
	var unique []string
	for _, m := range userOnly {
		if !slices.Contains(unique, m) {
			unique = append(unique, m)
		}
	}
	userOnly = unique
	slices.Sort(userOnly)

	for _, f := range []struct {
		name   string
		v      any
		pretty bool
	}{
		{"v4.json", okResult(r, "human_result", hr), false},
		{"vdiff.json", okResult(r, "human_result", hr), true},
		{"bot.json", okResult(list(userOnly)), false},
		{"user_only_diff.json", okResult(list(userOnly)), true},
		{"bot_only_diff.json", okResult(list(botOnly)), true},
		{"business_diff.json", okResult(list(business)), true},
		{"unauthed_diff.json", okResult(list(unauthed)), true},
	} {
		if err := w(f.name, f.v, f.pretty); err != nil {
			return err
		}
	}

	hr.Set("UPDATE_APP_TO_LOGIN", "Please update your client to login.")
	core := newArray()
	core.Set("errors", r)
	core.Set("descriptions", hr)
	core.Set("user_only", list(userOnly))
	core.Set("bot_only", list(botOnly))
	core.Set("business_supported", list(business))
	core.Set("unauthed_allowed", list(unauthed))
	core.Set("layer", layer)
	return w("core.json", core, false)
}

// arrayStrings returns the string values of a PHP list.
func arrayStrings(a *phpArray) []string {
	out := make([]string, 0, a.Len())
	for _, k := range a.Keys() {
		v, _ := a.Get(k)
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
