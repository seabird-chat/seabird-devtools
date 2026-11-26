// Package integration tests the fake backend and the plugin against a real
// seabird-core. The binary comes from SEABIRD_CORE_BIN, or seabird-core on
// PATH, and the tests are skipped when neither is available.
package integration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-devtools/internal/backend"
	"github.com/seabird-chat/seabird-devtools/internal/plugin"
	"github.com/seabird-chat/seabird-devtools/internal/testutil"
)

// testTimeout bounds each test, including starting core.
const testTimeout = 30 * time.Second

var tokenPattern = regexp.MustCompile(`sbt_\S+`)

func coreBinary(t *testing.T) string {
	t.Helper()

	if bin := os.Getenv("SEABIRD_CORE_BIN"); bin != "" {
		return bin
	}
	bin, err := exec.LookPath("seabird-core")
	if err != nil {
		t.Skip("seabird-core not found; set SEABIRD_CORE_BIN or add it to PATH")
	}
	return bin
}

// startCore runs seabird-core with a fresh database until the test ends, and
// returns its URL, an auth token, and its log output.
func startCore(t *testing.T, ctx context.Context) (url, token string, logs *testutil.Buffer) {
	t.Helper()

	bin := coreBinary(t)
	db := filepath.Join(t.TempDir(), "core.db")

	out, err := exec.CommandContext(ctx, bin, "token", "create", "--database-url", db, "devtools-test").CombinedOutput()
	assert.NoError(t, err, "token create: %s", out)
	token = tokenPattern.FindString(string(out))
	assert.NotEqual(t, "", token, "no token in output: %s", out)

	// Listening on port 0 picks a free port, which core is started on.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	addr := lis.Addr().String()
	lis.Close()

	logs = &testutil.Buffer{}
	cmd := exec.CommandContext(ctx, bin, "serve", "--bind-host", addr, "--database-url", db)
	cmd.Stdout, cmd.Stderr = logs, logs
	assert.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("seabird-core output:\n%s", logs.String())
		}
	})

	// Core logs this once it's listening.
	logs.WaitFor(t, "starting gRPC server")

	return "http://" + addr, token, logs
}

type harness struct {
	ctx   context.Context
	syncs int

	backend    *backend.Backend
	backendOut *testutil.Buffer

	plugin    *plugin.Plugin
	pluginOut *testutil.Buffer
}

// sync waits until core has handled every event the backend has sent. Core
// handles a backend's events in order, so once the plugin sees a message sent
// after them, they are done. Core also registers a plugin's commands before
// delivering it events, so the plugin's commands are registered too.
func (h *harness) sync(t *testing.T) {
	t.Helper()

	h.syncs++
	marker := fmt.Sprintf("sync %d", h.syncs)
	h.backendCmd(t, "pm sync "+marker)
	h.pluginOut.WaitFor(t, marker+"\n")
}

// backendCmd runs a backend prompt command.
func (h *harness) backendCmd(t *testing.T, line string) {
	t.Helper()
	assert.NoError(t, h.backend.Commands().Dispatch(h.ctx, h.backendOut, line), "backend> %s", line)
}

// pluginCmd runs a plugin prompt command and returns its output.
func (h *harness) pluginCmd(t *testing.T, line string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	err := h.plugin.Commands().Dispatch(h.ctx, &out, line)
	return out.String(), err
}

// newHarness starts core, connects a backend with #general joined, and starts
// a plugin watching events with the ping command registered.
func newHarness(t *testing.T) *harness {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	url, token, coreLogs := startCore(t, ctx)
	h := &harness{ctx: ctx, backendOut: &testutil.Buffer{}, pluginOut: &testutil.Buffer{}}

	ingest, err := seabird.NewChatIngestClient(url, token)
	assert.NoError(t, err)
	t.Cleanup(func() { ingest.Close() })

	h.backend = &backend.Backend{Type: "dev", ID: "it"}
	assert.NoError(t, h.backend.Connect(ctx, ingest.Inner))
	backendDone := make(chan error, 1)
	go func() { backendDone <- h.backend.Run(ctx, h.backendOut) }()

	client, err := seabird.NewClient(url, token)
	assert.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	h.plugin = &plugin.Plugin{Client: client.Inner}
	watchCtx, stopWatch := context.WithCancel(ctx)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- h.plugin.Watch(watchCtx, h.pluginOut, map[string]*pb.CommandMetadata{"ping": {Name: "ping"}}, 0)
	}()

	// Cleanups run in reverse, so these stop before the clients close.
	t.Cleanup(func() {
		stopWatch()
		assert.NoError(t, <-watchDone)
		h.backend.Close()
		assert.NoError(t, <-backendDone)
	})

	// Core logs this once the plugin is subscribed, so no events are missed
	// after it. Each test's core has only this plugin.
	coreLogs.WaitFor(t, "plugin connected")

	h.backendCmd(t, "join #general")
	h.sync(t)

	return h
}

func TestBackendEventsReachPlugin(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.backendCmd(t, "msg #general alice **hi** there")
	h.backendCmd(t, "cmd #general alice ping now")

	h.pluginOut.WaitFor(t, "message dev://it/%23general alice(dev://it/alice): hi there\n")
	h.pluginOut.WaitFor(t, "command dev://it/%23general alice(dev://it/alice): ping now\n")
}

func TestPluginRequestsReachBackend(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	out, err := h.pluginCmd(t, "send dev://it/%23general __pong__")
	assert.NoError(t, err)
	assert.Equal(t, "ok\n", out)

	_, err = h.pluginCmd(t, "join dev://it #new")
	assert.NoError(t, err)

	_, err = h.pluginCmd(t, "topic dev://it/%23general fresh topic")
	assert.NoError(t, err)

	out, err = h.pluginCmd(t, "channel dev://it/%23general")
	assert.NoError(t, err)
	assert.Equal(t, "dev://it/%23general name=\"#general\" topic=\"fresh topic\"\n", out)

	out, err = h.pluginCmd(t, "backend dev://it")
	assert.NoError(t, err)
	assert.Equal(t, "dev://it type=dev [devtools/backend=fake]\n", out)

	assert.Equal(t, ""+
		"send_message #general: pong\n"+
		"join_channel #new\n"+
		"update_channel_info #general topic=\"fresh topic\"\n"+
		"metadata\n", h.backendOut.String())
	assert.Equal(t, 2, len(h.backend.Channels()))
}

func TestBackendFailuresReachPlugin(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.backendCmd(t, "respond fail rate limited")
	_, err := h.pluginCmd(t, "send dev://it/%23general hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")

	h.backendCmd(t, "respond ok")
	_, err = h.pluginCmd(t, "send dev://it/%23general hello")
	assert.NoError(t, err)
}

func TestLeaveRemovesChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.backendCmd(t, "leave #general")
	h.sync(t)

	out, err := h.pluginCmd(t, "channels dev://it")
	assert.NoError(t, err)
	assert.Equal(t, "none\n", out)
}
