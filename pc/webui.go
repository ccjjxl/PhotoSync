package main

// webHTML is the single-page local UI served by `sync-client serve`.
const webHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>PhotoSync 桌面</title>
<style>
:root{--bg:#0b0d12;--card:#151a24;--line:#242b3a;--fg:#e9edf6;--dim:#8b94a8;--acc:#4f8cff;--ok:#3ed598;--err:#ff5f6d}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}
.wrap{max-width:940px;margin:0 auto;padding:28px 20px 60px}
h1{font-size:20px;margin:0 0 4px;letter-spacing:.2px}
.sub{color:var(--dim);font-size:12px;margin-bottom:24px}
.card{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px;margin-bottom:14px}
.card h2{font-size:13px;margin:0 0 12px;color:var(--dim);font-weight:600;letter-spacing:.4px;text-transform:uppercase}
button{background:var(--acc);color:#fff;border:0;border-radius:10px;padding:9px 16px;font-size:13px;font-weight:500;cursor:pointer}
button.ghost{background:#1d2431;color:var(--fg);border:1px solid var(--line)}
button:disabled{opacity:.45;cursor:not-allowed}
input{background:#101521;color:var(--fg);border:1px solid var(--line);border-radius:10px;padding:9px 12px;font-size:13px;outline:none;width:100%}
input:focus{border-color:var(--acc)}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap}
.dev{display:flex;justify-content:space-between;align-items:center;padding:11px 13px;border:1px solid var(--line);border-radius:11px;margin-bottom:8px;cursor:pointer}
.dev:hover{border-color:var(--acc)}
.dev.sel{border-color:var(--acc);background:#182136}
.dev .n{font-weight:500}
.dev .ip{color:var(--dim);font-size:12px;font-family:ui-monospace,monospace}
.bar{height:9px;background:#101521;border-radius:99px;overflow:hidden;margin:14px 0 8px}
.bar>i{display:block;height:100%;width:0;background:linear-gradient(90deg,#2c6bff,#6fa8ff);transition:width .2s}
.meta{display:flex;gap:16px;color:var(--dim);font-size:12px;flex-wrap:wrap}
.meta b{color:var(--fg);font-weight:500}
.err{color:var(--err);font-size:12px;margin-top:8px;white-space:pre-wrap}
.tag{display:inline-block;padding:2px 9px;border-radius:99px;font-size:11px;background:#1d2431;color:var(--dim);border:1px solid var(--line)}
.tag.on{background:#12362a;color:var(--ok);border-color:#1d5943}
#log{font-family:ui-monospace,monospace;font-size:11px;color:var(--dim);max-height:120px;overflow:auto;margin-top:10px;white-space:pre-wrap}
table.devs{width:100%;border-collapse:collapse;font-size:13px}
table.devs th{text-align:left;color:var(--dim);font-weight:500;font-size:11px;text-transform:uppercase;letter-spacing:.4px;padding:0 8px 8px}
table.devs td{padding:9px 8px;border-top:1px solid var(--line);vertical-align:middle}
table.devs tr:hover td{background:#12161f}
table.devs .nm{font-weight:500}
table.devs .sub{color:var(--dim);font-size:11px;font-family:ui-monospace,monospace}
table.devs .acts{text-align:right;white-space:nowrap}
button.sm{padding:6px 11px;font-size:12px;border-radius:8px}
.devprog{margin-top:14px;padding:13px 15px;border:1px solid var(--line);border-radius:12px}
.devprog .hd{display:flex;justify-content:space-between;align-items:center;margin-bottom:9px}
.devprog .hd b{font-size:13px}
.devprog .bar{margin:0 0 8px}
.pill{display:inline-block;padding:1px 8px;border-radius:99px;font-size:10px;border:1px solid var(--line);color:var(--dim)}
.pill.run{background:#12243d;color:#6fa8ff;border-color:#1d3d66}
.pill.ok{background:#12362a;color:var(--ok);border-color:#1d5943}
.pill.err{background:#3a1520;color:var(--err);border-color:#5d2130}
.empty{color:var(--dim);font-size:13px;padding:14px 4px}
.hidden{display:none}
label.chk{display:flex;gap:7px;align-items:center;font-size:13px;color:var(--dim);cursor:pointer}
input[type=checkbox]{width:auto}
</style></head><body><div class="wrap">
<h1>PhotoSync 桌面端</h1>
<div class="sub">把安卓相册高速同步到这台电脑 · v{{.Version}}</div>

<div class="card">
  <h2>1 · 连接手机</h2>
  <div class="row" style="margin-bottom:12px">
    <button id="btnScan">扫描局域网</button>
    <span id="scanNote" class="tag">未扫描</span>
  </div>
  <div id="devices"></div>
  <div class="row" style="margin-top:10px">
    <input id="manual" placeholder="或手动输入手机 IP，如 192.168.1.23" style="flex:1;min-width:200px">
    <input id="code" placeholder="配对码 6 位" style="width:140px">
    <button class="ghost" id="btnConnect">连接</button>
  </div>
  <div id="connMsg" class="err"></div>
</div>

<div class="card">
  <h2>2 · 同步</h2>
  <div class="meta" style="margin-bottom:12px">
    <span>相册根目录 <b id="outDir">-</b></span>
    <span>上次运行 <b id="lastSync">-</b></span>
  </div>
  <div class="row" style="margin-bottom:14px">
    <button id="btnSyncAll">同步全部已配对手机</button>
    <button class="ghost" id="btnFull">全量重新校验</button>
    <button class="ghost" id="btnRepair">仅补齐缺失</button>
    <label class="chk"><input type="checkbox" id="chkVideos" checked> 同步视频</label>
  </div>
  <div id="devTable"></div>
  <div id="devStates"></div>
  <div id="syncMsg" class="err"></div>
</div>

<div class="card">
  <h2>说明</h2>
  <div class="meta" style="display:block;line-height:1.9">
    · 首次使用需在手机 App 中查看 6 位配对码并填入上方。<br>
    · 支持多台手机：每台存到独立子目录 <code>&lt;相册根目录&gt;/&lt;设备名&gt;-&lt;设备ID&gt;/</code>，互不干扰。<br>
    · 增量同步记录在每台手机目录下的 <code>.photosync-manifest.json</code>，仅传输新增/修改的照片。<br>
    · 断线或中断后重新点击「开始增量同步」会自动续传未完成的文件。<br>
    · 所有传输都在本地局域网进行，不经过互联网。
  </div>
</div>
</div>
<script>
const $ = id => document.getElementById(id);
let selected = null;
let lastStates = {};
const fmt = b => b>=1073741824?(b/1073741824).toFixed(2)+' GB':b>=1048576?(b/1048576).toFixed(1)+' MB':b>=1024?(b/1024).toFixed(0)+' KB':b+' KB';

async function loadState(){
  const s = await (await fetch('/api/state')).json();
  $('outDir').textContent = s.outDir || '-';
  $('lastSync').textContent = s.lastSync || '-';
  renderDevices(s.devices || []);
}
function renderDevices(devs){
  const t = $('devTable');
  if (!devs.length) {
    t.innerHTML = '<div class="empty">还没有配对的手机。在上方扫描并连接，或直接点「同步全部已配对手机」自动发现。</div>';
    return;
  }
  let html = '<table class="devs"><thead><tr><th>手机</th><th>保存目录</th><th>已同步</th><th>上次</th><th></th></tr></thead><tbody>';
  devs.forEach(d => {
    html += '<tr>'
      + '<td><div class="nm">' + esc(d.name || d.id) + '</div>'
      + '<div class="sub">' + esc(d.model || '') + (d.android ? ' · Android ' + esc(d.android) : '') + ' · ID ' + esc(d.id) + '</div></td>'
      + '<td><div class="sub">' + esc(d.folder || '(根目录)') + '</div></td>'
      + '<td>' + d.files + ' 个<br><span class="sub">' + fmt(d.bytes) + '</span></td>'
      + '<td class="sub">' + (d.lastSync || '从未') + '</td>'
      + '<td class="acts"><button class="sm" data-sync="' + esc(d.id) + '">同步</button> '
      + '<button class="sm ghost" data-forget="' + esc(d.id) + '">移除</button></td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  t.innerHTML = html;
  t.querySelectorAll('[data-sync]').forEach(b => b.onclick = () => startSync(b.dataset.sync, 'incr'));
  t.querySelectorAll('[data-forget]').forEach(b => b.onclick = () => forget(b.dataset.forget));
}
function esc(s){ return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

async function scan(){
  $('btnScan').disabled = true; $('scanNote').textContent = '扫描中…';
  try {
    const r = await (await fetch('/api/discover')).json();
    $('btnScan').disabled = false;
    const offers = r.offers || [];
    if (!offers.length) { $('scanNote').textContent = '未发现设备'; $('scanNote').className='tag'; return; }
    $('scanNote').textContent = '发现 ' + offers.length + ' 台';
    $('scanNote').className='tag on';
    const box = $('devices'); box.innerHTML = '';
    offers.forEach(o => {
      const d = document.createElement('div');
      d.className = 'dev';
      const ip = (o.addresses && o.addresses.length) ? o.addresses[0].ip : '';
      d.innerHTML = '<div><div class="n">' + esc(o.device) + '</div><div class="ip">'
        + ip + ':' + o.port + ' · ID ' + esc(o.deviceId || '—') + ' · ' + esc(o.model) + '</div></div>'
        + (o.openAccess ? '<span class="tag on">免配对</span>' : '<span class="tag">需配对</span>');
      d.onclick = () => {
        [...box.children].forEach(c => c.classList.remove('sel'));
        d.classList.add('sel');
        selected = o;
        $('manual').value = ip;
      };
      box.appendChild(d);
    });
  } catch(e) {
    $('btnScan').disabled = false;
    $('scanNote').textContent = '扫描失败';
  }
}

async function connectDev(){
  const host = $('manual').value.trim();
  if (!host) { $('connMsg').textContent = '请先选择设备或输入 IP'; return; }
  $('connMsg').textContent = '连接中…'; $('connMsg').style.color = 'var(--dim)';
  const r = await (await fetch('/api/connect', {
    method:'POST', headers:{'Content-Type':'application/json'},
    body: JSON.stringify({ host, port: selected ? selected.port : 8737, code: $('code').value.trim() })
  })).json();
  if (r.error) { $('connMsg').textContent = r.error; $('connMsg').style.color='var(--err)'; return; }
  $('connMsg').textContent = '已连接 ' + r.device.device + '（' + r.device.photos + ' 张照片 / ' + r.device.videos + ' 个视频）→ ' + (r.dir || '');
  $('connMsg').style.color = 'var(--ok)';
  loadState();
}

async function forget(id){
  if (!confirm('移除这台手机的配对记录？照片文件会保留。')) return;
  await fetch('/api/forget', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ id }) });
  loadState();
}

async function startSync(devId, mode){
  $('syncMsg').textContent = '';
  const body = { device: devId || '', full: mode==='full', repair: mode==='repair', videos: $('chkVideos').checked };
  const r = await (await fetch('/api/sync', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body) })).json();
  if (r.error) { $('syncMsg').textContent = r.error; return; }
  poll();
}

let timer = null;
async function poll(){
  const s = await (await fetch('/api/status')).json();
  const list = s.devices || [];
  const box = $('devStates');
  if (!list.length) { box.innerHTML = ''; }
  else {
    box.innerHTML = list.map(st => {
      const pct = st.bytesTotal > 0 ? Math.min(100, Math.round(st.bytes*100/st.bytesTotal))
                : (st.total > 0 ? Math.round(st.done*100/st.total) : 0);
      const cls = st.running ? 'run' : (st.error ? 'err' : (st.finished ? 'ok' : ''));
      const label = st.running ? '同步中' : (st.error ? '失败' : (st.message || '空闲'));
      return '<div class="devprog">'
        + '<div class="hd"><b>' + esc(st.device) + '</b><span class="pill ' + cls + '">' + esc(label) + '</span></div>'
        + '<div class="bar"><i style="width:' + pct + '%"></i></div>'
        + '<div class="meta"><span><b>' + st.done + '</b>/<b>' + st.total + '</b> 文件</span>'
        + '<span><b>' + fmt(st.bytes) + '</b> / <b>' + fmt(st.bytesTotal) + '</b></span>'
        + '<span><b>' + fmt(st.speed) + '</b>/s</span>'
        + '<span>剩余 <b>' + (st.eta || '--') + '</b></span>'
        + '<span>跳过 <b>' + st.skipped + '</b></span>'
        + '<span>失败 <b>' + st.failed + '</b></span></div>'
        + (st.message && st.running ? '<div class="sub" style="margin-top:6px;color:var(--dim);font-size:11px;font-family:ui-monospace,monospace">' + esc(st.message) + '</div>' : '')
        + (st.error ? '<div class="err">' + esc(st.error) + '</div>' : '')
        + '</div>';
    }).join('');
  }
  if (s.running) { clearTimeout(timer); timer = setTimeout(poll, 600); }
  else {
    const errs = list.filter(x => x.error);
    if (errs.length) $('syncMsg').textContent = errs.map(e => e.error).join('\n');
    loadState();
  }
}

$('btnScan').onclick = scan;
$('btnConnect').onclick = connectDev;
$('btnSyncAll').onclick = () => startSync('', 'incr');
$('btnFull').onclick = () => startSync('', 'full');
$('btnRepair').onclick = () => startSync('', 'repair');
loadState().then(scan);
</script></body></html>`
