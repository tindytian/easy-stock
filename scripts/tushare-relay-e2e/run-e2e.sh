#!/bin/bash
# easy-stock Tushare 中转功能端到端验证。
# 所有数据路径指向隔离临时目录,不触碰真实用户数据;Token 使用假值。
# 用法: bash scripts/tushare-relay-e2e/run-e2e.sh [后端监听端口(默认 20099)]
set -uo pipefail
PORT=${1:-20099}
ROOT=$(mktemp -d /tmp/easy-stock-e2e.XXXXXX)
REPO=$(cd "$(dirname "$0")/../.." && pwd)
SCRIPT_DIR=$REPO/scripts/tushare-relay-e2e
mkdir -p "$ROOT/data"
export PATH=/opt/homebrew/bin:$PATH
command -v go >/dev/null || { echo "go 不在 PATH"; exit 1; }
PASS=0; FAIL=0
check() {
  if eval "$2"; then echo "PASS: $1"; PASS=$((PASS+1)); else echo "FAIL: $1"; FAIL=$((FAIL+1)); fi
}
cleanup() { kill ${BACKEND_PID:-0} ${RELAY_PID:-0} 2>/dev/null; }
trap cleanup EXIT

# 1. 启动模拟中转
node "$SCRIPT_DIR/mock-relay.cjs" "$ROOT/relay-seen.json" ok > "$ROOT/relay.out" 2>&1 &
RELAY_PID=$!
sleep 1
RELAY_URL=$(grep MOCK_RELAY_URL "$ROOT/relay.out" | cut -d' ' -f1 | sed 's/MOCK_RELAY_URL=//')
echo "relay: $RELAY_URL"

# 2. 构建并启动后端(数据全部指向隔离目录)
(cd "$REPO/backend" && go build -o "$ROOT/easy-stock-backend" ./cmd/server) || exit 1
start_backend() {
  A_STOCK_ADDR=127.0.0.1:$PORT \
  A_STOCK_SETTINGS_PATH=$ROOT/data/settings.json \
  A_STOCK_REVIEW_DB=$ROOT/data/reviews.db \
  A_STOCK_PORTFOLIO_DB=$ROOT/data/portfolio.db \
  A_STOCK_RESEARCH_DB=$ROOT/data/research.db \
  A_STOCK_MARKET_EMOTION_DB=$ROOT/data/emotion.db \
  A_STOCK_THEME_RADAR_DB=$ROOT/data/theme.db \
  A_STOCK_MASTERY_CACHE=$ROOT/data/mastery \
  A_STOCK_LOG_DIR=$ROOT/data/logs \
  "$ROOT/easy-stock-backend" > "$ROOT/backend.log" 2>&1 &
  BACKEND_PID=$!
  for _ in $(seq 1 40); do
    curl -sf "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1 && return 0
    sleep 0.5
  done
  echo "backend failed to start"; tail -5 "$ROOT/backend.log"; return 1
}
start_backend || exit 1

API=http://127.0.0.1:$PORT/api/v1
FAKE_TOKEN=e2e-fake-token-8888

# 3. 保存中转地址 + Token
PUT1=$(curl -s -X PUT $API/settings -H 'Content-Type: application/json' \
  -d "{\"tushare_api_url\":\"$RELAY_URL\",\"credentials\":{\"tushare_token\":\"$FAKE_TOKEN\"}}")
check "保存后响应回显中转地址" "echo '$PUT1' | grep -q '\"tushare_api_url\":\"$RELAY_URL\"'"
check "保存后 Token 仅返回掩码状态" "echo '$PUT1' | grep -q '\"tushare_token\":{\"configured\":true'"
check "保存响应不含明文 Token" "! echo '$PUT1' | grep -q '$FAKE_TOKEN'"

# 4. 测试连接(应命中模拟中转)
TEST1=$(curl -s -X POST $API/settings/tushare/test)
check "测试连接成功" "echo '$TEST1' | grep -q '\"ok\":true'"
check "测试结果显示自定义地址生效" "echo '$TEST1' | grep -q '\"base_url\":\"$RELAY_URL\"'"
check "测试结果不含明文 Token" "! echo '$TEST1' | grep -q '$FAKE_TOKEN'"
sleep 0.5
SEEN=$(cat "$ROOT/relay-seen.json")
check "中转收到 POST + JSON 类型" "echo \"\$SEEN\" | grep -q '\"method\": \"POST\"' && echo \"\$SEEN\" | grep -q 'application/json'"
check "请求格式含 api_name/token/params/fields" "echo \"\$SEEN\" | grep -q 'api_name' && echo \"\$SEEN\" | grep -q 'token' && echo \"\$SEEN\" | grep -q 'params' && echo \"\$SEEN\" | grep -q 'fields'"
check "api_name=trade_cal 且 fields=cal_date,is_open" "echo \"\$SEEN\" | grep -q '\"trade_cal\"' && echo \"\$SEEN\" | grep -q 'cal_date,is_open'"

# 5. 未保存 Token 时测试连接 → 明确错误
curl -s -X PUT $API/settings -H 'Content-Type: application/json' -d '{"clear_secrets":["tushare_token"]}' >/dev/null
NOTOKEN=$(curl -s -X POST $API/settings/tushare/test)
check "无 Token 时返回 412 明确提示" "echo '$NOTOKEN' | grep -q 'Tushare Pro Token'"

# 6. 重新保存,重启后端,验证持久化恢复
curl -s -X PUT $API/settings -H 'Content-Type: application/json' \
  -d "{\"tushare_api_url\":\"$RELAY_URL\",\"credentials\":{\"tushare_token\":\"$FAKE_TOKEN\"}}" >/dev/null
kill $BACKEND_PID 2>/dev/null; wait $BACKEND_PID 2>/dev/null
start_backend || exit 1
GET2=$(curl -s $API/settings)
check "重启后中转地址恢复" "echo '$GET2' | grep -q '\"tushare_api_url\":\"$RELAY_URL\"'"
check "重启后 Token 仍为已配置(掩码)" "echo '$GET2' | grep -q '\"tushare_token\":{\"configured\":true'"
TEST2=$(curl -s -X POST $API/settings/tushare/test)
check "重启后测试连接仍成功" "echo '$TEST2' | grep -q '\"ok\":true'"

# 7. 错误路径:切到 error 模式中转
kill $RELAY_PID 2>/dev/null; wait $RELAY_PID 2>/dev/null
node "$SCRIPT_DIR/mock-relay.cjs" "$ROOT/relay-seen2.json" error > "$ROOT/relay2.out" 2>&1 &
RELAY_PID=$!
sleep 1
RELAY_URL2=$(grep MOCK_RELAY_URL "$ROOT/relay2.out" | cut -d' ' -f1 | sed 's/MOCK_RELAY_URL=//')
curl -s -X PUT $API/settings -H 'Content-Type: application/json' -d "{\"tushare_api_url\":\"$RELAY_URL2\"}" >/dev/null
TESTERR=$(curl -s -X POST $API/settings/tushare/test)
check "中转返回业务错误时透出 code/msg" "echo '$TESTERR' | grep -q '1001' && echo '$TESTERR' | grep -q '权限不足'"

# 8. 非法地址校验
BAD=$(curl -s -o /dev/null -w '%{http_code}' -X PUT $API/settings -H 'Content-Type: application/json' -d '{"tushare_api_url":"ftp://bad"}')
check "非法协议被拒绝(400)" "[ "$BAD" = "400" ]"

# 9. 文件权限与日志脱敏
PERM=$(stat -f '%Lp' "$ROOT/data/settings.json")
check "settings.json 权限为 600" "[ "$PERM" = "600" ]"
check "Token 写入本机配置文件(允许)" "grep -q '$FAKE_TOKEN' '$ROOT/data/settings.json'"
LOGS=$(cat "$ROOT/backend.log" "$ROOT"/data/logs/*.log 2>/dev/null)
check "运行日志不含明文 Token" "! echo \"\$LOGS\" | grep -q '$FAKE_TOKEN'"

echo ""
echo "=== RESULT: $PASS passed, $FAIL failed (workspace: $ROOT) ==="
exit $FAIL