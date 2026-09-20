package worker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLocalIsolationSmoke(t *testing.T) {
	if os.Getenv("HARNESS_WORKER_SMOKE") != "1" {
		t.Skip("opt-in local systemd/bubblewrap isolation probe")
	}
	c, _ := fixture(t)
	adapter, err := filepath.Abs("../../.ai-team/bin/worker-process")
	if err != nil {
		t.Fatal(err)
	}
	c.Adapter = adapter
	c.Executor = CommandExecutor{}
	// The probe runs from the trusted runner outside masked runtime/secret roots.
	script := `#!/usr/bin/env python3
import os, socket
from pathlib import Path
root = Path(os.environ['HARNESS_ROOT'])
(root / 'probe-write').write_text('allowed')
for forbidden in [HOST_FILE, SECRET_FILE]:
    try:
        if forbidden == SECRET_FILE:
            Path(forbidden).read_text()
        else:
            Path(forbidden).write_text('forbidden')
    except OSError:
        pass
    else:
        raise RuntimeError('isolation failed: ' + forbidden)
assert all(name == "lo" for _, name in socket.if_nameindex()), "host network interfaces visible"
s = socket.socket()
s.settimeout(2)
try:
    s.connect(('1.1.1.1', 443))
except OSError:
    pass
else:
    raise RuntimeError('network enabled')
Path(os.environ['HARNESS_AGENT_RESULT']).write_text('PASS')
`
	script = "#!/usr/bin/env python3\nHOST_FILE=" + strconv.Quote(filepath.Join(c.Repository, "README")) + "\nSECRET_FILE=" + strconv.Quote(filepath.Join(c.SecretsRoot, "token")) + "\n" + script[len("#!/usr/bin/env python3\n"):]
	write(t, c.Runner, script)
	if err := os.Chmod(c.Runner, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h, err := m.Start(ctx, request())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	for {
		result, _ := os.ReadFile(h.Result)
		if string(result) == "PASS" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("probe did not pass; inspect journalctl --user -u " + h.Unit)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
