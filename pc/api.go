package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	discoveryPort  = 8738
	discoveryMagic = "PHOTOSYNC/1"
	apiVersion     = 1
)

// Offer is what the phone broadcasts back on a discovery probe.
type Offer struct {
	Magic      string    `json:"magic"`
	Type       string    `json:"type"`
	App        string    `json:"app"`
	Version    string    `json:"version"`
	API        int       `json:"api"`
	Device     string    `json:"device"`
	DeviceID   string    `json:"deviceId"`
	Folder     string    `json:"folder"`
	Model      string    `json:"model"`
	Port       int       `json:"port"`
	OpenAccess bool      `json:"openAccess"`
	Host       string    `json:"host"`
	Addresses  []Address `json:"addresses"`
}

// Address is a single LAN interface reported by the phone.
type Address struct {
	IP    string `json:"ip"`
	Iface string `json:"iface"`
	Wifi  bool   `json:"wifi"`
}

// BestIP picks the first Wi-Fi address, falling back to anything else.
func (o Offer) BestIP() string {
	for _, a := range o.Addresses {
		if a.Wifi && a.IP != "" {
			return a.IP
		}
	}
	if len(o.Addresses) > 0 {
		return o.Addresses[0].IP
	}
	return ""
}

// BaseURL returns the http endpoint for this device.
func (o Offer) BaseURL() string {
	ip := o.BestIP()
	port := o.Port
	if port == 0 {
		port = 8737
	}
	if ip == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", ip, port)
}

// DeviceInfo mirrors the phone's /api/info response.
type DeviceInfo struct {
	App          string    `json:"app"`
	Version      string    `json:"version"`
	API          int       `json:"api"`
	Device       string    `json:"device"`
	DeviceID     string    `json:"deviceId"`
	Folder       string    `json:"folder"`
	Model        string    `json:"model"`
	Manufacturer string    `json:"manufacturer"`
	Android      string    `json:"android"`
	Port         int       `json:"port"`
	OpenAccess   bool      `json:"openAccess"`
	Photos       int       `json:"photos"`
	Videos       int       `json:"videos"`
	Bytes        int64     `json:"bytes"`
	Addresses    []Address `json:"addresses"`
	// FolderFilter is true when the phone restricts syncing to specific folders.
	FolderFilter bool `json:"folderFilter"`
	// Folders lists the enabled folders ("all" when unrestricted).
	Folders string `json:"folders"`
	// Platform is "android" or "ios"; older Android builds omit it.
	Platform string `json:"platform"`
}

// PlatformLabel returns a short human label for logs.
func (d DeviceInfo) PlatformLabel() string {
	switch d.Platform {
	case "ios":
		return "iOS"
	case "android":
		return "Android"
	default:
		return "Android" // phone did not report it
	}
}

// BestIP picks the device's preferred LAN address.
func (d DeviceInfo) BestIP() string {
	for _, a := range d.Addresses {
		if a.Wifi && a.IP != "" {
			return a.IP
		}
	}
	if len(d.Addresses) > 0 {
		return d.Addresses[0].IP
	}
	return ""
}

// MediaItem is one photo or video on the phone.
type MediaItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Folder string `json:"folder"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"`
	Taken  int64  `json:"taken"`
	Mime   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Video  bool   `json:"video"`
	Key    string `json:"key"`
}

// ListResponse is one page from /api/list.
type ListResponse struct {
	Since   int64       `json:"since"`
	Offset  int         `json:"offset"`
	Limit   int         `json:"limit"`
	Total   int         `json:"total"`
	Count   int         `json:"count"`
	HasMore bool        `json:"hasMore"`
	Items   []MediaItem `json:"items"`
}

// Client talks to a single phone.
type Client struct {
	base   string
	token  string
	http   *http.Client
	device DeviceInfo
}

// dialTimeout is how long we wait for a TCP handshake. Kept short so an
// unreachable phone (stale IP, different Wi-Fi) fails fast instead of stalling
// discovery for 10s per device.
const dialTimeout = 2500 * time.Millisecond

// NewClient builds an API client with a transfer-tuned HTTP transport.
func NewClient(base, token string) *Client {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 300 * time.Millisecond,
		DisableCompression:    true, // media is already compressed
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        256 * 1024,
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Transport: tr, Timeout: 0},
	}
}

// Base returns the endpoint root.
func (c *Client) Base() string { return c.base }

// Token returns the pairing token in use.
func (c *Client) Token() string { return c.token }

// Device returns cached device info (populated by Handshake).
func (c *Client) Device() DeviceInfo { return c.device }

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	u := c.base + path
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PhotoSync-Client/1.0")
	if c.token != "" {
		req.Header.Set("X-PhotoSync-Token", c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// Ping checks that the endpoint speaks our API.
func (c *Client) Ping(ctx context.Context) (*Offer, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/ping", nil)
	if err != nil {
		return nil, err
	}
	defer drainClose(resp)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ping: http %d", resp.StatusCode)
	}
	var out struct {
		App        string `json:"app"`
		Version    string `json:"version"`
		API        int    `json:"api"`
		Device     string `json:"device"`
		DeviceID   string `json:"deviceId"`
		Folder     string `json:"folder"`
		OpenAccess bool   `json:"openAccess"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.API != apiVersion {
		return nil, fmt.Errorf("api version mismatch: phone=%d client=%d", out.API, apiVersion)
	}
	return &Offer{
		App: out.App, Version: out.Version, API: out.API,
		Device: out.Device, DeviceID: out.DeviceID, Folder: out.Folder,
		OpenAccess: out.OpenAccess,
	}, nil
}

// Pair exchanges a 6-digit code for the access token.
func (c *Client) Pair(ctx context.Context, code string) error {
	payload, _ := json.Marshal(map[string]string{"code": code})
	resp, err := c.do(ctx, http.MethodPost, "/api/pair", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer drainClose(resp)
	if resp.StatusCode == 403 {
		return errors.New("配对码不正确")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("pair: http %d", resp.StatusCode)
	}
	var out struct {
		Token    string `json:"token"`
		Device   string `json:"device"`
		DeviceID string `json:"deviceId"`
		Folder   string `json:"folder"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Token == "" {
		return errors.New("配对响应无 token")
	}
	c.token = out.Token
	if c.device.DeviceID == "" {
		c.device.DeviceID = out.DeviceID
	}
	if c.device.Device == "" {
		c.device.Device = out.Device
	}
	if c.device.Folder == "" {
		c.device.Folder = out.Folder
	}
	return nil
}

// Handshake loads /api/info, verifying auth along the way.
func (c *Client) Handshake(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/api/info", nil)
	if err != nil {
		return err
	}
	defer drainClose(resp)
	if resp.StatusCode == 401 {
		return ErrUnauthorized
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("info: http %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&c.device); err != nil {
		return err
	}
	return nil
}

// ErrUnauthorized means the token is missing or stale.
var ErrUnauthorized = errors.New("未授权：需要配对")

// List fetches one page of media metadata. When folder is non-empty the phone
// narrows the result to that folder server-side.
func (c *Client) List(ctx context.Context, since int64, offset, limit int, videos bool, folder string) (*ListResponse, error) {
	q := url.Values{}
	q.Set("since", strconv.FormatInt(since, 10))
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("asc", "1")
	if !videos {
		q.Set("videos", "0")
	}
	if folder != "" {
		q.Set("folder", folder)
	}
	resp, err := c.do(ctx, http.MethodGet, "/api/list?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer drainClose(resp)
	if resp.StatusCode == 401 {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("list: http %d", resp.StatusCode)
	}
	var out ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FolderEntry is one album on the phone.
type FolderEntry struct {
	Path    string `json:"path"`
	Count   int    `json:"count"`
	Enabled bool   `json:"enabled"`
}

// Folders lists the phone's albums and whether each is enabled for syncing.
func (c *Client) Folders(ctx context.Context) ([]FolderEntry, bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/folders", nil)
	if err != nil {
		return nil, false, err
	}
	defer drainClose(resp)
	if resp.StatusCode == 401 {
		return nil, false, ErrUnauthorized
	}
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("folders: http %d", resp.StatusCode)
	}
	var out struct {
		FilterActive bool          `json:"filterActive"`
		Folders      []FolderEntry `json:"folders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false, err
	}
	return out.Folders, out.FilterActive, nil
}

// FileURL is the direct download URL for an item.
func (c *Client) FileURL(id int64) string {
	return fmt.Sprintf("%s/api/file?id=%d", c.base, id)
}

// OpenFile starts a download, optionally resuming from byte offset.
func (c *Client) OpenFile(ctx context.Context, id int64, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.FileURL(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PhotoSync-Client/1.0")
	if c.token != "" {
		req.Header.Set("X-PhotoSync-Token", c.token)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	return c.http.Do(req)
}

func drainClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}

// ------------------------------------------------------------------ discovery

// ListenBroadcast opens a UDP socket with SO_BROADCAST enabled, which is
// required to send to 255.255.255.255 (plain ListenPacket cannot).
func listenBroadcast() (net.PacketConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = setBroadcast(fd)
			}); err != nil {
				return err
			}
			return serr
		},
	}
	return lc.ListenPacket(context.Background(), "udp4", ":0")
}

// Discover finds phones on the local network.
//
// It sends a UDP probe three ways, because real networks block broadcast in
// surprising ways (AP isolation, guest Wi-Fi, routers that drop 255.255.255.255,
// adb tunnels, VPNs):
//
//  1. subnet-directed broadcast (e.g. 192.168.5.255) — fastest, usual case
//  2. limited broadcast 255.255.255.255               — some routers only honour this
//  3. unicast sweep of every host in the /24           — works when broadcast is dead
//
// The sweep only starts if broadcasts produced no answer, so a healthy network
// still responds in ~100ms.
func DiscoverParallel(timeout time.Duration) ([]Offer, error) {
	conn, err := listenBroadcast()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	probe := []byte(discoveryMagic + " DISCOVER")

	seen := map[string]bool{}
	var offers []Offer
	buf := make([]byte, 2048)

	// drain collects replies until the deadline; it returns how many arrived.
	drain := func(until time.Time) int {
		got := 0
		for {
			_ = conn.SetReadDeadline(until)
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				return got
			}
			var offer Offer
			if err := json.Unmarshal(buf[:n], &offer); err != nil {
				continue
			}
			if offer.Type != "OFFER" {
				continue
			}
			key := fmt.Sprintf("%s|%s", offer.DeviceID+offer.Device, src.String())
			if seen[key] {
				continue
			}
			seen[key] = true
			offers = append(offers, offer)
			got++
		}
	}

	sendTo := func(targets []string) {
		for _, t := range targets {
			if addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", t, discoveryPort)); err == nil {
				_, _ = conn.WriteTo(probe, addr)
			}
		}
	}

	// --- phase 1: broadcast (two rounds; a busy AP may drop the first packet)
	bcast := broadcastTargets()
	sendTo(bcast)
	time.Sleep(120 * time.Millisecond)
	sendTo(bcast)

	// Broadcast replies arrive within ~1 RTT (<150ms). Don't burn the whole
	// budget waiting: if nothing came back we want to start the sweep early.
	bcastWait := timeout / 3
	if bcastWait > 450*time.Millisecond {
		bcastWait = 450 * time.Millisecond
	}
	if bcastWait < 200*time.Millisecond {
		bcastWait = 200 * time.Millisecond
	}
	drain(time.Now().Add(bcastWait))

	// Phase 2 (unicast sweep) runs in parallel from the caller, see resolveOffers.

	return offers, nil
}

// Discover is the simple synchronous entry point used by `discover`/`serve`:
// broadcast first, then a unicast sweep if broadcast found nothing reachable.
func Discover(timeout time.Duration) ([]Offer, error) {
	offers, err := DiscoverParallel(timeout)
	if err != nil && len(offers) == 0 {
		return nil, err
	}
	if len(offers) == 0 {
		offers = sweepLANOffers(timeout)
	}
	return offers, nil
}

// sweepLANOffers wraps sweep() returning only the offers.
func sweepLANOffers(timeout time.Duration) []Offer {
	var found []Offer
	var mu sync.Mutex
	window := timeout / 2
	if window < 350*time.Millisecond {
		window = 350 * time.Millisecond
	}
	if window > 700*time.Millisecond {
		window = 700 * time.Millisecond
	}
	sweep([]byte(discoveryMagic+" DISCOVER"), subnetHosts(), window, func(o *Offer) {
		mu.Lock()
		found = append(found, *o)
		mu.Unlock()
	})
	return found
}

// sweep fires the probe at every host on the LAN.
//
// Sequential sendto on one socket serialises on ARP resolution for each
// unresolved neighbour (seconds across a /24). Giving each host its own
// short-lived socket lets the kernel resolve them all in parallel (~10ms for
// 253 hosts).
//
// Design notes:
//   - a global done channel closes as soon as one phone answers, so silent
//     hosts do not hold up the whole sweep
//   - the reply window is short (default 600ms); unanswered hosts abort early
func sweep(probe []byte, hosts []string, replyWindow time.Duration, onReply func(*Offer)) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	sem := make(chan struct{}, 128)
	for _, h := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			select {
			case <-done:
				return
			default:
			}
			sem <- struct{}{}
			defer func() { <-sem }()

			addr := net.JoinHostPort(h, strconv.Itoa(discoveryPort))
			c, err := net.DialTimeout("udp4", addr, 200*time.Millisecond)
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(replyWindow))
			if _, err := c.Write(probe); err != nil {
				return
			}

			buf := make([]byte, 2048)
			for {
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				var offer Offer
				if err := json.Unmarshal(buf[:n], &offer); err != nil || offer.Type != "OFFER" {
					continue
				}
				mu.Lock()
				onReply(&offer)
				mu.Unlock()
				stop()
				return
			}
		}(h)
	}

	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()

	select {
	case <-waited:
	case <-done:
		// One phone answered; give the rest a brief moment then move on.
		select {
		case <-waited:
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// subnetHosts enumerates every usable host address on each local /24 (and
// smaller) IPv4 network, skipping our own address and network/broadcast.
func subnetHosts() []string {
	var out []string
	seen := map[string]bool{}

	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4()
			ones, bits := ipnet.Mask.Size()
			if bits != 32 {
				continue
			}
			// Only sweep networks small enough to be a LAN.
			if ones < 22 {
				continue
			}
			network := ip.Mask(ipnet.Mask)
			count := 1 << uint(32-ones)
			if count > 1024 {
				continue
			}
			for i := 1; i < count-1; i++ {
				cand := make(net.IP, 4)
				copy(cand, network)
				cand[0] |= byte(i >> 24)
				cand[1] |= byte(i >> 16)
				cand[2] |= byte(i >> 8)
				cand[3] |= byte(i)
				s := cand.String()
				if s == ip.String() || seen[s] {
					continue
				}
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func broadcastTargets() []string {
	// 127.0.0.1 makes discovery testable locally (and works with an emulator
	// reached through adb port forwarding).
	out := []string{"255.255.255.255", "127.0.0.1"}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			if len(mask) != 4 {
				continue
			}
			b := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				b[i] = ip[i] | ^mask[i]
			}
			out = append(out, b.String())
		}
	}
	return out
}

// GuessFromHost tries to reach a phone at host:port and verify it speaks our API.
func GuessFromHost(ctx context.Context, host string, port int) (*Offer, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("地址为空")
	}

	base := host
	effectivePort := port

	switch {
	case strings.HasPrefix(base, "http://"), strings.HasPrefix(base, "https://"):
		// Full URL: trust it as-is, derive the port for display.
		if u, err := url.Parse(base); err == nil && u.Port() != "" {
			if p, err := strconv.Atoi(u.Port()); err == nil {
				effectivePort = p
			}
		}
	default:
		// "ip" or "ip:port" — split an existing port before applying the default.
		if h, p, err := net.SplitHostPort(host); err == nil {
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				effectivePort = n
			}
		}
		if effectivePort == 0 {
			effectivePort = 8737
		}
		base = fmt.Sprintf("http://%s:%d", host, effectivePort)
	}
	if effectivePort == 0 {
		effectivePort = 8737
	}

	c := NewClient(base, "")
	offer, err := c.Ping(ctx)
	if err != nil {
		return nil, err
	}
	offer.Port = effectivePort
	offer.Addresses = []Address{{IP: host, Wifi: false}}
	return offer, nil
}
