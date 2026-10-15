package control

import "net/http"

const dashboardHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Pearl Fleet</title><style>
body{font:15px system-ui;background:#07111f;color:#e5eefc;margin:0}main{max-width:1100px;margin:36px auto;padding:0 20px}
h1{font-size:28px}button,input,select{background:#13243b;color:#fff;border:1px solid #355174;border-radius:7px;padding:8px}
table{width:100%;border-collapse:collapse;background:#0d1a2b}th,td{padding:10px;border-bottom:1px solid #21354f;text-align:left}
.ok{color:#4ade80}.off{color:#fbbf24}.controls{display:flex;gap:8px;margin:16px 0;flex-wrap:wrap}.muted{color:#91a4bf}
</style></head><body><main><h1>Pearl Fleet</h1>
<p class="muted">Private dashboard. The token stays in this browser session.</p>
<div class="controls"><input id="token" type="password" placeholder="Admin token" size="42"><button onclick="loadAgents()">Refresh</button></div>
<table><thead><tr><th>Agent</th><th>Status</th><th>Mode</th><th>Hashrate</th><th>Shares</th><th>Idle</th><th>Foreground</th><th>Action</th></tr></thead><tbody id="agents"></tbody></table>
<script>
async function api(path, options={}){const token=document.getElementById('token').value;options.headers={...(options.headers||{}),'Authorization':'Bearer '+token,'Content-Type':'application/json'};const r=await fetch(path,options);if(!r.ok)throw new Error(await r.text());return r.json()}
async function command(id,type,payload={}){await api('/api/v1/admin/agents/'+encodeURIComponent(id)+'/commands',{method:'POST',body:JSON.stringify({type,payload})});await loadAgents()}
async function loadAgents(){try{const agents=await api('/api/v1/admin/agents');const body=document.getElementById('agents');body.innerHTML='';for(const a of agents){const h=a.last_heartbeat;const tr=document.createElement('tr');const online=h&&(Date.now()-new Date(h.time).getTime()<90000);tr.innerHTML='<td>'+esc(a.id)+'</td><td class="'+(online?'ok':'off')+'">'+(online?'online':'offline')+'</td><td>'+esc(h?.policy_mode||'-')+'</td><td>'+rate(h?.hashrate_10s)+'</td><td>'+esc(h?.accepted_shares??0)+' / '+esc(h?.rejected_shares??0)+'</td><td>'+esc(h?.idle_seconds??'-')+'s</td><td>'+esc(h?.foreground||'-')+'</td><td><button data-c="start">Start</button> <button data-c="stop">Stop</button> <button data-c="resume">Resume policy</button> <select><option>smart</option><option>avoid</option><option>full</option><option>manual</option></select> <button data-c="mode">Set mode</button></td>';tr.querySelectorAll('button').forEach(b=>b.onclick=()=>b.dataset.c==='mode'?command(a.id,'set-mode',{mode:tr.querySelector('select').value}):command(a.id,b.dataset.c));body.appendChild(tr)}}catch(e){alert(e.message)}}
function rate(v){return v?Number(v).toLocaleString(undefined,{maximumFractionDigits:2})+' H/s':'-'}
function esc(v){return String(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
</script></main></body></html>`

func (s *Server) dashboard(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(dashboardHTML))
}
