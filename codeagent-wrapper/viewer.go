package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultViewerPort is the fixed port the live viewer binds by default.
// A fixed port lets you establish a single SSH tunnel once
// (ssh -L 8899:127.0.0.1:8899 host) and watch every task, forever.
const defaultViewerPort = 8899

// runViewerFromArgs parses viewer flags and starts the aggregator.
// Flags: --port <n> / --port=<n>, --host <h> / --host=<h>, --open.
// Env: CODEAGENT_WEB_PORT, CODEAGENT_WEB_HOST override the defaults.
func runViewerFromArgs(args []string) int {
	host := viewerHost()
	port := viewerPort()
	open := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--port":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil {
					port = v
				}
				i++
			}
		case strings.HasPrefix(arg, "--port="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--port=")); err == nil {
				port = v
			}
		case arg == "--host":
			if i+1 < len(args) {
				host = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--host="):
			host = strings.TrimPrefix(arg, "--host=")
		case arg == "--open":
			open = true
		}
	}
	return runViewer(host, port, open)
}

// runViewer starts the single live-output aggregator: one local web server
// that tails every session spool file in liveDir and renders them as
// concurrent panels.
func runViewer(host string, port int, open bool) int {
	dir := liveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot create live dir %s: %v\n", dir, err)
		return 1
	}
	cleanupOldLiveFiles(30 * time.Minute)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleViewerIndex)
	mux.HandleFunc("/api/sessions", handleViewerSessions)
	mux.HandleFunc("/api/stream/", handleViewerStream)

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		// When auto-spawned, another wrapper likely won the port first; exit quietly.
		if os.Getenv("CODEAGENT_VIEW_QUIET") == "1" {
			return 0
		}
		fmt.Fprintf(os.Stderr, "ERROR: cannot listen on %s:%d: %v\n", host, port, err)
		return 1
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	displayHost := host
	if host == "" || host == "0.0.0.0" || host == "::" {
		displayHost = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d", displayHost, actualPort)
	fmt.Fprintf(os.Stderr, "CCG live viewer: %s\n", url)
	fmt.Fprintf(os.Stderr, "  watching: %s\n", dir)
	fmt.Fprintln(os.Stderr, "  (Ctrl+C to stop)")

	if open {
		go openBrowser(url)
	}

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "ERROR: viewer server: %v\n", err)
		return 1
	}
	return 0
}

// viewerSession is the summary of one spool file returned by /api/sessions.
type viewerSession struct {
	ID      string `json:"id"`
	Backend string `json:"backend"`
	Task    string `json:"task"`
	Done    bool   `json:"done"`
	ModUnix int64  `json:"mod"`
}

func handleViewerSessions(w http.ResponseWriter, r *http.Request) {
	dir := liveDir()
	entries, _ := os.ReadDir(dir)
	sessions := make([]viewerSession, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		meta, done := readSpoolSummary(path)
		var mod int64
		if info, err := e.Info(); err == nil {
			mod = info.ModTime().Unix()
		}
		sessions = append(sessions, viewerSession{
			ID:      strings.TrimSuffix(e.Name(), ".jsonl"),
			Backend: meta.Backend,
			Task:    meta.Task,
			Done:    done,
			ModUnix: mod,
		})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ModUnix < sessions[j].ModUnix })

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}

// readSpoolSummary reads the meta header and detects whether the session ended.
func readSpoolSummary(path string) (meta spoolEvent, done bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	if line, err := bufio.NewReader(f).ReadBytes('\n'); err == nil || len(line) > 0 {
		var ev spoolEvent
		if json.Unmarshal(bytes.TrimSpace(line), &ev) == nil && ev.Type == "meta" {
			meta = ev
		}
	}

	// Cheap done detection: scan only the tail of the file for a done record.
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		const tailN = 512
		start := info.Size() - tailN
		if start < 0 {
			start = 0
		}
		buf := make([]byte, info.Size()-start)
		if _, err := f.ReadAt(buf, start); err == nil || err == io.EOF {
			if bytes.Contains(buf, []byte(`"type":"done"`)) {
				done = true
			}
		}
	}

	// Fallback: a task whose owning process has exited without a done record
	// (e.g. hard kill / crash) is finished — don't show it as perpetually live.
	if !done {
		if pid := meta.PID; pid > 0 && !processRunningCheck(pid) {
			done = true
		}
	}
	return
}

// handleViewerStream tails one session file and streams its lines over SSE.
// It replays from the beginning on every (re)connect so late-joining browsers
// see the full history; the client clears its panel on each open to dedupe.
func handleViewerStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/stream/")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	path := filepath.Join(liveDir(), sanitizeSessionID(id)+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	deadline := time.Now().Add(6 * time.Hour) // absolute safety cap per connection
	var pending []byte
	buf := make([]byte, 8192)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				line := pending[:idx]
				pending = pending[idx+1:]
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
				if bytes.Contains(line, []byte(`"type":"done"`)) {
					return
				}
			}
		}
		if readErr == io.EOF {
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				return
			}
			continue
		}
		if readErr != nil {
			return
		}
	}
}

func handleViewerIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(viewerHTML))
}

// openBrowser opens the specified URL in the default browser.
// Used only when the viewer is started with --open (never automatically,
// so it is harmless on headless servers).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-g", url) // -g: don't steal focus
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		hideWindowsConsole(cmd)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		return // silently fail if browser can't be opened
	}
	go func() { _ = cmd.Wait() }() // reap to avoid a zombie
}

const viewerHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>CCG Live Viewer</title>
<style>
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; background:#0d1117; color:#c9d1d9; height:100vh; display:flex; flex-direction:column; }
header { background:#161b22; padding:10px 18px; border-bottom:1px solid #30363d; display:flex; align-items:center; gap:12px; flex:0 0 auto; }
header .brand { font-size:16px; font-weight:600; color:#e6edf3; }
.tabs { display:flex; gap:6px; margin-left:8px; }
.tab { background:transparent; border:1px solid #30363d; color:#8b949e; padding:4px 12px; border-radius:6px; cursor:pointer; font-size:12px; font-family:inherit; }
.tab:hover { border-color:#8b949e; }
.tab.sel { background:#21262d; color:#e6edf3; border-color:#8b949e; }
.tab .badge { display:inline-block; min-width:16px; margin-left:6px; padding:0 5px; border-radius:8px; background:#30363d; color:#c9d1d9; font-size:11px; text-align:center; }
.tab.sel .badge { background:#3fb950; color:#0d1117; }
#main { flex:1; position:relative; overflow:hidden; }
#grid { position:absolute; inset:0; display:grid; gap:12px; padding:12px; overflow:auto; grid-auto-rows:minmax(240px,1fr); }
#empty { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; color:#8b949e; pointer-events:none; }
.panel { display:flex; flex-direction:column; background:#0d1117; border:1px solid #30363d; border-radius:8px; overflow:hidden; height:100%; min-height:0; }
.panel-head { display:flex; align-items:center; gap:8px; padding:8px 10px; background:#161b22; border-bottom:1px solid #30363d; flex:0 0 auto; }
.panel-icon { width:26px; height:26px; border-radius:5px; display:flex; align-items:center; justify-content:center; font-size:10px; font-weight:bold; color:#fff; flex:0 0 auto; }
.panel-title { font-size:13px; font-weight:600; }
.panel-task { color:#8b949e; font-size:11px; margin-left:auto; max-width:55%; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.dot { width:8px; height:8px; border-radius:50%; background:#3fb950; animation:blink 1s infinite; flex:0 0 auto; }
@keyframes blink { 0%,100%{opacity:1;} 50%{opacity:.3;} }
.panel-body { flex:1; padding:10px 12px; font-family:'Monaco','Menlo','Consolas',monospace; font-size:12px; line-height:1.55; white-space:pre-wrap; word-break:break-word; overflow-y:auto; }
.reasoning { color:#8b949e; font-style:italic; }
.command { color:#fbbf24; background:#1e1e1e; padding:6px 8px; margin:6px 0; display:block; border-left:3px solid #d97706; }
.message, .text { color:#c9d1d9; }
.done-tag { color:#8b949e; font-style:italic; margin-top:10px; padding-top:8px; border-top:1px solid #30363d; }
</style>
</head>
<body>
<header>
  <div class="brand">CCG Live Viewer</div>
  <div class="tabs">
    <button class="tab sel" data-tab="active">进行中 <span class="badge" id="cnt-active">0</span></button>
    <button class="tab" data-tab="done">已结束 <span class="badge" id="cnt-done">0</span></button>
  </div>
</header>
<div id="main">
  <div id="grid"></div>
  <div id="empty">等待任务… waiting for sessions</div>
</div>
<script>
var MAXCOLS = 4;
var COLORS = {
  codex:{bg:'#238636',title:'#3fb950',icon:'CDX'},
  gemini:{bg:'#8957e5',title:'#a371f7',icon:'GEM'},
  claude:{bg:'#d97706',title:'#fbbf24',icon:'CLD'},
  antigravity:{bg:'#0ea5e9',title:'#38bdf8',icon:'AGY'}
};
function colorFor(b){ return COLORS[b] || {bg:'#238636',title:'#3fb950',icon:(b||'AGT').slice(0,3).toUpperCase()}; }
var grid = document.getElementById('grid');
var empty = document.getElementById('empty');
var cntActive = document.getElementById('cnt-active');
var cntDone = document.getElementById('cnt-done');
var panels = {};
var currentTab = 'active';

function atBottom(el){ return el.scrollHeight - el.scrollTop - el.clientHeight < 40; }

// Show only panels of the current tab; grid columns adapt to the visible count.
function relayout(){
  var active = 0, done = 0, visible = 0;
  for (var id in panels){
    var p = panels[id];
    if (p.done) done++; else active++;
    var show = (currentTab === 'done') ? p.done : !p.done;
    p.panel.style.display = show ? '' : 'none';
    if (show) visible++;
  }
  var cols = Math.min(Math.max(visible, 1), MAXCOLS);
  grid.style.gridTemplateColumns = 'repeat(' + cols + ', 1fr)';
  cntActive.textContent = active;
  cntDone.textContent = done;
  empty.style.display = visible > 0 ? 'none' : 'flex';
  empty.textContent = (currentTab === 'done') ? '暂无已结束任务 no finished sessions' : '等待任务… waiting for sessions';
}

function createPanel(s){
  var c = colorFor(s.backend);
  var panel = document.createElement('div'); panel.className='panel';
  var head = document.createElement('div'); head.className='panel-head';
  var icon = document.createElement('div'); icon.className='panel-icon'; icon.style.background=c.bg; icon.textContent=c.icon;
  var title = document.createElement('div'); title.className='panel-title'; title.style.color=c.title; title.textContent=s.backend||'agent';
  var task = document.createElement('div'); task.className='panel-task'; task.textContent=s.task||''; task.title=s.task||'';
  var dot = document.createElement('div'); dot.className='dot';
  head.appendChild(icon); head.appendChild(title); head.appendChild(task); head.appendChild(dot);
  var body = document.createElement('div'); body.className='panel-body';
  panel.appendChild(head); panel.appendChild(body);
  grid.appendChild(panel);

  var p = {panel:panel, body:body, dot:dot, done:false, userScrolled:false, es:null};
  body.addEventListener('scroll', function(){ p.userScrolled = !atBottom(body); });
  panels[s.id] = p;

  var es = new EventSource('/api/stream/' + encodeURIComponent(s.id));
  p.es = es;
  es.onopen = function(){ body.innerHTML=''; p.userScrolled=false; }; // reset on (re)connect to dedupe replay
  es.onmessage = function(ev){
    var d; try { d = JSON.parse(ev.data); } catch(e){ return; }
    if(!d || !d.type || d.type==='meta') return;
    if(d.type==='done'){ markDone(p); return; }
    if(d.content){
      var cls = (d.type==='reasoning') ? 'reasoning' : (d.type==='command') ? 'command' : 'message';
      var span = document.createElement('span');
      span.className = cls;
      span.textContent = (d.type==='reasoning' ? '💭 ' : '') + d.content;
      body.appendChild(span);
      if(!p.userScrolled){ body.scrollTop = body.scrollHeight; }
    }
  };
  relayout();
}

function markDone(p){
  if(p.done) return;
  p.done = true;
  if(p.dot) p.dot.style.display='none';
  var tag = document.createElement('div'); tag.className='done-tag'; tag.textContent='✓ 完成 done';
  p.body.appendChild(tag);
  if(!p.userScrolled){ p.body.scrollTop = p.body.scrollHeight; }
  if(p.es){ try { p.es.close(); } catch(e){} }
  relayout();
}

function poll(){
  fetch('/api/sessions').then(function(r){ return r.json(); }).then(function(list){
    (list||[]).forEach(function(s){
      if(!panels[s.id]) createPanel(s);
      else if(s.done && !panels[s.id].done) markDone(panels[s.id]); // crashed sessions have no done record
    });
  }).catch(function(){}).then(function(){ setTimeout(poll, 2000); });
}

var tabEls = document.querySelectorAll('.tab');
for (var i=0; i<tabEls.length; i++){
  tabEls[i].addEventListener('click', function(){
    currentTab = this.getAttribute('data-tab');
    for (var j=0; j<tabEls.length; j++){
      tabEls[j].classList.toggle('sel', tabEls[j].getAttribute('data-tab') === currentTab);
    }
    relayout();
  });
}

relayout();
poll();
</script>
</body>
</html>`
