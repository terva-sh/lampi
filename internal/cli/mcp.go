package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"terva.sh/lampi/internal/upload"
)

const mcpUsage = `terva-lampi mcp — serve a lake's recall tools to an agent over stdio

usage:
  terva-lampi mcp [--lake NAME | --server URL] [--token-file FILE]

Runs an MCP server on stdin and stdout for an agent such as Claude Code
or Codex. Each message the agent sends goes to the lake's MCP endpoint
(/api/read/v1/mcp) with the read token, and the lake's answer comes back
on stdout. The agent's MCP configuration names the token file, so the
token is never in it.

The tools are the lake's: search, read_events and copy_excerpt. They
read the sessions the token's scope holds, on every machine that
uploaded to the lake.

The read token is one an admin minted on the lake's Read tokens page
with the events:read permission. It is read from --token-file, or from
the file LAMPI_READ_TOKEN_FILE names. There is no default path. Keep
the file mode 0600. The token is never printed.

The lake is --server, or the server of the config.json lake --lake
names, or the only lake config.json lists.

Claude Code:
  claude mcp add --scope user lampi -- terva-lampi mcp --token-file ~/.config/terva-lampi/read-token
`

// mcpEndpoint is the lake's MCP route.
const mcpEndpoint = "/api/read/v1/mcp"

// mcpLineMax caps one message from the agent. The lake refuses a body
// over 64 KiB, so a longer line is a broken client.
const mcpLineMax = 1 << 20

// mcpReplyMax caps one answer from the lake. Its largest, an events page,
// is about 1 MiB of items.
const mcpReplyMax = 16 << 20

// mcpInFlight caps the requests the bridge has open to the lake at once.
// When all are taken, it reads no further line from the agent until one
// is answered, so a cancellation sent then waits with the rest.
const mcpInFlight = 8

func runMCP(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), mcpUsage)
		return nil
	}
	// stdout carries the protocol, so a usage message goes to stderr.
	usageEnv := env
	usageEnv.Stdout = env.stderr()
	var lake, server, tokenFile string
	rest, err := parseFlags(usageEnv, args, mcpUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lake, "lake", "", "the config.json lake to read")
		fs.StringVar(&server, "server", "", "the lake URL")
		fs.StringVar(&tokenFile, "token-file", "", "file holding the read token (or LAMPI_READ_TOKEN_FILE)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if lake != "" && server != "" {
		return errors.New("--lake and --server both name the lake; pass one")
	}
	if server == "" {
		if server, err = queryServer(env, lake); err != nil {
			return err
		}
	}
	if tokenFile == "" {
		tokenFile = env.getenv("LAMPI_READ_TOKEN_FILE")
	}
	if tokenFile == "" {
		return errors.New("no read token: pass --token-file, or set LAMPI_READ_TOKEN_FILE")
	}
	token, err := readReadToken(tokenFile)
	if err != nil {
		return err
	}
	if err := upload.CheckToken(server, token); err != nil {
		return fmt.Errorf("refusing to send the read token over plain http to %s; use an https URL, or http only to a loopback address", server)
	}
	b := &mcpBridge{url: strings.TrimRight(server, "/") + mcpEndpoint, token: token, out: env.stdout(), log: env.stderr()}
	fmt.Fprintf(env.stderr(), "terva-lampi mcp: serving the recall tools of %s\n", server)
	return b.serve(env.stdin())
}

// mcpBridge forwards newline-delimited JSON-RPC from an agent to the
// lake, one HTTP request per message, and writes each answer as one
// line. Up to mcpInFlight requests run concurrently, so a slow search
// does not hold up a ping; their answers can arrive in any order, which
// JSON-RPC allows.
type mcpBridge struct {
	url, token string
	out, log   io.Writer

	mu sync.Mutex
	// version is what initialize negotiated, for the header an
	// initialize-era client's later requests carry.
	version string
	// werr is the first failed write to the agent. It ends serve and
	// cancels ctx, which the lake requests run under (review 2107).
	werr   error
	ctx    context.Context
	cancel context.CancelFunc
	// calls holds each request in flight by the value of its id, for
	// notifications/cancelled to stop.
	calls map[string]*mcpCall
	// beforeReply, when set, runs as an answer is about to be written.
	// A test sets it to read a cancellation at that point.
	beforeReply func()
}

// mcpCall is one message the bridge forwards, and ctx is what its call
// to the lake runs under. notifications/cancelled sets cancelled under
// the bridge's mu, and an answer is written under the same lock only
// while it is unset, so no answer follows a cancellation the bridge has
// read (review 2135).
type mcpCall struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool
}

// mcpMessage is what the bridge reads of a message to route it.
type mcpMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name string `json:"name"`
		// RequestID is the request notifications/cancelled names.
		RequestID json.RawMessage `json:"requestId"`
		Meta      struct {
			Version string `json:"io.modelcontextprotocol/protocolVersion"`
		} `json:"_meta"`
	} `json:"params"`
}

func (b *mcpBridge) serve(in io.Reader) error {
	b.ctx, b.cancel = context.WithCancel(context.Background())
	defer b.cancel()
	b.calls = map[string]*mcpCall{}
	// Lines are read on their own goroutine, so a failed write to the
	// agent ends serve even while the agent sends nothing more (review
	// 2111). That reader is left blocked on stdin; the process exits.
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64<<10), mcpLineMax)
		for sc.Scan() {
			select {
			case lines <- append([]byte(nil), sc.Bytes()...):
			case <-b.ctx.Done():
				return
			}
		}
		readErr <- sc.Err()
	}()
	var wg sync.WaitGroup
	slots := make(chan struct{}, mcpInFlight)
read:
	for {
		var line []byte
		select {
		case <-b.ctx.Done():
			break read
		case l, ok := <-lines:
			if !ok {
				break read
			}
			line = bytes.TrimSpace(l)
		}
		if len(line) == 0 {
			continue
		}
		var m mcpMessage
		if err := json.Unmarshal(line, &m); err != nil {
			b.write(mcpErrorLine(nil, -32700, "not a JSON-RPC message"))
			continue
		}
		if m.Method == "" {
			// A response from the agent. The lake sends no requests, so
			// there is nothing to answer.
			continue
		}
		if m.Method == "notifications/cancelled" && m.ID == nil {
			// Closing the request to the lake is the cancellation. The
			// lake keeps no session, so the notification goes no further.
			b.cancelCall(m.Params.RequestID)
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-b.ctx.Done():
			break read
		}
		if m.Method == "initialize" {
			// Later requests carry the version it negotiates, so they
			// wait for it, as the protocol has a client wait. It takes a
			// slot like any request (review 2135), and is never
			// cancelled.
			b.forward(&mcpCall{ctx: b.ctx}, m, line)
			<-slots
			continue
		}
		c, done := b.track(m.ID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer done()
			b.forward(c, m, line)
		}()
	}
	wg.Wait()
	b.mu.Lock()
	werr := b.werr
	b.mu.Unlock()
	if werr != nil {
		return fmt.Errorf("mcp: writing to the agent: %w", werr)
	}
	select {
	case err := <-readErr:
		if err != nil {
			return fmt.Errorf("mcp: reading from the agent: %w", err)
		}
	default:
	}
	return nil
}

// track registers a request in flight under its id, and returns it with
// the func that ends it. A notification, or an id that is neither a
// string nor a number, has no id to cancel it by.
func (b *mcpBridge) track(id json.RawMessage) (*mcpCall, func()) {
	key, ok := mcpIDKey(id)
	if !ok {
		return &mcpCall{ctx: b.ctx}, func() {}
	}
	ctx, cancel := context.WithCancel(b.ctx)
	c := &mcpCall{ctx: ctx, cancel: cancel}
	b.mu.Lock()
	b.calls[key] = c
	b.mu.Unlock()
	return c, func() {
		b.mu.Lock()
		// A client that reuses an id in flight has replaced this call.
		if b.calls[key] == c {
			delete(b.calls, key)
		}
		b.mu.Unlock()
		cancel()
	}
}

// cancelCall stops the request in flight that id names. One already
// answered, or never sent, has nothing to stop.
func (b *mcpBridge) cancelCall(id json.RawMessage) {
	key, ok := mcpIDKey(id)
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if c := b.calls[key]; c != nil {
		c.cancelled = true
		c.cancel()
	}
}

// mcpIDKey is the value of a JSON-RPC id as a key that every spelling of
// it shares: "a" and "\u0061" (review 2135), or 1000000, 1e6 and
// 1000000.0 (review 2136). A string and a number stay apart. ok is false
// for an id that is neither.
func mcpIDKey(id json.RawMessage) (key string, ok bool) {
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return "", false
	}
	switch v := v.(type) {
	case string:
		return "s" + v, true
	case json.Number:
		if k, ok := mcpNumberKey(v.String()); ok {
			return "n" + k, true
		}
	}
	return "", false
}

// mcpNumberKey writes a JSON number as its significant digits, with no
// leading or trailing zero, and the power of ten that scales them, so
// 1000000, 1e6 and 1000000.0 are each "1e6". Nothing is expanded, so a
// short id with a large exponent costs no more than its length (review
// 2137). ok is false for an exponent beyond ±2^62, which leaves room to
// adjust it by the digits' count without overflow (review 2139).
func mcpNumberKey(n string) (string, bool) {
	sign := ""
	if rest, ok := strings.CutPrefix(n, "-"); ok {
		sign, n = "-", rest
	}
	mantissa, exponent, _ := strings.Cut(strings.ToLower(n), "e")
	var exp int64
	if exponent != "" {
		var err error
		if exp, err = strconv.ParseInt(exponent, 10, 64); err != nil || exp < -1<<62 || exp > 1<<62 {
			return "", false
		}
	}
	whole, frac, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+frac, "0")
	significant := strings.TrimRight(digits, "0")
	if significant == "" {
		return "0", true
	}
	exp += int64(len(digits)-len(significant)) - int64(len(frac))
	return sign + significant + "e" + strconv.FormatInt(exp, 10), true
}

func (b *mcpBridge) forward(c *mcpCall, m mcpMessage, body []byte) {
	notification := m.ID == nil
	req, err := http.NewRequestWithContext(c.ctx, http.MethodPost, b.url, bytes.NewReader(body))
	if err != nil {
		b.fail(c, m, -32603, err.Error())
		return
	}
	h := req.Header
	h.Set("Authorization", "Bearer "+b.token)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/event-stream")
	if v := m.Params.Meta.Version; v != "" {
		// A per-request-metadata client: the headers repeat the body.
		h.Set("MCP-Protocol-Version", v)
		h.Set("Mcp-Method", m.Method)
		if m.Method == "tools/call" {
			h.Set("Mcp-Name", headerSafe(m.Params.Name))
		}
	} else if m.Method != "initialize" {
		b.mu.Lock()
		v := b.version
		b.mu.Unlock()
		if v != "" {
			h.Set("MCP-Protocol-Version", v)
		}
	}
	resp, err := queryClient.Do(req)
	if err != nil {
		b.fail(c, m, -32603, "lake unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, mcpReplyMax+1))
	if err != nil {
		b.fail(c, m, -32603, "reading the lake's answer: "+err.Error())
		return
	}
	if notification {
		// The lake accepts a notification with 202 and no body. Anything
		// else is a refusal, such as of a revoked token, that the agent
		// would otherwise not see until its next request (review 2115).
		if resp.StatusCode/100 != 2 {
			b.fail(c, m, -32603, mcpRefusal(resp.StatusCode, resp.Header.Get("Content-Type"), reply))
		}
		return
	}
	var rpc struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	isRPC := strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") && len(reply) <= mcpReplyMax && json.Unmarshal(reply, &rpc) == nil && rpc.JSONRPC == "2.0"
	if !isRPC {
		b.fail(c, m, -32603, mcpRefusal(resp.StatusCode, resp.Header.Get("Content-Type"), reply))
		return
	}
	if m.Method == "initialize" && rpc.Result.ProtocolVersion != "" {
		b.mu.Lock()
		b.version = rpc.Result.ProtocolVersion
		b.mu.Unlock()
	}
	var line bytes.Buffer
	if err := json.Compact(&line, reply); err != nil {
		b.fail(c, m, -32603, "the lake's answer is not JSON")
		return
	}
	b.reply(c, line.Bytes())
}

// fail answers a request the lake did not answer in JSON-RPC, and logs
// a notification's failure, which has no one to answer.
func (b *mcpBridge) fail(c *mcpCall, m mcpMessage, code int, msg string) {
	if m.ID == nil {
		// Once the bridge is stopping, the failure is its own.
		if c.ctx.Err() == nil {
			fmt.Fprintf(b.log, "terva-lampi mcp: %s: %s\n", m.Method, msg)
		}
		return
	}
	b.reply(c, mcpErrorLine(m.ID, code, msg))
}

// reply writes the answer to a request, unless the agent cancelled it.
// It decides under the lock cancelCall takes, so a cancellation read
// before the write is never answered.
func (b *mcpBridge) reply(c *mcpCall, line []byte) {
	if b.beforeReply != nil {
		b.beforeReply()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !c.cancelled {
		b.writeLocked(line)
	}
}

// write sends one line to the agent.
func (b *mcpBridge) write(line []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeLocked(line)
}

// writeLocked sends one line to the agent, with b.mu held. After a write
// fails, nothing more is written, and serve stops.
func (b *mcpBridge) writeLocked(line []byte) {
	if b.werr != nil {
		return
	}
	if _, err := b.out.Write(append(line, '\n')); err != nil {
		b.werr = err
		b.cancel()
	}
}

func mcpErrorLine(id json.RawMessage, code int, msg string) []byte {
	if id == nil {
		id = json.RawMessage("null")
	}
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
	return line
}

// mcpRefusal says what to do about an answer that is not JSON-RPC: the
// lake refused the token, has no MCP endpoint, or refused the message.
func mcpRefusal(status int, contentType string, body []byte) string {
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &refusal)
	reason := refusal.Error
	if reason == "" && strings.HasPrefix(contentType, "text/plain") {
		// The lake's MCP server refuses a message it cannot read with
		// one plain line, such as an id that is not a string or number.
		line, _, _ := bytes.Cut(body, []byte("\n"))
		reason = strings.Map(func(r rune) rune {
			if r < 0x20 || r > 0x7e {
				return -1
			}
			return r
		}, string(line[:min(len(line), 200)]))
	}
	msg := fmt.Sprintf("the lake answered %d %s", status, reason)
	switch {
	case status == http.StatusUnauthorized:
		return msg + ": the read token is unknown, expired or revoked; ask an admin for a new one"
	case status == http.StatusNotFound:
		return msg + ": the token lacks events:read, or the lake is older than its MCP endpoint"
	case status >= 300 && status < 400:
		return msg + ": a redirect; pass the URL it names as --server if it is the lake"
	}
	return strings.TrimSpace(msg)
}

// headerSafe is v as a header value: as it is when it is visible ASCII
// with no space at either end, and otherwise in the transport's base64
// form, as is any value that already looks like that form.
func headerSafe(v string) string {
	safe := v == strings.TrimSpace(v) && !(strings.HasPrefix(v, "=?base64?") && strings.HasSuffix(v, "?="))
	for i := 0; safe && i < len(v); i++ {
		safe = v[i] >= 0x20 && v[i] <= 0x7e
	}
	if safe {
		return v
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(v)) + "?="
}
