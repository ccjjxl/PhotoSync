package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// printJSON pretty-prints a value to stdout.
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// runWizard is the interactive first-run experience. It supports several
// phones at once: every selected device syncs into its own subdirectory.
func runWizard() {
	cfg := cfgOrDie()

	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────────╮")
	fmt.Printf("  │  PhotoSync 桌面客户端 %-18s │\n", version)
	fmt.Println("  │  把安卓相册快速同步到这台电脑            │")
	fmt.Println("  ╰──────────────────────────────────────────╯")
	fmt.Println()

	// ---------------------------------------------------------------- 1. devices
	fmt.Println("① 搜索设备")
	offers, err := Discover(1800 * time.Millisecond)
	if err != nil || len(offers) == 0 {
		fmt.Println("   未自动发现设备。")
		fmt.Println("   请确认手机 App 已开启「同步服务」，且与电脑在同一 Wi-Fi。")
		host := prompt("   手动输入手机 IP（直接回车退出）: ")
		if strings.TrimSpace(host) == "" {
			fmt.Println("\n已退出。")
			os.Exit(1)
		}
		o, err := GuessFromHost(mustCtx(), host, 8737)
		if err != nil {
			fmt.Fprintf(os.Stderr, "   ✗ 连接失败: %v\n", err)
			os.Exit(1)
		}
		offers = []Offer{*o}
	}

	// Let the user pick one or several phones.
	var chosen []Offer
	if len(offers) == 1 {
		chosen = offers
		fmt.Printf("   ✓ %s  %s\n", offers[0].Device, offers[0].BaseURL())
	} else {
		fmt.Printf("   发现 %d 台设备:\n", len(offers))
		for i, o := range offers {
			known := cfg.FindDevice(o.DeviceID)
			tag := "新设备"
			if known != nil {
				tag = "已配对 → " + known.Folder
			}
			fmt.Printf("   %d) %-24s %-30s %s\n", i+1, o.Device, o.BaseURL(), tag)
		}
		fmt.Println("   a) 全部同步")
		ans := strings.ToLower(strings.TrimSpace(prompt("   选择（多个用逗号分隔，回车=全部）: ")))
		if ans == "" || ans == "a" || ans == "all" {
			chosen = offers
		} else {
			seen := map[int]bool{}
			for _, part := range strings.Split(ans, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(part))
				if err == nil && n >= 1 && n <= len(offers) && !seen[n] {
					seen[n] = true
					chosen = append(chosen, offers[n-1])
				}
			}
			if len(chosen) == 0 {
				chosen = offers[:1]
			}
		}
		fmt.Printf("   ✓ 已选择 %d 台\n", len(chosen))
	}
	fmt.Println()

	// ---------------------------------------------------------------- 2. library root
	fmt.Println("② 相册根目录")
	fmt.Println("   每台手机会在其中创建自己的子文件夹，互不覆盖。")
	def := cfg.OutDir
	if def == "" {
		def = DefaultOutDir()
	}
	root := prompt(fmt.Sprintf("   根目录 [%s]: ", def))
	if strings.TrimSpace(root) == "" {
		root = def
	}
	root = filepath.Join(expandHome(strings.TrimSpace(root)))
	if err := os.MkdirAll(root, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "   ✗ 无法创建目录: %v\n", err)
		os.Exit(1)
	}
	cfg.OutDir = root
	_ = cfg.Save()
	fmt.Printf("   ✓ %s\n\n", root)

	// ---------------------------------------------------------------- 3. mode
	fmt.Println("③ 同步方式")
	fmt.Println("   1) 增量同步 — 只传输新增或修改的照片（推荐）")
	fmt.Println("   2) 全量同步 — 重新校验并下载全部文件")
	fmt.Println("   3) 仅补齐缺失 — 不动已有文件")
	mode := promptInt("   选择 [1]: ", 1)

	workers := defaultWorkers()
	w := prompt(fmt.Sprintf("   每台手机并发数 [%d]: ", workers))
	if n, err := strconv.Atoi(strings.TrimSpace(w)); err == nil && n > 0 {
		workers = n
	}
	fmt.Println()

	// ---------------------------------------------------------------- 4. connect
	fmt.Println("④ 连接设备")
	ctx := mustCtx()
	conns := make([]*connection, 0, len(chosen))
	for _, o := range chosen {
		c, err := connectOne(ctx, cfg, o, "", "", true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "   ✗ %v\n", err)
			continue
		}
		conns = append(conns, c)
	}
	if len(conns) == 0 {
		fmt.Fprintln(os.Stderr, "✗ 没有可用设备")
		os.Exit(1)
	}
	_ = cfg.Save()

	for _, c := range conns {
		info := c.Client.Device()
		fmt.Printf("   %-22s 照片 %d · 视频 %d · %s\n",
			c.Device.Name, info.Photos, info.Videos, humanBytes(info.Bytes))
		fmt.Printf("   %-22s → %s\n", "", cfg.DeviceDir(c.Device))
	}
	fmt.Println()

	// ---------------------------------------------------------------- 5. sync
	fmt.Printf("⑤ 开始同步 %d 台手机\n\n", len(conns))
	syncMode := []SyncMode{ModeIncremental, ModeFull, ModeRepair}[mode-1]

	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	var mu sync.Mutex
	var failures []string

	for _, c := range conns {
		wg.Add(1)
		go func(c *connection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dir := cfg.DeviceDir(c.Device)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", c.Device.Name, err))
				mu.Unlock()
				return
			}
			man, err := OpenManifest(dir, c.Device.Name)
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", c.Device.Name, err))
				mu.Unlock()
				return
			}
			opts := Options{
				OutDir:        dir,
				Workers:       workers,
				IncludeVideos: true,
				Mode:          syncMode,
				PageSize:      300,
				MaxRetries:    3,
				Label:         c.Device.Name,
			}
			rend := newRenderer(false, false, c.Device.Name, len(conns) > 1)
			syncer := NewSyncer(c.Client, man, opts, rend.progress())
			if err := syncer.Run(ctx); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", c.Device.Name, err))
				mu.Unlock()
				return
			}
			if d := cfg.FindDevice(c.Device.ID); d != nil {
				d.LastSync = nowUnix()
			}
		}(c)
	}
	wg.Wait()

	cfg.LastSync = nowUnix()
	_ = cfg.Save()

	fmt.Println()
	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "✗ %s\n", f)
		}
	}
	for _, c := range conns {
		fmt.Printf("✓ %-22s %s\n", c.Device.Name, cfg.DeviceDir(c.Device))
	}
	if len(conns) == 1 {
		_ = openInBrowser(cfg.DeviceDir(conns[0].Device))
	} else {
		_ = openInBrowser(cfg.OutDir)
	}
	fmt.Println("\n下次直接运行 `sync-client` 即可增量同步所有手机。")
	fmt.Println("查看设备列表: sync-client devices")
}
