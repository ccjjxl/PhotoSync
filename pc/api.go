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

// NewClient builds an API client with a transfer-tuned HTTP transport.
func NewClient(base, token string) *Client {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
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

// List fetches one page of media metadata.
func (c *Client) List(ctx context.Context, since int64, offset, limit int, videos bool) (*ListResponse, error) {
	q := url.Values{}
	q.Set("since", strconv.FormatInt(since, 10))
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("asc", "1")
	if !videos {
		q.Set("videos", "0")
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

// Discover broadcasts a probe and collects every phone that answers.
func Discover(timeout time.Duration) ([]Offer, error) {
	conn, err := listenBroadcast()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	probe := []byte(discoveryMagic + " DISCOVER")

	// Send to the subnet broadcast address of each local interface and the global one.
	dests := broadcastTargets()
	var sendErr error
	for _, d := range dests {
		addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", d, discoveryPort))
		if err != nil {
			continue
		}
		if _, err := conn.WriteTo(probe, addr); err != nil {
			sendErr = err
		}
	}
	for i := 0; i < 2; i++ {
		// A second probe helps when the first packet is dropped by a busy AP.
		time.Sleep(120 * time.Millisecond)
		for _, d := range dests {
			if addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", d, discoveryPort)); err == nil {
				_, _ = conn.WriteTo(probe, addr)
			}
		}
	}
	_ = sendErr

	seen := map[string]bool{}
	var offers []Offer
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 2048)
	for {
		_ = conn.SetReadDeadline(deadline)
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		var offer Offer
		if err := json.Unmarshal(buf[:n], &offer); err != nil {
			continue
		}
		if offer.Type != "OFFER" {
			continue
		}
		key := fmt.Sprintf("%s|%s", offer.Device, src.String())
		if seen[key] {
			continue
		}
		seen[key] = true
		offers = append(offers, offer)

		// Answer immediately and keep the socket open for stragglers.
		deadline = time.Now().Add(300 * time.Millisecond)
	}
	return offers, nil
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
