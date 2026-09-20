// Package snapshot implements the Python harness HMAC wire format. Authentication
// alone does not establish graph validity or authorize a lifecycle operation.
package snapshot

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Signer holds a private copy of a 256-bit key. Bootstrap and broker callers must
// construct separate signers from their respective configured keys.
type Signer struct {
	key        [32]byte
	configured bool
}

func NewSigner(key string) (*Signer, error) {
	if len(key) != 64 || strings.ToLower(key) != key {
		return nil, errors.New("key must be 64 lowercase hexadecimal characters")
	}
	b, err := hex.DecodeString(key)
	if err != nil {
		return nil, errors.New("invalid signing key")
	}
	s := &Signer{configured: true}
	copy(s.key[:], b)
	return s, nil
}

// decode rejects ambiguous JSON rather than allowing duplicate keys or Unicode
// replacement to change the authenticated meaning. The current graph contracts
// contain integers only; floats are rejected instead of rounded or reinterpreted.
func decode(data []byte) (map[string]any, error) {
	if len(data) > 4*1024*1024 || !utf8.Valid(data) {
		return nil, errors.New("invalid JSON size or UTF-8")
	}
	// encoding/json replaces lone surrogates. Reject them before decoding.
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return nil, errors.New("invalid Unicode escape")
		}
		n, e := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if e != nil {
			return nil, e
		}
		i += 4
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(data) || string(data[i+1:i+3]) != "\\u" {
				return nil, errors.New("unpaired surrogate")
			}
			low, e := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return nil, errors.New("unpaired surrogate")
			}
			i += 6
		} else if n >= 0xDC00 && n <= 0xDFFF {
			return nil, errors.New("unpaired surrogate")
		}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("expected JSON object")
	}
	return m, nil
}
func value(d *json.Decoder, depth int) (any, error) {
	if depth > 100 {
		return nil, errors.New("JSON too deep")
	}
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	switch t {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			key, ok := k.(string)
			if !ok {
				return nil, errors.New("invalid key")
			}
			if _, ok = m[key]; ok {
				return nil, errors.New("duplicate key")
			}
			v, e := value(d, depth+1)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		_, e = d.Token()
		return m, e
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, e := value(d, depth+1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, e = d.Token()
		return a, e
	}
	if n, ok := t.(json.Number); ok {
		if strings.ContainsAny(string(n), ".eE") {
			return nil, errors.New("non-integer number unsupported")
		}
		i, ok := new(big.Int).SetString(string(n), 10)
		if !ok {
			return nil, errors.New("invalid integer")
		}
		return json.Number(i.String()), nil
	}
	return t, nil
}
func canonical(v any) []byte {
	var b bytes.Buffer
	var emit func(any)
	emit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			b.WriteByte('{')
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				emit(k)
				b.WriteByte(':')
				emit(x[k])
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, v := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				emit(v)
			}
			b.WriteByte(']')
		case string:
			b.WriteByte('"')
			for _, r := range x {
				switch r {
				case '"', '\\':
					b.WriteByte('\\')
					b.WriteRune(r)
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
					if r < 32 {
						fmt.Fprintf(&b, `\u%04x`, r)
					} else {
						b.WriteRune(r)
					}
				}
			}
			b.WriteByte('"')
		case json.Number:
			b.WriteString(string(x))
		case bool:
			if x {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}
		case nil:
			b.WriteString("null")
		default:
			panic("unsupported internal JSON value")
		}
	}
	emit(v)
	return b.Bytes()
}
func (s *Signer) digest(v any) string {
	h := hmac.New(sha256.New, s.key[:])
	h.Write(canonical(v))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Signer) verify(data []byte) (map[string]any, error) {
	if s == nil || !s.configured {
		return nil, errors.New("missing signer")
	}
	m, e := decode(data)
	if e != nil {
		return nil, e
	}
	sig, ok := m["signature"].(string)
	delete(m, "signature")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.digest(m))) {
		return nil, errors.New("invalid signature")
	}
	m["signature"] = sig
	return m, nil
}

// Verify authenticates every field, including unknown extension fields. It does
// not check schema, repository identity, freshness or lifecycle eligibility.
func (s *Signer) Verify(data []byte) error { _, e := s.verify(data); return e }

// Sign is for trusted producers only. Never use it to bless unverified input.
func (s *Signer) Sign(data []byte) ([]byte, error) {
	if s == nil || !s.configured {
		return nil, errors.New("missing signer")
	}
	m, e := decode(data)
	if e != nil {
		return nil, e
	}
	delete(m, "signature")
	m["signature"] = s.digest(m)
	return canonical(m), nil
}

// Migrate verifies before returning a lossless canonical representation. Unknown
// fields and exact integers survive; the existing signature remains unchanged.
func (s *Signer) Migrate(data []byte) ([]byte, error) {
	m, e := s.verify(data)
	if e != nil {
		return nil, e
	}
	return canonical(m), nil
}
