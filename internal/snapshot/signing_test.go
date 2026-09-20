package snapshot

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func signer(t *testing.T) *Signer {
	t.Helper()
	s, e := NewSigner(strings.Repeat("ab", 32))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("..", "domain", "testdata", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestRecordedPythonSignatures(t *testing.T) {
	s := signer(t)
	for _, name := range []string{"bootstrap", "snapshot"} {
		t.Run(name, func(t *testing.T) {
			b := fixture(t, name)
			if e := s.Verify(b); e != nil {
				t.Fatal(e)
			}
			m, e := decode(b)
			if e != nil {
				t.Fatal(e)
			}
			signed, e := s.Sign(b)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(signed, canonical(m)) {
				t.Fatal("Python signature changed")
			}
			for _, change := range []string{"unsigned", "tampered", "invalid", "uppercase"} {
				t.Run(change, func(t *testing.T) {
					m, _ := decode(b)
					switch change {
					case "unsigned":
						delete(m, "signature")
					case "tampered":
						m["repo"] = "attacker/repo"
					case "invalid":
						m["signature"] = "bad"
					case "uppercase":
						m["signature"] = strings.ToUpper(m["signature"].(string))
					}
					if s.Verify(canonical(m)) == nil {
						t.Fatal("accepted invalid signature")
					}
					if _, e := s.Migrate(canonical(m)); e == nil {
						t.Fatal("migrated invalid signature")
					}
				})
			}
			wrong, _ := NewSigner(strings.Repeat("cd", 32))
			if wrong.Verify(b) == nil {
				t.Fatal("accepted wrong key")
			}
		})
	}
}

// Exercise the actual Python verifiers, not a copied HMAC implementation. The
// recorded durable graph passes through Go and back without losing any field.
func TestMigrationBackToPython(t *testing.T) {
	s := signer(t)
	dir := t.TempDir()
	for _, name := range []string{"bootstrap", "snapshot"} {
		original := fixture(t, name)
		migrated, e := s.Migrate(original)
		if e != nil {
			t.Fatal(e)
		}
		before, _ := decode(original)
		after, _ := decode(migrated)
		if !bytes.Equal(canonical(before), canonical(after)) {
			t.Fatal("lossy migration")
		}
		if e = os.WriteFile(filepath.Join(dir, name+".json"), migrated, 0600); e != nil {
			t.Fatal(e)
		}
	}
	script := `
import importlib.util,json,os,pathlib,sys
root=pathlib.Path(sys.argv[1]); out=pathlib.Path(sys.argv[2])
def load(name,path):
 spec=importlib.util.spec_from_file_location(name,root/path)
 mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod);return mod
os.environ['HARNESS_BOOTSTRAP_HMAC_KEY']='ab'*32
os.environ['HARNESS_BROKER_HMAC_KEY']='ab'*32
os.environ['HARNESS_REPO']='acme/widget'
b=load('bootstrap','.ai-team/bootstrap/bootstrap.py')
c=load('broker','.ai-team/coordinator/broker.py')
e=json.loads((out/'bootstrap.json').read_text())
assert b.read_envelope(b.PLAN_BEGIN+json.dumps(e)+b.PLAN_END)==e
c.validate_snapshot(json.loads((out/'snapshot.json').read_text()),now=1700000000)
# Cover Python escaping, Unicode key order, exact large integers, and -0.
v={'unicode':'é😀<>&\u2028\u2029\x00\b\f\n\r\t', '😀':1, '\ue000':2, 'integer':123456789012345678901234567890, 'zero':0, 'extension':None}
v['signature']=b.envelope_signature(v)
(out/'unicode.json').write_text(json.dumps(v))
`
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("python3", "-c", script, root, dir)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("Python verification: %v\n%s", e, out)
	}
	b, e := os.ReadFile(filepath.Join(dir, "unicode.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Verify(b); e != nil {
		t.Fatal(e)
	}
	signed, e := s.Sign(b)
	if e != nil {
		t.Fatal(e)
	}
	m, _ := decode(b)
	if !bytes.Equal(signed, canonical(m)) {
		t.Fatal("Unicode signature changed")
	}
}
func TestRejectAmbiguousJSON(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{"x":1.1}`, `{"x":1e2}`, `{"x":"\ud800"}`, `{"x":"\udc00"}`, `{} {}`, `[]`, "{\"x\":\"\xff\"}"} {
		if _, e := signer(t).Sign([]byte(input)); e == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, key := range []string{"", strings.Repeat("A", 64), strings.Repeat("z", 64), "ab"} {
		if _, e := NewSigner(key); e == nil {
			t.Error("accepted invalid key")
		}
	}
	var s *Signer
	if s.Verify([]byte(`{}`)) == nil {
		t.Fatal("nil signer accepted")
	}
}
func TestExtensionsSurviveMigration(t *testing.T) {
	s := signer(t)
	m, _ := decode(fixture(t, "bootstrap"))
	m["extension"] = map[string]any{"null": nil, "empty": []any{}, "integer": json.Number("9007199254740993")}
	b, e := s.Sign(canonical(m))
	if e != nil {
		t.Fatal(e)
	}
	migrated, e := s.Migrate(b)
	if e != nil || !bytes.Equal(b, migrated) {
		t.Fatal("extension lost", e)
	}
}
