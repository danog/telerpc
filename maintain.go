package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// maintain is the PHP version's command line (php server.php): it cleans up the database,
// asks for the descriptions it lacks, then exports rpc-db's files (see export). Prompts and
// answers go through in and out.
func maintain(ctx context.Context, st *Store, schemas *tlMethods, dir string, in io.Reader, out io.Writer) error {
	ask := prompter(in, out)

	// Errors with numbers in them become their sanitized form; "errors" whose method is the
	// error itself are deleted.
	byError, order, err := errorMethods(ctx, st)
	if err != nil {
		return err
	}
	for _, e := range order {
		methods := byError[e]
		fixed := sanitize(e)
		if fixed != e {
			for _, m := range methods {
				if err := st.exec(ctx, "UPDATE OR REPLACE errors SET error = ? WHERE error = ? AND method = ?", fixed, e, m); err != nil {
					return err
				}
				if err := st.exec(ctx, "UPDATE OR REPLACE error_descriptions SET error = ? WHERE error = ?", fixed, e); err != nil {
					return err
				}
				if sanitize(m) == fixed {
					if err := st.exec(ctx, "DELETE FROM errors WHERE error = ? AND method = ?", fixed, fixed); err != nil {
						return err
					}
					fmt.Fprintf(out, "Delete strange %s\n", e)
				}
			}
			fmt.Fprintf(out, "%s => %s\n", e, fixed)
		}
		for _, m := range methods {
			if sanitize(m) == fixed {
				if err := st.exec(ctx, "DELETE FROM errors WHERE error = ? AND method = ?", fixed, m); err != nil {
					return err
				}
				fmt.Fprintf(out, "Delete strange %s\n", e)
			}
		}
	}

	// The errors of the methods, then the global ones (replacing an error of the same name):
	// bad reports are deleted, and each remaining error without a description is asked for.
	byError, order, err = errorMethods(ctx, st)
	if err != nil {
		return err
	}
	global := map[string]bool{}
	for _, k := range globalCodes.Keys() {
		g := fmt.Sprint(k)
		global[g] = true
		if _, ok := byError[g]; !ok {
			order = append(order, g)
		}
	}
	allowed := map[string]bool{}
	for _, e := range order {
		var methods []string
		if global[e] {
			allowed[e] = true
		} else {
			methods = byError[e]
		}
		anyOK := false
		for _, m := range methods {
			if isBad(e, 0, m) {
				if err := st.exec(ctx, "DELETE FROM errors WHERE error = ? AND method = ?", e, m); err != nil {
					return err
				}
				fmt.Fprintf(out, "Delete bad %s for %s\n", e, m)
				continue
			}
			anyOK = true
		}
		if !anyOK && len(methods) > 0 {
			continue
		}
		allowed[e] = true

		desc, _, err := st.Description(ctx, e)
		if err != nil {
			return err
		}
		// PHP's !$description: "" and "0" are missing.
		if desc.String != "" && desc.String != "0" {
			continue
		}
		d := ask(fmt.Sprintf("Insert description for %s (%s): ", e, strings.Join(methods, ", ")))
		if strings.Contains(ask(fmt.Sprintf("%s - %s OK? ", e, d)), "n") || d == "" {
			continue
		}
		if d == "drop" || d == "delete" || d == "d" {
			if err := st.exec(ctx, "DELETE FROM errors WHERE error = ?", e); err != nil {
				return err
			}
			fmt.Fprintf(out, "Delete %s\n", e)
		} else if err := st.exec(ctx, "REPLACE INTO error_descriptions (error, description) VALUES (?, ?)", e, d); err != nil {
			return err
		}
	}

	// Descriptions of errors no longer allowed go, with their errors; the others get absolute
	// links to core.telegram.org.
	descs, err := st.Descriptions(ctx)
	if err != nil {
		return err
	}
	for _, d := range descs {
		if !allowed[d.Error] {
			if err := st.exec(ctx, "DELETE FROM errors WHERE error = ?", d.Error); err != nil {
				return err
			}
			if err := st.exec(ctx, "DELETE FROM error_descriptions WHERE error = ?", d.Error); err != nil {
				return err
			}
			fmt.Fprintf(out, "Delete disallowed %s\n", d.Error)
			continue
		}
		if fixed := strings.ReplaceAll(d.Description.String, "](/", "](https://core.telegram.org/"); fixed != d.Description.String {
			if err := st.exec(ctx, "UPDATE error_descriptions SET description = ? WHERE description = ?", fixed, d.Description.String); err != nil {
				return err
			}
		}
	}

	return export(ctx, st, schemas, dir, out)
}

// errorMethods returns the methods of each error, and the errors in key order.
func errorMethods(ctx context.Context, st *Store) (map[string][]string, []string, error) {
	rows, err := st.Errors(ctx)
	if err != nil {
		return nil, nil, err
	}
	by := map[string][]string{}
	var order []string
	for _, r := range rows {
		if _, ok := by[r.Error]; !ok {
			order = append(order, r.Error)
		}
		by[r.Error] = append(by[r.Error], r.Method)
	}
	return by, order, nil
}

// prompter is readline: it prints the prompt and returns the line typed ("" at the end of input).
func prompter(in io.Reader, out io.Writer) func(string) string {
	sc := bufio.NewScanner(in)
	return func(prompt string) string {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			return ""
		}
		return strings.TrimRight(sc.Text(), "\r")
	}
}
