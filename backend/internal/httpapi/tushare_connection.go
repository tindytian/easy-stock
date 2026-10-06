package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
)

// Tushare 连接探针：按官方标准报文（api_name/token/params/fields）请求
// 用户配置的接口地址（官方或兼容中转），用交易日历做最小验证。
const (
	tushareProbeAPI     = "trade_cal"
	tushareProbeFields  = "cal_date,is_open"
	tushareTestTimeout  = 10 * time.Second
	tushareResponseCap  = 1 << 20
	tushareMessageLimit = 200
)

var tushareHTTPClient = &http.Client{}

type tushareConnectionTestResult struct {
	OK        bool   `json:"ok"`
	BaseURL   string `json:"base_url"`
	APIName   string `json:"api_name"`
	LatencyMS int64  `json:"latency_ms"`
	Rows      int    `json:"rows"`
	Message   string `json:"message"`
}

type tushareProbeRequest struct {
	APIName string            `json:"api_name"`
	Token   string            `json:"token"`
	Params  map[string]string `json:"params"`
	Fields  string            `json:"fields"`
}

type tushareProbeResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Fields []string `json:"fields"`
		Items  [][]any  `json:"items"`
	} `json:"data"`
}

func resolveTushareEndpoint(values appsettings.Values) string {
	if endpoint := strings.TrimSpace(values.TushareAPIURL); endpoint != "" {
		return endpoint
	}
	return appsettings.DefaultTushareAPIURL
}

func (s *Server) settingsTushareTest(w http.ResponseWriter, r *http.Request) {
	values := s.settingsStore.Snapshot()
	token := strings.TrimSpace(values.Credentials.TushareToken)
	if token == "" {
		writeError(w, http.StatusPreconditionFailed, "请先填写并保存 Tushare Pro Token")
		return
	}
	endpoint := resolveTushareEndpoint(values)
	// 用昨日日期查一行交易日历：数据量最小，也避免探针依赖积分较高的接口。
	probeDay := time.Now().AddDate(0, 0, -1).Format("20060102")
	payload, err := json.Marshal(tushareProbeRequest{
		APIName: tushareProbeAPI,
		Token:   token,
		Params:  map[string]string{"exchange": "SSE", "start_date": probeDay, "end_date": probeDay},
		Fields:  tushareProbeFields,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构造 Tushare 探针请求失败")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tushareTestTimeout)
	defer cancel()
	// 保留用户配置中的路径与端口：地址有路径时按原样请求，不做拼接。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Tushare 接口地址无效: "+sanitizeTushareMessage(err.Error(), token))
		return
	}
	request.Header.Set("Content-Type", "application/json")
	startedAt := time.Now()
	response, err := tushareHTTPClient.Do(request)
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		writeError(w, http.StatusBadGateway, "Tushare 接口连接失败: "+sanitizeTushareMessage(friendlyTushareNetworkError(err), token))
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, tushareResponseCap))
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取 Tushare 响应失败: "+sanitizeTushareMessage(err.Error(), token))
		return
	}
	if response.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Tushare 接口返回 HTTP %d: %s", response.StatusCode, sanitizeTushareMessage(string(body), token)))
		return
	}
	var probe tushareProbeResponse
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, http.StatusBadGateway, "Tushare 响应不是标准 JSON: "+sanitizeTushareMessage(string(body), token))
		return
	}
	if probe.Code != 0 {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Tushare 返回错误 %d: %s", probe.Code, sanitizeTushareMessage(probe.Msg, token)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tushareConnectionTestResult{
		OK:        true,
		BaseURL:   endpoint,
		APIName:   tushareProbeAPI,
		LatencyMS: latency,
		Rows:      len(probe.Data.Items),
		Message:   fmt.Sprintf("连接成功，%s 返回 %d 行数据", tushareProbeAPI, len(probe.Data.Items)),
	}})
}

// friendlyTushareNetworkError 把常见网络错误翻译成可操作的提示。
func friendlyTushareNetworkError(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "请求超时（10 秒），请检查接口地址、端口或网络"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "connection refused"):
		return "连接被拒绝，请确认地址和端口是否正确"
	case strings.Contains(message, "no such host"):
		return "域名解析失败，请检查接口地址"
	default:
		return message
	}
}

// sanitizeTushareMessage 防止中转或官方把请求报文回显进错误信息导致 Token 泄漏。
func sanitizeTushareMessage(message string, token string) string {
	message = truncateRunes(strings.TrimSpace(message), tushareMessageLimit)
	if token != "" {
		message = strings.ReplaceAll(message, token, "••••")
	}
	return message
}
