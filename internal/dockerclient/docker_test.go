package dockerclient

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/api/types/container"
)

// versionedPath strips the /vX.Y prefix the API client puts on requests.
var versionedPath = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

// fakeResponse is a canned HTTP response body plus status code.
type fakeResponse struct {
	status int
	body   string
}

// fakeDaemon is a minimal Docker Engine API double. The zero value
// answers a successful ping with an empty container list.
type fakeDaemon struct {
	mu            sync.Mutex
	list          fakeResponse
	stats         map[string]fakeResponse
	startStatus   int
	stopStatus    int
	restartStatus int
	logs          fakeResponse
	inspect       fakeResponse
	inspectTTY    bool
	statsSeen     map[string]bool
}

func (f *fakeDaemon) statusOf(s int) int {
	if s == 0 {
		return http.StatusOK
	}
	return s
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := versionedPath.ReplaceAllString(r.URL.Path, "")
	switch {
	case p == "/_ping":
		w.Header().Set("Api-Version", "1.55")
		w.Header().Set("Ostype", "linux")
		w.WriteHeader(http.StatusOK)
		return
	case p == "/containers/json":
		f.mu.Lock()
		resp := f.list
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.statusOf(resp.status))
		_, _ = w.Write([]byte(resp.body))
		return
	}

	rest := strings.TrimPrefix(p, "/containers/")
	id, action, found := strings.Cut(rest, "/")
	if !found || id == "" {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch action {
	case "stats":
		if f.statsSeen == nil {
			f.statsSeen = make(map[string]bool)
		}
		f.statsSeen[id] = true
		resp := f.stats[id]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.statusOf(resp.status))
		_, _ = w.Write([]byte(resp.body))
	case "start":
		w.WriteHeader(f.statusOf(f.startStatus))
	case "stop":
		w.WriteHeader(f.statusOf(f.stopStatus))
	case "restart":
		w.WriteHeader(f.statusOf(f.restartStatus))
	case "logs":
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		w.WriteHeader(f.statusOf(f.logs.status))
		_, _ = w.Write([]byte(f.logs.body))
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.statusOf(f.inspect.status))
		if f.statusOf(f.inspect.status) < 300 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":     id,
				"Config": map[string]any{"Tty": f.inspectTTY},
			})
		}
	default:
		http.NotFound(w, r)
	}
}

// startFakeDaemon launches the fake daemon and points DOCKER_HOST at it.
func startFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	f := &fakeDaemon{stats: map[string]fakeResponse{}}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(ts.URL, "http://"))
	// Hermeticity: never inherit TLS or a pinned API version from the
	// developer's environment.
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_API_VERSION", "")
	return f
}

// longID returns an n-character pseudo container id.
func longID(c byte) string { return strings.Repeat(string(c), 64) }

func TestNewAndClose(t *testing.T) {
	f := startFakeDaemon(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("New returned a nil client")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(f.statsSeen) != 0 {
		t.Errorf("unexpected requests: %v", f.statsSeen)
	}
}

func TestNewInvalidHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "this is not a docker host")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_API_VERSION", "")
	if _, err := New(); err == nil {
		t.Fatal("New accepted a malformed DOCKER_HOST")
	}
}

func TestNewUnreachableDaemon(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // closed port: connect is refused immediately

	t.Setenv("DOCKER_HOST", "tcp://"+addr)
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_API_VERSION", "")

	_, err = New()
	if err == nil || !strings.Contains(err.Error(), "docker daemon unreachable") {
		t.Fatalf("err = %v, want daemon unreachable", err)
	}
}

// statsA exercises every populated branch of fillStats: CPU from a
// nonzero system delta with online_cpus, the inactive_file memory
// adjustment, a memory limit, network and blkio deltas.
const statsA = `{
  "cpu_stats": {"cpu_usage": {"total_usage": 3000000, "percpu_usage": [1500000, 1500000]},
                "system_cpu_usage": 10000000000, "online_cpus": 2},
  "precpu_stats": {"cpu_usage": {"total_usage": 1000000}, "system_cpu_usage": 9000000000},
  "memory_stats": {"usage": 3000000, "limit": 10000000, "stats": {"inactive_file": 500000}},
  "networks": {"eth0": {"rx_bytes": 111, "tx_bytes": 222}},
  "blkio_stats": {"io_service_bytes_recursive": [
    {"major": 8, "minor": 0, "op": "read", "value": 1000},
    {"major": 8, "minor": 0, "op": "Write", "value": 2000},
    {"major": 8, "minor": 0, "op": "trim", "value": 7}]}
}`

// statsB covers the online_cpus==0 fallback to percpu_usage and the
// cases where inactive_file exceeds usage and no memory limit is set.
const statsB = `{
  "cpu_stats": {"cpu_usage": {"total_usage": 500, "percpu_usage": [250, 250]},
                "system_cpu_usage": 1000},
  "precpu_stats": {"cpu_usage": {"total_usage": 100}, "system_cpu_usage": 500},
  "memory_stats": {"usage": 800, "stats": {"inactive_file": 900}}
}`

func TestList(t *testing.T) {
	f := startFakeDaemon(t)
	ids := []string{longID('a'), longID('b'), longID('c'), longID('d'), longID('e')}
	items := []container.Summary{
		{ID: ids[0], Names: []string{"/web"}, Image: "nginx:1.27",
			State: container.StateRunning, Status: "Up 2 hours"},
		{ID: ids[1], Names: []string{"/db"}, Image: "postgres:16",
			State: container.StateRunning, Status: "Up 3 hours"},
		{ID: ids[2], Names: []string{"/broken"}, Image: "busybox",
			State: container.StateRunning, Status: "Up 4 hours"},
		{ID: ids[3], Names: []string{"/garbage"}, Image: "busybox",
			State: container.StateRunning, Status: "Up 5 hours"},
		{ID: ids[4], Names: []string{"/stopped-one"}, Image: "alpine",
			State: container.StateExited, Status: "Exited (0)"},
	}
	listBody, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.list = fakeResponse{body: string(listBody)}
	f.stats[ids[0]] = fakeResponse{body: statsA}
	f.stats[ids[1]] = fakeResponse{body: statsB}
	f.stats[ids[2]] = fakeResponse{status: 500, body: `{"message": "stats exploded"}`}
	f.stats[ids[3]] = fakeResponse{body: `{not json`}
	f.mu.Unlock()

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	got, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("List returned %d containers, want %d", len(got), len(items))
	}

	a := got[0]
	if a.ID != ids[0][:12] {
		t.Errorf("ID = %q, want %q", a.ID, ids[0][:12])
	}
	if a.Name != "web" {
		t.Errorf("Name = %q, want %q (leading slash trimmed)", a.Name, "web")
	}
	if a.Image != "nginx:1.27" || a.State != "running" || a.Status != "Up 2 hours" {
		t.Errorf("identity fields = %+v", a)
	}
	// CPU = 2ms/1s * 2 online cpus * 100
	if a.CPU < 0.39 || a.CPU > 0.41 {
		t.Errorf("CPU = %v, want 0.4", a.CPU)
	}
	if a.Mem != 2500000 {
		t.Errorf("Mem = %d, want 2500000 (usage minus inactive_file)", a.Mem)
	}
	if a.MemPct != 25 {
		t.Errorf("MemPct = %v, want 25", a.MemPct)
	}
	if a.NetRx != 111 || a.NetTx != 222 {
		t.Errorf("NetRx/Tx = %d/%d, want 111/222", a.NetRx, a.NetTx)
	}
	if a.BlkR != 1000 || a.BlkW != 2000 {
		t.Errorf("BlkR/BlkW = %d/%d, want 1000/2000", a.BlkR, a.BlkW)
	}

	b := got[1]
	// CPU = 400ns/500ns * 2 percpu slots * 100
	if b.CPU < 159 || b.CPU > 161 {
		t.Errorf("CPU = %v, want 160 (percpu fallback)", b.CPU)
	}
	if b.Mem != 800 {
		t.Errorf("Mem = %d, want 800 (inactive_file above usage ignored)", b.Mem)
	}
	if b.MemPct != 0 {
		t.Errorf("MemPct = %v, want 0 (no limit)", b.MemPct)
	}

	for _, i := range []int{2, 3} { // failed stats degrade to zeros
		if got[i].CPU != 0 || got[i].Mem != 0 || got[i].NetRx != 0 || got[i].BlkR != 0 {
			t.Errorf("container %d carried stats %+v after a stats failure", i, got[i])
		}
	}
	if got[4].Name != "stopped-one" || got[4].State != "exited" {
		t.Errorf("stopped container = %+v", got[4])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statsSeen[ids[4]] {
		t.Error("stats were fetched for a stopped container")
	}
	for _, id := range ids[:4] {
		if !f.statsSeen[id] {
			t.Errorf("stats were not fetched for running container %s…", id[:12])
		}
	}
}

func TestListError(t *testing.T) {
	f := startFakeDaemon(t)
	f.mu.Lock()
	f.list = fakeResponse{status: 500, body: `{"message": "daemon on fire"}`}
	f.mu.Unlock()

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("List accepted a 500 response")
	}
}

func TestStartStopRestart(t *testing.T) {
	f := startFakeDaemon(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		f.mu.Lock()
		f.startStatus, f.stopStatus, f.restartStatus = 0, 0, 0
		f.mu.Unlock()
		for _, tc := range []struct {
			name string
			call func(string) error
		}{
			{"start", func(id string) error { return c.Start(ctx, id) }},
			{"stop", func(id string) error { return c.Stop(ctx, id) }},
			{"restart", func(id string) error { return c.Restart(ctx, id) }},
		} {
			if err := tc.call("abc123"); err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
		}
	})

	t.Run("daemon error", func(t *testing.T) {
		f.mu.Lock()
		f.startStatus, f.stopStatus, f.restartStatus = 500, 500, 500
		f.mu.Unlock()
		for _, tc := range []struct {
			name string
			call func(string) error
		}{
			{"start", func(id string) error { return c.Start(ctx, id) }},
			{"stop", func(id string) error { return c.Stop(ctx, id) }},
			{"restart", func(id string) error { return c.Restart(ctx, id) }},
		} {
			if err := tc.call("abc123"); err == nil {
				t.Errorf("%s: accepted a 500 response", tc.name)
			}
		}
	})
}

func TestLogs(t *testing.T) {
	f := startFakeDaemon(t)
	f.mu.Lock()
	f.logs = fakeResponse{body: "log line one\nlog line two\n"}
	f.mu.Unlock()
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	ctx := context.Background()

	t.Run("tty", func(t *testing.T) {
		f.mu.Lock()
		f.inspectTTY = true
		f.mu.Unlock()
		rc, tty, err := c.Logs(ctx, "abc123", false)
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
		if !tty {
			t.Error("tty = false, want true")
		}
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read logs: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
		if !strings.Contains(string(body), "log line one") {
			t.Errorf("logs body = %q", body)
		}
	})

	t.Run("no tty", func(t *testing.T) {
		f.mu.Lock()
		f.inspectTTY = false
		f.mu.Unlock()
		rc, tty, err := c.Logs(ctx, "abc123", true)
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
		if tty {
			t.Error("tty = true, want false")
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	t.Run("inspect fails", func(t *testing.T) {
		f.mu.Lock()
		f.inspect = fakeResponse{status: 404, body: `{"message": "no such container"}`}
		f.mu.Unlock()
		rc, tty, err := c.Logs(ctx, "abc123", false)
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
		defer rc.Close()
		if tty {
			t.Error("tty = true, want false when inspect fails")
		}
	})

	t.Run("logs error", func(t *testing.T) {
		f.mu.Lock()
		f.logs = fakeResponse{status: 500, body: `{"message": "logs exploded"}`}
		f.mu.Unlock()
		if _, _, err := c.Logs(ctx, "abc123", false); err == nil {
			t.Error("Logs accepted a 500 response")
		}
	})
}
