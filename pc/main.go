package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// version is overridden at build time with -X main.version=… (see build.sh / CI).
// The fallback matches VERSION in the repo root.
var version = "1.3.1"

const usageText = `PhotoSync 桌面客户端 %s — 快速把安卓照片同步到电脑

用法:
  sync-client                     启动交互式向导（推荐）
  sync-client sync [参数]         同步所有已配对手机
  sync-client devices             列出已配对手机
  sync-client folders             列出手机上的相簿及是否同步
  sync-client discover            扫描局域网内的手机
  sync-client pair --host IP      与手机配对
  sync-client forget <设备>       移除已配对手机
  sync-client status              查看配置与同步状态
  sync-client serve               启动 Web UI（用浏览器管理）

多手机:
  默认会同步「所有」发现到的 / 已配对的手机，每台各存到独立子目录：
       ~/Pictures/Android/<设备名>-<设备ID>/DCIM/Camera/...
  想只同步某一台，用 --device 指定名称、ID 或目录片段。

常用示例:
  sync-client sync                       同步全部手机（增量）
  sync-client sync --device Pixel        只同步名字含 Pixel 的手机
  sync-client sync --device 3f2a91c4     按设备 ID 同步
  sync-client sync --host 192.168.1.20 --full
  sync-client sync --watch 60            每 60 秒检查所有手机的新照片
  sync-client devices                    查看已配对手机列表

sync 参数:
  --host IP[:PORT]     指定手机地址（跳过自动发现）
  --device NAME|ID     只同步匹配的手机（可重复使用，逗号分隔）
  --folder PATH        只同步指定文件夹（可重复或逗号分隔）
  --all                同步所有发现的手机（多设备时的默认行为）
  --out DIR            相册根目录（默认 ~/Pictures/Android），每台手机一个子目录
  --flat-dir           所有手机平铺到 --out，不用子目录（不推荐）
  --workers N          每台手机的并发下载数（默认 CPU 核数，4~12）
  --parallel-devices N 同时同步几台手机（默认 2）
  --full               全量同步（重新校验并覆盖）
  --repair             只补齐缺失或损坏的文件
  --since SEC/时间     仅同步该时间之后修改的文件，如 24h、7d、2024-01-01
  --no-videos          不同步视频
  --flat               不保留相簿目录结构
  --overwrite          忽略清单，强制重新下载
  --dry-run            只列出将要传输的内容
  --watch SEC          循环增量同步
  --quiet              只输出错误与最终统计
  --code 123456        非交互提供配对码（多手机时对所有设备生效）
  --token HEX          直接使用令牌
  --verbose            打印更多细节
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runWizard()
		return
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Printf(usageText, version)
	case "-v", "--version", "version":
		fmt.Println("sync-client", version)
	case "sync":
		os.Exit(runSync(args[1:]))
	case "discover", "scan":
		os.Exit(runDiscover(args[1:]))
	case "pair":
		os.Exit(runPair(args[1:]))
	case "folders", "albums":
		os.Exit(runFolders(args[1:]))
	case "devices", "list":
		os.Exit(runDevices(args[1:]))
	case "forget", "remove":
		os.Exit(runForget(args[1:]))
	case "status":
		os.Exit(runStatus())
	case "serve", "web":
		os.Exit(runServe(args[1:]))
	case "wizard", "ui":
		runWizard()
	default:
		// Allow `sync-client --out dir` shorthand for `sync-client sync --out dir`.
		if strings.HasPrefix(args[0], "-") {
			os.Exit(runSync(args))
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", args[0])
		fmt.Printf(usageText, version)
		os.Exit(2)
	}
}

// ------------------------------------------------------------------ plumbing

func cfgOrDie() *Config {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// Selector narrows down which phone(s) to talk to.
type Selector struct {
	Host      string // explicit IP[:PORT]
	Port      int
	DeviceID  string // match a known device by id
	DeviceSel string // match by name or folder substring
	All       bool   // every discovered/known device
	Quiet     bool   // suppress the "searching" banner
}

// connection is one ready-to-use phone.
type connection struct {
	Client *Client
	Offer  *Offer
	Device *Device
}

// resolveOffers finds candidate phones according to the selector.
func resolveOffers(ctx context.Context, cfg *Config, sel Selector, interactive bool) ([]Offer, error) {
	// 1. explicit host wins
	if sel.Host != "" {
		o, err := GuessFromHost(ctx, sel.Host, sel.Port)
		if err != nil {
			return nil, fmt.Errorf("无法连接 %s: %w", sel.Host, err)
		}
		return []Offer{*o}, nil
	}

	// 2. discovery: broadcast and unicast sweep run concurrently, because many
	//    routers silently drop broadcast. Merging both keeps discovery at ~1-2s
	//    whether or not broadcast works.
	if !sel.Quiet {
		fmt.Println("正在局域网内搜索手机…")
	}
	type discResult struct {
		offers []Offer
		err    error
	}
	bcastCh := make(chan discResult, 1)
	sweepCh := make(chan []Offer, 1)
	go func() {
		o, err := DiscoverParallel(1500 * time.Millisecond)
		bcastCh <- discResult{offers: o, err: err}
	}()
	go func() { sweepCh <- sweepLAN() }()

	br := <-bcastCh
	scanned := <-sweepCh
	if br.err != nil && len(br.offers) == 0 && len(scanned) == 0 {
		return nil, br.err
	}
	offers := mergeOffers(br.offers, scanned)

	// A known device's stored endpoint is often better than a freshly
	// discovered one (NAT, VPN, port-forwarding, adb tunnel). Always probe the
	// stored endpoint first and prefer it when it answers.
	byID := map[string]int{}
	for i := range offers {
		if offers[i].DeviceID != "" {
			byID[offers[i].DeviceID] = i
		}
	}
	for _, d := range cfg.Devices {
		if d.Endpoint == "" {
			continue
		}
		probe, perr := GuessFromHost(ctx, d.Endpoint, 0)
		if perr != nil {
			continue
		}
		// Carry over identity discovered over the network if the probe lacked it.
		if probe.DeviceID == "" {
			probe.DeviceID = d.ID
		}
		if probe.Device == "" {
			probe.Device = d.Name
		}
		if probe.Folder == "" {
			probe.Folder = d.Folder
		}
		if idx, ok := byID[d.ID]; ok {
			// Replace the discovery entry with the reachable stored endpoint.
			offers[idx] = *probe
		} else {
			offers = append(offers, *probe)
			byID[d.ID] = len(offers) - 1
		}
	}

	// Verify reachability. A broadcast reply can advertise an address we cannot
	// reach (NAT, VPN, wrong interface), so everything gets a quick HTTP probe.
	offers = filterReachable(ctx, cfg, offers, !sel.Quiet)

	if len(offers) == 0 {
		return nil, errors.New("未发现设备。请确认：\n  1) 手机与电脑在同一 Wi-Fi\n  2) 手机 App 中「同步服务」已开启\n  3) 路由器未开启 AP 隔离\n也可用 --host <手机IP> 手动指定")
	}

	// Stable order: known devices first (by last sync), then new ones.
	sort.SliceStable(offers, func(i, j int) bool {
		return offers[i].Device < offers[j].Device
	})
	return offers, nil
}

// filterReachable keeps only offers that answer a quick HTTP ping.
// Probes run in parallel so several phones cost one round-trip, not N.
func filterReachable(ctx context.Context, cfg *Config, offers []Offer, warn bool) []Offer {
	type verdict struct {
		idx int
		ok  bool
	}
	results := make([]verdict, len(offers))
	var wg sync.WaitGroup
	for i, o := range offers {
		if o.BaseURL() == "" {
			continue
		}
		wg.Add(1)
		go func(i int, o Offer) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			_, err := GuessFromHost(probeCtx, o.BestIP(), o.Port)
			results[i] = verdict{idx: i, ok: err == nil}
		}(i, o)
	}
	wg.Wait()

	out := make([]Offer, 0, len(offers))
	seen := map[string]bool{}
	for _, r := range results {
		if !r.ok {
			o := offers[r.idx]
			if warn && cfg.FindDevice(o.DeviceID) == nil {
				fmt.Fprintf(os.Stderr, "  ⚠ 跳过不可达设备 %s (%s)\n", o.Device, o.BaseURL())
			}
			continue
		}
		o := offers[r.idx]
		base := o.BaseURL()
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, o)
	}
	return out
}

// mergeOffers deduplicates offers by device id, preferring the sweep result
// (which is inherently reachable) over a broadcast reply.
func mergeOffers(broadcast, swept []Offer) []Offer {
	byID := map[string]Offer{}
	order := make([]string, 0, len(broadcast)+len(swept))
	add := func(o Offer) {
		key := o.DeviceID
		if key == "" {
			key = o.Device + "|" + o.BaseURL()
		}
		if _, exists := byID[key]; !exists {
			order = append(order, key)
		}
		byID[key] = o
	}
	for _, o := range broadcast {
		add(o)
	}
	for _, o := range swept {
		add(o)
	}
	out := make([]Offer, 0, len(order))
	for _, k := range order {
		out = append(out, byID[k])
	}
	return out
}

// sweepLAN performs a unicast sweep of every LAN host and returns the phones
// that answered. Used when UDP broadcast is filtered by the network.
func sweepLAN() []Offer {
	return sweepLANOffers(900 * time.Millisecond)
}

// pickOffers applies the selector and (when interactive) asks the user.
func pickOffers(offers []Offer, sel Selector, cfg *Config, interactive bool) ([]Offer, error) {
	// Filter by id / name when requested.
	if sel.DeviceID != "" || sel.DeviceSel != "" {
		want := strings.ToLower(strings.TrimSpace(firstNonEmpty(sel.DeviceID, sel.DeviceSel)))
		var out []Offer
		for _, o := range offers {
			hay := strings.ToLower(o.Device + "|" + o.DeviceID + "|" + o.Folder)
			if strings.Contains(hay, want) {
				out = append(out, o)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("没有匹配 %q 的设备（可用 sync-client devices 查看）", want)
		}
		return out, nil
	}

	if sel.All {
		return offers, nil
	}

	if len(offers) == 1 {
		return offers, nil
	}

	// Multiple phones: interactive pick, otherwise sync them all.
	if !interactive {
		fmt.Printf("发现 %d 台设备，将全部同步（可用 --device 只选一台）\n", len(offers))
		return offers, nil
	}
	fmt.Printf("\n发现 %d 台设备:\n", len(offers))
	for i := range offers {
		o := offers[i]
		known := cfg.FindDevice(o.DeviceID)
		mark := "新设备"
		if known != nil {
			mark = "已配对"
			if known.LastSync > 0 {
				mark = "上次 " + time.Unix(known.LastSync, 0).Format("01-02 15:04")
			}
		}
		fmt.Printf("  %d) %-24s %-28s %s\n", i+1, o.Device, o.BaseURL(), mark)
	}
	fmt.Printf("  a) 全部同步\n")
	for {
		ans := strings.ToLower(prompt(fmt.Sprintf("请选择 [1-%d/a]（直接回车=全部）: ", len(offers))))
		if ans == "" || ans == "a" || ans == "all" {
			return offers, nil
		}
		if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(offers) {
			return []Offer{offers[n-1]}, nil
		}
	}
}

// endpointFromURL turns "http://1.2.3.4:8737" into "1.2.3.4:8737".
func endpointFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// connectOne establishes a session with a single phone, pairing when needed.
func connectOne(ctx context.Context, cfg *Config, offer Offer, code, token string, interactive bool) (*connection, error) {
	base := offer.BaseURL()
	if base == "" {
		return nil, errors.New("设备未报告可用地址")
	}

	// Reuse the stored token for this exact phone.
	known := cfg.FindDevice(offer.DeviceID)
	tok := token
	if tok == "" && known != nil {
		tok = known.Token
	}
	client := NewClient(base, tok)

	err := client.Handshake(ctx)
	if errors.Is(err, ErrUnauthorized) || (err == nil && tok == "") {
		if offer.OpenAccess {
			fmt.Printf("→ %s 已开启免配对访问\n", offer.Device)
		} else {
			inputCode := code
			if inputCode == "" {
				if !interactive {
					return nil, errors.New("需要配对：请加 --code <6位配对码>")
				}
				inputCode = prompt(fmt.Sprintf("请输入 %s 的 6 位配对码: ", offer.Device))
			}
			if err := client.Pair(ctx, strings.TrimSpace(inputCode)); err != nil {
				return nil, fmt.Errorf("%s: %w", offer.Device, err)
			}
			fmt.Printf("✓ %s 配对成功\n", offer.Device)
		}
		if err := client.Handshake(ctx); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("%s: %w", offer.Device, err)
	}

	info := client.Device()
	// The phone tells us its preferred folder; fall back to name-id.
	suggested := info.Folder
	if suggested == "" {
		suggested = sanitizeDeviceFolder(idOr(info.Device, info.DeviceID))
	}
	dev := cfg.UpsertDevice(info, cfg.uniqueFolder(sanitizeDeviceFolder(suggested), info.DeviceID))
	dev.Token = client.Token()
	// Remember the address we actually reached, not the one the phone reports:
	// behind NAT / adb forwarding / a router port-map they differ, and only the
	// reachable one is useful next time.
	if ep := endpointFromURL(base); ep != "" {
		dev.Endpoint = ep
	}
	if code != "" || token != "" {
		_ = cfg.Save()
	}

	return &connection{Client: client, Offer: &offer, Device: dev}, nil
}

// connectAll opens sessions with every selected phone.
func connectAll(ctx context.Context, cfg *Config, sel Selector, code, token string, interactive bool) ([]*connection, error) {
	offers, err := resolveOffers(ctx, cfg, sel, interactive)
	if err != nil {
		return nil, err
	}
	chosen, err := pickOffers(offers, sel, cfg, interactive)
	if err != nil {
		return nil, err
	}
	var out []*connection
	var firstErr error
	for _, o := range chosen {
		c, err := connectOne(ctx, cfg, o, code, token, interactive)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errors.New("没有可用的设备")
	}
	return out, nil
}

// connect is the single-device variant kept for pair/serve paths.
func connect(ctx context.Context, cfg *Config, host string, port int, code string, token string, interactive bool) (*Client, *Offer, error) {
	sel := Selector{Host: host, Port: port}
	conns, err := connectAll(ctx, cfg, sel, code, token, interactive)
	if err != nil {
		return nil, nil, err
	}
	// Single-device callers want exactly one; pick the first if several appeared.
	return conns[0].Client, conns[0].Offer, nil
}

// ------------------------------------------------------------------ commands

// multiFlag collects a repeatable / comma separated string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*m = append(*m, p)
		}
	}
	return nil
}

func runSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	var (
		host        = fs.String("host", "", "手机地址 IP[:PORT]")
		port        = fs.Int("port", 0, "手机端口")
		out         = fs.String("out", "", "相册根目录")
		workers     = fs.Int("workers", defaultWorkers(), "每台手机并发数")
		devParallel = fs.Int("parallel-devices", 2, "同时同步几台手机")
		full        = fs.Bool("full", false, "全量同步")
		repair      = fs.Bool("repair", false, "修复缺失文件")
		since       = fs.String("since", "", "只同步此时间之后的文件")
		noVideos    = fs.Bool("no-videos", false, "不同步视频")
		flat        = fs.Bool("flat", false, "不保留相簿目录结构")
		flatDir     = fs.Bool("flat-dir", false, "所有手机共用一个目录")
		overwrite   = fs.Bool("overwrite", false, "强制重新下载")
		dryRun      = fs.Bool("dry-run", false, "只列出计划")
		watch       = fs.Duration("watch", 0, "循环增量同步间隔")
		quiet       = fs.Bool("quiet", false, "静默模式")
		verbose     = fs.Bool("verbose", false, "详细输出")
		code        = fs.String("code", "", "配对码")
		token       = fs.String("token", "", "访问令牌")
		pageSize    = fs.Int("page-size", 300, "分页大小")
		maxRetries  = fs.Int("retries", 3, "失败重试次数")
		allDevices  = fs.Bool("all", false, "同步所有设备")
	)
	var deviceSel multiFlag
	fs.Var(&deviceSel, "device", "只同步匹配的手机（名称/ID/目录，可重复或逗号分隔）")
	var folderSel multiFlag
	fs.Var(&folderSel, "folder", "只同步这些文件夹（可重复或逗号分隔）")
	fs.Usage = func() { fmt.Printf(usageText, version) }
	_ = fs.Parse(args)

	cfg := cfgOrDie()

	// ---------------------------------------------------------------- library root
	rootDir := *out
	if rootDir == "" {
		rootDir = cfg.OutDir
	}
	if rootDir == "" {
		rootDir = DefaultOutDir()
	}
	abs, err := filepath.Abs(expandHome(rootDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "目录无效: %v\n", err)
		return 1
	}
	rootDir = abs
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "无法创建目录 %s: %v\n", rootDir, err)
		return 1
	}
	cfg.OutDir = rootDir
	_ = cfg.Save()

	// ---------------------------------------------------------------- time filter
	sinceTS, err := parseSince(*since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "时间格式错误: %v\n", err)
		return 1
	}

	mode := ModeIncremental
	if *full {
		mode = ModeFull
	} else if *repair {
		mode = ModeRepair
	}

	sel := Selector{
		Host:  *host,
		Port:  *port,
		All:   *allDevices || len(deviceSel) == 0,
		Quiet: *quiet,
	}
	if len(deviceSel) > 0 {
		sel.All = *allDevices
		sel.DeviceSel = strings.Join(deviceSel, "|")
		sel.DeviceID = sel.DeviceSel
	}

	interactive := !*quiet && isTTY()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanup := installSignalHandler(cancel)
	defer cleanup()

	round := 0
	for {
		round++
		if *watch > 0 && round > 1 {
			// Later rounds only pick up newly modified files.
			if newest := cfg.newestMtime(); newest > 0 {
				sinceTS = newest - 300
			} else {
				sinceTS = time.Now().Add(-1 * time.Hour).Unix()
			}
			mode = ModeIncremental
			*overwrite = false
		}

		conns, err := connectAll(ctx, cfg, sel, *code, *token, interactive)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
			return 1
		}

		// -------------------------------------------------------- describe plan
		if !*quiet {
			fmt.Println()
			switch len(conns) {
			case 1:
				fmt.Printf("设备: %s   保存到 %s\n",
					conns[0].Device.Name, cfg.DeviceDir(conns[0].Device))
			default:
				fmt.Printf("同步 %d 台手机到 %s\n", len(conns), rootDir)
				for _, c := range conns {
					fmt.Printf("  · %-22s → %s\n", c.Device.Name, relTo(rootDir, cfg.DeviceDir(c.Device)))
				}
			}
		}

		var (
			totalFiles, totalBytes, totalFailed, totalSkipped int64
			syncErrs                                          []string
		)

		// -------------------------------------------------------- per-device run
		type result struct {
			conn    *connection
			files   int64
			bytes   int64
			failed  int64
			skipped int64
			err     error
		}
		results := make(chan result, len(conns))
		sem := make(chan struct{}, maxInt(1, *devParallel))
		var wg sync.WaitGroup

		for _, conn := range conns {
			wg.Add(1)
			go func(conn *connection) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				dir := cfg.DeviceDir(conn.Device)
				if *flatDir {
					dir = rootDir
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					results <- result{conn: conn, err: err}
					return
				}
				man, err := OpenManifest(dir, conn.Device.Name)
				if err != nil {
					results <- result{conn: conn, err: err}
					return
				}

				opts := Options{
					OutDir:        dir,
					Workers:       *workers,
					Folders:       folderSel,
					IncludeVideos: !*noVideos && conn.Device.VideosEnabled(),
					Mode:          mode,
					DryRun:        *dryRun,
					Overwrite:     *overwrite,
					Since:         sinceTS,
					PageSize:      *pageSize,
					Quiet:         *quiet,
					Verbose:       *verbose,
					MaxRetries:    *maxRetries,
					Flat:          *flat,
					Label:         conn.Device.Name,
				}

				rend := newRenderer(*quiet, *verbose, conn.Device.Name, len(conns) > 1)
				syncer := NewSyncer(conn.Client, man, opts, rend.progress())
				err = syncer.Run(ctx)
				st := syncer.Stats()
				results <- result{
					conn:    conn,
					files:   atomic.LoadInt64(&st.DoneFiles),
					bytes:   atomic.LoadInt64(&st.DoneBytes),
					failed:  atomic.LoadInt64(&st.Failed),
					skipped: atomic.LoadInt64(&st.Skipped),
					err:     err,
				}
			}(conn)
		}

		// Watch is only meaningful for a single round; we loop ourselves.
		wg.Wait()
		close(results)

		for r := range results {
			totalFiles += r.files
			totalBytes += r.bytes
			totalFailed += r.failed
			totalSkipped += r.skipped
			if r.err != nil {
				if errors.Is(r.err, context.Canceled) {
					fmt.Println("已中断，进度已保存。")
					return 130
				}
				if errors.Is(r.err, ErrUnauthorized) {
					// Token rotated: forget it so the next run re-pairs.
					if d := cfg.FindDevice(r.conn.Device.ID); d != nil {
						d.Token = ""
						_ = cfg.Save()
					}
				}
				syncErrs = append(syncErrs, fmt.Sprintf("%s: %v", r.conn.Device.Name, r.err))
				continue
			}
			if d := cfg.FindDevice(r.conn.Device.ID); d != nil {
				d.LastSync = time.Now().Unix()
				_ = cfg.Save()
			}
		}

		cfg.LastSync = time.Now().Unix()
		_ = cfg.Save()

		// -------------------------------------------------------- summary
		if len(conns) > 1 && !*quiet {
			fmt.Printf("\n═══ 合计：%d 台手机，传输 %d 个文件 (%s)，跳过 %d，失败 %d ═══\n",
				len(conns), totalFiles-totalFailed, humanBytes(totalBytes), totalSkipped, totalFailed)
			for _, c := range conns {
				fmt.Printf("  %-22s %s\n", c.Device.Name, cfg.DeviceDir(c.Device))
			}
		}

		if len(syncErrs) > 0 {
			for _, e := range syncErrs {
				fmt.Fprintf(os.Stderr, "✗ %s\n", e)
			}
			return 1
		}

		if *watch <= 0 {
			return 0
		}
		if !*quiet {
			fmt.Printf("\n等待 %s 后进行下一次增量检查（Ctrl-C 退出）…\n", *watch)
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(*watch):
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// relTo returns path relative to base, falling back to path itself.
func relTo(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel + string(filepath.Separator)
	}
	return path
}

// newestMtime returns the most recent per-device sync timestamp.
func (c *Config) newestMtime() int64 {
	var max int64
	for _, d := range c.Devices {
		if d.LastSync > max {
			max = d.LastSync
		}
	}
	return max
}

// ------------------------------------------------------------------ helpers

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func isTTY() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func prompt(label string) string {
	fmt.Print(label)
	rd := bufio.NewReader(os.Stdin)
	line, _ := rd.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptInt(label string, def int) int {
	v := prompt(label)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// parseSince accepts unix seconds, "24h"/"7d"/"30m", or YYYY-MM-DD.
func parseSince(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1_000_000_000 { // already a unix timestamp
			return n, nil
		}
		return time.Now().Add(-time.Duration(n) * time.Second).Unix(), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d).Unix(), nil
	}
	if strings.HasSuffix(s, "d") {
		if days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64); err == nil {
			return time.Now().Add(-time.Duration(days * 24 * float64(time.Hour))).Unix(), nil
		}
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04", "2006/01/02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Unix(), nil
		}
	}
	return 0, fmt.Errorf("无法识别 %q（可用 24h、7d、2024-05-01 或 unix 时间戳）", s)
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ------------------------------------------------------------------ commands

func runDiscover(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	timeout := fs.Duration("timeout", 2*time.Second, "扫描时长")
	jsonOut := fs.Bool("json", false, "输出 JSON")
	_ = fs.Parse(args)

	cfg := cfgOrDie()
	if !*jsonOut {
		fmt.Println("正在搜索局域网中的 PhotoSync 设备…")
	}
	offers, err := Discover(*timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描失败: %v\n", err)
		return 1
	}
	if len(offers) == 0 {
		fmt.Println("未发现设备。提示：确认手机与电脑在同一 Wi-Fi，且 App 中同步服务已开启。")
		return 1
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Device < offers[j].Device })

	if *jsonOut {
		printJSON(offers)
		return 0
	}
	fmt.Printf("\n发现 %d 台设备:\n\n", len(offers))
	for _, o := range offers {
		known := cfg.FindDevice(o.DeviceID)
		status := "未配对"
		if known != nil {
			status = "已配对"
			if known.LastSync > 0 {
				status = "上次同步 " + time.Unix(known.LastSync, 0).Format("2006-01-02 15:04")
			}
		}
		fmt.Printf("  %-24s %s\n", o.Device, o.BaseURL())
		fmt.Printf("  %-24s 设备ID %s · %s · App %s · %s\n",
			"", orDash(o.DeviceID), o.Model, o.Version, status)
		fmt.Println()
	}
	return 0
}

func runPair(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	host := fs.String("host", "", "手机地址")
	port := fs.Int("port", 0, "端口")
	code := fs.String("code", "", "配对码")
	token := fs.String("token", "", "直接设置令牌")
	_ = fs.Parse(args)

	cfg := cfgOrDie()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	conns, err := connectAll(ctx, cfg, Selector{Host: *host, Port: *port, All: true},
		*code, *token, isTTY())
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		return 1
	}

	fmt.Printf("\n✓ 已配对 %d 台设备\n", len(conns))
	for _, c := range conns {
		info := c.Client.Device()
		fmt.Printf("  %-22s 照片 %d · 视频 %d · %s\n",
			c.Device.Name, info.Photos, info.Videos, humanBytes(info.Bytes))
		fmt.Printf("  %-22s 保存到 %s\n", "", cfg.DeviceDir(c.Device))
	}
	fmt.Printf("\n配置已保存到 %s\n", ConfigPath())
	return 0
}

// runFolders prints the albums on each phone and whether they are enabled.
func runFolders(args []string) int {
	fs := flag.NewFlagSet("folders", flag.ExitOnError)
	host := fs.String("host", "", "手机地址")
	port := fs.Int("port", 0, "端口")
	code := fs.String("code", "", "配对码")
	_ = fs.Parse(args)

	cfg := cfgOrDie()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conns, err := connectAll(ctx, cfg, Selector{Host: *host, Port: *port, All: true, Quiet: true},
		*code, "", isTTY())
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		return 1
	}

	for _, c := range conns {
		folders, filtering, err := c.Client.Folders(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", c.Device.Name, err)
			continue
		}
		info := c.Client.Device()
		fmt.Printf("\n%s (%s)\n", c.Device.Name, c.Device.ID)
		if filtering {
			fmt.Println("  手机端已限定同步范围（在 App 内「同步文件夹」中修改）")
		} else if info.FolderFilter {
			fmt.Println("  手机端已限定同步范围")
		} else {
			fmt.Println("  同步全部文件夹")
		}
		fmt.Printf("  %-44s %8s  %s\n", "文件夹", "数量", "状态")
		for _, f := range folders {
			state := "✓ 同步"
			if !f.Enabled {
				state = "— 已排除"
			}
			fmt.Printf("  %-44s %8d  %s\n", clip(f.Path, 44), f.Count, state)
		}
	}
	fmt.Println("\n提示: 用 --folder <路径> 只同步某个文件夹（手机端必须已允许它）。")
	return 0
}

func runDevices(args []string) int {
	_ = args
	cfg := cfgOrDie()

	out := cfg.OutDir
	if out == "" {
		out = DefaultOutDir()
	}
	fmt.Printf("相册根目录: %s\n", out)
	if len(cfg.Devices) == 0 {
		fmt.Println("\n尚未配对任何手机。运行 sync-client 发现并配对。")
		return 0
	}

	fmt.Printf("\n已配对 %d 台手机:\n", len(cfg.Devices))
	fmt.Printf("  %-4s %-22s %-26s %-17s %s\n", "序号", "名称", "目录", "上次同步", "在线")
	for i, d := range cfg.Devices {
		dir := relTo(out, cfg.DeviceDir(d))
		last := "从未"
		if d.LastSync > 0 {
			last = time.Unix(d.LastSync, 0).Format("01-02 15:04")
		}
		online := "—"
		ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
		if o, err := GuessFromHost(ctx, endpointOr(d), 0); err == nil && o != nil {
			online = "● 在线"
		}
		cancel()
		fmt.Printf("  %-4d %-22s %-26s %-17s %s\n", i+1, clip(d.Name, 22), clip(dir, 26), last, online)
		if d.Model != "" {
			fmt.Printf("       %-20s ID %s · %s · Android %s\n", "", d.ID, d.Model, d.Android)
		}
	}
	fmt.Println("\n提示: 用 --device <名称或ID> 只同步某一台；sync-client forget <名称或ID> 可移除。")
	return 0
}

func runForget(args []string) int {
	fs := flag.NewFlagSet("forget", flag.ExitOnError)
	all := fs.Bool("all", false, "移除全部已配对手机")
	_ = fs.Parse(args)

	cfg := cfgOrDie()
	if *all {
		if !isTTY() {
			fmt.Fprintln(os.Stderr, "✗ 非交互模式请显式指定设备，或用 --all 并加 --yes")
		}
		n := len(cfg.Devices)
		cfg.Devices = nil
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "保存失败: %v\n", err)
			return 1
		}
		fmt.Printf("已移除 %d 台设备的配对记录（照片文件未删除）\n", n)
		return 0
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "用法: sync-client forget <设备名称或ID> [更多…]  |  --all")
		return 2
	}
	removed := 0
	for _, target := range fs.Args() {
		want := strings.ToLower(strings.TrimSpace(target))
		var hit *Device
		for _, d := range cfg.Devices {
			if strings.EqualFold(d.ID, target) ||
				strings.Contains(strings.ToLower(d.Name), want) ||
				strings.Contains(strings.ToLower(d.Folder), want) {
				hit = d
				break
			}
		}
		if hit == nil {
			fmt.Fprintf(os.Stderr, "✗ 未找到设备 %q\n", target)
			continue
		}
		cfg.RemoveDevice(hit.ID)
		fmt.Printf("已移除 %s（照片文件保留在 %s）\n", hit.DisplayName(), cfg.DeviceDir(hit))
		removed++
	}
	if removed > 0 {
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "保存失败: %v\n", err)
			return 1
		}
	}
	if removed == 0 {
		return 1
	}
	return 0
}

func runStatus() int {
	cfg := cfgOrDie()
	fmt.Println("PhotoSync 客户端", version)
	fmt.Println("配置文件:", ConfigPath())
	out := cfg.OutDir
	if out == "" {
		out = DefaultOutDir()
	}
	fmt.Println("相册根目录:", out)
	if cfg.LastSync > 0 {
		fmt.Println("上次运行:", time.Unix(cfg.LastSync, 0).Format("2006-01-02 15:04:05"))
	}

	if len(cfg.Devices) == 0 {
		fmt.Println("\n尚未配对任何手机。")
		return 0
	}

	fmt.Printf("\n已配对 %d 台手机:\n", len(cfg.Devices))
	var totalFiles int
	var totalBytes int64
	for _, d := range cfg.Devices {
		dir := cfg.DeviceDir(d)
		man, err := OpenManifest(dir, d.Name)
		if err != nil {
			fmt.Printf("  %-22s 清单读取失败: %v\n", d.Name, err)
			continue
		}
		totalFiles += man.Len()
		totalBytes += man.TotalBytes()
		last := "从未"
		if d.LastSync > 0 {
			last = time.Unix(d.LastSync, 0).Format("01-02 15:04")
		}
		fmt.Printf("  %-22s %6d 个文件  %9s  上次 %s\n",
			clip(d.Name, 22), man.Len(), humanBytes(man.TotalBytes()), last)
		fmt.Printf("  %-22s %s\n", "", dir)
	}
	fmt.Printf("\n合计: %d 个文件, %s\n", totalFiles, humanBytes(totalBytes))
	return 0
}

// ------------------------------------------------------------------ small utils

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func endpointOr(d *Device) string {
	if d.Endpoint != "" {
		return d.Endpoint
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
