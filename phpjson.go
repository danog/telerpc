package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// phpArray is a PHP array: ordered, with integer or string keys. The JSON files of rpc-db
// were written by PHP's json_encode, and their shape depends on PHP's rules: an array whose
// keys are 0, 1, 2... in order is a JSON list, any other is an object; a string key that is a
// canonical integer ("400") is an integer key.
type phpArray struct {
	keys []any // int64 or string
	vals map[any]any
	next int64
}

func newArray() *phpArray { return &phpArray{vals: map[any]any{}} }

var intKey = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)

func normKey(k any) any {
	switch k := k.(type) {
	case int:
		return int64(k)
	case int64:
		return k
	case string:
		if intKey.MatchString(k) {
			if n, err := strconv.ParseInt(k, 10, 64); err == nil {
				return n
			}
		}
		return k
	}
	panic(fmt.Sprintf("bad PHP array key %T", k))
}

func (a *phpArray) Len() int { return len(a.keys) }

func (a *phpArray) Get(k any) (any, bool) {
	v, ok := a.vals[normKey(k)]
	return v, ok
}

func (a *phpArray) Has(k any) bool {
	_, ok := a.vals[normKey(k)]
	return ok
}

// Set is $a[$k] = $v.
func (a *phpArray) Set(k, v any) {
	k = normKey(k)
	if _, ok := a.vals[k]; !ok {
		a.keys = append(a.keys, k)
		if n, ok := k.(int64); ok && n >= a.next {
			a.next = n + 1
		}
	}
	a.vals[k] = v
}

// Append is $a[] = $v.
func (a *phpArray) Append(v any) { a.Set(a.next, v) }

// Sub returns $a[$k], an array, creating it if needed (as $a[$k][...] = ... does).
func (a *phpArray) Sub(k any) *phpArray {
	if v, ok := a.Get(k); ok {
		return v.(*phpArray)
	}
	s := newArray()
	a.Set(k, s)
	return s
}

// Keys returns the keys in order.
func (a *phpArray) Keys() []any { return a.keys }

func (a *phpArray) isList() bool {
	for i, k := range a.keys {
		if n, ok := k.(int64); !ok || n != int64(i) {
			return false
		}
	}
	return true
}

// list makes a PHP list (array with keys 0, 1, 2...) of strings.
func list(items []string) *phpArray {
	a := newArray()
	for _, s := range items {
		a.Append(s)
	}
	return a
}

// phpJSON encodes v as PHP's json_encode does with no flags (pretty: JSON_PRETTY_PRINT):
// "/" escaped as "\/", every non-ASCII character as \uXXXX (lowercase hex, UTF-16).
// Values: *phpArray, string, int, int64, bool, nil. Like json_encode, it fails on invalid UTF-8.
func phpJSON(v any, pretty bool) ([]byte, error) {
	var b bytes.Buffer
	if err := encodePHP(&b, v, pretty, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encodePHP(b *bytes.Buffer, v any, pretty bool, depth int) error {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case string:
		return encodeString(b, v)
	case *phpArray:
		isList := v.isList()
		open, close := byte('{'), byte('}')
		if isList {
			open, close = '[', ']'
		}
		b.WriteByte(open)
		if len(v.keys) == 0 {
			b.WriteByte(close)
			return nil
		}
		for i, k := range v.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if pretty {
				newline(b, depth+1)
			}
			if !isList {
				ks, ok := k.(string)
				if !ok {
					ks = strconv.FormatInt(k.(int64), 10)
				}
				if err := encodeString(b, ks); err != nil {
					return err
				}
				b.WriteByte(':')
				if pretty {
					b.WriteByte(' ')
				}
			}
			if err := encodePHP(b, v.vals[k], pretty, depth+1); err != nil {
				return err
			}
		}
		if pretty {
			newline(b, depth)
		}
		b.WriteByte(close)
	default:
		return fmt.Errorf("phpJSON: unsupported %T", v)
	}
	return nil
}

func newline(b *bytes.Buffer, depth int) {
	b.WriteByte('\n')
	for range depth {
		b.WriteString("    ")
	}
}

const hexDigits = "0123456789abcdef"

func encodeString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("phpJSON: invalid UTF-8 in %q", s)
	}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '/':
			b.WriteString(`\/`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r < 0x20:
				writeU(b, r)
			case r < 0x80:
				b.WriteRune(r)
			case r < 0x10000:
				writeU(b, r)
			default:
				r -= 0x10000
				writeU(b, 0xd800+(r>>10))
				writeU(b, 0xdc00+(r&0x3ff))
			}
		}
	}
	b.WriteByte('"')
	return nil
}

func writeU(b *bytes.Buffer, r rune) {
	b.WriteString(`\u`)
	b.WriteByte(hexDigits[r>>12&0xf])
	b.WriteByte(hexDigits[r>>8&0xf])
	b.WriteByte(hexDigits[r>>4&0xf])
	b.WriteByte(hexDigits[r&0xf])
}
