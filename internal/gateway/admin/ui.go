package admin

const indexHTML = `<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>dsmctl Gateway</title><style>
body{font:15px system-ui,sans-serif;max-width:1180px;margin:2rem auto;padding:0 1rem;color:#172033;background:#f6f8fb}
h1{margin-bottom:.25rem}.card{background:white;border:1px solid #dce2ea;border-radius:10px;padding:1rem;margin:1rem 0;overflow:auto}
input,select,button{font:inherit;padding:.5rem;margin:.2rem;border:1px solid #aeb8c5;border-radius:6px}button{cursor:pointer;background:#1d6fe8;color:white}.danger{background:#b42318}.secondary{background:#526173}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #e4e8ee;vertical-align:top}code{overflow-wrap:anywhere}
#message{white-space:pre-wrap;color:#b42318}.muted{color:#627084;font-size:.9rem}.secret{background:#fff7d6;border:1px solid #e2b93b;padding:.8rem;white-space:pre-wrap}[hidden]{display:none!important}</style></head><body>
<h1>dsmctl Gateway</h1><p class="muted">Gateway 與 DSM 身分完全分離。每台 NAS（包含安裝 Gateway 的 NAS）都必須以可連線的 URL 個別新增並登入。管理登入使用限時 HttpOnly/SameSite browser session。</p>
<div id="message"></div>

<section id="setupCard" class="card" hidden><h2>建立 Gateway 管理員</h2>
<p>第一次啟動後一小時內可建立管理員。逾時且仍未設定時，請重新啟動 Gateway。</p>
<p id="setupExpiry" class="muted"></p>
<input id="setupUsername" autocomplete="username" placeholder="管理員帳號（3–64 字元）">
<input id="setupPassword" type="password" autocomplete="new-password" placeholder="密碼（至少 12 bytes）">
<input id="setupConfirm" type="password" autocomplete="new-password" placeholder="再次輸入密碼">
<button onclick="setupAdministrator()">建立並登入</button></section>

<section id="expiredCard" class="card" hidden><h2>設定時間已過</h2><p>Gateway 尚未初始化。重新啟動未初始化的 container 或套件後，會重新開放一小時。</p></section>

<section id="loginCard" class="card" hidden><h2>登入 Gateway</h2>
<p id="initializedAt" class="muted"></p>
<input id="loginUsername" autocomplete="username" placeholder="管理員帳號">
<input id="loginPassword" type="password" autocomplete="current-password" placeholder="密碼">
<button onclick="loginAdministrator()">登入</button>
<p class="muted">若你從未建立管理員卻看到此登入頁，請不要繼續新增 NAS；請由部署主機清除 Gateway 資料後重新初始化。清除資料會刪除所有 NAS session。</p></section>

<main id="application" hidden>
<div class="card"><h2>管理員 Session</h2><p id="sessionInfo"></p>
<input id="currentPassword" type="password" autocomplete="current-password" placeholder="目前密碼">
<input id="newPassword" type="password" autocomplete="new-password" placeholder="新密碼（至少 12 bytes）">
<button onclick="changePassword()">變更密碼</button><button class="secondary" onclick="revokeOthers()">登出其他裝置</button><button class="danger" onclick="logoutAdministrator()">登出</button></div>

<div class="card"><h2>新增 NAS</h2><p class="muted">請填 LAN IP 或 DNS 名稱；container 裡的 localhost 不是 Host NAS。</p>
<input id="name" placeholder="名稱"><input id="url" size="32" placeholder="https://nas:5001"><input id="username" placeholder="DSM 帳號">
<select id="tls"><option value="system_ca">System CA</option><option value="pinned_fingerprint">Pinned fingerprint</option></select><input id="fingerprint" size="40" placeholder="SHA-256 fingerprint"><button onclick="addProfile()">新增</button></div>

<div class="card"><h2>NAS Profiles</h2><button onclick="loadProfiles()">重新整理</button><table><thead><tr><th>名稱</th><th>URL</th><th>Revision</th><th>登入狀態</th><th>操作</th></tr></thead><tbody id="profiles"></tbody></table></div>

<div class="card"><h2>MCP Token</h2><p class="muted">新 Token 預設只有 nas.read；NAS allowlist 留空代表不允許任何 NAS。</p>
<input id="tokenName" placeholder="Token 名稱"><input id="tokenNAS" size="28" placeholder="NAS 名稱，以逗號分隔">
<label><input type="checkbox" class="scope" value="nas.read" checked>read</label><label><input type="checkbox" class="scope" value="nas.plan">plan</label><label><input type="checkbox" class="scope" value="nas.apply">apply</label><label><input type="checkbox" class="scope" value="nas.admin">admin</label>
<button onclick="createMCPToken()">建立 Token</button><div id="issued" class="secret" hidden></div>
<table><thead><tr><th>名稱 / ID</th><th>Scope</th><th>NAS allowlist</th><th>狀態</th><th>操作</th></tr></thead><tbody id="tokens"></tbody></table></div>

<div class="card"><h2>高風險 Apply 核准</h2><input id="approvalHash" size="48" placeholder="Plan SHA-256 hash"><input id="approvalNAS" placeholder="NAS"><input id="approvalRevision" type="number" min="1" placeholder="Profile revision"><input id="approvalToken" size="34" placeholder="Requesting token ID"><button onclick="createApproval()">建立核准</button>
<table><thead><tr><th>Plan / NAS</th><th>Requesting token</th><th>期限</th><th>狀態</th></tr></thead><tbody id="approvals"></tbody></table></div>

<div class="card"><h2>Audit</h2><button onclick="loadAudit()">重新整理</button><button onclick="exportAudit()">匯出 JSONL</button><pre id="audit"></pre></div>
</main>

<script>
const $=id=>document.getElementById(id),adminBase=location.pathname.replace(/\/?$/,'/'),apiBase=adminBase+'api';
function show(value){$('message').textContent=value instanceof Error?value.message:String(value||'')}
async function api(path,options={}){let method=(options.method||'GET').toUpperCase(),headers=Object.assign({},options.headers||{});if(method!=='GET'&&method!=='HEAD'){headers['Content-Type']='application/json';headers['X-DSMCTL-Request']='1'}let response=await fetch(apiBase+path,Object.assign({credentials:'same-origin'},options,{headers})),text=await response.text(),value={};if(text){try{value=JSON.parse(text)}catch{value={error:text}}}if(!response.ok){let error=new Error(value.error||response.statusText);error.status=response.status;throw error}return value}
function hideEntry(){for(const id of ['setupCard','expiredCard','loginCard','application'])$(id).hidden=true}
async function initialize(){hideEntry();try{let status=await api('/setup/status');if(status.state==='setup_available'){$('setupCard').hidden=false;$('setupExpiry').textContent='本次設定期限：'+new Date(status.setup_expires_at).toLocaleString();return}if(status.state==='setup_expired'){$('expiredCard').hidden=false;return}$('initializedAt').textContent=status.initialized_at?'此 Gateway 已於 '+new Date(status.initialized_at).toLocaleString()+' 初始化。':'';try{await showApplication()}catch(error){if(error.status===401)$('loginCard').hidden=false;else throw error}}catch(error){show(error)}}
async function setupAdministrator(){if($('setupPassword').value!==$('setupConfirm').value){show('兩次輸入的密碼不一致');return}try{await api('/setup',{method:'POST',body:JSON.stringify({username:$('setupUsername').value,password:$('setupPassword').value})});$('setupPassword').value=$('setupConfirm').value='';await showApplication()}catch(error){show(error)}}
async function loginAdministrator(){try{await api('/login',{method:'POST',body:JSON.stringify({username:$('loginUsername').value,password:$('loginPassword').value})});$('loginPassword').value='';await showApplication()}catch(error){$('loginPassword').value='';show(error)}}
async function showApplication(){let value=await api('/session');hideEntry();$('application').hidden=false;$('sessionInfo').textContent=value.session.username+'，Session 到期：'+new Date(value.session.expires_at).toLocaleString();show('');await Promise.allSettled([loadProfiles(),loadTokens(),loadApprovals(),loadAudit()])}
async function logoutAdministrator(){try{await api('/logout',{method:'POST',body:'{}'});await initialize()}catch(error){show(error)}}
async function changePassword(){try{await api('/password',{method:'PUT',body:JSON.stringify({current_password:$('currentPassword').value,new_password:$('newPassword').value})});$('currentPassword').value=$('newPassword').value='';show('密碼已更新，其他管理員 Session 已撤銷。')}catch(error){$('currentPassword').value=$('newPassword').value='';show(error)}}
async function revokeOthers(){try{await api('/sessions/revoke-others',{method:'POST',body:'{}'});show('其他管理員 Session 已撤銷。')}catch(error){show(error)}}
function cell(row,text){let td=row.insertCell();td.textContent=text;return td}function button(parent,label,fn,danger=false){let item=document.createElement('button');item.textContent=label;item.onclick=fn;if(danger)item.className='danger';parent.appendChild(item)}
async function addProfile(){try{let pinned=$('tls').value==='pinned_fingerprint',fingerprint=$('fingerprint').value.trim(),confirmed=!pinned||confirm('確認信任此 SHA-256 certificate fingerprint？\n'+fingerprint);if(!confirmed)return;await api('/profiles',{method:'POST',body:JSON.stringify({name:$('name').value,url:$('url').value,username:$('username').value,tls_mode:$('tls').value,certificate_fingerprint:fingerprint,confirm_certificate_fingerprint:confirmed})});await loadProfiles()}catch(error){show(error)}}
async function loadProfiles(){try{let value=await api('/profiles'),body=$('profiles');body.textContent='';for(const profile of value.profiles){let row=body.insertRow();cell(row,(profile.default?'★ ':'')+profile.name);cell(row,profile.url);cell(row,String(profile.revision));cell(row,profile.session_stored?'Web session':profile.password_stored?'Password':'尚未登入');let actions=cell(row,'');button(actions,'設為預設',()=>setDefault(profile.name));button(actions,'測試',()=>testNAS(profile.name));button(actions,'Web Login',()=>webLogin(profile.name));button(actions,'密碼/OTP',()=>passwordLogin(profile.name));button(actions,'刪除',()=>removeNAS(profile),true)}show('')}catch(error){show(error)}}
async function setDefault(name){try{await api('/profiles/'+encodeURIComponent(name)+'/default',{method:'POST',body:'{}'});await loadProfiles()}catch(error){show(error)}}async function testNAS(name){try{show(JSON.stringify(await api('/profiles/'+encodeURIComponent(name)+'/test',{method:'POST',body:'{}'}),null,2))}catch(error){show(error)}}
async function passwordLogin(name){let password=prompt('DSM password（只用於這次 enrollment）');if(password===null)return;let otp=prompt('OTP（沒有就留空）')||'';try{await api('/profiles/'+encodeURIComponent(name)+'/credentials/password',{method:'POST',body:JSON.stringify({password,otp})});password=otp='';await loadProfiles()}catch(error){password=otp='';show(error)}}
async function webLogin(name){try{let start=await api('/profiles/'+encodeURIComponent(name)+'/weblogin/start',{method:'POST',body:'{}'}),popup=window.open(start.login_url,'dsmctl_signin','width=560,height=720'),listener=async event=>{if(event.origin!==start.nas_origin)return;let data=event.data||{};if(!data.code)return;window.removeEventListener('message',listener);try{await api('/profiles/'+encodeURIComponent(name)+'/weblogin/complete',{method:'POST',body:JSON.stringify({enrollment_id:start.enrollment_id,code:data.code,rs:data.rs,state:data.state||start.state})});if(popup)popup.close();await loadProfiles()}catch(error){show(error)}};window.addEventListener('message',listener)}catch(error){show(error)}}
async function removeNAS(profile){if(!confirm('刪除 '+profile.name+' 及其 credentials？'))return;try{await api('/profiles/'+encodeURIComponent(profile.name)+'?revision='+profile.revision,{method:'DELETE',body:'{}'});await loadProfiles()}catch(error){show(error)}}
async function createMCPToken(){try{let scopes=[...document.querySelectorAll('.scope:checked')].map(item=>item.value),nas=$('tokenNAS').value.split(',').map(item=>item.trim()).filter(Boolean),value=await api('/mcp-tokens',{method:'POST',body:JSON.stringify({name:$('tokenName').value,scopes,nas_allowlist:nas})});$('issued').hidden=false;$('issued').textContent='請立即保存，之後不會再次顯示：\n'+value.bearer_token;await loadTokens()}catch(error){show(error)}}
async function loadTokens(){try{let value=await api('/mcp-tokens'),body=$('tokens');body.textContent='';for(const token of value.tokens){let row=body.insertRow();cell(row,token.name+'\n'+token.id);cell(row,token.scopes.join(', '));cell(row,token.nas_allowlist.join(', '));cell(row,token.revoked_at?'revoked':token.expires_at?'expires '+token.expires_at:'active');let actions=cell(row,'');button(actions,'輪替',()=>rotateToken(token.id));button(actions,'撤銷',()=>revokeToken(token.id),true)}}catch(error){show(error)}}
async function rotateToken(id){if(!confirm('舊 Token 會立即失效，確定輪替？'))return;try{let value=await api('/mcp-tokens/'+id+'/rotate',{method:'POST',body:'{}'});$('issued').hidden=false;$('issued').textContent='請立即保存新 Token：\n'+value.bearer_token;await loadTokens()}catch(error){show(error)}}async function revokeToken(id){if(!confirm('確定撤銷？'))return;try{await api('/mcp-tokens/'+id,{method:'DELETE',body:'{}'});await loadTokens()}catch(error){show(error)}}
async function createApproval(){try{await api('/approvals',{method:'POST',body:JSON.stringify({plan_hash:$('approvalHash').value,nas:$('approvalNAS').value,profile_revision:Number($('approvalRevision').value),requesting_token_id:$('approvalToken').value})});await loadApprovals()}catch(error){show(error)}}async function loadApprovals(){try{let value=await api('/approvals?include_consumed=true'),body=$('approvals');body.textContent='';for(const approval of value.approvals){let row=body.insertRow();cell(row,approval.plan_hash+'\n'+approval.nas+' @ '+approval.profile_revision);cell(row,approval.requesting_token_id);cell(row,approval.expires_at);cell(row,approval.consumed_at?'consumed '+approval.consumed_at:new Date(approval.expires_at)<new Date()?'expired':'ready')}}catch(error){show(error)}}
async function loadAudit(){try{$('audit').textContent=JSON.stringify((await api('/audit?limit=100')).events,null,2)}catch(error){show(error)}}async function exportAudit(){try{let response=await fetch(apiBase+'/audit/export?limit=1000',{credentials:'same-origin'});if(!response.ok)throw new Error('匯出失敗');let target=URL.createObjectURL(await response.blob()),link=document.createElement('a');link.href=target;link.download='dsmctl-audit.jsonl';link.click();URL.revokeObjectURL(target)}catch(error){show(error)}}
initialize();
</script></body></html>`
