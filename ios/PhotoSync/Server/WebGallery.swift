import Foundation

/// The browser gallery served at "/", mirroring the Android version.
///
/// `__TOKEN__` is replaced at request time so the page's own API calls
/// authenticate without the user re-entering anything.
enum WebGallery {

    static let html = """
<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>PhotoSync · 照片浏览</title>
<style>
:root{--bg:#0b0d12;--card:#161a23;--line:#242a38;--fg:#e8ecf5;--dim:#8b94a7;--acc:#4f8cff}
*{box-sizing:border-box}
body{margin:0;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:var(--bg);color:var(--fg)}
header{position:sticky;top:0;z-index:10;display:flex;gap:12px;align-items:center;padding:12px 18px;background:rgba(11,13,18,.86);backdrop-filter:blur(14px);border-bottom:1px solid var(--line)}
h1{font-size:15px;margin:0;font-weight:600}
.badge{font-size:11px;color:var(--dim);border:1px solid var(--line);padding:2px 8px;border-radius:99px}
.grow{flex:1}
input,select,button{background:var(--card);color:var(--fg);border:1px solid var(--line);border-radius:9px;padding:7px 11px;font-size:13px;outline:none}
button{cursor:pointer}
input:focus,select:focus,button:hover{border-color:var(--acc)}
main{padding:18px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(158px,1fr));gap:10px}
.tile{position:relative;aspect-ratio:1;border-radius:12px;overflow:hidden;background:var(--card);cursor:pointer}
.tile img{width:100%;height:100%;object-fit:cover;display:block;transition:transform .25s}
.tile:hover img{transform:scale(1.05)}
.tile .meta{position:absolute;inset:auto 0 0 0;padding:16px 8px 6px;font-size:11px;color:#fff;background:linear-gradient(transparent,rgba(0,0,0,.78));white-space:nowrap;overflow:hidden;text-overflow:ellipsis;opacity:0;transition:opacity .2s}
.tile:hover .meta{opacity:1}
.tile .vt{position:absolute;top:8px;right:8px;background:rgba(0,0,0,.62);border-radius:6px;padding:1px 6px;font-size:10px}
#more{display:block;margin:22px auto 60px;padding:10px 26px}
#viewer{position:fixed;inset:0;background:rgba(3,4,7,.96);display:none;flex-direction:column;z-index:50}
#viewer.open{display:flex}
#viewer .bar{display:flex;gap:10px;align-items:center;padding:10px 16px;border-bottom:1px solid #1d2230;font-size:12px;color:var(--dim)}
#viewer .stage{flex:1;display:flex;align-items:center;justify-content:center;overflow:auto;padding:14px}
#viewer img,#viewer video{max-width:100%;max-height:100%;object-fit:contain;border-radius:6px}
.stat{color:var(--dim);font-size:12px}
a{color:var(--acc);text-decoration:none}
</style></head>
<body>
<header>
  <h1>PhotoSync</h1>
  <span class="badge" id="dev">…</span>
  <span class="stat" id="stat"></span>
  <span class="grow"></span>
  <input id="q" placeholder="搜索文件名…" style="width:160px">
  <select id="folder"><option value="">全部相簿</option></select>
  <select id="kind"><option value="all">全部</option><option value="img">照片</option><option value="vid">视频</option></select>
  <button id="refresh">刷新</button>
</header>
<main><div class="grid" id="grid"></div><button id="more" style="display:none">加载更多</button></main>
<div id="viewer">
  <div class="bar"><b id="vname"></b><span id="vinfo" class="grow"></span>
    <a id="vdl" download>下载</a><button id="vclose">关闭</button></div>
  <div class="stage" id="vstage"></div>
</div>
<script>
const TOKEN = "__TOKEN__";
const api = (p) => p + (p.includes('?') ? '&' : '?') + 'token=' + encodeURIComponent(TOKEN);
let offset = 0, limit = 120, total = 0, all = [], busy = false, done = false;
const el = id => document.getElementById(id);
const fmt = b => b > 1073741824 ? (b/1073741824).toFixed(1)+' GB' : b > 1048576 ? (b/1048576).toFixed(1)+' MB' : (b/1024).toFixed(0)+' KB';

async function boot(){
  const info = await (await fetch(api('/api/info'))).json();
  el('dev').textContent = info.device + ' · ' + info.photos + ' 张照片 · ' + info.videos + ' 个视频';
  const fo = await (await fetch(api('/api/folders'))).json();
  for (const f of fo.folders) {
    const o = document.createElement('option'); o.value = f.path;
    o.textContent = f.path + ' (' + f.count + ')';
    el('folder').appendChild(o);
  }
  await load();
}
async function load(){
  if (busy || done) return; busy = true;
  const r = await (await fetch(api('/api/list?offset=' + offset + '&limit=' + limit))).json();
  total = r.total; all = all.concat(r.items); offset += r.items.length;
  if (r.items.length < limit) done = true;
  el('stat').textContent = all.length + '/' + total;
  apply(); busy = false;
}
function apply(){
  const q = el('q').value.toLowerCase(), f = el('folder').value, kind = el('kind').value;
  const filtered = all.filter(it => (!q || it.name.toLowerCase().includes(q))
    && (!f || it.folder === f)
    && (kind === 'all' || (kind === 'vid' ? it.video : !it.video)));
  const g = el('grid'); g.innerHTML = '';
  const frag = document.createDocumentFragment();
  for (const it of filtered) {
    const t = document.createElement('div'); t.className = 'tile';
    const img = document.createElement('img'); img.loading = 'lazy';
    img.src = api('/api/thumb?id=' + encodeURIComponent(it.id) + '&size=320');
    t.appendChild(img);
    if (it.video) { const v = document.createElement('span'); v.className='vt'; v.textContent='视频'; t.appendChild(v); }
    const m = document.createElement('div'); m.className='meta'; m.textContent = it.name; t.appendChild(m);
    t.onclick = () => open(it);
    frag.appendChild(t);
  }
  g.appendChild(frag);
  el('more').style.display = done ? 'none' : 'block';
}
function open(it){
  el('vname').textContent = it.name;
  el('vinfo').textContent = fmt(it.size) + ' · ' + it.path + (it.width ? ' · ' + it.width + '×' + it.height : '');
  const url = api('/api/file?id=' + encodeURIComponent(it.id));
  el('vdl').href = url; el('vdl').download = it.name;
  const stage = el('vstage'); stage.innerHTML = '';
  if (it.video) {
    const v = document.createElement('video'); v.src = url; v.controls = true; v.autoplay = true; stage.appendChild(v);
  } else {
    const i = document.createElement('img'); i.src = url; stage.appendChild(i);
  }
  el('viewer').classList.add('open');
}
el('viewer').onclick = e => {
  if (['viewer','vstage','vclose'].includes(e.target.id)) {
    el('viewer').classList.remove('open'); el('vstage').innerHTML='';
  }
};
el('more').onclick = load;
el('refresh').onclick = () => { offset=0; all=[]; done=false; load(); };
el('q').oninput = apply; el('folder').onchange = apply; el('kind').onchange = apply;
addEventListener('scroll', () => {
  if (innerHeight + scrollY > document.body.offsetHeight - 600) load();
});
boot().catch(e => { el('stat').textContent = '加载失败: ' + e.message; });
</script>
</body></html>
"""
}
