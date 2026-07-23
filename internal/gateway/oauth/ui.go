package oauth

import "html/template"

type authorizationPageData struct {
	TraditionalChinese bool
	Message            string
	ClientName         string
	RedirectHost       string
	Resource           string
	Scopes             []string
	NAS                []string
	Hidden             map[string]string
	DSMWebLogin        bool
	LocalLogin         bool
	SessionUser        string
	SessionDSM         bool
	Nonce              string
}

type errorPageData struct {
	TraditionalChinese bool
	Message            string
	AdminURL           string
}

var authorizationTemplate = template.Must(template.New("oauth-authorization").Parse(`<!doctype html>
<html lang="{{if .TraditionalChinese}}zh-Hant{{else}}en{{end}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>{{if .TraditionalChinese}}授權 MCP Client{{else}}Authorize MCP client{{end}} · dsmctl</title>
  <link rel="icon" href="favicon.svg" type="image/svg+xml">
  <style>
    :root{color-scheme:light;--blue:#2563eb;--blue-dark:#1d4ed8;--slate:#0f172a;--muted:#64748b;--line:#dbe3ee;--bg:#f3f7fb;--panel:#fff;--warn:#fff7ed;--warn-line:#fdba74;--danger:#b91c1c}*{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at top left,#dbeafe 0,transparent 38%),var(--bg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;color:var(--slate);display:grid;place-items:center;padding:28px}.shell{width:min(680px,100%)}.brand{display:flex;align-items:center;gap:12px;margin:0 0 18px 4px}.mark{width:34px;height:34px;border-radius:10px;background:linear-gradient(145deg,#2563eb,#1e40af);display:grid;place-items:center;color:#fff;font-weight:800}.brand strong{display:block;font-size:17px}.brand span{display:block;color:var(--muted);font-size:13px}.card{background:var(--panel);border:1px solid rgba(148,163,184,.35);border-radius:18px;box-shadow:0 18px 55px rgba(15,23,42,.12);overflow:hidden}.head{padding:28px 30px 22px;border-bottom:1px solid var(--line)}h1{font-size:24px;line-height:1.2;margin:0 0 8px}.head p{margin:0;color:var(--muted)}.body{padding:24px 30px 30px}.detail{display:grid;grid-template-columns:130px 1fr;gap:12px 18px;padding:0 0 22px;margin:0 0 22px;border-bottom:1px solid var(--line)}.detail dt{color:var(--muted)}.detail dd{margin:0;min-width:0;overflow-wrap:anywhere}.chips{display:flex;gap:7px;flex-wrap:wrap}.chip{padding:4px 9px;border:1px solid #bfdbfe;background:#eff6ff;color:#1e40af;border-radius:999px;font:600 12px/1.3 ui-monospace,SFMono-Regular,monospace}.nas{margin:0;padding-left:18px}.notice{border:1px solid var(--warn-line);background:var(--warn);border-radius:12px;padding:13px 15px;margin:0 0 20px}.notice strong{display:block;margin-bottom:3px}.notice span{color:#9a3412}.error{border:1px solid #fecaca;background:#fef2f2;color:var(--danger);border-radius:10px;padding:11px 13px;margin-bottom:18px}.fields{display:grid;grid-template-columns:1fr 1fr;gap:14px}.field{display:grid;gap:6px}.field label{font-weight:650}.control{width:100%;height:42px;padding:0 12px;border:1px solid #cbd5e1;border-radius:9px;background:#fff;font:inherit}.control:focus{outline:3px solid rgba(37,99,235,.16);border-color:var(--blue)}.actions{display:flex;justify-content:flex-end;gap:10px;margin-top:22px}.button{border:0;border-radius:9px;padding:10px 16px;font:650 14px/1.2 inherit;cursor:pointer}.button.primary{background:var(--blue);color:#fff}.button.primary:hover{background:var(--blue-dark)}.button.secondary{background:#eef2f7;color:#334155}.button.full{width:100%}.divider{display:flex;align-items:center;gap:12px;color:var(--muted);font-size:13px;margin:18px 0}.divider:before,.divider:after{content:"";height:1px;background:var(--line);flex:1}a.button{display:block;text-align:center;text-decoration:none}.session-note{border:1px solid #bbf7d0;background:#f0fdf4;color:#166534;border-radius:10px;padding:11px 13px;margin:0 0 6px}.signin-head{font-weight:650;margin:0 0 12px;padding-top:4px}.dsm-status{margin:0 0 14px;padding:11px 13px;border-radius:10px;border:1px solid #bfdbfe;background:#eff6ff;color:#1e40af;font-size:13px}@media(max-width:560px){body{padding:16px}.head,.body{padding-left:20px;padding-right:20px}.detail{grid-template-columns:1fr;gap:4px}.detail dd{margin-bottom:8px}.fields{grid-template-columns:1fr}.actions{flex-direction:column-reverse}.button{width:100%}}
  </style>
</head>
<body><main class="shell">
  <div class="brand"><div class="mark">d</div><div><strong>dsmctl MCP Server</strong><span>{{if .TraditionalChinese}}私人 NAS 管理 Gateway{{else}}Private NAS management gateway{{end}}</span></div></div>
  <section class="card">
    <header class="head"><h1>{{if .TraditionalChinese}}授權 MCP Client{{else}}Authorize MCP client{{end}}</h1><p>{{if .TraditionalChinese}}使用 Gateway 管理員帳號核准這個 Client。{{else}}Approve this client with the Gateway administrator account.{{end}}</p></header>
    <div class="body">
      {{if .Message}}<div class="error" role="alert">{{.Message}}</div>{{end}}
      <dl class="detail">
        <dt>{{if .TraditionalChinese}}Client{{else}}Client{{end}}</dt><dd><strong>{{.ClientName}}</strong></dd>
        <dt>{{if .TraditionalChinese}}完成後返回{{else}}Returns to{{end}}</dt><dd>{{.RedirectHost}}</dd>
        <dt>MCP endpoint</dt><dd><code>{{.Resource}}</code></dd>
        <dt>{{if .TraditionalChinese}}允許的 NAS{{else}}NAS access{{end}}</dt><dd>{{if .NAS}}<ul class="nas">{{range .NAS}}<li>{{.}}</li>{{end}}</ul>{{else}}{{if .TraditionalChinese}}無（僅 LAN discovery）{{else}}None (LAN discovery only){{end}}{{end}}</dd>
        <dt>{{if .TraditionalChinese}}權限{{else}}Permissions{{end}}</dt><dd><div class="chips">{{range .Scopes}}<span class="chip">{{.}}</span>{{end}}</div></dd>
      </dl>
      <div class="notice"><strong>{{if .TraditionalChinese}}這是高權限 Client{{else}}This is a powerful client{{end}}</strong><span>{{if .TraditionalChinese}}Agent 應在套用變更前詢問你；高風險計畫仍需在 Admin UI 額外核准。任何取得 Client token 的人都擁有以上權限。{{else}}The agent should ask before applying changes; high-risk plans still require separate Admin UI approval. Anyone holding the client token receives the permissions above.{{end}}</span></div>
      <p class="signin-head">{{if .TraditionalChinese}}以 Gateway 管理員身分核准{{else}}Approve as the Gateway administrator{{end}}</p>
      {{if and .DSMWebLogin (not .SessionUser)}}<p id="dsmStatus" class="dsm-status" hidden></p>{{end}}
      <form method="post">
        {{range $name,$value := .Hidden}}<input type="hidden" name="{{$name}}" value="{{$value}}">{{end}}
        {{if .SessionUser}}
        <p class="session-note">{{if .TraditionalChinese}}將以{{if .SessionDSM}} DSM 管理員{{else}}本機管理員{{end}} <strong>{{.SessionUser}}</strong> 的 Gateway 工作階段核准。{{else}}Approving with the Gateway session of {{if .SessionDSM}}DSM administrator{{else}}local administrator{{end}} <strong>{{.SessionUser}}</strong>.{{end}}</p>
        <div class="actions"><button class="button secondary" type="submit" name="decision" value="deny" formnovalidate>{{if .TraditionalChinese}}拒絕{{else}}Deny{{end}}</button><button class="button primary" type="submit" name="auth_method" value="session">{{if .TraditionalChinese}}允許存取{{else}}Allow access{{end}}</button></div>
        {{else}}
        {{if .DSMWebLogin}}<button class="button primary full" type="button" id="dsmBtn">{{if .TraditionalChinese}}使用 DSM Web Login 登入{{else}}Sign in with DSM Web Login{{end}}</button>{{end}}
        {{if and .DSMWebLogin .LocalLogin}}<div class="divider">{{if .TraditionalChinese}}或使用本機備援帳號{{else}}or use the local fallback account{{end}}</div>{{end}}
        {{if .LocalLogin}}
        <div class="fields">
          <div class="field"><label for="username">{{if .TraditionalChinese}}管理員帳號{{else}}Administrator username{{end}}</label><input class="control" id="username" name="username" autocomplete="username" required autofocus></div>
          <div class="field"><label for="password">{{if .TraditionalChinese}}管理員密碼{{else}}Administrator password{{end}}</label><input class="control" id="password" name="password" type="password" autocomplete="current-password" required></div>
        </div>
        {{end}}
        <div class="actions"><button class="button secondary" type="submit" name="decision" value="deny" formnovalidate>{{if .TraditionalChinese}}拒絕{{else}}Deny{{end}}</button>{{if .LocalLogin}}<button class="button primary" type="submit" name="auth_method" value="local">{{if .TraditionalChinese}}登入並允許{{else}}Sign in and allow{{end}}</button>{{end}}</div>
        {{end}}
      </form>
    </div>
  </section>
</main>
{{if and .DSMWebLogin (not .SessionUser)}}<script nonce="{{.Nonce}}">
(function(){
  var btn=document.getElementById('dsmBtn'); if(!btn){return;}
  var status=document.getElementById('dsmStatus');
  var apiBase=location.pathname.replace(/\/oauth\/authorize\/?$/,'/admin/api');
  var TW={{if .TraditionalChinese}}true{{else}}false{{end}};
  function say(m){ if(status){ status.hidden=false; status.textContent=m; } }
  function post(path,body){ return fetch(apiBase+path,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-DSMCTL-Request':'1'},body:body||'{}'}).then(function(r){ if(!r.ok){ throw new Error('HTTP '+r.status); } return r.status===204?{}:r.json(); }); }
  btn.addEventListener('click',function(){
    var popup=window.open('about:blank','dsmctl_dsm_signin','width=980,height=760');
    if(!popup){ say(TW?'DSM 登入視窗被瀏覽器封鎖，請允許彈出視窗後再試一次。':'The DSM sign-in window was blocked. Allow pop-ups and try again.'); return; }
    btn.disabled=true; say(TW?'請在彈出的 DSM 視窗完成登入…':'Complete sign-in in the DSM window…');
    post('/dsm-login/start').then(function(start){
      var done=false, got=false;
      function cleanup(){ window.removeEventListener('message',onmsg); clearTimeout(tmr); clearInterval(watch); }
      function finish(err){ if(done){ return; } done=true; cleanup(); if(!popup.closed){ popup.close(); } if(err){ btn.disabled=false; say((TW?'登入失敗：':'Sign-in failed: ')+err.message); } else { location.reload(); } }
      function onmsg(ev){ if(ev.origin!==start.nas_origin||ev.source!==popup){ return; } var d=ev.data||{}; if(typeof d.code!=='string'||typeof d.rs!=='string'||!d.code||!d.rs||got){ return; } got=true; post('/dsm-login/complete',JSON.stringify({enrollment_id:start.enrollment_id,code:d.code,rs:d.rs,state:d.state||start.state})).then(function(){ finish(); }).catch(finish); }
      var tmr=setTimeout(function(){ finish(new Error(TW?'逾時':'timed out')); },300000);
      var watch=setInterval(function(){ if(popup.closed&&!got){ finish(new Error(TW?'已取消':'cancelled')); } },500);
      window.addEventListener('message',onmsg);
      popup.location.href=start.login_url;
    }).catch(function(err){ if(!popup.closed){ popup.close(); } btn.disabled=false; say((TW?'無法開始 DSM 登入：':'Could not start DSM sign-in: ')+err.message); });
  });
})();
</script>{{end}}
</body></html>`))

var errorTemplate = template.Must(template.New("oauth-error").Parse(`<!doctype html><html lang="{{if .TraditionalChinese}}zh-Hant{{else}}en{{end}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OAuth · dsmctl</title><style>body{margin:0;min-height:100vh;background:#f3f7fb;color:#0f172a;font:15px/1.5 system-ui;display:grid;place-items:center;padding:24px}.card{max-width:560px;background:#fff;border:1px solid #dbe3ee;border-radius:16px;padding:28px;box-shadow:0 16px 48px rgba(15,23,42,.1)}h1{margin:0 0 10px;font-size:22px}p{color:#475569}a{display:inline-block;margin-top:12px;color:#1d4ed8;font-weight:650}</style></head><body><main class="card"><h1>{{if .TraditionalChinese}}無法授權 Client{{else}}Could not authorize client{{end}}</h1><p>{{.Message}}</p><a href="{{.AdminURL}}">{{if .TraditionalChinese}}開啟 dsmctl Admin{{else}}Open dsmctl Admin{{end}}</a></main></body></html>`))
