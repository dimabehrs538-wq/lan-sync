// Package stclient 是 Syncthing REST + 事件 API 的精简客户端。
package stclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Base   string
	APIKey string
	HTTP   *http.Client
	// EventTypes 订阅的事件类型；默认事件流不含 Local/RemoteChangeDetected，必须显式订阅
	EventTypes []string
}

// DefaultEventTypes 是内核关心的事件。
var DefaultEventTypes = []string{
	"DeviceConnected", "DeviceDisconnected", "PendingDevicesChanged", "PendingFoldersChanged",
	"RemoteChangeDetected", "LocalChangeDetected", "FolderSummary", "StateChanged",
	"FolderCompletion", "FolderErrors", "ConfigSaved", "ItemFinished",
}

func New(base, key string) *Client {
	return &Client{Base: base, APIKey: key, HTTP: &http.Client{Timeout: 30 * time.Second}, EventTypes: DefaultEventTypes}
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("syncthing API %d: %s", e.Status, e.Body) }

func (c *Client) Do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &APIError{resp.StatusCode, string(data)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type SystemStatus struct {
	MyID      string `json:"myID"`
	Uptime    int    `json:"uptime"`
	StartTime string `json:"startTime"`
}

type Version struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

type Device struct {
	DeviceID          string   `json:"deviceID"`
	Name              string   `json:"name"`
	Addresses         []string `json:"addresses"`
	Introducer        bool     `json:"introducer"`
	AutoAcceptFolders bool     `json:"autoAcceptFolders"`
	Paused            bool     `json:"paused"`
}

type FolderDevice struct {
	DeviceID string `json:"deviceID"`
}

type Versioning struct {
	Type             string            `json:"type"`
	Params           map[string]string `json:"params"`
	CleanupIntervalS int               `json:"cleanupIntervalS"`
}

type Folder struct {
	ID               string         `json:"id"`
	Label            string         `json:"label"`
	Path             string         `json:"path"`
	Type             string         `json:"type"`
	Devices          []FolderDevice `json:"devices"`
	RescanIntervalS  int            `json:"rescanIntervalS"`
	FSWatcherEnabled bool           `json:"fsWatcherEnabled"`
	FSWatcherDelayS  float64        `json:"fsWatcherDelayS"`
	Paused           bool           `json:"paused"`
	Versioning       Versioning     `json:"versioning"`
	MarkerName       string         `json:"markerName,omitempty"`
}

type Connection struct {
	Connected     bool   `json:"connected"`
	Paused        bool   `json:"paused"`
	Address       string `json:"address"`
	Type          string `json:"type"`
	ClientVersion string `json:"clientVersion"`
	InBytesTotal  int64  `json:"inBytesTotal"`
	OutBytesTotal int64  `json:"outBytesTotal"`
}

type Connections struct {
	Connections map[string]Connection `json:"connections"`
}

type PendingDevice struct {
	Time    string `json:"time"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

type PendingFolder struct {
	OfferedBy map[string]struct {
		Time  string `json:"time"`
		Label string `json:"label"`
	} `json:"offeredBy"`
}

type FolderStatus struct {
	State       string `json:"state"`
	GlobalFiles int    `json:"globalFiles"`
	LocalFiles  int    `json:"localFiles"`
	NeedFiles   int    `json:"needFiles"`
	NeedBytes   int64  `json:"needBytes"`
	GlobalBytes int64  `json:"globalBytes"`
	Errors      int    `json:"errors"`
	Error       string `json:"error,omitempty"`
	// 仅接收（只读）文件夹中本机改动、未被同步的文件数
	ReceiveOnlyChangedFiles int `json:"receiveOnlyChangedFiles"`
}

type Event struct {
	ID   int             `json:"id"`
	Type string          `json:"type"`
	Time time.Time       `json:"time"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) Status(ctx context.Context) (s SystemStatus, err error) {
	err = c.Do(ctx, "GET", "/rest/system/status", nil, nil, &s)
	return
}
func (c *Client) Version(ctx context.Context) (v Version, err error) {
	err = c.Do(ctx, "GET", "/rest/system/version", nil, nil, &v)
	return
}
func (c *Client) PatchOptions(ctx context.Context, opts map[string]any) error {
	return c.Do(ctx, "PATCH", "/rest/config/options", nil, opts, nil)
}
func (c *Client) Options(ctx context.Context) (m map[string]any, err error) {
	err = c.Do(ctx, "GET", "/rest/config/options", nil, nil, &m)
	return
}
func (c *Client) Devices(ctx context.Context) (d []Device, err error) {
	err = c.Do(ctx, "GET", "/rest/config/devices", nil, nil, &d)
	return
}
func (c *Client) PutDevice(ctx context.Context, d Device) error {
	return c.Do(ctx, "PUT", "/rest/config/devices/"+url.PathEscape(d.DeviceID), nil, d, nil)
}
func (c *Client) PatchDevice(ctx context.Context, id string, fields map[string]any) error {
	return c.Do(ctx, "PATCH", "/rest/config/devices/"+url.PathEscape(id), nil, fields, nil)
}
func (c *Client) DeleteDevice(ctx context.Context, id string) error {
	return c.Do(ctx, "DELETE", "/rest/config/devices/"+url.PathEscape(id), nil, nil, nil)
}
func (c *Client) Folders(ctx context.Context) (f []Folder, err error) {
	err = c.Do(ctx, "GET", "/rest/config/folders", nil, nil, &f)
	return
}
func (c *Client) Folder(ctx context.Context, id string) (f Folder, err error) {
	err = c.Do(ctx, "GET", "/rest/config/folders/"+url.PathEscape(id), nil, nil, &f)
	return
}
func (c *Client) PutFolder(ctx context.Context, f Folder) error {
	return c.Do(ctx, "PUT", "/rest/config/folders/"+url.PathEscape(f.ID), nil, f, nil)
}
func (c *Client) PatchFolder(ctx context.Context, id string, fields map[string]any) error {
	return c.Do(ctx, "PATCH", "/rest/config/folders/"+url.PathEscape(id), nil, fields, nil)
}
func (c *Client) Connections(ctx context.Context) (cs Connections, err error) {
	err = c.Do(ctx, "GET", "/rest/system/connections", nil, nil, &cs)
	return
}
func (c *Client) PendingDevices(ctx context.Context) (m map[string]PendingDevice, err error) {
	err = c.Do(ctx, "GET", "/rest/cluster/pending/devices", nil, nil, &m)
	return
}
func (c *Client) DismissPendingDevice(ctx context.Context, id string) error {
	return c.Do(ctx, "DELETE", "/rest/cluster/pending/devices", url.Values{"device": {id}}, nil, nil)
}
func (c *Client) PendingFolders(ctx context.Context) (m map[string]PendingFolder, err error) {
	err = c.Do(ctx, "GET", "/rest/cluster/pending/folders", nil, nil, &m)
	return
}

// Discovery 返回已发现设备（本地发现缓存）：设备ID -> 地址列表。
func (c *Client) Discovery(ctx context.Context) (map[string]struct {
	Addresses []string `json:"addresses"`
}, error) {
	var m map[string]struct {
		Addresses []string `json:"addresses"`
	}
	err := c.Do(ctx, "GET", "/rest/system/discovery", nil, nil, &m)
	return m, err
}
func (c *Client) FolderStatus(ctx context.Context, folder string) (s FolderStatus, err error) {
	err = c.Do(ctx, "GET", "/rest/db/status", url.Values{"folder": {folder}}, nil, &s)
	return
}
func (c *Client) Scan(ctx context.Context, folder, sub string) error {
	q := url.Values{"folder": {folder}}
	if sub != "" {
		q.Set("sub", sub)
	}
	return c.Do(ctx, "POST", "/rest/db/scan", q, nil, nil)
}
func (c *Client) Restart(ctx context.Context) error {
	return c.Do(ctx, "POST", "/rest/system/restart", nil, nil, nil)
}

// Events 长轮询事件，since 为上次最后的事件 ID。
func (c *Client) Events(ctx context.Context, since int, timeout time.Duration) ([]Event, error) {
	var ev []Event
	q := url.Values{"since": {fmt.Sprint(since)}, "timeout": {fmt.Sprint(int(timeout.Seconds()))}}
	if len(c.EventTypes) > 0 {
		q.Set("events", strings.Join(c.EventTypes, ","))
	}
	cl := *c
	cl.HTTP = &http.Client{Timeout: timeout + 10*time.Second}
	err := cl.Do(ctx, "GET", "/rest/events", q, nil, &ev)
	return ev, err
}

// WatchEvents 持续拉取事件并回调，直到 ctx 结束。
func (c *Client) WatchEvents(ctx context.Context, fn func(Event)) {
	since := 0
	for ctx.Err() == nil {
		evs, err := c.Events(ctx, since, 60*time.Second)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, e := range evs {
			if e.ID > since {
				since = e.ID
			}
			fn(e)
		}
	}
}

type Completion struct {
	Completion  float64 `json:"completion"`
	NeedItems   int     `json:"needItems"`
	RemoteState string  `json:"remoteState"` // unknown | notSharing | paused | valid
}

// Completion 查询某设备在某文件夹上的同步完成度与共享状态。
func (c *Client) Completion(ctx context.Context, folder, device string) (r Completion, err error) {
	err = c.Do(ctx, "GET", "/rest/db/completion", url.Values{"folder": {folder}, "device": {device}}, nil, &r)
	return
}

// Revert 把仅接收文件夹的本机改动恢复为共享版本（POST /rest/db/revert）。
func (c *Client) Revert(ctx context.Context, folder string) error {
	return c.Do(ctx, "POST", "/rest/db/revert", url.Values{"folder": {folder}}, nil, nil)
}

// Override 以本机（仅发送）版本覆盖其他成员的改动（POST /rest/db/override）。
func (c *Client) Override(ctx context.Context, folder string) error {
	return c.Do(ctx, "POST", "/rest/db/override", url.Values{"folder": {folder}}, nil, nil)
}

// FileVersion 是历史版本中的一个版本。
type FileVersion struct {
	VersionTime time.Time `json:"versionTime"`
	ModTime     time.Time `json:"modTime"`
	Size        int64     `json:"size"`
}

// Versions 列出文件夹的历史版本（GET /rest/folder/versions），键为相对路径（使用 "/" 分隔）。
func (c *Client) Versions(ctx context.Context, folder string) (m map[string][]FileVersion, err error) {
	err = c.Do(ctx, "GET", "/rest/folder/versions", url.Values{"folder": {folder}}, nil, &m)
	return
}

// RestoreVersions 恢复指定版本（POST /rest/folder/versions），返回每个路径的错误信息（成功的不出现）。
func (c *Client) RestoreVersions(ctx context.Context, folder string, which map[string]time.Time) (errs map[string]string, err error) {
	body := map[string]string{}
	for p, t := range which {
		body[p] = t.Format(time.RFC3339Nano)
	}
	err = c.Do(ctx, "POST", "/rest/folder/versions", url.Values{"folder": {folder}}, body, &errs)
	return
}
