package weblogin

// buildPage renders the loopback helper page that hosts the DSM sign-in
// popup and relays the one-time code to the local callback.
//
// The visual identity (brand/slate token scales, semantic aliases,
// typography, card language) is copied verbatim from the gateway
// administration UI; internal/gateway/admin/ui.go is the source of truth.
// TestPageCarriesSharedDesignTokens pins the shared literals here and
// internal/gateway/admin/handler_test.go pins them there, so drift fails a
// build. Copy is localized (en, zh-TW, zh-CN, ja, de) from
// navigator.language; the terminal state (success or error) is chosen by
// the /callback HTTP status, never by injecting server response text.
func buildPage(loginURL, dsmOrigin string) string {
	// loginURL and dsmOrigin are produced internally from a validated base URL,
	// so they are safe to embed in the JS string literals below.
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>dsmctl sign-in</title>
<style>
:root{
  color-scheme:light;
  --brand-50:#eef7ff;
  --brand-100:#d9ecff;
  --brand-200:#b9dcff;
  --brand-300:#88c3ff;
  --brand-400:#4da5f4;
  --brand-500:#2588df;
  --brand-600:#146fbd;
  --brand-700:#155b97;
  --brand-800:#174b78;
  --brand-900:#173d61;
  --brand-950:#0d263f;
  --slate-25:#fbfcfe;
  --slate-50:#f7f9fc;
  --slate-100:#eef2f6;
  --slate-200:#dde5ed;
  --slate-300:#c6d1dc;
  --slate-400:#93a3b5;
  --slate-500:#66778b;
  --slate-600:#485a70;
  --slate-700:#34465b;
  --slate-800:#223246;
  --slate-900:#162334;
  --slate-950:#0f1927;
  --white:#ffffff;
  --color-action:var(--brand-500);
  --color-action-hover:var(--brand-600);
  --color-action-soft:var(--brand-50);
  --color-action-text:var(--white);
  --color-focus:rgba(37,136,223,.28);
  --color-focus-soft:rgba(37,136,223,.12);
  --canvas:var(--slate-100);
  --surface:var(--white);
  --surface-soft:var(--slate-50);
  --text:var(--slate-900);
  --muted:var(--slate-500);
  --line:var(--slate-200);
  --line-strong:var(--slate-300);
  --success:#1f9d68;
  --success-vivid:#35b47b;
  --success-soft:#eaf8f2;
  --success-text:#267b59;
  --danger:#cf3f3f;
  --danger-soft:#fff1f1;
  --danger-text:#a43131;
  --shadow-large:0 28px 80px rgba(25,54,86,.18);
  font-family:Inter,"Segoe UI","Noto Sans TC",system-ui,-apple-system,sans-serif;
}
*{box-sizing:border-box}
html,body{margin:0;min-height:100%}
body{display:flex;align-items:center;justify-content:center;min-height:100vh;padding:24px;background:radial-gradient(circle at 86% 12%,rgba(77,165,244,.16),transparent 28%),linear-gradient(145deg,var(--brand-50) 0,var(--slate-50) 52%,var(--slate-100) 100%);color:var(--text);font-size:14px;line-height:1.55}
button{font:inherit;cursor:pointer}
:focus-visible{outline:3px solid var(--color-focus);outline-offset:2px}
.card{width:min(430px,100%);padding:34px;border:1px solid rgba(198,209,220,.92);border-radius:18px;background:rgba(255,255,255,.92);box-shadow:var(--shadow-large);backdrop-filter:blur(14px)}
.brand{display:flex;align-items:center;gap:11px;margin-bottom:26px}
.brand-mark{display:grid;grid-template-columns:repeat(2,9px);grid-template-rows:repeat(2,9px);gap:3px;padding:8px;border-radius:10px;background:linear-gradient(145deg,var(--brand-400),var(--brand-600));box-shadow:0 7px 16px rgba(20,111,189,.25)}
.brand-mark i{display:block;border-radius:2px;background:rgba(255,255,255,.94)}
.brand-copy strong{display:block;font-size:15px;line-height:1.15;letter-spacing:.01em}
.brand-copy span{display:block;margin-top:2px;color:var(--muted);font-size:11px}
h1{margin:0 0 18px;font-size:25px;letter-spacing:-.02em}
.status{display:flex;gap:11px;align-items:flex-start;margin:0 0 22px;padding:12px 14px;border-radius:10px;background:var(--color-action-soft);color:var(--brand-700);font-size:13px}
.dot{flex:none;width:9px;height:9px;margin-top:5px;border-radius:50%;background:var(--color-action)}
[data-state="waiting"] .dot{animation:pulse 1.6s ease-in-out infinite}
[data-state="exchanging"] .dot{animation:pulse .8s ease-in-out infinite}
[data-state="success"] .status{background:var(--success-soft);color:var(--success-text)}
[data-state="success"] .dot{background:var(--success)}
[data-state="error"] .status{background:var(--danger-soft);color:var(--danger-text)}
[data-state="error"] .dot{background:var(--danger)}
@keyframes pulse{0%,100%{box-shadow:0 0 0 0 var(--color-focus-soft)}50%{box-shadow:0 0 0 7px var(--color-focus-soft)}}
.msg{display:none;margin:0}
[data-state="waiting"] .msg-waiting{display:block}
[data-state="exchanging"] .msg-exchanging{display:block}
[data-state="success"] .msg-success{display:block}
[data-state="error"] .msg-error{display:block}
.primary{display:none;width:100%;min-height:40px;align-items:center;justify-content:center;gap:7px;padding:8px 15px;border:1px solid transparent;border-radius:7px;background:var(--color-action);color:var(--color-action-text);font-weight:600;box-shadow:0 2px 4px rgba(20,111,189,.16);transition:background .15s,transform .15s,box-shadow .15s}
.primary:hover{background:var(--color-action-hover);box-shadow:0 5px 12px rgba(20,111,189,.2)}
.primary:active{transform:translateY(1px)}
[data-state="waiting"] .primary{display:inline-flex}
.foot{margin:20px 0 0;padding:0;list-style:none;display:flex;flex-direction:column;gap:6px;color:var(--muted);font-size:12px}
.foot li{display:flex;align-items:center;gap:6px}
.foot li:before{content:"\2713";color:var(--success-vivid)}
</style>
</head>
<body data-state="waiting">
<main class="card">
  <div class="brand"><span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span><span class="brand-copy"><strong>dsmctl</strong><span data-i18n="brandSub">DSM web sign-in</span></span></div>
  <h1 data-i18n="heading">Sign in to DSM</h1>
  <div class="status" role="status">
    <span class="dot" aria-hidden="true"></span>
    <span>
      <p class="msg msg-waiting" data-i18n="waiting">Opening the NAS sign-in window… if nothing appears, use the button.</p>
      <p class="msg msg-exchanging" data-i18n="exchanging">Completing sign-in…</p>
      <p class="msg msg-success" data-i18n="success">Signed in. You can close this window and return to the terminal.</p>
      <p class="msg msg-error" data-i18n="failure">Sign-in failed. Return to the terminal for details.</p>
    </span>
  </div>
  <button id="go" class="primary" data-i18n="button">Open sign-in window</button>
  <ul class="foot">
    <li data-i18n="footPassword">Password entered only on the NAS's own page</li>
    <li data-i18n="footCrypto">PKCE + Noise-encrypted code exchange</li>
  </ul>
</main>
<script>
var loginUrl = "` + loginURL + `";
var dsmOrigin = "` + dsmOrigin + `";
var strings = {
en:{brandSub:"DSM web sign-in",heading:"Sign in to DSM",waiting:"Opening the NAS sign-in window… if nothing appears, use the button.",exchanging:"Completing sign-in…",success:"Signed in. You can close this window and return to the terminal.",failure:"Sign-in failed. Return to the terminal for details.",button:"Open sign-in window",footPassword:"Password entered only on the NAS's own page",footCrypto:"PKCE + Noise-encrypted code exchange"},
"zh-TW":{brandSub:"DSM 網頁登入",heading:"登入 DSM",waiting:"正在開啟 NAS 登入視窗…若沒有出現，請按下方按鈕。",exchanging:"正在完成登入…",success:"已登入。可以關閉此視窗，回到終端機。",failure:"登入失敗。請回到終端機查看詳細資訊。",button:"開啟登入視窗",footPassword:"密碼只在 NAS 自己的頁面輸入",footCrypto:"PKCE + Noise 加密的代碼交換"},
"zh-CN":{brandSub:"DSM 网页登录",heading:"登录 DSM",waiting:"正在打开 NAS 登录窗口…若未出现，请点击下方按钮。",exchanging:"正在完成登录…",success:"已登录。可以关闭此窗口，回到终端。",failure:"登录失败。请回到终端查看详细信息。",button:"打开登录窗口",footPassword:"密码只在 NAS 自己的页面输入",footCrypto:"PKCE + Noise 加密的代码交换"},
ja:{brandSub:"DSM ウェブサインイン",heading:"DSM にサインイン",waiting:"NAS のサインインウィンドウを開いています…表示されない場合は下のボタンを押してください。",exchanging:"サインインを完了しています…",success:"サインインしました。このウィンドウを閉じてターミナルに戻れます。",failure:"サインインに失敗しました。詳細はターミナルをご確認ください。",button:"サインインウィンドウを開く",footPassword:"パスワードは NAS 自身のページでのみ入力されます",footCrypto:"PKCE + Noise 暗号化によるコード交換"},
de:{brandSub:"DSM-Web-Anmeldung",heading:"Bei DSM anmelden",waiting:"Das NAS-Anmeldefenster wird geöffnet … Falls nichts erscheint, nutzen Sie die Schaltfläche.",exchanging:"Anmeldung wird abgeschlossen …",success:"Angemeldet. Sie können dieses Fenster schließen und zum Terminal zurückkehren.",failure:"Anmeldung fehlgeschlagen. Details finden Sie im Terminal.",button:"Anmeldefenster öffnen",footPassword:"Passwort wird nur auf der Seite des NAS eingegeben",footCrypto:"Code-Austausch mit PKCE + Noise-Verschlüsselung"}
};
function normalizeLocale(value){var input=String(value||"").toLowerCase();if(input.indexOf("zh-hant")===0||input.indexOf("zh-tw")===0||input.indexOf("zh-hk")===0)return "zh-TW";if(input.indexOf("zh")===0)return "zh-CN";if(input.indexOf("ja")===0)return "ja";if(input.indexOf("de")===0)return "de";return "en"}
var locale = normalizeLocale(navigator.language);
document.documentElement.lang = {"zh-TW":"zh-Hant","zh-CN":"zh-Hans",ja:"ja",de:"de",en:"en"}[locale];
var table = strings[locale];
for (var nodes = document.querySelectorAll("[data-i18n]"), i = 0; i < nodes.length; i++) {
  nodes[i].textContent = table[nodes[i].getAttribute("data-i18n")];
}
function setState(s){ document.body.setAttribute("data-state", s); }
function start(){ window.open(loginUrl, "dsmctl_signin", "width=560,height=720"); }
document.getElementById("go").onclick = start;
window.addEventListener("message", function(e){
  if (e.origin !== dsmOrigin) return;
  var d = e.data || {};
  if (!d.code) return;
  setState("exchanging");
  fetch("/callback", {method:"POST", headers:{"Content-Type":"application/json"},
    body: JSON.stringify({code:d.code, rs:d.rs, state:d.state || ""})})
    .then(function(r){ setState(r.ok?"success":"error"); })
    .catch(function(){ setState("error"); });
});
window.addEventListener("load", function(){ try { start(); } catch (e) {} });
</script>
</body></html>`
}
