package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rpc posts one JSON-RPC request to /mcp with a token.
func (h *harness) rpc(token, method string, params any) (int, rpcResponse) {
	h.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out rpcResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			h.t.Fatalf("%s: decode: %v", method, err)
		}
	}
	return resp.StatusCode, out
}

type toolResult struct {
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	Content           []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// call runs a tool and decodes its structured result into out.
func (h *harness) call(token, name string, args map[string]any, out any) toolResult {
	h.t.Helper()
	code, resp := h.rpc(token, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 || resp.Error != nil {
		h.t.Fatalf("%s: %d %+v", name, code, resp.Error)
	}
	var res toolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		h.t.Fatal(err)
	}
	if out != nil && !res.IsError {
		if err := json.Unmarshal(res.StructuredContent, out); err != nil {
			h.t.Fatalf("%s: %v in %s", name, err, res.StructuredContent)
		}
	}
	return res
}

func mcpHarness(t *testing.T) (*harness, string) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.do("PATCH", "/api/inventory/"+url.PathEscape("mac:00:11:32:aa:00:01"), map[string]any{"note": "Backups, living room"}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, token := h.newToken("assistant")
	return h, token
}

func TestMCPInitializeAndList(t *testing.T) {
	h, token := mcpHarness(t)
	code, resp := h.rpc(token, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"},
	})
	if code != 200 || resp.Error != nil {
		t.Fatalf("initialize: %d %+v", code, resp.Error)
	}
	var init struct {
		ProtocolVersion string                `json:"protocolVersion"`
		ServerInfo      struct{ Name string } `json:"serverInfo"`
		Capabilities    struct {
			Tools *struct{} `json:"tools"`
		} `json:"capabilities"`
		Instructions string `json:"instructions"`
	}
	_ = json.Unmarshal(resp.Result, &init)
	if init.ServerInfo.Name != "omini" || init.Capabilities.Tools == nil || init.ProtocolVersion == "" || init.Instructions == "" {
		t.Fatalf("initialize result %s", resp.Result)
	}

	code, resp = h.rpc(token, "tools/list", map[string]any{})
	if code != 200 || resp.Error != nil {
		t.Fatalf("tools/list: %d %+v", code, resp.Error)
	}
	var list struct {
		Tools []struct {
			Name        string `json:"name"`
			Annotations struct {
				ReadOnly bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(resp.Result, &list)
	var names []string
	for _, tl := range list.Tools {
		if !tl.Annotations.ReadOnly {
			t.Fatalf("%s is not marked read-only", tl.Name)
		}
		names = append(names, tl.Name)
	}
	want := "get_device get_dns_dhcp get_internet_usage get_map_summary list_alerts list_networks search_devices"
	if strings.Join(names, " ") != want {
		t.Fatalf("tools %v", names)
	}
}

func TestMCPTools(t *testing.T) {
	h, token := mcpHarness(t)

	var summary struct {
		Nodes        int            `json:"nodes"`
		Online       int            `json:"online"`
		ByKind       map[string]int `json:"by_kind"`
		Integrations []struct {
			Type string `json:"type"`
			OK   *bool  `json:"ok"`
		} `json:"integrations"`
		Alerts map[string]int `json:"open_alerts"`
	}
	res := h.call(token, "get_map_summary", nil, &summary)
	if summary.Nodes == 0 || summary.Online == 0 || summary.ByKind["device"] == 0 || len(summary.Integrations) != 1 ||
		summary.Integrations[0].OK == nil || !*summary.Integrations[0].OK {
		t.Fatalf("summary %s", res.StructuredContent)
	}
	if _, ok := summary.Alerts["critical"]; !ok {
		t.Fatalf("every severity is counted: %v", summary.Alerts)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" || !json.Valid([]byte(res.Content[0].Text)) {
		t.Fatalf("the text content is the JSON: %+v", res.Content)
	}

	var found struct {
		Devices []struct {
			ID   string `json:"id"`
			Note string `json:"note"`
		} `json:"devices"`
		Total int `json:"total"`
	}
	h.call(token, "search_devices", map[string]any{"query": "LIVING ROOM"}, &found)
	if found.Total != 1 || found.Devices[0].ID != "mac:00:11:32:aa:00:01" || found.Devices[0].Note != "Backups, living room" {
		t.Fatalf("the note is searched: %+v", found)
	}
	h.call(token, "search_devices", map[string]any{"limit": 2}, &found)
	if len(found.Devices) != 2 || found.Total <= 2 {
		t.Fatalf("the limit cuts the list, total counts all: %+v", found)
	}

	var dev struct {
		ID     string   `json:"id"`
		Note   string   `json:"note"`
		IPs    []string `json:"ips"`
		MACs   []string `json:"macs"`
		Online bool     `json:"online"`
		Uplink *struct {
			NodeID string `json:"node_id"`
		} `json:"connected_to"`
		Alerts []any `json:"open_alerts"`
	}
	res = h.call(token, "get_device", map[string]any{"id": "mac:00:11:32:aa:00:01"}, &dev)
	if dev.Note != "Backups, living room" || len(dev.MACs) == 0 || len(dev.IPs) == 0 || dev.Uplink == nil || dev.Alerts == nil {
		t.Fatalf("device %s", res.StructuredContent)
	}
	var sw struct {
		Ports *struct {
			Total int `json:"total"`
		} `json:"ports"`
	}
	h.call(token, "get_device", map[string]any{"id": dev.Uplink.NodeID}, &sw)
	if sw.Ports == nil || sw.Ports.Total == 0 {
		t.Fatalf("a managed device has its ports: %+v", sw)
	}
	if res := h.call(token, "get_device", map[string]any{"id": "nope"}, nil); !res.IsError {
		t.Fatal("an unknown device is a tool error")
	}

	var alerts struct {
		Alerts []struct {
			Severity string `json:"severity"`
			Title    string `json:"title"`
		} `json:"alerts"`
	}
	h.call(token, "list_alerts", map[string]any{"severity": "warning"}, &alerts)
	for _, a := range alerts.Alerts {
		if a.Severity != "warning" || a.Title == "" {
			t.Fatalf("filtered alerts %+v", alerts)
		}
	}
	if res := h.call(token, "list_alerts", map[string]any{"severity": "bad"}, nil); !res.IsError {
		t.Fatal("a wrong severity is a tool error")
	}

	var nets struct {
		Vlans []any `json:"vlans"`
		Pools []any `json:"dhcp_pools"`
	}
	if res := h.call(token, "list_networks", nil, &nets); res.IsError || nets.Vlans == nil || nets.Pools == nil {
		t.Fatalf("networks %s", res.StructuredContent)
	}
	var dd struct {
		Leases int   `json:"leases"`
		DNS    []any `json:"dns_servers"`
	}
	if res := h.call(token, "get_dns_dhcp", nil, &dd); res.IsError || dd.DNS == nil {
		t.Fatalf("dns/dhcp %s", res.StructuredContent)
	}
	var inet struct {
		Seconds int   `json:"seconds"`
		Devices []any `json:"devices"`
	}
	if res := h.call(token, "get_internet_usage", map[string]any{"range": "1h"}, &inet); res.IsError || inet.Seconds != 3600 || inet.Devices == nil {
		t.Fatalf("internet %s", res.StructuredContent)
	}
	if res := h.call(token, "get_internet_usage", map[string]any{"range": "1y"}, nil); !res.IsError {
		t.Fatal("a wrong range is a tool error")
	}

	code, resp := h.rpc(token, "tools/call", map[string]any{"name": "set_port", "arguments": map[string]any{}})
	if code != 200 || resp.Error == nil {
		t.Fatalf("an unknown tool is a JSON-RPC error: %d %s", code, resp.Result)
	}
}

func TestMCPAuth(t *testing.T) {
	h, token := mcpHarness(t)
	if code, _ := h.rpc("", "tools/list", map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := h.rpc("omini_wrong", "tools/list", map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("a wrong token: %d", code)
	}
	if code, _ := h.bearer(token, "GET", "/mcp", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("no SSE stream: %d", code)
	}

	// The admin session opens it too, but not from another site.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	for origin, want := range map[string]int{"": 200, "https://evil.example": http.StatusForbidden} {
		req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if origin != "" {
			req.Header.Set("Origin", origin)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("session from %q: %d, want %d", origin, resp.StatusCode, want)
		}
	}

	var list []struct{ ID int64 }
	h.do("GET", "/api/tokens", nil, &list)
	h.do("DELETE", "/api/tokens/"+itoa(list[0].ID), nil, nil)
	if code, _ := h.rpc(token, "tools/list", map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("a revoked token: %d", code)
	}
}
