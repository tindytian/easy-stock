package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

func TestSettingsTushareTestRequiresSavedToken(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 428; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Tushare Pro Token") {
		t.Fatalf("missing token hint: %s", rec.Body.String())
	}
}

func TestResolveTushareEndpointPrefersUserConfiguration(t *testing.T) {
	if endpoint := resolveTushareEndpoint(appsettings.Values{}); endpoint != appsettings.DefaultTushareAPIURL {
		t.Fatalf("default endpoint = %q, want %q", endpoint, appsettings.DefaultTushareAPIURL)
	}
	values := appsettings.Values{TushareAPIURL: "  http://127.0.0.1:8020/relay  "}
	if endpoint := resolveTushareEndpoint(values); endpoint != "http://127.0.0.1:8020/relay" {
		t.Fatalf("custom endpoint = %q", endpoint)
	}
}

func TestSettingsTushareTestSendsStandardRequestToConfiguredEndpoint(t *testing.T) {
	store, _ := appsettings.Open("")
	const token = "tushare-secret-token-2468"
	var capturedPath string
	var capturedBody map[string]any
	var capturedContentType string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode relay request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"relay-1","code":0,"msg":"","data":{"fields":["cal_date","is_open"],"items":[["20261005",1]],"total":1}}`))
	}))
	defer relay.Close()
	_, err := store.Update(func(values *appsettings.Values) error {
		values.Credentials.TushareToken = token
		values.TushareAPIURL = relay.URL + "/tushare-relay"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) || !strings.Contains(rec.Body.String(), `"rows":1`) {
		t.Fatalf("missing success details: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatalf("test response leaked token: %s", rec.Body.String())
	}
	if capturedPath != "/tushare-relay" {
		t.Fatalf("relay path = %q, want /tushare-relay (user path must be preserved)", capturedPath)
	}
	if capturedContentType != "application/json" {
		t.Fatalf("relay content type = %q", capturedContentType)
	}
	if capturedBody["api_name"] != "trade_cal" {
		t.Fatalf("api_name = %v", capturedBody["api_name"])
	}
	if capturedBody["token"] != token {
		t.Fatalf("token = %v", capturedBody["token"])
	}
	if capturedBody["fields"] != "cal_date,is_open" {
		t.Fatalf("fields = %v", capturedBody["fields"])
	}
	params, ok := capturedBody["params"].(map[string]any)
	if !ok || params["exchange"] != "SSE" || params["start_date"] != params["end_date"] || len(params["start_date"].(string)) != 8 {
		t.Fatalf("params = %#v", capturedBody["params"])
	}
}

func TestSettingsTushareTestReportsRelayErrorCode(t *testing.T) {
	store, _ := appsettings.Open("")
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"relay-2","code":1001,"msg":"抱歉，您没有访问该接口的权限","data":null}`))
	}))
	defer relay.Close()
	_, err := store.Update(func(values *appsettings.Values) error {
		values.Credentials.TushareToken = "tushare-relay-token"
		values.TushareAPIURL = relay.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1001") || !strings.Contains(rec.Body.String(), "权限") {
		t.Fatalf("missing relay error detail: %s", rec.Body.String())
	}
}

func TestSettingsTushareTestMasksTokenEchoedInRelayError(t *testing.T) {
	store, _ := appsettings.Open("")
	const token = "echoed-secret-13579"
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"relay-3","code":2001,"msg":"invalid token echoed-secret-13579 supplied","data":null}`))
	}))
	defer relay.Close()
	_, err := store.Update(func(values *appsettings.Values) error {
		values.Credentials.TushareToken = token
		values.TushareAPIURL = relay.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatalf("relay error leaked raw token: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "••••") {
		t.Fatalf("masked token missing: %s", rec.Body.String())
	}
}

func TestSettingsTushareTestRejectsUnreachableEndpoint(t *testing.T) {
	store, _ := appsettings.Open("")
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	relay.Close()
	_, err := store.Update(func(values *appsettings.Values) error {
		values.Credentials.TushareToken = "tushare-relay-token"
		values.TushareAPIURL = relay.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Tushare 接口连接失败") {
		t.Fatalf("missing connection failure detail: %s", rec.Body.String())
	}
}

func TestSettingsTushareTestReportsNonJSONResponse(t *testing.T) {
	store, _ := appsettings.Open("")
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>gateway error page</html>"))
	}))
	defer relay.Close()
	_, err := store.Update(func(values *appsettings.Values) error {
		values.Credentials.TushareToken = "tushare-relay-token"
		values.TushareAPIURL = relay.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tushare/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "标准 JSON") {
		t.Fatalf("missing JSON hint: %s", rec.Body.String())
	}
}

func TestSettingsAPIStoresPersistsAndClearsTushareAPIURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store, AgentGateway: &fakeAgentGateway{status: agent.Status{Available: true}}})
	save := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"tushare_api_url":"http://111.170.140.159:8020/"}`))
	saveRec := httptest.NewRecorder()
	server.ServeHTTP(saveRec, save)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body=%s", saveRec.Code, saveRec.Body.String())
	}
	if !strings.Contains(saveRec.Body.String(), `"tushare_api_url":"http://111.170.140.159:8020/"`) {
		t.Fatalf("saved url not echoed: %s", saveRec.Body.String())
	}
	reopened, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().TushareAPIURL != "http://111.170.140.159:8020/" {
		t.Fatalf("url not persisted to disk: %+v", reopened.Snapshot())
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), `"tushare_api_url":"http://111.170.140.159:8020/"`) {
		t.Fatalf("GET after restart mismatch: status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	clear := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"tushare_api_url":""}`))
	clearRec := httptest.NewRecorder()
	server.ServeHTTP(clearRec, clear)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status = %d; body=%s", clearRec.Code, clearRec.Body.String())
	}
	afterClear, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterClear.Snapshot().TushareAPIURL != "" || store.Snapshot().TushareAPIURL != "" {
		t.Fatalf("url was not cleared: %q", store.Snapshot().TushareAPIURL)
	}
}

func TestSettingsAPIValidatesTushareAPIURL(t *testing.T) {
	store, _ := appsettings.Open("")
	server := NewServer(Config{SettingsStore: store})
	for _, invalid := range []string{"ftp://relay.example", "not a url", "http://", "https:///path"} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"tushare_api_url":"`+invalid+`"}`))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid url %q status = %d, want 400; body=%s", invalid, rec.Code, rec.Body.String())
		}
	}
	for _, valid := range []string{"http://127.0.0.1:8020/", "https://relay.example/tushare", ""} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"tushare_api_url":"`+valid+`"}`))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("valid url %q status = %d, want 200; body=%s", valid, rec.Code, rec.Body.String())
		}
	}
}
