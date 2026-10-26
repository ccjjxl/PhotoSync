package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// runServe exposes a small local web UI so the phone's library can be browsed
// and synced from a browser — useful on Windows where people prefer a GUI.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8788", "本地监听地址")
	openBrowser := fs.Bool("open", true, "启动后自动打开浏览器")
	_ = fs.Parse(args)

	cfg := cfgOrDie()
	srv := &webUI{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.index)
	mux.HandleFunc("/api/state", srv.stateAPI)
	mux.HandleFunc("/api/discover", srv.discoverAPI)
	mux.HandleFunc("/api/connect", srv.connectAPI)
	mux.HandleFunc("/api/sync", srv.syncAPI)
	mux.HandleFunc("/api/status", srv.statusAPI)
	mux.HandleFunc("/api/cancel", srv.cancelAPI)
	mux.HandleFunc("/api/forget", srv.forgetAPI)
	mux.HandleFunc("/thumb", srv.thumbProxy)
	mux.HandleFunc("/media", srv.mediaProxy)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法监听 %s: %v\n", *addr, err)
		return 1
	}
	url := "http://" + ln.Addr().String()
	fmt.Println("PhotoSync Web UI:", url)
	fmt.Println("按 Ctrl-C 退出")
	if *openBrowser {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = openInBrowser(url)
		}()
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "服务异常: %v\n", err)
		return 1
	}
	return 0
}

type webUI struct {
	cfg     *Config
	mu      sync.Mutex
	cli     *Client
	dev     *Device
	syncing map[string]syncState // keyed by device id
	order   []string             // device ids in start order
	cancel  context.CancelFunc
}

type syncState struct {
	Device   string    `json:"device"`
	Running  bool      `json:"running"`
	Message  string    `json:"message"`
	Done     int64     `json:"done"`
	Total    int64     `json:"total"`
	Bytes    int64     `json:"bytes"`
	BytesTot int64     `json:"bytesTotal"`
	Failed   int64     `json:"failed"`
	Skipped  int64     `json:"skipped"`
	Speed    float64   `json:"speed"`
	ETA      string    `json:"eta"`
	Finished bool      `json:"finished"`
	Error    string    `json:"error"`
	Started  time.Time `json:"started"`
}

// reconnectDevice opens an API client for an already-paired device using its
// stored token and endpoint.
func reconnectDevice(ctx context.Context, cfg *Config, d *Device) (*Client, error) {
	if d.Endpoint == "" {
		// Endpoint unknown: fall back to discovery and match by id.
		offers, err := Discover(1800 * time.Millisecond)
		if err != nil {
			return nil, err
		}
		for _, o := range offers {
			if o.DeviceID == d.ID {
				c := NewClient(o.BaseURL(), d.Token)
				if err := c.Handshake(ctx); err != nil {
					return nil, err
				}
				return c, nil
			}
		}
		return nil, fmt.Errorf("设备不在线")
	}
	c := NewClient("http://"+d.Endpoint, d.Token)
	if err := c.Handshake(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (w *webUI) index(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = webTemplate.Execute(rw, map[string]string{"Version": version})
}

func (w *webUI) stateAPI(rw http.ResponseWriter, r *http.Request) {
	outDir := w.cfg.OutDir
	if outDir == "" {
		outDir = DefaultOutDir()
	}
	type devView struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Folder   string `json:"folder"`
		Dir      string `json:"dir"`
		Model    string `json:"model,omitempty"`
		Android  string `json:"android,omitempty"`
		LastSync string `json:"lastSync,omitempty"`
		Files    int    `json:"files"`
		Bytes    int64  `json:"bytes"`
		Online   bool   `json:"online"`
	}
	views := make([]devView, 0, len(w.cfg.Devices))
	for _, d := range w.cfg.Devices {
		v := devView{
			ID: d.ID, Name: d.Name, Folder: d.Folder,
			Dir: w.cfg.DeviceDir(d), Model: d.Model, Android: d.Android,
		}
		if d.LastSync > 0 {
			v.LastSync = time.Unix(d.LastSync, 0).Format("01-02 15:04")
		}
		if man, err := OpenManifest(w.cfg.DeviceDir(d), d.Name); err == nil {
			v.Files = man.Len()
			v.Bytes = man.TotalBytes()
		}
		views = append(views, v)
	}
	out := map[string]any{
		"outDir":  outDir,
		"path":    ConfigPath(),
		"devices": views,
	}
	if w.cfg.LastSync > 0 {
		out["lastSync"] = time.Unix(w.cfg.LastSync, 0).Format("2006-01-02 15:04")
	}
	writeJSON(rw, out)
}

func (w *webUI) discoverAPI(rw http.ResponseWriter, r *http.Request) {
	offers, err := Discover(2 * time.Second)
	if err != nil {
		writeJSON(rw, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(rw, map[string]any{"offers": offers})
}

func (w *webUI) connectAPI(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		Code  string `json:"code"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(rw, map[string]any{"error": "请求格式错误"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	client, offer, err := connect(ctx, w.cfg, body.Host, body.Port, body.Code, body.Token, false)
	if err != nil {
		writeJSON(rw, map[string]any{"error": err.Error()})
		return
	}
	if w.cfg.OutDir == "" {
		w.cfg.OutDir = DefaultOutDir()
		_ = os.MkdirAll(w.cfg.OutDir, 0o755)
	}
	info := client.Device()
	suggested := info.Folder
	if suggested == "" {
		suggested = sanitizeDeviceFolder(idOr(info.Device, info.DeviceID))
	}
	dev := w.cfg.UpsertDevice(info, w.cfg.uniqueFolder(sanitizeDeviceFolder(suggested), info.DeviceID))
	dev.Token = client.Token()
	_ = w.cfg.Save()

	w.mu.Lock()
	w.cli = client
	w.dev = dev
	w.mu.Unlock()

	writeJSON(rw, map[string]any{
		"ok":     true,
		"device": info,
		"offer":  offer,
		"outDir": w.cfg.OutDir,
		"dir":    w.cfg.DeviceDir(dev),
		"record": dev,
	})
}

func (w *webUI) syncAPI(rw http.ResponseWriter, r *http.Request) {
	// Decode the body first: it may name a specific device.
	body := struct {
		Full   bool   `json:"full"`
		Videos bool   `json:"videos"`
		Repair bool   `json:"repair"`
		Device string `json:"device"`
	}{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	w.mu.Lock()
	cli := w.cli
	picked := w.dev
	w.mu.Unlock()

	// When a device id was supplied (or we have known devices), resolve it.
	if body.Device != "" {
		if d := w.cfg.FindDevice(body.Device); d != nil {
			picked = d
			cli = nil // force reconnect using the stored token
		}
	} else if len(w.cfg.Devices) > 1 {
		// "同步全部": launch one goroutine per known device and return early.
		names := make([]string, 0, len(w.cfg.Devices))
		for _, d := range w.cfg.Devices {
			names = append(names, d.Name)
		}
		go w.syncMany(body.Full, body.Videos, body.Repair)
		writeJSON(rw, map[string]any{"ok": true, "queued": names})
		return
	}
	if cli == nil && picked != nil {
		// Re-home the client onto this device's recorded endpoint.
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		c, err := reconnectDevice(ctx, w.cfg, picked)
		cancel()
		if err != nil {
			writeJSON(rw, map[string]any{"error": fmt.Sprintf("%s: %v", picked.Name, err)})
			return
		}
		cli = c
	}
	if cli == nil {
		writeJSON(rw, map[string]any{"error": "请先连接设备"})
		return
	}
	dev := picked
	w.mu.Lock()
	devID := ""
	if dev != nil {
		devID = dev.ID
	}
	if st, ok := w.syncing[devID]; ok && st.Running {
		w.mu.Unlock()
		writeJSON(rw, map[string]any{"error": "该设备已在同步中"})
		return
	}
	w.mu.Unlock()

	if w.cfg.OutDir == "" {
		w.cfg.OutDir = DefaultOutDir()
	}
	outDir := w.cfg.DeviceDir(dev)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		writeJSON(rw, map[string]any{"error": err.Error()})
		return
	}
	man, err := OpenManifest(outDir, cli.Device().Device)
	if err != nil {
		writeJSON(rw, map[string]any{"error": err.Error()})
		return
	}
	mode := ModeIncremental
	if body.Full {
		mode = ModeFull
	} else if body.Repair {
		mode = ModeRepair
	}
	opts := Options{
		OutDir:        outDir,
		Workers:       defaultWorkers(),
		IncludeVideos: body.Videos,
		Mode:          mode,
		PageSize:      300,
		MaxRetries:    3,
	}

	ctx, cancel := context.WithCancel(context.Background())

	w.mu.Lock()
	if w.syncing == nil {
		w.syncing = map[string]syncState{}
	}
	w.syncing[devID] = syncState{Running: true, Started: time.Now(), Message: "扫描中…", Device: devID}
	if !containsStr(w.order, devID) {
		w.order = append(w.order, devID)
	}
	w.cancel = cancel
	w.mu.Unlock()

	go func() {
		defer cancel()
		syncer := NewSyncer(cli, man, opts, func(s *Stats, ev Event, item *MediaItem, err error, dst string) {
			w.mu.Lock()
			defer w.mu.Unlock()
			st := w.syncing[devID]
			st.Done = atomicInt(&s.DoneFiles)
			st.Total = atomicInt(&s.TotalFiles)
			st.Bytes = atomicInt(&s.LiveBytes)
			st.BytesTot = atomicInt(&s.TotalBytes)
			st.Failed = atomicInt(&s.Failed)
			st.Skipped = atomicInt(&s.Skipped)
			st.Speed = s.Speed()
			st.ETA = humanETA(s.ETA())
			st.Device = devID
			if err != nil {
				st.Error = err.Error()
			}
			switch ev {
			case EventStart:
				if item != nil {
					st.Message = item.Path
				}
			case EventDiscovered:
				st.Message = "开始传输"
			case EventFinished:
				st.Message = "完成"
			}
			w.syncing[devID] = st
		})
		err := syncer.Run(ctx)
		w.mu.Lock()
		st := w.syncing[devID]
		st.Running = false
		st.Finished = true
		if err != nil {
			if errors.Is(err, context.Canceled) {
				st.Message = "已取消"
			} else {
				st.Error = err.Error()
				st.Message = "同步失败"
			}
		} else {
			st.Message = "同步完成"
			if d := w.cfg.FindDevice(devID); d != nil {
				d.LastSync = time.Now().Unix()
			}
			w.cfg.LastSync = time.Now().Unix()
			_ = w.cfg.Save()
		}
		w.syncing[devID] = st
		w.cancel = nil
		w.mu.Unlock()
	}()

	writeJSON(rw, map[string]any{"ok": true})
}

func atomicInt(p *int64) int64 { return loadInt64(p) }

func (w *webUI) statusAPI(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	list := make([]syncState, 0, len(w.order))
	anyRunning := false
	for _, id := range w.order {
		if st, ok := w.syncing[id]; ok {
			list = append(list, st)
			if st.Running {
				anyRunning = true
			}
		}
	}
	w.mu.Unlock()
	writeJSON(rw, map[string]any{
		"running": anyRunning,
		"devices": list,
	})
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (w *webUI) forgetAPI(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(rw, map[string]any{"error": "缺少 id"})
		return
	}
	w.cfg.RemoveDevice(body.ID)
	if err := w.cfg.Save(); err != nil {
		writeJSON(rw, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(rw, map[string]any{"ok": true})
}

func (w *webUI) cancelAPI(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(rw, map[string]any{"ok": true})
}

func (w *webUI) thumbProxy(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	cli := w.cli
	w.mu.Unlock()
	if cli == nil {
		http.Error(rw, "not connected", 400)
		return
	}
	q := r.URL.Query()
	url := cli.Base() + "/api/thumb?id=" + q.Get("id") + "&size=" + q.Get("size")
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-PhotoSync-Token", cli.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(rw, err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	rw.Header().Set("Content-Type", "image/jpeg")
	rw.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(rw, resp.Body)
}

func (w *webUI) mediaProxy(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	cli := w.cli
	w.mu.Unlock()
	if cli == nil {
		http.Error(rw, "not connected", 400)
		return
	}
	url := cli.Base() + "/api/file?" + r.URL.RawQuery
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-PhotoSync-Token", cli.Token())
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(rw, err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			rw.Header().Set(h, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(rw, resp.Body)
}

// syncMany runs every known device concurrently (bounded), each into its own dir.
func (w *webUI) syncMany(full, videos, repair bool) {
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, d := range w.cfg.Devices {
		wg.Add(1)
		go func(d *Device) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.syncOne(d, full, videos, repair)
		}(d)
	}
	wg.Wait()
}

// syncOne is the per-device worker used by syncMany (no HTTP response).
func (w *webUI) syncOne(d *Device, full, videos, repair bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	devID := d.ID
	w.mu.Lock()
	if w.syncing == nil {
		w.syncing = map[string]syncState{}
	}
	w.syncing[devID] = syncState{Running: true, Started: time.Now(), Message: "连接中…", Device: devID}
	if !containsStr(w.order, devID) {
		w.order = append(w.order, devID)
	}
	w.mu.Unlock()

	setState := func(fn func(*syncState)) {
		w.mu.Lock()
		st := w.syncing[devID]
		fn(&st)
		st.Device = devID
		w.syncing[devID] = st
		w.mu.Unlock()
	}

	client, err := reconnectDevice(ctx, w.cfg, d)
	if err != nil {
		setState(func(st *syncState) {
			st.Running, st.Finished = false, true
			st.Error = err.Error()
			st.Message = "连接失败"
		})
		return
	}

	if w.cfg.OutDir == "" {
		w.cfg.OutDir = DefaultOutDir()
	}
	dir := w.cfg.DeviceDir(d)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		setState(func(st *syncState) {
			st.Running, st.Finished = false, true
			st.Error = err.Error()
		})
		return
	}
	man, err := OpenManifest(dir, d.Name)
	if err != nil {
		setState(func(st *syncState) {
			st.Running, st.Finished = false, true
			st.Error = err.Error()
		})
		return
	}
	mode := ModeIncremental
	if full {
		mode = ModeFull
	} else if repair {
		mode = ModeRepair
	}
	opts := Options{
		OutDir:        dir,
		Workers:       defaultWorkers(),
		IncludeVideos: videos && d.VideosEnabled(),
		Mode:          mode,
		PageSize:      300,
		MaxRetries:    3,
		Label:         d.Name,
	}

	syncer := NewSyncer(client, man, opts, func(s *Stats, ev Event, item *MediaItem, err error, dst string) {
		setState(func(st *syncState) {
			st.Done = atomicInt(&s.DoneFiles)
			st.Total = atomicInt(&s.TotalFiles)
			st.Bytes = atomicInt(&s.LiveBytes)
			st.BytesTot = atomicInt(&s.TotalBytes)
			st.Failed = atomicInt(&s.Failed)
			st.Skipped = atomicInt(&s.Skipped)
			st.Speed = s.Speed()
			st.ETA = humanETA(s.ETA())
			if err != nil {
				st.Error = err.Error()
			}
			switch ev {
			case EventStart:
				if item != nil {
					st.Message = item.Path
				}
			case EventDiscovered:
				st.Message = "开始传输"
			case EventFinished:
				st.Message = "完成"
			}
		})
	})
	runErr := syncer.Run(ctx)
	setState(func(st *syncState) {
		st.Running, st.Finished = false, true
		if runErr != nil {
			st.Error = runErr.Error()
			st.Message = "同步失败"
		} else {
			st.Message = "同步完成"
		}
	})
	if runErr == nil {
		if dd := w.cfg.FindDevice(devID); dd != nil {
			dd.LastSync = time.Now().Unix()
		}
		w.cfg.LastSync = time.Now().Unix()
		_ = w.cfg.Save()
	}
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(rw).Encode(v)
}

var webTemplate = template.Must(template.New("ui").Funcs(template.FuncMap{
	"upper": strings.ToUpper,
}).Parse(webHTML))
