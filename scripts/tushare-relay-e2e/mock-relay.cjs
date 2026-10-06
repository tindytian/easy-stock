#!/usr/bin/env node
// 本地模拟 Tushare 中转:校验标准报文格式,记录请求,返回标准响应。
// 用法: node mock-relay.cjs <请求日志路径> [ok|error|garbage]
const http = require('http');
const fs = require('fs');
const logPath = process.argv[2];
const mode = process.argv[3] || 'ok';
const seen = [];
const server = http.createServer((req, res) => {
  let body = '';
  req.on('data', (chunk) => { body += chunk; });
  req.on('end', () => {
    let parsed = {};
    try { parsed = JSON.parse(body); } catch {}
    seen.push({ path: req.url, method: req.method, contentType: req.headers['content-type'], body: parsed });
    fs.writeFileSync(logPath, JSON.stringify(seen, null, 2));
    res.setHeader('Content-Type', 'application/json');
    if (mode === 'error') {
      res.end(JSON.stringify({ code: 1001, msg: 'token 权限不足', data: null }));
      return;
    }
    if (mode === 'garbage') {
      res.end('<html>bad gateway</html>');
      return;
    }
    res.end(JSON.stringify({ request_id: 'mock-1', code: 0, msg: '', data: { fields: ['cal_date', 'is_open'], items: [['20261005', 1]], total: 1 } }));
  });
});
server.listen(0, '127.0.0.1', () => {
  console.log(`MOCK_RELAY_URL=http://127.0.0.1:${server.address().port}/`);
});
