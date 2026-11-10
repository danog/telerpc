package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// schemaURL is where MadelineProto keeps its TL schemas; %s is the file name.
const schemaURL = "https://raw.githubusercontent.com/danog/MadelineProto/v8/src/%s"

// tlMethods are the methods of the API and MTProto schemas, and the API layer.
type tlMethods struct {
	layer   int64
	methods map[string]bool
}

var layerLine = regexp.MustCompile(`^//\s*LAYER\s+(\d+)\s*$`)

// parseTL adds the methods of a TL schema to m, and returns its layer (0 if it has none).
func parseTL(r io.Reader, m map[string]bool) (int64, error) {
	var layer int64
	functions := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if g := layerLine.FindStringSubmatch(line); g != nil {
			layer, _ = strconv.ParseInt(g[1], 10, 64)
			continue
		}
		switch {
		case line == "" || strings.HasPrefix(line, "//"):
			continue
		case line == "---functions---":
			functions = true
			continue
		case line == "---types---":
			functions = false
			continue
		}
		if !functions {
			continue
		}
		name, _, _ := strings.Cut(line, " ")
		name, _, _ = strings.Cut(name, "#")
		if name != "" {
			m[name] = true
		}
	}
	return layer, sc.Err()
}

// loadSchemas reads the API schema of the given layer and the MTProto one, from dir if it's
// set (MadelineProto's src directory, say), else from MadelineProto's repository. A missing
// schema for the layer means MadelineProto isn't on that layer (the PHP version checked that
// its own layer was the database's).
func loadSchemas(ctx context.Context, dir string, layer int64) (*tlMethods, error) {
	m := &tlMethods{methods: map[string]bool{}}
	for _, name := range []string{fmt.Sprintf("TL_telegram_v%d.tl", layer), "TL_mtproto_v1.tl"} {
		body, err := readSchema(ctx, dir, name)
		if err != nil {
			return nil, err
		}
		l, err := parseTL(strings.NewReader(body), m.methods)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if strings.HasPrefix(name, "TL_telegram_") {
			if l != layer {
				return nil, fmt.Errorf("Layer mismatch, %s is layer %d, but have errors for layer %d!", name, l, layer)
			}
			m.layer = l
		}
	}
	return m, nil
}

func readSchema(ctx context.Context, dir, name string) (string, error) {
	if dir != "" {
		b, err := os.ReadFile(filepath.Join(dir, name))
		return string(b), err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	url := fmt.Sprintf(schemaURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s: not found: MadelineProto isn't on this layer yet", url)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", errors.New(url + ": empty")
	}
	return string(b), nil
}
