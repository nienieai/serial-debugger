// ---- i18n system ----
// Loaded first so t(), formatSysMsg(), etc. are available to all other scripts.

let i18n = {};

function langMenuId(lang) {
  if (lang === 'zh-Hant') return 'menuLangZhHant';
  return 'menuLang' + lang.charAt(0).toUpperCase() + lang.slice(1);
}

async function setLanguage(lang) {
  state.language = lang;
  // Load i18n first so _dir / _font are available
  await loadI18n(lang);
  document.documentElement.dir = i18nDir();
  applyFont();
  // Update language menu checkmarks
  updateLangMenu();
  applyI18n();
  // 标签浮窗（c-tabpop）是 JS 拼的 HTML，applyI18n 扫不到，换语言后手动重画一遍
  if (typeof _tabPop !== 'undefined' && _tabPop && typeof _tabPopRender === 'function') {
    try { _tabPopRender(); } catch (_) {}
  }
  // Re-render history with new language
  var tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId], false);
  }
  saveSettings();
  if (typeof _refreshThemeNames === 'function') _refreshThemeNames();
  // Refresh settings page if it's open (both the tab and the overlay)
  if (typeof refreshSettingsPage === 'function') refreshSettingsPage();
}

var _defaultFont = '';
function applyFont() {
  var font = i18n._font || '';
  if (!font) { document.body.style.fontFamily = _defaultFont; return; }
  if (!_defaultFont) _defaultFont = document.body.style.fontFamily || getComputedStyle(document.body).fontFamily;
  // Try first font name in the list; if not available, fall back to default
  var first = font.split(',')[0].trim().replace(/"/g, '');
  if (first && document.fonts && document.fonts.check) {
    if (document.fonts.check('12px ' + first)) {
      document.body.style.fontFamily = font + ',' + _defaultFont;
      return;
    }
  }
  document.body.style.fontFamily = _defaultFont;
}

var _i18nLoaded = false;   // 语言包是否已经装好（启动早期为 false）
var _i18nSig = '';         // 当前语言包的签名（键数 + 文本总长），用来判断重读后有没有变

function _i18nSignature() {
  var n = 0, len = 0;
  for (var k in i18n) { n++; len += k.length + String(i18n[k]).length; }
  return n + ':' + len;
}

async function loadI18n(lang) {
  // 1. Load built-in first (always available)
  var builtin = {};
  try {
    builtin = await window.go.main.App.GetI18n(lang) || {};
  } catch(e) {}
  // 2. Try external file (i18n/ folder next to exe)
  var ext = null;
  try {
    ext = await window.go.main.App.LoadExternalI18n(lang);
  } catch(e) {}
  if (ext && Object.keys(ext).length > 0) {
    // Determine fallback language for missing keys
    var fbLang = ext._fallback || 'en';
    window._i18nFallback = fbLang;
    // If fallback equals the external lang, use built-in of same lang
    var fbData = {};
    try {
      fbData = await window.go.main.App.GetI18n(fbLang) || {};
    } catch(e) {}
    // Merge: fallback base + external overrides
    var merged = {};
    for (var k in fbData) { merged[k] = fbData[k]; }
    for (var k2 in ext) { merged[k2] = ext[k2]; }
    i18n = merged;
    _i18nLoaded = true;
    _i18nSig = _i18nSignature();
    return;
  }
  // 3. No external file: use built-in directly
  window._i18nFallback = null;
  if (Object.keys(builtin).length > 0) { i18n = builtin; _i18nLoaded = true; _i18nSig = _i18nSignature(); return; }
  i18n = {};
  _i18nLoaded = true;
  _i18nSig = _i18nSignature();
}

function i18nDir() {
  return i18n._dir || 'ltr';
}

// 缺键一律给空串：既不回显 key 名，也不借用别的语言或硬编码中文兜底。
// 语言包（含 builtin/i18n 与 i18n/ 外置包）里没有的键，界面就留空。
//
// 例外只有启动那一瞬间：语言包是异步加载的，而标签页/快捷面板可能比它先建出来
// （守护进程事件一到就建）。那时候 i18n 还是空对象，若也返回空串，这些控件会
// 永远空着——没人会再给它们补一次文案。所以 `_i18nLoaded` 为 false 时先用调用处
// 传进来的兜底文案顶一下；语言包装好后仍然缺键，就按规则留空。
function t(key, fallback) {
  var v = i18n[key];
  if (v !== undefined && v !== null) return v;
  if (!_i18nLoaded) return fallback || '';
  return '';
}

// Translate a system message stored as i18n key|arg1|arg2|...
// Args that look like i18n keys (contain '.') are translated recursively.
// Falls back to raw text if it doesn't start with "sys."
function formatSysMsg(raw) {
  if (!raw || !raw.startsWith('sys.')) return raw || '';
  var parts = raw.split('|');
  var key = parts[0];
  var tmpl = t(key);
  if (!tmpl) return '';
  for (var i = 1; i < parts.length; i++) {
    var arg = parts[i];
    var val = t(arg); // translate if arg is an i18n key
    tmpl = tmpl.replace(/%[sd]/, val);
  }
  return tmpl;
}

// Retranslate all visible system message DOM nodes in-place without touching cache or re-rendering.
//
// 必须遍历**所有**标签页：displayContent 是每个 TabPage 各自创建的一份
// （tabpage.js 给每页都发了同一个 id="displayContent"），而
// document.getElementById 只返回文档里第一个匹配元素——于是切换语言时
// 只有第一个标签页的系统消息会被刷新，其余标签页仍显示旧语言。
// 这里按类名取，覆盖每一个页面。见 TODO #15。
function refreshSysMsgI18n() {
  var nodes = document.querySelectorAll('.display-content .sys-msg[data-sys-raw]');
  for (var i = 0; i < nodes.length; i++) {
    var raw = nodes[i].getAttribute('data-sys-raw');
    nodes[i].textContent = '── ' + formatSysMsg(raw) + ' ──';
  }
}

// ---- external i18n ----

let stateExternalLangs = [];
const builtinLangs = ['zh','zh-Hant','en','fr','es','ru','ar','ja','ko'];

async function initExternalLangs() {
  try {
    stateExternalLangs = await window.go.main.App.ListExternalI18n() || [];
  } catch(e) { stateExternalLangs = []; }
  window._externalLangs = stateExternalLangs;
  // Only show external entries for lang codes NOT already in the built-in menu
  const newLangs = stateExternalLangs.filter(function(info) { return !builtinLangs.includes(info.lang); });
  const dropdown = document.querySelector('#menuLangEn').parentNode;
  if (!dropdown) return;
  // Remove old external buttons
  dropdown.querySelectorAll('.ext-lang').forEach(function(b) { b.remove(); });
  // Add a separator before new external languages
  if (newLangs.length > 0) {
    const sep = document.createElement('div');
    sep.className = 'menu-sep ext-lang';
    dropdown.appendChild(sep);
  }
  newLangs.forEach(function(info) {
    const btn = document.createElement('button');
    btn.className = 'ext-lang';
    btn.id = 'menuLangExt_' + info.lang;
    btn.onclick = function() { setLanguage(info.lang); };
    btn.innerHTML = '<span class="menu-chk chk"></span><span class="menu-text">' + escHtml(info.name) + '</span><span class="menu-shortcut"></span><span class="menu-arrow"></span>';
    dropdown.appendChild(btn);
  });
  updateLangMenu();
}

function updateLangMenu() {
  const lang = state.language || 'zh';
  // Built-in languages
  ['zh','zh-Hant','en','fr','es','ru','ar','ja','ko'].forEach(function(l) {
    const el = document.getElementById(langMenuId(l));
    if (el) {
      el.querySelector('.chk').classList.toggle('on', l === lang);
    }
  });
  // External languages
  stateExternalLangs.forEach(function(info) {
    const el = document.getElementById('menuLangExt_' + info.lang);
    if (el) {
      el.querySelector('.chk').classList.toggle('on', info.lang === lang);
    }
  });
}
