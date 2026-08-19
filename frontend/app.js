// ---- state ----
const state = {
  daemonOnline: false,
  processRunning: false,
  connectFailures: 0,
  tabs: [],
  activeTab: 0,
  tabCounter: 0,
  displayMode: 'text',
  txDisplayMode: 'text',
  p1DisplayMode: 'text',
  p2DisplayMode: 'text',
  currentMode: 'single',
  statusInterval: null,
  lastPorts: '',
  knownDaemonSessions: {},
  historyCache: {},
  sendRatio: 0.3,
  quickPanelRatio: 0,
  quickPresets: [],
  _qpCycleActive: false,
  _qpCycleTimer: null,
  encoding: 'utf-8',
  hexCase: 'upper',
  hexPrefix: true,
  hexSep: 'space',
  crVisible: true,
  hexEscapeMode: 'show',
  hexEscapeFormat: 'slash',
  copyHexEscapes: true,
  displayFontFamily: 'Consolas',
  displayFontSize: 14,
  tabSize: 4,
  eolSequence: 'lf',
  colorThemeId: 'system',
  colorThemeMode: 'auto',
  colorThemes: [],
  iconThemeId: '',
  iconThemes: [],
  language: 'zh',
  scrollLocked: false,
  statsBase: {},
  autoCreateSession: false,
  autoSaveHistory: true,
};

function newTabSendState() {
  return { sendFormat: 'text', sendTextBuf: '', sendHexBuf: '', appendSuffix: '', autoSend: false, sendInterval: 1000 };
}

function getSendState() {
  const tab = state.tabs[state.activeTab];
  if (!tab) return newTabSendState();
  if (!tab._s) tab._s = newTabSendState();
  return tab._s;
}

// ---- settings persistence ----
let _saveSettingsTimer = null;
let _syncQuickPresetsTimer = null;
function saveSettings() {
  clearTimeout(_saveSettingsTimer);
  _saveSettingsTimer = setTimeout(() => {
    const s = {
      displayMode: state.displayMode,
      txDisplayMode: state.txDisplayMode,
      sendRatio: state.sendRatio,
      quickPanelRatio: state.quickPanelRatio,
      serial: {
        baud: parseInt((pageEl('baudSelect')||{}).value) || 115200,
        dataBits: parseInt((pageEl('dataBitsSelect')||{}).value) || 8,
        stopBits: (pageEl('stopBitsSelect')||{}).value || '1',
        parity: (pageEl('paritySelect')||{}).value || 'none',
        flowControl: (pageEl('flowControlSelect')||{}).value || 'none',
      },
      autoSend: {
        intervalMs: parseInt((pageEl('sendInterval')||{}).value) || 1000,
      },
      append: {
        suffix: (pageEl('appendSelect')||{}).value,
      },
      encoding: state.encoding,
      hexCase: state.hexCase,
      hexPrefix: state.hexPrefix,
      hexSep: state.hexSep,
      crVisible: state.crVisible,
      hexEscapeMode: state.hexEscapeMode,
      hexEscapeFormat: state.hexEscapeFormat,
      copyHexEscapes: state.copyHexEscapes,
      displayFontFamily: state.displayFontFamily,
      displayFontSize: state.displayFontSize,
      tabSize: state.tabSize,
      eolSequence: state.eolSequence,
      theme: state.colorThemeMode,
      colorThemeId: state.colorThemeId,
      colorThemeMode: state.colorThemeMode,
      iconThemeId: state.iconThemeId,
      language: state.language,
      autoCreateSession: state.autoCreateSession,
      displayColors: JSON.stringify(state.displayColors || _defaultDisplayColors()),
    };
    window.go.main.App.SaveAppSettings(s).catch(() => {});
    // Also persist multistr entries to daemon
    var tab = getActiveTab();
    if (tab && tab.sessionId) {
      window.go.main.App.MultistrSave(tab.sessionId).catch(() => {});
    }
  }, 500);
}

async function loadSettings() {
  try {
    const s = await window.go.main.App.LoadAppSettings();
    if (s) {
      if (s.displayMode === 'hex') {
        state.displayMode = 'hex';
        const btn = pageEl('btnToggleDisplay');
        if (btn) { btn.textContent = '⇃ ' + t('display.rx_hex', '接收Hex显示'); btn.classList.add('btn-hex'); }
      }
      if (s.txDisplayMode === 'hex') {
        state.txDisplayMode = 'hex';
        const btn2 = pageEl('btnToggleSend');
      }

      state.sendRatio = (typeof s.sendRatio === 'number') ? s.sendRatio : 0.3;
      state.quickPanelRatio = (typeof s.quickPanelRatio === 'number') ? s.quickPanelRatio : 0;

      if (s.serial) {
        setSelect('baudSelect', s.serial.baud);
        setSelect('dataBitsSelect', s.serial.dataBits);
        setSelect('stopBitsSelect', s.serial.stopBits);
        setSelect('paritySelect', s.serial.parity);
        setSelect('flowControlSelect', s.serial.flowControl);
        setSelect('baudSelectA', s.serial.baud);
        setSelect('dataBitsSelectA', s.serial.dataBits);
        setSelect('stopBitsSelectA', s.serial.stopBits);
        setSelect('paritySelectA', s.serial.parity);
        setSelect('flowControlSelectA', s.serial.flowControl);
        setSelect('baudSelectB', s.serial.baud);
        setSelect('dataBitsSelectB', s.serial.dataBits);
        setSelect('stopBitsSelectB', s.serial.stopBits);
        setSelect('paritySelectB', s.serial.parity);
        setSelect('flowControlSelectB', s.serial.flowControl);
      }

      if (s.autoSend && s.autoSend.intervalMs) {
        var si = pageEl('sendInterval'); if (si) si.value = s.autoSend.intervalMs;
      }

      if (s.append && s.append.suffix !== undefined) {
        const sel = pageEl('appendSelect');
        if (sel) { sel.value = s.append.suffix; var csWrap = sel.closest('.cs-wrap'); if (csWrap && typeof _csRefresh === 'function') _csRefresh(csWrap); }
              }

      if (s.encoding && (s.encoding === 'ascii' || s.encoding === 'gb2312' || s.encoding === 'utf-8')) {
        state.encoding = s.encoding;
        updateEncodingDisplay();
      }
      if (s.hexCase === 'upper' || s.hexCase === 'lower') state.hexCase = s.hexCase;
      if (typeof s.hexPrefix === 'boolean') state.hexPrefix = s.hexPrefix;
      if (s.hexSep === 'space' || s.hexSep === 'comma' || s.hexSep === 'none') state.hexSep = s.hexSep;
      if (typeof s.crVisible === 'boolean') state.crVisible = s.crVisible;
      if (s.hexEscapeMode === 'show' || s.hexEscapeMode === 'hide' || s.hexEscapeMode === 'raw') state.hexEscapeMode = s.hexEscapeMode;
      else if (typeof s.showHexEscapes === 'boolean') state.hexEscapeMode = s.showHexEscapes ? 'show' : 'raw';
      if (typeof s.hexEscapeFormat === 'string') state.hexEscapeFormat = s.hexEscapeFormat;
      if (typeof s.copyHexEscapes === 'boolean') state.copyHexEscapes = s.copyHexEscapes;
      updateHexMenu();
      updateTextMenu();
      if (s.colorThemeId && typeof s.colorThemeId === 'string') {
        var cid = s.colorThemeId;
        // Migrate old virtual IDs → base id + mode
        if (cid === 'system:auto' || cid === 'system-auto' || cid === 'auto') { state.colorThemeId = 'system'; state.colorThemeMode = 'auto'; }
        else if (cid === 'system:dark' || cid === 'system-dark' || cid === 'dark') { state.colorThemeId = 'system'; state.colorThemeMode = 'dark'; }
        else if (cid === 'system:light' || cid === 'system-light' || cid === 'light') { state.colorThemeId = 'system'; state.colorThemeMode = 'light'; }
        else { state.colorThemeId = cid; state.colorThemeMode = s.colorThemeMode || (s.theme || 'auto'); }
      } else if (s.theme === 'auto' || s.theme === 'dark' || s.theme === 'light') {
        state.colorThemeId = 'system'; state.colorThemeMode = s.theme;
      }
      if (s.iconThemeId && typeof s.iconThemeId === 'string') state.iconThemeId = s.iconThemeId;
      if (typeof s.autoCreateSession === 'boolean') state.autoCreateSession = s.autoCreateSession;
      updateAutoCreateCheck(state.autoCreateSession);
      if (s.language && typeof s.language === 'string') state.language = s.language;
      if (typeof s.displayFontFamily === 'string') state.displayFontFamily = s.displayFontFamily;
      if (typeof s.displayFontSize === 'number') state.displayFontSize = s.displayFontSize;
      if (typeof s.tabSize === 'number') state.tabSize = s.tabSize;
      if (typeof s.eolSequence === 'string') state.eolSequence = s.eolSequence;
      _applyDisplayStyle();
      if (_statusBar) _statusBar.updateEol();
      // Display color overrides
      if (s.displayColors) {
        try { state.displayColors = JSON.parse(s.displayColors); } catch(e) {}
      }
      if (!state.displayColors) state.displayColors = _defaultDisplayColors();
      if (typeof _applyDisplayColors === 'function') _applyDisplayColors();
    }
    // Always initialize UI regardless of whether settings exist
    await initColorThemes();
    await initIconThemes();
    await loadI18n(state.language);
    document.documentElement.dir = i18nDir();
    applyFont();
    updateLangMenu();
    applyI18n();

    applySendRatio();
    applyQuickPanelRatio();
    quickPanelRender();
  } catch (e) {
    // Settings file absent or unreadable — initialize UI with defaults
    initColorThemes().catch(() => {});
    initIconThemes().catch(() => {});
    _applyDisplayStyle();
    if (_statusBar) _statusBar.updateEol();
    await loadI18n(state.language).catch(() => {});
    document.documentElement.dir = i18nDir();
    applyFont();
    updateLangMenu();
    applyI18n();
    applySendRatio();
    applyQuickPanelRatio();
    quickPanelRender();
  }
}

function _renderSendMirror(text, container) {
  var frag = document.createDocumentFragment();
  if (!text) {
    var ph = document.createElement('span');
    ph.className = 'send-mirror-placeholder';
    ph.textContent = '输入要发送的数据...';
    frag.appendChild(ph);
  } else {
    for (var i = 0; i < text.length; i++) {
      var c = text[i];
      if (c === '\r' && i + 1 < text.length && text[i + 1] === '\n') {
        // CRLF pair
        var sp2 = _makeCtrlSpan('\r\n', 'crlf');
        frag.appendChild(sp2);
        i++;
      } else if (c === ' ') {
        frag.appendChild(_makeCtrlSpan(' ', 'space'));
      } else if (c === '\t') {
        frag.appendChild(_makeCtrlSpan('\t', 'tab'));
      } else if (c === '\r') {
        frag.appendChild(_makeCtrlSpan('\r', 'cr'));
      } else if (c === '\n') {
        frag.appendChild(_makeCtrlSpan('\n', 'lf'));
      } else {
        frag.appendChild(document.createTextNode(c));
      }
    }
  }
  container.innerHTML = '';
  container.appendChild(frag);
}

function _makeCtrlSpan(real, ws) {
  var markers = { space: '·', tab: null, cr: '←', lf: '↵', crlf: '←↵' };
  var sp = document.createElement('span');
  sp.className = 'ctrl-char';
  sp.setAttribute('data-ws', ws);
  var realEl = document.createElement('span');
  realEl.className = 'ctrl-real';
  realEl.textContent = real;
  var mk = markers[ws];
  if (mk) {
    var markEl = document.createElement('span');
    markEl.className = 'ctrl-mark';
    markEl.textContent = mk;
    sp.appendChild(markEl);
  }
  sp.appendChild(realEl);
  return sp;
}

function _renderSendMirrorHex(text, container) {
  var frag = document.createDocumentFragment();
  if (!text) {
    var ph = document.createElement('span');
    ph.className = 'send-mirror-placeholder';
    ph.textContent = '输入要发送的数据...';
    container.innerHTML = '';
    container.appendChild(ph);
    return;
  }
  // Tokenize: split on whitespace/commas/0x, render CR/LF as line breaks
  var tokens = [];
  var cur = '';
  for (var i = 0; i < text.length; i++) {
    var ch = text[i];
    if (ch === '\r' || ch === '\n') {
      if (cur) { tokens.push(cur); cur = ''; }
      tokens.push(ch); // render as line break
    } else if (ch === ' ' || ch === ',') {
      if (cur) { tokens.push(cur); cur = ''; }
      tokens.push(ch);
    } else if (ch === '0' && i + 2 < text.length && (text[i+1] === 'x' || text[i+1] === 'X')) {
      // 0x / 0X — push as display token preserving case
      if (cur) { tokens.push(cur); cur = ''; }
      tokens.push(text[i+1] === 'X' ? '0X' : '0x');
      i += 1; // skip the x/X
    } else {
      cur += ch;
    }
  }
  if (cur) tokens.push(cur);
  // Render tokens
  for (var t = 0; t < tokens.length; t++) {
    var tok = tokens[t];
    if (tok === ' ' || tok === ',' || tok === '\r' || tok === '\n') {
      frag.appendChild(document.createTextNode(tok));
    } else if (tok === '0x' || tok === '0X') {
      var px = document.createElement('span');
      px.className = 'send-hex-prefix';
      px.textContent = tok;
      frag.appendChild(px);
    } else {
      var hex = tok;
      var isHex = /^[0-9a-fA-F]+$/.test(hex);
      if (isHex && hex.length > 2) {
        // Continuous hex — split into 2-char bytes
        for (var c = 0; c < hex.length; c += 2) {
          var chunk = hex.substring(c, Math.min(c + 2, hex.length));
          var sp2 = document.createElement('span');
          sp2.className = chunk.length === 2 ? 'send-hex-ok' : 'send-hex-bad';
          sp2.textContent = chunk;
          frag.appendChild(sp2);
        }
      } else {
        var valid = isHex && hex.length >= 1 && hex.length <= 2;
        var sp = document.createElement('span');
        sp.className = valid ? 'send-hex-ok' : 'send-hex-bad';
        sp.textContent = tok;
        frag.appendChild(sp);
      }
    }
  }
  container.innerHTML = '';
  container.appendChild(frag);
}

function _syncSendMirror(ta) {
  if (!ta) ta = pageEl('sendInput');
  if (!ta) return;
  var wrap = ta.parentNode;
  if (!wrap || !wrap.classList.contains('send-input-wrap')) return;
  var mirror = wrap.querySelector('.send-input-mirror');
  if (!mirror) return;
  var ss = typeof getSendState === 'function' ? getSendState() : null;
  var isHex = ss && ss.sendFormat === 'hex';
  if (isHex) _renderSendMirrorHex(ta.value, mirror);
  else _renderSendMirror(ta.value, mirror);
  mirror.scrollTop = ta.scrollTop;
  mirror.scrollLeft = ta.scrollLeft;
}

function _applyDisplayStyle() {
  var el = document.getElementById('display-font-style');
  if (!el) {
    el = document.createElement('style');
    el.id = 'display-font-style';
    document.head.appendChild(el);
  }
  el.textContent = '.display-content,#sendInput,#sendMirror{font-family:"' + state.displayFontFamily + '",monospace;font-size:' + state.displayFontSize + 'px;tab-size:' + state.tabSize + ';}';
  if (_statusBar) _statusBar.updateTabSize({tabSize: state.tabSize});
}

function updateEncodingDisplay() {
  if (_statusBar) _statusBar.updateEncoding({encoding: state.encoding});
  ['ascii','gb2312','utf-8'].forEach(e => {
    const btn = document.getElementById('menuEnc' + (e === 'utf-8' ? 'Utf8' : e.charAt(0).toUpperCase() + e.slice(1)));
    if (!btn) return;
    const chk = btn.querySelector('.chk');
    if (chk) chk.classList.toggle('on', state.encoding === e);
  });
}

async function setEncoding(enc) {
  if (state.encoding === enc) return;
  state.encoding = enc;
  updateEncodingDisplay();
  saveSettings();
  // Notify Go: update all tab decode workers
  if (state.daemonOnline) {
    state.tabs.forEach(function(t) {
      if (t.sessionId) window.go.main.App.SetTabEncoding(t.sessionId, enc);
    });
  }
  // Re-decode cached history entries with new encoding
  for (var key in state.historyCache) {
    if (!state.historyCache.hasOwnProperty(key) || key === '_id') continue;
    var entries = state.historyCache[key];
    var hexes = [];
    for (var i = 0; i < entries.length; i++) {
      if (entries[i].hex) hexes.push(entries[i].hex);
    }
    if (hexes.length > 0) {
      try {
        var results = await window.go.main.App.BatchDecodeForTab(hexes, enc);
        for (var j = 0; j < entries.length && j < results.length; j++) {
          if (entries[j].hex) entries[j].segments = results[j];
        }
      } catch (e) {}
    }
  }
  // Re-render active tab
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
}

// ---- alert helper ----
var _alertCallback = null;

// okKey / cancelKey: optional i18n keys for context-specific button labels
function showAlert(title, message, okKey) {
  _alertCallback = null;
  document.getElementById('alertTitle').textContent = title;
  document.getElementById('alertBody').textContent = message;
  document.getElementById('alertCancelBtn').classList.add('is-hidden');
  document.getElementById('alertOkBtn').textContent = t(okKey || 'confirm.ok', '确定');
  document.getElementById('alertOverlay').classList.remove('is-hidden');
}

function showConfirm(title, message, onOk, okKey, cancelKey) {
  _alertCallback = onOk;
  document.getElementById('alertTitle').textContent = title;
  document.getElementById('alertBody').textContent = message;
  document.getElementById('alertCancelBtn').classList.remove('is-hidden');
  document.getElementById('alertOkBtn').textContent = t(okKey || 'confirm.ok', '确定');
  document.getElementById('alertCancelBtn').textContent = t(cancelKey || 'confirm.cancel', '取消');
  document.getElementById('alertOverlay').classList.remove('is-hidden');
}

function closeAlert(ok) {
  document.getElementById('alertOverlay').classList.add('is-hidden');
  if (ok && _alertCallback) _alertCallback();
  _alertCallback = null;
}

// ---- theme ----
// ── Theme: single-axis colorThemeId model ──
// ── Color theme: one file = one theme card, mode buttons per _modes ──

function toggleThemePopup() {
  openSettingsPage();
  // Switch to appearance nav after render
  setTimeout(function() {
    var navItem = document.querySelector('.settings-nav-item[data-nav="appearance"]');
    if (navItem) navItem.click();
  }, 100);
}

function closeThemePopup() {
  // No-op — theme popup replaced by settings page
}

var _colorSchemeQuery = window.matchMedia('(prefers-color-scheme: dark)');
_colorSchemeQuery.addEventListener('change', function() {
  if (state.colorThemeMode === 'auto') applyColorTheme();
});

function clearThemeVars() {
  var root = document.documentElement;
  var known = ['--accent','--green','--red','--yellow','--fg','--bg','--bg2','--bg3','--border','--input-bg','--hover','--placeholder','--scrollbar-thumb',
    '--fwd-p1','--fwd-p2','--fwd-p1-hover','--fwd-p2-hover','--hex-escape','--badge-cli','--status-offline','--tab-close'];
  for (var i = 0; i < known.length; i++) root.style.removeProperty(known[i]);
}

function applyThemeVars(vars) {
  var root = document.documentElement;
  for (var k in vars) root.style.setProperty(k, vars[k]);
}

// Extract display name from raw _name (string or {zh:"",en:"",_:""})
function _themeName(raw) {
  if (!raw) return '';
  if (typeof raw === 'string') return raw;
  if (typeof raw === 'object') {
    var lang = state.language || 'zh';
    // 1. Exact match for current language
    if (raw[lang]) return raw[lang];
    // 2. Short form match (e.g. "zh-Hant" → "zh")
    if (lang.indexOf('-') > 0 && raw[lang.split('-')[0]]) return raw[lang.split('-')[0]];
    // 3. Try the i18n fallback language (set by loadI18n for external languages)
    if (window._i18nFallback && raw[window._i18nFallback]) return raw[window._i18nFallback];
    // 4. Theme's own default
    if (raw._) return raw._;
    // 5. Pick first available
    for (var k in raw) { if (k !== '_') return raw[k]; }
  }
  return '';
}

// Called by i18n setLanguage() to refresh theme display names
function _refreshThemeNames() {
  for (var i = 0; i < state.colorThemes.length; i++) {
    var ct = state.colorThemes[i];
    if (ct._nameRaw) ct.name = _themeName(ct._nameRaw);
  }
  // Refresh icon theme built-in name
  for (var j = 0; j < state.iconThemes.length; j++) {
    var it = state.iconThemes[j];
    if (it._i18nKey) it.name = t(it._i18nKey, it.name || '');
  }
  // Also refresh the popup if open
  var ov = document.getElementById('themeOverlay');
  if (ov && !ov.classList.contains('is-hidden')) buildThemePopup();
}

function _findColorTheme(id) {
  for (var i = 0; i < state.colorThemes.length; i++) {
    if (state.colorThemes[i].id === id) return state.colorThemes[i];
  }
  return null;
}

function _effectiveMode() {
  var mode;
  if (state.colorThemeMode === 'auto') {
    mode = _colorSchemeQuery.matches ? 'dark' : 'light';
  } else {
    mode = state.colorThemeMode || 'dark';
  }
  // Fallback: if current theme doesn't support resolved mode, use first supported mode
  var t = _findColorTheme(state.colorThemeId);
  if (t && t.modes && t.modes.length && t.modes.indexOf(mode) === -1) {
    mode = t.modes[0];
  }
  return mode;
}

async function applyColorTheme() {
  var root = document.documentElement;
  var id = state.colorThemeId;
  var mode = _effectiveMode();
  root.setAttribute('data-theme', mode);
  clearThemeVars();
  try {
    var colors = {};
    function _mergeTheme(raw) {
      if (!raw) return {};
      return Object.assign({}, raw.common || {}, raw[mode] || {});
    }
    // external first, then built-in
    var ext = await window.go.main.App.GetExternalColorTheme(id);
    if (ext && (ext.common || ext[mode])) {
      var fb = ext._fallback || 'system';
      if (fb && fb !== '_none') {
        var base = await window.go.main.App.GetTheme(fb);
        colors = Object.assign(_mergeTheme(base), _mergeTheme(ext));
      } else {
        colors = _mergeTheme(ext);
      }
    } else {
      var builtin = await window.go.main.App.GetTheme(id);
      colors = _mergeTheme(builtin);
    }
    if (Object.keys(colors).length) applyThemeVars(colors);
  } catch(e) {}
}

async function initColorThemes() {
  try {
    var builtin = await window.go.main.App.ListThemes();
    var external = await window.go.main.App.ListExternalColorThemes();
    var all = [];
    // System theme
    if (builtin) {
      for (var i = 0; i < builtin.length; i++) {
        var b = builtin[i];
        all.push({ id: b.id, _nameRaw: b._name, name: _themeName(b._name), modes: (b._modes && b._modes.length) ? b._modes : ['dark'], builtin: true, _isSystem: b.id === 'system' });
      }
    }
    // External themes
    if (external) {
      for (var j = 0; j < external.length; j++) {
        var ext = external[j];
        all.push({ id: ext.id, _nameRaw: ext.name, name: _themeName(ext.name), modes: ext.modes || ['dark'], builtin: false });
      }
    }
    state.colorThemes = all;
    if (!_findColorTheme(state.colorThemeId)) { state.colorThemeId = 'system'; state.colorThemeMode = 'auto'; }
    await applyColorTheme();
  } catch(e) {}
}

function setColorTheme(id, mode) {
  state.colorThemeId = id;
  state.colorThemeMode = mode || 'auto';
  applyColorTheme().then(function() {
    saveSettings();
    buildThemePopup();
  });
}

// ── Icon theme (SVG icons, built-in + external) ──

var _iconThemesAll = [];

async function initIconThemes() {
  try {
    var external = await window.go.main.App.ListExternalIconThemes();
    var all = [];
    // built-in default is always available — i18n key for dynamic translation
    all.push({ id: '', name: '', _i18nKey: 'theme.icons_default', builtin: true });
    if (external) {
      for (var j = 0; j < external.length; j++) {
        all.push({ id: external[j].id, name: external[j].name, builtin: false });
      }
    }
    _iconThemesAll = all;
    state.iconThemes = all;
    var saved = state.iconThemeId;
    if (saved) {
      var ext = await window.go.main.App.GetExternalIconTheme(saved);
      if (ext && ext.icons) {
        applyIconTheme(ext);
        state.iconThemeId = saved;
            return;
      }
    }
    // Default: built-in icons
    applyIconTheme(null);
    state.iconThemeId = '';
  } catch(e) {}
}

function setIconTheme(id) {
  if (!id) {
    applyIconTheme(null);
    state.iconThemeId = '';
    saveSettings();
    _populateIcons();
    return;
  }
  window.go.main.App.GetExternalIconTheme(id).then(function(ext) {
    if (ext && ext.icons) {
      applyIconTheme(ext);
      state.iconThemeId = id;
      saveSettings();
        _populateIcons();
    }
  }).catch(function() {});
}

// ── Theme popup ──

function _safeT(key, fb) { try { return (typeof t === 'function') ? t(key, fb) : fb; } catch(e) { return fb; } }

function buildThemePopup() {
  var list = document.getElementById('themeCardList');
  if (!list) return;
  list.innerHTML = '';
  if (!state.colorThemes || !state.colorThemes.length) {
    list.innerHTML = '<div style="color:var(--placeholder);font-size:13px;padding:8px 0">' + _safeT('theme.empty','暂无主题，请检查 themes/colors/ 目录') + '</div>';
    return;
  }
  for (var i = 0; i < state.colorThemes.length; i++) {
    (function(idx) {
      var t = state.colorThemes[idx];
      // Card wrapper
      var card = document.createElement('div');
      card.className = 't-theme-card';
      var isActive = (t.id === state.colorThemeId);
      if (isActive) card.classList.add('is-active');

      // Header: swatch + name
      var hdr = document.createElement('div');
      hdr.className = 't-card-hdr';
      var bg = _guessSwatch(t);
      hdr.innerHTML = '<span class="t-theme-swatch" style="background:' + bg + '"></span><span class="t-card-name">' + escHtml(t.name) + '</span>';
      card.appendChild(hdr);

      // Mode buttons bar
      var bar = document.createElement('div');
      bar.className = 't-mode-bar';
      var modes = t.modes || ['dark'];
      var modeLabels = { auto: _safeT('theme.auto_label','自动'), dark: _safeT('theme.dark_label','深色'), light: _safeT('theme.light_label','浅色') };
      var btnModes = (modes.length >= 2) ? ['auto','dark','light'] : modes;
      for (var m = 0; m < btnModes.length; m++) {
        (function(mode) {
          var btn = document.createElement('button');
          btn.className = 't-mode-btn';
          btn.textContent = modeLabels[mode] || mode;
          if (isActive && state.colorThemeMode === mode) btn.classList.add('is-active');
          btn.onclick = function(e) { e.stopPropagation(); setColorTheme(t.id, mode); };
          bar.appendChild(btn);
        })(btnModes[m]);
      }
      card.appendChild(bar);
      // Clicking the card itself toggles selection too
      card.onclick = function() { setColorTheme(t.id, modes.length >= 2 ? 'auto' : modes[0]); };
      list.appendChild(card);
    })(i);
  }

  // icon theme grid
  var iGrid = document.getElementById('iconThemeGrid');
  if (iGrid) {
    iGrid.innerHTML = '';
    for (var j = 0; j < state.iconThemes.length; j++) {
      (function(idx) {
        var ic = state.iconThemes[idx];
        var card = document.createElement('div');
        card.className = 't-theme-card' + (ic.id === state.iconThemeId ? ' is-active' : '');
        card.innerHTML = '<span>' + escHtml(ic.name) + '</span>';
        card.onclick = function() { setIconTheme(ic.id); buildThemePopup(); };
        iGrid.appendChild(card);
      })(j);
    }
  }
}

function _guessSwatch(t) {
  if (!t) return 'var(--accent)';
  if (t._isSystem) return 'linear-gradient(90deg, #333 50%, #f3f3f3 50%)';
  return 'var(--accent)';
}

function escHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// Default display colors — key → { dark, light, bgDark, bgLight, bold }
function _defaultDisplayColors() {
  var T = 'transparent';
  return {
    tsRx:    { dark: '#16c60c', light: '#059669', bgDark: T, bgLight: T, bold: false, italic: false },
    tsTx:    { dark: '#4cc2ff', light: '#2563eb', bgDark: T, bgLight: T, bold: false, italic: false },
    ts1:     { dark: '#60a5fa', light: '#2563eb', bgDark: T, bgLight: T, bold: false, italic: false },
    ts2:     { dark: '#f59e0b', light: '#d97706', bgDark: T, bgLight: T, bold: false, italic: false },
    textRx:  { dark: '#ffffff', light: '#000000', bgDark: T, bgLight: T, bold: false, italic: false },
    textTx:  { dark: '#ffffff', light: '#000000', bgDark: T, bgLight: T, bold: false, italic: false },
    text1:   { dark: '#ffffff', light: '#000000', bgDark: T, bgLight: T, bold: false, italic: false },
    text2:   { dark: '#ffffff', light: '#000000', bgDark: T, bgLight: T, bold: false, italic: false },
    hexRx:   { dark: '#f59e0b', light: '#d97706',  bgDark: T, bgLight: T, bold: false, italic: false },
    hexTx:   { dark: '#f59e0b', light: '#d97706',  bgDark: T, bgLight: T, bold: false, italic: false },
    hex1:    { dark: '#f59e0b', light: '#d97706',  bgDark: T, bgLight: T, bold: false, italic: false },
    hex2:    { dark: '#f59e0b', light: '#d97706',  bgDark: T, bgLight: T, bold: false, italic: false },
    hexEsc:  { dark: '#e91e63', light: '#c2185b', bgDark: T, bgLight: T, bold: false, italic: false },
    hexInv:  { dark: '#ff5c5c', light: '#dc2626', bgDark: T, bgLight: T, bold: false, italic: false },
    ctrlBg:  { dark: T, light: T, bgDark: T, bgLight: T, bold: false, italic: false },
    ctrlFg:  { dark: '#cccccc', light: '#bbbbbb', bgDark: T, bgLight: T, bold: false, italic: false },
    stat:    { dark: '#888888', light: '#999999', bgDark: T, bgLight: T, bold: false, italic: false },
    fhexEsc: { dark: '#e91e63', light: '#c2185b', bgDark: T, bgLight: T, bold: false, italic: false },
    fctrlBg: { dark: T, light: T, bgDark: T, bgLight: T, bold: false, italic: false },
    fctrlFg: { dark: '#cccccc', light: '#bbbbbb', bgDark: T, bgLight: T, bold: false, italic: false },
    fstat:   { dark: '#888888', light: '#999999', bgDark: T, bgLight: T, bold: false, italic: false }
  };
}

function applyI18n() {
  // Menu labels: 0=文件, 1=设置, 2=工具, 3=守护进程, 4=帮助, 5=语言
  var mls = document.querySelectorAll('.menu-label');
  if (mls.length >= 6) {
    mls[0].textContent = t('menu.file', '文件');
    mls[1].textContent = t('menu.settings', '设置');
    mls[2].textContent = t('menu.tools', '工具');
    mls[3].textContent = t('menu.daemon', '守护进程');
    mls[4].textContent = t('menu.help', '帮助');
    mls[5].textContent = t('menu.language', '语言');
  }
  // Submenu triggers (encoding, hex display, text display)
  // Submenu triggers (encoding, hex display, text display)
  document.querySelectorAll('.menu-sub-trigger .menu-text').forEach(function(el, i) {
    var keys = ['menu.encoding', 'menu.hex_display', 'menu.text_display'];
    var fbs  = ['编码格式', 'Hex显示设置', '文本显示设置'];
    if (i < 3) el.textContent = t(keys[i], fbs[i]);
  });
  // Status bar
  if (_statusBar) _statusBar.updateDaemon({online: state.daemonOnline, tabsCount: state.tabs.length});
  // Daemon toggle (menu - full text)
  var mt = document.getElementById('menuToggleDaemon');
  if (mt) { var mts = mt.querySelector('.menu-text'); if (mts) mts.textContent = state.daemonOnline ? t('menu.stop_daemon', '停止守护进程') : t('menu.start_daemon', '启动守护进程'); }
  // Menu button texts (all buttons inside menu dropdowns)
  var _menuI18n = {
    menuThemeSettings:    ['menu.theme_settings',  '主题设置'],
    menuAppSettings:      ['menu.app_settings',    '软件设置'],
    menuNewSession:       ['menu.new_session',     '新建会话'],
    menuCloseSession:     ['menu.close_session',   '关闭会话'],
    menuQuit:             ['menu.quit',            '退出'],
    menuAutoSaveHistory:  ['menu.auto_save_history', '自动保存历史'],
    menuAutoCreateSession:['menu.auto_create_session', '启动时自动创建会话'],
    menuHexPrefix:        ['menu.hex_prefix',      '0x 前缀'],
    menuHexLower:         ['menu.hex_lower',       '小写显示'],
    menuHexSepN:          ['menu.hex_sep_none',    '无分割'],
    menuHexSepS:          ['menu.hex_sep_space',   '空格分隔'],
    menuHexSepC:          ['menu.hex_sep_comma',   '逗号分隔'],
    menuCrVisible:        ['menu.visible_cr',      '可见控制字符'],
    menuHexEscShow:       ['menu.hex_escape_show', '显示转义字节'],
    menuHexEscHide:       ['menu.hex_escape_hide', '隐藏转义字节'],
    menuHexEscRaw:        ['menu.hex_escape_raw',  '编码器原始输出'],
    menuQuickPanel:       ['menu.quick_panel',     '多字符串'],
    menuRefreshPorts:     ['menu.refresh_ports',   '刷新串口列表'],
    menuViewThreads:      ['menu.view_threads',    '查看线程'],
    menuViewClients:      ['menu.view_clients',    '查看客户端'],
    menuDevTools:         ['menu.dev_tools',       '开发人员工具'],
    menuAbout:            ['menu.about',           '关于'],
  };
  for (var _mid in _menuI18n) {
    var _el = document.getElementById(_mid);
    if (_el) { var _ts = _el.querySelector('.menu-text'); if (_ts) _ts.textContent = t(_menuI18n[_mid][0], _menuI18n[_mid][1]); }
  }
  // Offline overlay
  var ov = document.getElementById('offlineOverlay');
  if (ov) {
    var om = ov.querySelector('.o-overlay__title'); if (om) om.textContent = t('offline.title', '守护进程未启用');
    var oh = ov.querySelector('.o-overlay__desc'); if (oh) oh.textContent = t('offline.hint', '请点击菜单 守护进程 → 启停');
  }
  // Settings button
  var bs = pageEl('btnSettings');
  if (bs) bs.title = t('btn.settings', '详细设置');
  // Mode toggle
  var bm = pageEl('btnModeToggle');
  if (bm) bm.textContent = (activeMode() === 'forward') ? ('↔ ' + t('btn.forward', '端口转发')) : ('⇄ ' + t('btn.single', '单端口'));
  // Display toggle
  var bd = pageEl('btnToggleDisplay');
  if (bd) {
    bd.textContent = (state.displayMode === 'hex') ? ('⇃ ' + t('display.rx_hex_short', 'H')) : ('⇃ ' + t('display.rx_text_short', '字'));
    bd.title = (state.displayMode === 'hex') ? t('display.switch_to_text', '点击切换至文本显示') : t('display.switch_to_hex', '点击切换至Hex显示');
  }
  // Send display toggle
  var bst = pageEl('btnToggleSend');
  if (bst) {
    bst.textContent = (state.txDisplayMode === 'hex') ? ('↾ ' + t('display.tx_hex_short', 'H')) : ('↾ ' + t('display.tx_text_short', '字'));
    bst.title = (state.txDisplayMode === 'hex') ? t('display.switch_to_text', '点击切换至文本显示') : t('display.switch_to_hex', '点击切换至Hex显示');
  }
  // Send format button
  updateSendQuickBtn();
  // Open port button
  updateOpenBtn();
  // Clear button
  var bc = pageEl('btnClearFloat');
  if (bc) bc.textContent = t('btn.clear', '清空');
  // Send input placeholder
  var si = pageEl('sendInput');
  if (si) si.placeholder = t('send.placeholder', '输入要发送的数据...');
  // P1/P2 display buttons
  var bp1 = pageEl('btnP1Display');
  if (bp1) bp1.textContent = (state.p1DisplayMode === 'hex') ? 'P1 H' : 'P1 文';
  var bp2 = pageEl('btnP2Display');
  if (bp2) bp2.textContent = (state.p2DisplayMode === 'hex') ? 'P2 H' : 'P2 文';
  // Scroll lock buttons
  updateScrollLockBtns();
  // Theme button title
  var tbtn = document.getElementById('btnTheme');
  if (tbtn) tbtn.title = t('tooltip.theme', '主题设置');
  // Encoding status
  updateEncodingDisplay();
  // All [data-i18n] elements
  document.querySelectorAll('[data-i18n]').forEach(function(el) {
    var key = el.getAttribute('data-i18n');
    el.textContent = t(key, el.textContent);
  });
  // All [data-i18n-title] elements
  document.querySelectorAll('[data-i18n-title]').forEach(function(el) {
    var key = el.getAttribute('data-i18n-title');
    el.title = t(key, el.title);
  });
  // Restore toggle button active states after textContent reset — always call to refresh i18n text
  _setQpCycleBtn(!!state._qpCycleActive);
  var tab = getActiveTab();
  _setAutoSendBtn(!!(tab && tab._autoSendActive));
  // Rebuild tab labels for language switch
  for (var ti = 0; ti < state.tabs.length; ti++) {
    _rebuildTabLabel(state.tabs[ti]);
  }
  renderTabs();
  // Re-render multi-string panel for language switch
  quickPanelRender();
  // Daemon menu buttons
  var _set = function(id,key,fb) { var e=document.getElementById(id); if(!e)return; var s=e.querySelector('.menu-text'); if(s)s.textContent=t(key,fb); else e.textContent=t(key,fb); };
  // menuToggleDaemon is state-dependent, handled above
  _set('menuRefreshPorts','menu.refresh_ports','刷新串口列表');
  _set('menuViewThreads','menu.view_threads','查看线程');
  _set('menuViewClients','menu.view_clients','查看客户端');
  _set('menuDevTools','menu.dev_tools','开发人员工具');
  _set('menuAbout','menu.about','关于');
  _set('btnSend','btn.send','发送');
  var sb = document.getElementById('btnSend');
  if (sb) sb.innerHTML = icon('send', 14, 14) + ' ' + sb.textContent;
  // Quick panel title
  var qpt = document.querySelector('.qp-title-text');
  if (qpt) qpt.textContent = t('quick_panel.title', '多字符串发送');
  // File menu items
  _set('menuNewSession','menu.new_session','新建会话');
  _set('menuCloseSession','menu.close_session','关闭会话');
  _set('menuQuit','menu.quit','退出');
  _set('menuAutoSaveHistory','menu.auto_save_history','自动保存历史');
  _set('menuAutoCreateSession','menu.auto_create_session','启动时自动创建会话');
  // Empty state
  var em = document.getElementById('emptyMsg');
  if (em) em.textContent = t('empty.no_session', '尚无串口会话');
  var es = document.getElementById('emptySub');
  if (es) es.textContent = t('empty.no_session_hint', '请点击 + 按钮或菜单 文件 → 新建会话 来开始');
  var eb = document.getElementById('emptyBtn');
  if (eb) eb.textContent = '+ ' + t('menu.new_session', '新建会话');
  // Hex/Text display submenu items (preserve <span class="chk">)
  var _setChk = function(id,key,fb){
    var e=document.getElementById(id); if(!e)return;
    var s=e.querySelector('.menu-text');
    if(s){ s.textContent=t(key,fb); return; }
    // Fallback: try nextSibling of .chk span
    var chk=e.querySelector('.chk');
    if(chk&&chk.nextSibling&&chk.nextSibling.nodeType===3){
      chk.nextSibling.textContent=t(key,fb); return;
    }
    var n=e.lastChild;
    while(n&&n.nodeType!==3)n=n.previousSibling;
    if(n)n.textContent=t(key,fb);
  };
  _setChk('menuQuickPanel','menu.quick_panel','多字符串');
  _setChk('menuHexPrefix','menu.hex_prefix','0x 前缀');
  _setChk('menuHexLower','menu.hex_lower','小写显示');
  _setChk('menuHexSepN','menu.hex_sep_none','无分割');
  _setChk('menuHexSepS','menu.hex_sep_space','空格分割');
  _setChk('menuHexSepC','menu.hex_sep_comma','逗号分割');
  _setChk('menuCrVisible','menu.visible_cr','可见控制字符');
  _setChk('menuHexEscShow','menu.hex_escape_show','显示转义字节');
  _setChk('menuHexEscHide','menu.hex_escape_hide','隐藏转义字节');
  _setChk('menuHexEscRaw','menu.hex_escape_raw','编码器原始输出');
  // Alert buttons
  _set('alertOkBtn','confirm.ok','确定');
  _set('alertCancelBtn','confirm.cancel','取消');
  // Port selects placeholder
  _refreshPortPlaceholders();
  // Tabs
  renderTabs();
  // Stats
  refreshStatsDOM();
  setStatArrows(activeMode() === 'forward');
  // Sync hex/text display menu checkmarks
  updateHexMenu();
  updateTextMenu();
  // Update toggle button width sizers
  updateToggleSizers();
  // Retranslate visible history system messages in-place (no cache rebuild)
  refreshSysMsgI18n();
}

function updateToggleSizers() {
  var btns = [
    { id: 'btnModeToggle', a: '⇄ ' + t('btn.single','单端口'), b: '↔ ' + t('btn.forward','端口转发') },
    { id: 'btnToggleDisplay', a: '⇃ ' + t('display.rx_text_short','字'), b: '⇃ ' + t('display.rx_hex_short','H') },
    { id: 'btnToggleSend', a: '↾ ' + t('display.tx_text_short','字'), b: '↾ ' + t('display.tx_hex_short','H') },
    { id: 'btnSendQuick', a: t('send.tx_text_short','字'), b: t('send.tx_hex_short','H') },
    { id: 'btnScrollLock', a: t('btn.lock_scroll','自动滚动'), b: t('btn.unlock_scroll','锁定滚动') },

    { id: 'btnToggleDaemon', a: t('btn.start_daemon','启动'), b: t('btn.stop_daemon','停止') },
    { id: 'btnP1Display', a: 'P1 文', b: 'P1 H' },
    { id: 'btnP2Display', a: 'P2 文', b: 'P2 H' },
    { id: 'btnOpen', a: t('serial.open','打开串口'), b: t('serial.close','关闭串口') },
    { id: 'autoSend', a: t('send.auto_send','自动发送'), b: t('send.auto_stop','停止循环') },
    { id: 'qpBtnSendEnabled', a: t('quick_panel.send_enabled','发送启用'), b: t('quick_panel.send_enabled','发送启用') },
    { id: 'qpCycleSend', a: t('quick_panel.cycle_send','循环发送'), b: t('quick_panel.cycle_stop','停止循环') },
    { id: 'btnClearFloat', a: t('btn.clear','清空'), b: t('btn.clear','清空') },
    { id: 'btnSend', a: t('btn.send','发送'), b: t('btn.send','发送') },
    { id: 'btnQuickAdd', a: t('quick_panel.add','+ 添加'), b: t('quick_panel.add','+ 添加') }
  ];
  btns.forEach(function(b) {
    var el = pageEl(b.id);
    if (!el) return;
    el.setAttribute('data-wide', b.a.length >= b.b.length ? b.a : b.b);
  });
}

function _refreshPortPlaceholders() {
  var sel = pageEl('portSelect');
  if (sel && sel.options[0] && sel.options[0].value === '') sel.options[0].textContent = t('status.select_port', '-- 选择端口 --');
  var sa = pageEl('portSelectA');
  if (sa && sa.options[0] && sa.options[0].value === '') sa.options[0].textContent = t('status.port1', '-- 端口1 --');
  var sb = pageEl('portSelectB');
  if (sb && sb.options[0] && sb.options[0].value === '') sb.options[0].textContent = t('status.port2', '-- 端口2 --');
}

function setSelect(id, val) {
  const el = document.getElementById(id);
  if (!el || val === undefined || val === null) return;
  const str = String(val);
  if (el.tagName === 'INPUT') { el.value = str; return; }
  for (let i = 0; i < el.options.length; i++) {
    if (el.options[i].value === str) { el.value = str; return; }
  }
}

// ---- mode toggle ----

function activeMode() {
  const tab = getActiveTab();
  return (tab && tab.mode) ? tab.mode : state.currentMode;
}


function updateFwdBtns() {
  const b1 = pageEl('btnP1Display');
  const b2 = pageEl('btnP2Display');
  if (b1) {
    if (state.p1DisplayMode === 'hex') {
      b1.textContent = 'P1 H';
      b1.title = t('display.switch_to_text', '点击切换至文本显示');
      b1.classList.add('btn-hex');
    } else {
      b1.textContent = 'P1 文';
      b1.title = t('display.switch_to_hex', '点击切换至Hex显示');
      b1.classList.remove('btn-hex');
    }
  }
  if (b2) {
    if (state.p2DisplayMode === 'hex') {
      b2.textContent = 'P2 H';
      b2.title = t('display.switch_to_text', '点击切换至文本显示');
      b2.classList.add('btn-hex');
    } else {
      b2.textContent = 'P2 文';
      b2.title = t('display.switch_to_hex', '点击切换至Hex显示');
      b2.classList.remove('btn-hex');
    }
  }
}

async function toggleMode() {
  const tab = getActiveTab();
  if (!tab || !tab.sessionId) return;

  if (tab.portOpen) {
    showAlert(t('confirm.title','提示'), t('serial.close_first','请先关闭所有串口连接再切换模式'));
    return;
  }

  const newMode = tab.mode === 'forward' ? 'single' : 'forward';

  if (!state.daemonOnline) {
    tab.mode = newMode;
    state.currentMode = newMode;
    updateModeUI();
    return;
  }

  try {
    await window.go.main.App.SetProcessMode(tab.sessionId, newMode);
    tab.mode = newMode;
    state.currentMode = newMode;
    tab.label = newMode === 'forward' ? t('tab.idle_forward', '端口转发空闲') : t('tab.idle_single', '单端口空闲');
    updateModeUI();
    updateOpenBtn();
    renderTabs();
  } catch (e) {
    showAlert(t('cli.error','错误'), t('serial.mode_fail','切换模式失败: ') + e);
  }
}

function updateModeUI() {
  // Settings tab has no port UI — bail out early
  var activeTab = getActiveTab();
  if (activeTab && activeTab.type === 'settings') return;

  const btn = pageEl('btnModeToggle');
  const singleRow = pageEl('singlePortRow');
  const forwardRow = pageEl('forwardPortRow');
  const sendArea = pageEl('sendArea');
  const divider = pageEl('divider');
  const settingsSingle = document.getElementById('settingsSingle');
  const settingsForward = document.getElementById('settingsForward');
  const fwdBtns = pageEl('fwdFloatBtns');
  const displayArea = pageEl('displayArea');
  const mode = activeMode();

  // Float bar button visibility — toggle is-hidden based on mode markers
  var singleMarkers = fwdBtns ? fwdBtns.querySelectorAll('.is-hidden-single') : [];
  var fwdMarkers = fwdBtns ? fwdBtns.querySelectorAll('.is-hidden-fwd') : [];

  if (mode === 'forward') {
    if (btn) { btn.textContent = '↔ ' + t('btn.forward', '端口转发'); btn.classList.add('is-forward'); }
    if (singleRow) singleRow.classList.add('is-hidden');
    if (forwardRow) forwardRow.classList.remove('is-hidden');
    if (sendArea) sendArea.classList.add('is-hidden');
    if (divider) { divider.classList.add('is-hidden'); divider.style.display = 'none'; }
    var qd = pageEl('quickDivider');
    var qp = pageEl('quickPanel');
    if (qd) { qd.classList.add('is-hidden'); qd.style.display = 'none'; }
    if (qp) { qp.classList.add('is-hidden'); qp.style.display = 'none'; }
    if (settingsSingle) settingsSingle.classList.add('is-hidden');
    if (settingsForward) settingsForward.classList.remove('is-hidden');
    if (displayArea) displayArea.classList.add('c-fwd-display');
    // Show P1/P2 buttons, hide TX/send buttons
    singleMarkers.forEach(function(b) { b.classList.remove('is-hidden'); });
    fwdMarkers.forEach(function(b) { b.classList.add('is-hidden'); });
  } else {
    if (btn) { btn.textContent = '⇄ ' + t('btn.single', '单端口'); btn.classList.remove('is-forward'); }
    if (singleRow) singleRow.classList.remove('is-hidden');
    if (forwardRow) forwardRow.classList.add('is-hidden');
    if (sendArea) sendArea.classList.remove('is-hidden');
    if (divider) { divider.classList.remove('is-hidden'); divider.style.display = ''; }
    var qd = pageEl('quickDivider');
    var qp = pageEl('quickPanel');
    if (qd) { qd.classList.remove('is-hidden'); qd.style.display = ''; }
    if (qp) { qp.classList.remove('is-hidden'); qp.style.display = ''; }
    if (settingsSingle) settingsSingle.classList.remove('is-hidden');
    if (settingsForward) settingsForward.classList.add('is-hidden');
    if (displayArea) displayArea.classList.remove('c-fwd-display');
    // Show TX/send buttons, hide P1/P2 buttons
    singleMarkers.forEach(function(b) { b.classList.add('is-hidden'); });
    fwdMarkers.forEach(function(b) { b.classList.remove('is-hidden'); });
  }
  updateFwdBtns();
  updatePortOccupied();
  _syncPortSelectsFromTab();
  applySendRatio();
  applyQuickPanelRatio();
  loadTabHistory();
  refreshStatsDOM();
}

function saveSendUI() {
  const ss = getSendState();
  ss.sendTextBuf = pageEl('sendInput').value;
  // hex buf is only saved when in hex mode
  if (ss.sendFormat === 'hex') ss.sendHexBuf = pageEl('sendInput').value;
  const appendSel = pageEl('appendSelect');
  ss.appendSuffix = appendSel ? appendSel.value : 'crlf';
  ss.autoSend = !!(getActiveTab() && getActiveTab()._autoSendActive);
  ss.sendInterval = parseInt(pageEl('sendInterval').value) || 1000;
}

function restoreSendUI() {
  const ss = getSendState();
  pageEl('sendInput').value = ss.sendFormat === 'hex' ? ss.sendHexBuf : ss.sendTextBuf;
  _syncSendMirror(pageEl('sendInput'));
  const appendSel2 = pageEl('appendSelect');
  if (appendSel2 && ss.appendSuffix !== undefined) { appendSel2.value = ss.appendSuffix; var csWrap2 = appendSel2.closest('.cs-wrap'); if (csWrap2 && typeof _csRefresh === 'function') _csRefresh(csWrap2); }
    const tab = getActiveTab();
  _setAutoSendBtn(!!(tab && tab._autoSendActive));
  pageEl('sendInterval').value = ss.sendInterval;
  updateSendQuickBtn();
  updateSendInfo();
}

// ---- Custom Select ----
let _csOpen = null;
let _csPortalScroll = null;
let _csResizeObserver = null;

function _csPortalCleanup() {
  if (_csPortalScroll) { document.removeEventListener('scroll', _csPortalScroll, true); _csPortalScroll = null; }
  if (_csResizeObserver) { _csResizeObserver.disconnect(); _csResizeObserver = null; }
}

function _csRefresh(wrap) {
  var sel = wrap.querySelector('select');
  var dd = wrap.querySelector('.cs-dropdown');
  // Dropdown may have been portalled to body — look there too
  if (!dd) dd = document.querySelector('.cs-dropdown.is-portalled');
  var trigText = wrap.querySelector('.cs-text');
  if (!sel || !trigText) return;
  var html = '';
  for (var i = 0; i < sel.options.length; i++) {
    var opt = sel.options[i];
    var cls = 'cs-option';
    if (i === sel.selectedIndex) cls += ' selected';
    if (opt.className) cls += ' ' + opt.className;
    html += '<div class="' + cls + '" data-val="' + escHtml(opt.value) + '">' + escHtml(opt.textContent || opt.text) + '</div>';
  }
  dd.innerHTML = html;
  if (sel.selectedIndex >= 0) {
    trigText.textContent = sel.options[sel.selectedIndex].textContent || sel.options[sel.selectedIndex].text;
  }
  dd.querySelectorAll('.cs-option').forEach(function(optEl) {
    optEl.addEventListener('mousedown', function(e) {
      e.preventDefault();
      sel.value = optEl.dataset.val;
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      _csClose(wrap);
    });
  });
}

function _csOpenDropdown(wrap) {
  if (_csOpen && _csOpen !== wrap) _csClose(_csOpen);
  _csRefresh(wrap);
  var dd = wrap.querySelector('.cs-dropdown');
  if (!dd) return;
  // Portal dropdown to body to escape parent overflow:hidden clipping
  document.body.appendChild(dd);
  dd.classList.add('is-portalled');
  dd.style.display = 'block';
  var trig = wrap.querySelector('.cs-trigger');
  var rect = trig ? trig.getBoundingClientRect() : { left: 0, bottom: 0, width: 0 };
  dd.style.position = 'fixed';
  dd.style.left = rect.left + 'px';
  dd.style.top = (rect.bottom + 2) + 'px';
  dd.style.minWidth = rect.width + 'px';
  dd.style.maxWidth = Math.min(rect.width * 2, window.innerWidth - rect.left - 12) + 'px';
  // max-height: from trigger bottom to statusbar top (minus margin)
  var sb = document.querySelector('.statusbar');
  var sbTop = sb ? sb.getBoundingClientRect().top : window.innerHeight;
  var avail = sbTop - rect.bottom - 6;
  dd.style.maxHeight = Math.max(80, Math.floor(avail)) + 'px';
  dd.style.zIndex = '200';
  wrap.classList.add('is-open');
  _csOpen = wrap;
  // Close on scroll (capture phase to catch all scroll containers)
  _csPortalScroll = function() { _csClose(wrap); };
  document.addEventListener('scroll', _csPortalScroll, true);
  // Close on window resize
  _csResizeObserver = new ResizeObserver(function() {
    if (!_csOpen) return;
    var r2 = trig.getBoundingClientRect();
    dd.style.left = r2.left + 'px';
    dd.style.top = (r2.bottom + 2) + 'px';
    dd.style.minWidth = r2.width + 'px';
  });
  _csResizeObserver.observe(document.body);
  var selOpt = wrap.querySelector('.cs-option.is-selected');
  if (selOpt) selOpt.scrollIntoView({ block: 'nearest' });
}

function _csClose(wrap) {
  wrap.classList.remove('is-open');
  // Dropdown may be portalled to body — search there first
  var dd = document.querySelector('.cs-dropdown.is-portalled');
  if (!dd) dd = wrap.querySelector('.cs-dropdown');
  if (dd) {
    if (dd.classList.contains('is-portalled')) {
      dd.classList.remove('is-portalled');
      dd.style.display = '';
      dd.style.position = '';
      dd.style.left = '';
      dd.style.top = '';
      dd.style.minWidth = '';
      dd.style.maxWidth = '';
      dd.style.maxHeight = '';
      dd.style.zIndex = '';
      wrap.appendChild(dd);
    }
  }
  if (_csOpen === wrap) _csOpen = null;
  _csPortalCleanup();
}

function initCustomSelect(sel) {
  if (sel.closest('.cs-wrap')) return;
  var wrap = document.createElement('div');
  wrap.className = 'cs-wrap';
  // Copy semantic classes from <select> to wrapper for CSS targeting
  if (sel.classList.contains('port-sel')) wrap.classList.add('port-wrap');
  if (sel.classList.contains('baud-sel')) wrap.classList.add('baud-wrap');
  wrap.tabIndex = 0;
  sel.parentNode.insertBefore(wrap, sel);
  wrap.appendChild(sel);
  var trig = document.createElement('div');
  trig.className = 'cs-trigger';
  var textSpan = document.createElement('span');
  textSpan.className = 'cs-text';
  trig.appendChild(textSpan);
  var arrow = document.createElement('span');
  arrow.className = 'cs-arrow';
  arrow.innerHTML = icon('chevron_down', 14, 14);
  trig.appendChild(arrow);
  wrap.appendChild(trig);
  var dd = document.createElement('div');
  dd.className = 'cs-dropdown';
  wrap.appendChild(dd);

  var isBaud = sel.classList.contains('baud-sel');

  if (isBaud) {
    // Baud: click trigger → dropdown + input cursor, type to edit inline
    textSpan.contentEditable = 'true';
    trig.addEventListener('click', function(e) {
      e.stopPropagation();
      if (wrap.classList.contains('is-open')) { _csClose(wrap); return; }
      _csOpenDropdown(wrap);
      textSpan.focus();
    });
    textSpan.addEventListener('focus', function() {
      var rng = document.createRange();
      rng.selectNodeContents(textSpan);
      var s = window.getSelection();
      s.removeAllRanges();
      s.addRange(rng);
    });
    textSpan.addEventListener('blur', function() {
      // Commit typed value silently (no change event to avoid double-fire with dropdown click)
      var val = textSpan.textContent.trim();
      var num = parseInt(val, 10);
      var minV = parseInt(sel.getAttribute('data-min'));
      var maxV = parseInt(sel.getAttribute('data-max'));
      if (isNaN(minV)) minV = 1;
      if (!isNaN(num) && num >= minV && (isNaN(maxV) || num <= maxV)) {
        var found = false;
        for (var j = 0; j < sel.options.length; j++) {
          if (sel.options[j].value === String(num)) { found = true; break; }
        }
        if (!found) {
          var opt = document.createElement('option');
          opt.value = String(num);
          opt.textContent = String(num);
          sel.appendChild(opt);
        }
        sel.value = String(num);
      }
      _csRefresh(wrap);
    });
    textSpan.addEventListener('keydown', function(e) {
      if (e.key === 'Enter') { e.preventDefault(); textSpan.blur(); }
      if (e.key === 'Escape') { textSpan.textContent = sel.value; textSpan.blur(); }
    });
  } else {
    // Non-baud: click trigger to toggle dropdown
    trig.addEventListener('click', function(e) {
      e.stopPropagation();
      wrap.classList.contains('is-open') ? _csClose(wrap) : _csOpenDropdown(wrap);
    });
  }

  wrap.addEventListener('keydown', function(e) {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); _csOpenDropdown(wrap); }
    if (e.key === 'Escape') _csClose(wrap);
  });
  sel.addEventListener('change', function() { _csRefresh(wrap); });
  new MutationObserver(function() { _csRefresh(wrap); })
    .observe(sel, { childList: true, subtree: true, attributes: true });
  _csRefresh(wrap);
  return { refresh: function() { _csRefresh(wrap); } };
}

function initAllCustomSelects() {
  document.querySelectorAll('select').forEach(function(s) { initCustomSelect(s); });
}

document.addEventListener('click', function(e) {
  if (!_csOpen) return;
  // Check portalled dropdown or in-place dropdown
  var dd = _csOpen.querySelector('.cs-dropdown');
  if (_csOpen.contains(e.target)) return;
  if (dd && dd.contains(e.target)) return;
  _csClose(_csOpen);
});

// Copy transform: skip visual marker spans from clipboard, keep real characters
document.addEventListener('copy', function(e) {
  var sel = window.getSelection();
  if (!sel || !sel.rangeCount) return;
  var rng;
  try {
    rng = sel.getRangeAt(0);
    var dc = rng.startContainer;
    while (dc && dc !== document.body) {
      if (dc.classList && dc.classList.contains('display-content')) break;
      dc = dc.parentNode;
    }
    if (!dc || dc === document.body) return;
  } catch(_) { return; }

  // Control chars use child spans: .ctrl-mark (visual marker) + .ctrl-real (real char).
  // Strip markers before extracting text so only real characters enter the clipboard.
  var frag = rng.cloneContents();
  frag.querySelectorAll('.ctrl-mark').forEach(function(el) { el.remove(); });
  if (!state.copyHexEscapes) {
    var hex = frag.querySelectorAll('.hex-escape,.hex-fescape');
    for (var i = 0; i < hex.length; i++) hex[i].remove();
  }
  // Insert \n between block-level siblings to preserve timestamp / data line breaks
  frag.querySelectorAll('.data-line,.sys-msg').forEach(function(el) {
    el.parentNode.insertBefore(document.createTextNode('\n'), el);
  });
  var tmp = document.createElement('div');
  tmp.appendChild(frag);
  var t2 = tmp.textContent || '';

  if (t2 !== sel.toString()) {
    e.preventDefault();
    e.clipboardData.setData('text/plain', t2);
  }
});

// ---- Tab drag (stable handlers, bound once) ----
var _tabDragSide = 'left';

function _tabHideIndicator() {
  var ind = document.getElementById('tabDropIndicator');
  if (ind) ind.classList.remove('show');
  _tabDragSide = 'left';
}

function _tabDragOver(e) {
  e.preventDefault();
  var bar = document.getElementById('tabsBar');
  var scroll = document.getElementById('tabsScroll');
  if (!bar || !scroll) return;
  var tabs = scroll.querySelectorAll('.tab-btn:not(.tab-new)');
  if (tabs.length === 0) return;
  var gap = tabs.length;
  for (var j = 0; j < tabs.length; j++) {
    var r = tabs[j].getBoundingClientRect();
    if (e.clientX <= r.left) { gap = j; break; }
    if (e.clientX >= r.right) continue;
    var pct = (e.clientX - r.left) / r.width;
    if (pct < 0.4) { gap = j; _tabDragSide = 'left'; break; }
    if (pct > 0.6) { gap = j + 1; _tabDragSide = 'right'; break; }
    gap = _tabDragSide === 'right' ? j + 1 : j;
    break;
  }
  var ind = document.getElementById('tabDropIndicator');
  if (!ind) return;
  ind.dataset.gap = gap;
  var barRect = bar.getBoundingClientRect();
  var left;
  if (gap === 0) {
    left = tabs[0].getBoundingClientRect().left - barRect.left - 2;
  } else if (gap >= tabs.length) {
    left = tabs[tabs.length - 1].getBoundingClientRect().right - barRect.left + 2;
  } else {
    var prevR = tabs[gap - 1].getBoundingClientRect().right;
    var nextL = tabs[gap].getBoundingClientRect().left;
    left = (prevR + nextL) / 2 - barRect.left;
  }
  ind.style.left = left + 'px';
  ind.classList.add('show');
}

function _tabDragLeave(e) {
  var bar = document.getElementById('tabsBar');
  if (bar && !bar.contains(e.relatedTarget)) _tabHideIndicator();
}

function _tabDrop(e) {
  e.preventDefault();
  var ind = document.getElementById('tabDropIndicator');
  var gap = ind ? parseInt(ind.dataset.gap) : state.tabs.length;
  if (isNaN(gap)) gap = state.tabs.length;
  _tabHideIndicator();
  var from = parseInt(e.dataTransfer.getData('text/plain'));
  if (isNaN(from) || from === gap || from + 1 === gap) return;
  var moved = state.tabs.splice(from, 1)[0];
  var insertAt = (from < gap) ? gap - 1 : gap;
  state.tabs.splice(insertAt, 0, moved);
  if (state.activeTab === from) state.activeTab = insertAt;
  else if (from < state.activeTab && insertAt >= state.activeTab) state.activeTab--;
  else if (from > state.activeTab && insertAt <= state.activeTab) state.activeTab++;
  renderTabs();
}


// ---- overlay creation (built in JS, no static HTML) ----

function createOfflineOverlay() {
  var ov = document.createElement('div');
  ov.className = 'o-overlay is-hidden';
  ov.id = 'offlineOverlay';
  var title = document.createElement('div');
  title.className = 'o-overlay__title';
  title.setAttribute('data-i18n', 'status.daemon_offline');
  title.textContent = t('status.daemon_offline', '守护进程未启用');
  var desc = document.createElement('div');
  desc.className = 'o-overlay__desc';
  desc.setAttribute('data-i18n', 'status.daemon_offline_hint');
  desc.textContent = t('status.daemon_offline_hint', '请点击菜单 守护进程 → 启停');
  ov.appendChild(title);
  ov.appendChild(desc);
  document.getElementById('mainContent').appendChild(ov);
  return ov;
}

function createAlertOverlay() {
  var ov = document.createElement('div');
  ov.className = 'o-overlay--modal is-hidden';
  ov.id = 'alertOverlay';
  var popup = document.createElement('div');
  popup.className = 'alert-popup';
  var head = document.createElement('div');
  head.className = 'alert-head';
  var title = document.createElement('span');
  title.id = 'alertTitle';
  title.textContent = t('confirm.title', '通知');
  var closeBtn = document.createElement('button');
  closeBtn.className = 'c-close-btn';
  closeBtn.textContent = '✕';
  closeBtn.addEventListener('click', function() { closeAlert(null); });
  head.appendChild(title);
  head.appendChild(closeBtn);
  var body = document.createElement('div');
  body.className = 'alert-body';
  body.id = 'alertBody';
  var btns = document.createElement('div');
  btns.className = 'alert-btns';
  var cancelBtn = document.createElement('button');
  cancelBtn.id = 'alertCancelBtn';
  cancelBtn.className = 'is-hidden';
  cancelBtn.textContent = t('confirm.cancel', '取消');
  cancelBtn.addEventListener('click', function() { closeAlert(false); });
  var okBtn = document.createElement('button');
  okBtn.id = 'alertOkBtn';
  okBtn.textContent = t('confirm.ok', '确定');
  okBtn.addEventListener('click', function() { closeAlert(true); });
  btns.appendChild(cancelBtn);
  btns.appendChild(okBtn);
  popup.appendChild(head);
  popup.appendChild(body);
  popup.appendChild(btns);
  ov.appendChild(popup);
  document.getElementById('app').appendChild(ov);
  return ov;
}

function createThemeOverlay() {
  var ov = document.createElement('div');
  ov.className = 'o-overlay--modal is-hidden';
  ov.id = 'themeOverlay';
  ov.addEventListener('click', function(e) { if (e.target === ov) closeThemePopup(); });
  var popup = document.createElement('div');
  popup.className = 'theme-popup';
  var head = document.createElement('div');
  head.className = 'settings-head';
  var title = document.createElement('span');
  title.setAttribute('data-i18n', 'theme.title');
  title.textContent = t('theme.title', '主题设置');
  var closeBtn = document.createElement('button');
  closeBtn.className = 'c-close-btn';
  closeBtn.textContent = '✕';
  closeBtn.addEventListener('click', closeThemePopup);
  head.appendChild(title);
  head.appendChild(closeBtn);
  popup.appendChild(head);
  // Color theme section
  var cs = document.createElement('div');
  cs.className = 'theme-section';
  var ct = document.createElement('div');
  ct.className = 'theme-section__title';
  ct.setAttribute('data-i18n', 'theme.color');
  ct.textContent = t('theme.color', '颜色主题');
  cs.appendChild(ct);
  var cl = document.createElement('div');
  cl.id = 'themeCardList';
  cs.appendChild(cl);
  popup.appendChild(cs);
  // Icon theme section
  var isec = document.createElement('div');
  isec.className = 'theme-section';
  var it = document.createElement('div');
  it.className = 'theme-section__title';
  it.setAttribute('data-i18n', 'theme.icons');
  it.textContent = t('theme.icons', '图标主题');
  isec.appendChild(it);
  var ig = document.createElement('div');
  ig.className = 'theme-grid';
  ig.id = 'iconThemeGrid';
  isec.appendChild(ig);
  popup.appendChild(isec);
  ov.appendChild(popup);
  document.getElementById('app').appendChild(ov);
  return ov;
}


// ---- init ----
window.addEventListener('DOMContentLoaded', () => {
  // Bind stable tab drag handlers to bar
  var bar = document.getElementById('tabsBar');
  if (bar) {
    bar.addEventListener('dragover', _tabDragOver);
    bar.addEventListener('dragleave', _tabDragLeave);
    bar.addEventListener('drop', _tabDrop);
    // Mouse wheel horizontal scroll for tab bar
    bar.addEventListener('wheel', function(e) {
      var scroll = document.getElementById('tabsScroll');
      if (scroll) { scroll.scrollLeft += e.deltaY; e.preventDefault(); }
    }, { passive: false });
  }

  initStatusBar();
  // Fetch version once
  window.go.main.App.GetVersion().then(function(v) { window._appVersion = v || '0.6.1'; document.title = '串口调试工具 v' + window._appVersion; }).catch(function() { window._appVersion = '0.6.1'; });
  // Create global overlays (moved from static HTML to JS)
  createOfflineOverlay();
  createAlertOverlay();
  createThemeOverlay();
  if (typeof createSettingsOverlay === 'function') {
  createSettingsOverlay();
  // Initialize custom selects in settings overlay
  setTimeout(function() {
    var so = document.getElementById('settingsOverlay');
    if (so && typeof initCustomSelect === 'function') {
      so.querySelectorAll('select').forEach(function(s) { initCustomSelect(s); });
    }
  }, 50);
}
  loadSettings();
  initExternalLangs();

  // Create welcome tab page (hidden tab, no tab button)
  if (typeof createWelcomePage === 'function') {
    var welcomePage = createWelcomePage();
    var mc = document.getElementById('mainContent');
    if (mc) mc.appendChild(welcomePage.root);
    state._welcomePage = welcomePage;
    // Welcome tab is the initial active view
    state._welcomePage.show();
  }
  updateEmptyState();
  renderTabs();

  try {
    window.runtime.EventsOn('rx', (data) => appendLine('rx', data));
    window.runtime.EventsOn('tx', (data) => appendLine('tx', data));
    window.runtime.EventsOn('1', (data) => appendLine('1', data));
    window.runtime.EventsOn('2', (data) => appendLine('2', data));
    window.runtime.EventsOn('ports-changed', () => refreshPorts());
    window.runtime.EventsOn('ports-list', (data) => updatePortList(data.ports));
    window.runtime.EventsOn('daemon-offline', () => onDaemonGone('offline'));
    window.runtime.EventsOn('daemon-shutdown', () => onDaemonGone('shutdown'));
    window.runtime.EventsOn('process-changed', (data) => syncDaemonSessions(data));
    window.runtime.EventsOn('stats-count', (data) => onStatsCount(data));
    window.runtime.EventsOn('stats-rate', (data) => onStatsRate(data));
    window.runtime.EventsOn('clients-changed', (data) => onClientsChanged(data));
    window.runtime.EventsOn('multistr-changed', (data) => onMultistrChanged(data));
  } catch {}

  initDividerDrag();
  initQuickPanelDrag();
  initHistoryScroll();
  initMenuClick();
  initConfigListeners();

  // Connect to daemon — syncDaemonSessions will create tabs for existing processes
  tryConnect();
  // Show overlay immediately if daemon is not running (tryConnect returns
  // quickly when the process is absent, entering the retry/timeout path).
  setTimeout(function() { updateDaemonUI(); }, 100);
});

// ---- menu click ----
let _langMenuRefreshTs = 0;
function initMenuClick() {
  let menuOpen = false;
  const langLabel = document.querySelectorAll('.menu-label')[5]; // 语言
  document.querySelectorAll('.menu-label').forEach((label, idx) => {
    const item = label.parentElement;
    label.onclick = (e) => {
      e.stopPropagation();
      const wasOpen = item.classList.contains('is-open');
      document.querySelectorAll('.menu-item.is-open').forEach((el) => el.classList.remove('is-open'));
      if (!wasOpen) { item.classList.add('is-open'); menuOpen = true; }
      else menuOpen = false;
      // Debounced i18n folder re-scan when language menu opens
      if (!wasOpen && label === langLabel) {
        const now = Date.now();
        if (now - _langMenuRefreshTs > 800) {
          _langMenuRefreshTs = now;
          initExternalLangs();
        }
      }
    };
    item.onmouseenter = () => {
      if (!menuOpen) return;
      const prev = document.querySelector('.menu-item.is-open');
      document.querySelectorAll('.menu-item.is-open').forEach((el) => el.classList.remove('is-open'));
      item.classList.add('is-open');
      // Debounced re-scan when hovering into language menu from another menu
      if (prev !== item && label === langLabel) {
        const now = Date.now();
        if (now - _langMenuRefreshTs > 800) {
          _langMenuRefreshTs = now;
          initExternalLangs();
        }
      }
    };
  });
  // clicking a menu button closes the menu
  document.querySelectorAll('.menu-dropdown button').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.menu-item.is-open').forEach((el) => el.classList.remove('is-open'));
      menuOpen = false;
    });
  });
  // clicking anywhere else closes menus
  document.addEventListener('click', () => {
    document.querySelectorAll('.menu-item.is-open').forEach((el) => el.classList.remove('is-open'));
    menuOpen = false;
  });
}

function initConfigListeners() {
  // Serial config selects — save when changed
  ['baudSelect','dataBitsSelect','stopBitsSelect','paritySelect',
   'baudSelectA','portSelectASettings','baudSelectASettings','dataBitsSelectA','stopBitsSelectA','paritySelectA',
   'baudSelectB','portSelectBSettings','baudSelectBSettings','dataBitsSelectB','stopBitsSelectB','paritySelectB',
  ].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.addEventListener('change', () => saveSettings());
  });
  const appendSel = pageEl('appendSelect');
  if (appendSel) appendSel.addEventListener('change', () => saveSettings());
  const intervalEl = pageEl('sendInterval');
  if (intervalEl) intervalEl.addEventListener('change', () => saveSettings());
}

// When online the Go-side heartbeat monitors the connection; no JS polling.
// When offline we check the process list every second (lightweight tasklist, no IPC).
function startOfflineCheck() {
  if (state.statusInterval) clearInterval(state.statusInterval);
  state.statusInterval = setInterval(checkOffline, 1000);
}

function stopOfflineCheck() {
  if (state.statusInterval) { clearInterval(state.statusInterval); state.statusInterval = null; }
}

// Prevent overlapping connection attempts when daemon is detected externally.
// checkOffline fires every 1s; without this guard, each tick spawns a new
// tryConnect while the previous one is still blocking on WaitNamedPipeW (5 s),
// leading to retry-chain storms and missed detections.
var _connecting = false;

async function checkOffline() {
  if (state.daemonOnline) { stopOfflineCheck(); return; }
  if (_connecting) return; // let the running tryConnect finish
  try {
    state.processRunning = await window.go.main.App.DaemonProcessRunning();
  } catch { state.processRunning = false; }
  if (state.processRunning && !_connecting) {
    connectRetries = 0;
    tryConnect();
  }
}

let connectRetries = 0;
async function tryConnect() {
  if (_connecting) return; // guard against concurrent calls (e.g. setTimeout retry + checkOffline)
  _connecting = true;
  try {
    const ok = await window.go.main.App.CheckDaemonStatus();
    if (ok) {
      if (!state.daemonOnline) {
        state.daemonOnline = true;
        state.processRunning = true;
        state.connectFailures = 0;
        connectRetries = 0;
        stopOfflineCheck();
        updateDaemonUI();
        updateOpenBtn();
        await syncDaemonSessions();
        goGetHistoryStatus().then(on => updateAutoSaveCheck(on));
        const t = getActiveTab();
        state.currentMode = (t && t.mode) ? t.mode : 'single';
        updateModeUI();
        refreshPortsForce().then(() => _syncPortSelectsFromTab());
        applyI18n();
      }
      _connecting = false;
    } else {
      connectRetries++;
      if (connectRetries < 3 && !state.daemonOnline) {
        // Quick retry before falling back to slow offline check
        _connecting = false;
        setTimeout(tryConnect, 500);
      } else {
        _connecting = false;
        if (!state.daemonOnline) startOfflineCheck();
      }
    }
  } catch (e) {
    _connecting = false;
    if (!state.daemonOnline) startOfflineCheck();
  }
}

function onDaemonGone(reason) {
  if (!state.daemonOnline) return;
  state.daemonOnline = false;
  state.connectFailures = 0;
  updateDaemonUI();
  handleDaemonOffline();
  startOfflineCheck();
}

// ---- session sync ----
let syncPending = false;
async function syncDaemonSessions(procData) {
  if (syncPending) return;
  syncPending = true;
  try {
  if (!state.daemonOnline) return;

  let daemonSessions;
  // Accept process-changed push data or fetch explicitly
  if (procData && procData.processes) {
    // Event data uses "processId"; normalize to "id" for consistency with GetSessions()
    daemonSessions = procData.processes.map(p => {
      const e = Object.assign({}, p);
      if (e.processId !== undefined && e.id === undefined) e.id = e.processId;
      return e;
    });
  } else {
    try {
      daemonSessions = await window.go.main.App.GetSessions();
    } catch { return; }
  }
  if (!daemonSessions) daemonSessions = [];

  // Auto-create idle process if daemon just came online with no processes.
  // Skip while a tab is being intentionally closed (suppress window to avoid
  // the process-changed event from the destroy triggering immediate re-creation).
  const suppress = state._suppressAutoCreate && Date.now() < state._suppressAutoCreate;
  // autoCreateSession: add a local tab (no daemon process) when no tabs exist
  if (!suppress && state.autoCreateSession && daemonSessions.length === 0 && state.tabs.length === 0) {
    await addTab(false);  // add tab without switching (switchTo=false)
    if (state.tabs.length > 0) daemonSessions = [];
  }

  const dsMap = {};
  daemonSessions.forEach(s => { dsMap[s.id] = s; });

  const guiSessionIds = new Set();
  state.tabs.forEach(t => { if (t.sessionId) guiSessionIds.add(t.sessionId); });

  let tabsChanged = false;

  // 1. Create tabs for new daemon sessions not tracked in any GUI tab
  daemonSessions.forEach(ds => {
    if (!guiSessionIds.has(ds.id)) {
          // Skip unmatched idle processes while GUI is creating one (addTab in flight)
          if (_creatingTab && ds.status !== 'connected') return;
	      const isConnected = ds.status === 'connected';
	      const isForward = ds.mode === 'forward';
        const hasDeclared = !!ds.portName;
        const label = isForward
          ? (isConnected ? ds.portName : (hasDeclared ? `${ds.portName} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_forward', '端口转发空闲')))
          : (isConnected ? `${ds.portName} @ ${ds.baud}` : (hasDeclared ? `${ds.portName} @ ${ds.baud} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_single', '单端口空闲')));
      var newTab = {
        id: state.tabCounter++,
        label: label,
        sessionId: ds.id,
        portOpen: isConnected,
        mode: ds.mode || 'single',
        source: 'gui',
        viewers: ds.viewers || {gui:0, cli:0, mcp:0},
        syncedParams: (isConnected || hasDeclared) ? {
          portName: ds.portName, baud: ds.baud,
          dataBits: ds.dataBits, stopBits: ds.stopBits, parity: ds.parity,
          forwardPortB: ds.forwardPortB, forwardBaudB: ds.forwardBaudB,
        } : null,
        displayMode: state.displayMode,
        txDisplayMode: state.txDisplayMode,
        p1DisplayMode: 'text',
        p2DisplayMode: 'text',
        scrollLocked: false,
        sendRatio: state.sendRatio || 0.3,
        quickPanelRatio: state.quickPanelRatio,
        quickPresets: [],
      };
      newTab._page = new TabPage(state.tabs.length, newTab);
      var mainContent = document.getElementById('mainContent');
      if (mainContent) { mainContent.appendChild(newTab._page.root); initTabPage(newTab._page.root); }
      state.tabs.push(newTab);
      // Start per-tab Go decode worker
      if (ds.id && state.daemonOnline) {
        window.go.main.App.StartTabDecoder(ds.id);
        window.go.main.App.SetTabEncoding(ds.id, state.encoding);
      }
      tabsChanged = true;
    }
  });

  // 2. Update existing tabs with latest daemon params
  state.tabs.forEach(tab => {
    if (tab.sessionId && dsMap[tab.sessionId]) {
      const ds = dsMap[tab.sessionId];
      const isConnected = ds.status === 'connected';
        const hasDeclared2 = !!ds.portName;
        const newLabel = (ds.mode === 'forward') ? (isConnected ? ds.portName : (hasDeclared2 ? `${ds.portName} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_forward', '端口转发空闲'))) : (isConnected ? `${ds.portName} @ ${ds.baud}` : (hasDeclared2 ? `${ds.portName} @ ${ds.baud} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_single', '单端口空闲')));
      const newMode = ds.mode || 'single';
      const newViewers = ds.viewers || {gui:0, cli:0, mcp:0};
      const oldViewers = tab.viewers || {gui:0, cli:0, mcp:0};
      const viewersChanged = oldViewers.gui !== newViewers.gui || oldViewers.cli !== newViewers.cli || oldViewers.mcp !== newViewers.mcp;
      const modeChanged = tab.mode !== newMode;
      tab.mode = newMode;
      tab.viewers = newViewers;
      if (tab.portOpen !== isConnected || tab.label !== newLabel || viewersChanged || modeChanged) {
        tab.portOpen = isConnected;
        tab.label = newLabel;
        if (isConnected || hasDeclared2) {
          tab.syncedParams = {
            portName: ds.portName, baud: ds.baud,
            dataBits: ds.dataBits, stopBits: ds.stopBits, parity: ds.parity,
            forwardPortB: ds.forwardPortB, forwardBaudB: ds.forwardBaudB,
          };
        } else if (!hasDeclared2) {
          tab.syncedParams = null;
        }
        tabsChanged = true;
      }
    }
  });

  // 3. Remove daemon-synced tabs whose session is gone
  const removed = [];
  state.tabs.forEach((tab, i) => {
    if (tab.sessionId && !dsMap[tab.sessionId]) {
      if (tab.connectedAt && (Date.now() - tab.connectedAt) < 3000) {
        return;
      }
      tab.portOpen = false;
      tab.sessionId = null;
      tab.syncedParams = null;
      tabsChanged = true;
    }
  });
  for (let i = removed.length - 1; i >= 0; i--) {
    state.tabs.splice(removed[i], 1);
    tabsChanged = true;
  }

  // 4. Remove zombie GUI tabs whose sessions are gone (sessionId=null, not settings).
  // These are dead tabs left behind by a daemon disconnect or external session destroy.
  // Skip settings tabs — they are intentionally sessionless.
  if (daemonSessions.length > 0) {
    for (let i = state.tabs.length - 1; i >= 0; i--) {
      const t = state.tabs[i];
      if (t.type === 'settings') continue;
      if (t.source === 'gui' && !t.sessionId && state.tabs.length > 1) {
        if (t._page) t._page.destroy();
        state.tabs.splice(i, 1);
        tabsChanged = true;
      }
    }
    if (tabsChanged && state.activeTab >= state.tabs.length) {
      state.activeTab = Math.max(0, state.tabs.length - 1);
    }
  }

  // Track current daemon sessions
  state.knownDaemonSessions = {};
  daemonSessions.forEach(s => { state.knownDaemonSessions[s.id] = true; });

  if (tabsChanged || state.tabs.length === 0) {
    if (state.activeTab >= state.tabs.length) state.activeTab = Math.max(0, state.tabs.length - 1);
    const newTab = getActiveTab();
    state.currentMode = (newTab && newTab.mode) ? newTab.mode : 'single';
    // Sync display modes from new tab (consistent with switchToTab)
    if (newTab && !newTab.isWelcome) {
      state.displayMode = newTab.displayMode || 'text';
      state.txDisplayMode = newTab.txDisplayMode || 'text';
      state.p1DisplayMode = newTab.p1DisplayMode || 'text';
      state.p2DisplayMode = newTab.p2DisplayMode || 'text';
    }

    // Ensure only the active tab page is visible; hide all others
    state.tabs.forEach((t, i) => {
      if (t._page && !t._page.isWelcome) {
        if (i === state.activeTab) t._page.show(); else t._page.hide();
      }
    });

    updateModeUI();
    // Refresh display mode buttons after tab activation
    var bd = pageEl('btnToggleDisplay');
    if (bd) {
      bd.textContent = (state.displayMode === 'hex') ? ('⇃ ' + t('display.rx_hex_short', 'H')) : ('⇃ ' + t('display.rx_text_short', '字'));
      bd.title = (state.displayMode === 'hex') ? t('display.switch_to_text', '点击切换至文本显示') : t('display.switch_to_hex', '点击切换至Hex显示');
      bd.classList.toggle('btn-hex', state.displayMode === 'hex');
    }
    var bst = pageEl('btnToggleSend');
    if (bst) {
      bst.textContent = (state.txDisplayMode === 'hex') ? ('↾ ' + t('display.tx_hex_short', 'H')) : ('↾ ' + t('display.tx_text_short', '字'));
      bst.title = (state.txDisplayMode === 'hex') ? t('display.switch_to_text', '点击切换至文本显示') : t('display.switch_to_hex', '点击切换至Hex显示');
      bst.classList.toggle('btn-hex', state.txDisplayMode === 'hex');
    }
    updateEmptyState();
    renderTabs();
    updateOpenBtn();
    updateSyncIndicator();
    updatePortOccupied();
    _syncPortSelectsFromTab();
    syncAutoSendState(daemonSessions);
    await loadTabHistory();
  }
  } finally {
    syncPending = false;
  }
}

function syncAutoSendState(daemonSessions) {
  const tab = getActiveTab();
  if (!tab || !tab.sessionId) return;
  const ds = daemonSessions.find(s => s.id === tab.sessionId);
  if (!ds || !ds.autoSend) return;
  const as = ds.autoSend;
  const wasActive = tab._autoSendActive;
  tab._autoSendActive = (as.enabled === true);
  if (wasActive !== tab._autoSendActive) {
    updateAutoSendUI();
  }
}

function _markOccupied(selId) {
  const sel = document.getElementById(selId);
  if (!sel) return;
  const tab = getActiveTab();
  const myPort = (tab && tab.portOpen && tab.syncedParams) ? tab.syncedParams.portName : '';
  const myPortB = (tab && tab.portOpen && tab.mode === 'forward' && tab.syncedParams) ? tab.syncedParams.forwardPortB : '';
  // Extract individual ports for the active forward tab
  const myPorts = tab && tab.mode === 'forward' && tab.portOpen ? _forwardPorts(tab) : null;
  for (let i = 0; i < sel.options.length; i++) {
    const opt = sel.options[i];
    const portName = opt.value;
    if (!portName) { opt.className = ''; continue; }
    let occupied = false;
    for (let j = 0; j < state.tabs.length; j++) {
      if (j === state.activeTab) continue;
      const t = state.tabs[j];
      if (!t.portOpen || !t.syncedParams) continue;
      if (_tabHasPort(t, portName)) { occupied = true; break; }
    }
    // In forward mode, mark own other port as occupied to prevent self-duplicate
    if (!occupied && tab && tab.portOpen && tab.mode === 'forward' && myPorts) {
      if ((selId === 'portSelectA' && portName === myPorts[1]) ||
          (selId === 'portSelectB' && portName === myPorts[0])) {
        occupied = true;
      }
    }
    opt.className = occupied ? 'port-occupied' : '';
  }
}

// Check if a tab uses the given port (handles both single and forward mode).
function _tabHasPort(t, portName) {
  if (!t.syncedParams) return false;
  // Single mode: portName is just the port
  if (t.mode !== 'forward') return t.syncedParams.portName === portName;
  // Forward mode: portName is "COM4 ↔ COM14", also check forwardPortB
  var ports = _forwardPorts(t);
  return ports[0] === portName || ports[1] === portName;
}

// Extract individual port names from a forward-mode tab.
// Returns [portA, portB]; for single mode returns [portName, ''].
function _forwardPorts(t) {
  if (!t.syncedParams) return ['', ''];
  if (t.mode !== 'forward') return [t.syncedParams.portName || '', ''];
  var combined = t.syncedParams.portName || '';
  var parts = combined.split(' ↔ ');
  return [parts[0] || '', parts[1] || t.syncedParams.forwardPortB || ''];
}

function updatePortOccupied() {
  _markOccupied('portSelect');
  _markOccupied('portSelectA');
  _markOccupied('portSelectB');
}

function _syncPortSelectsFromTab() {
  const tab = getActiveTab();
  if (!tab || !tab.syncedParams) return;
  const sp = tab.syncedParams;
  const isFwd = tab.mode === 'forward';
  const hasPort = !!(tab.portOpen || sp.portName); // open or declared
  if (!hasPort) return;
  // Set port select values from tab's synced params
  if (isFwd) {
    // portName for connected forward is "COM4 ↔ COM14" — split to get individual ports
    const p1Port = sp.portName ? sp.portName.split(' ↔ ')[0] : '';
    _setSelectVal('portSelectA', p1Port);
    _setSelectVal('portSelectB', sp.forwardPortB);
    _setSelectVal('baudSelectA', sp.baud);
    _setSelectVal('baudSelectB', sp.forwardBaudB || sp.baud);
    if (tab.portOpen) {
      _setSelectVal('dataBitsSelectA', sp.dataBits);
      _setSelectVal('dataBitsSelectB', sp.dataBits);
      _setSelectVal('stopBitsSelectA', sp.stopBits);
      _setSelectVal('stopBitsSelectB', sp.stopBits);
      _setSelectVal('paritySelectA', sp.parity);
      _setSelectVal('paritySelectB', sp.parity);
    }
  } else {
    _setSelectVal('portSelect', sp.portName);
    _setSelectVal('baudSelect', sp.baud);
    if (tab.portOpen) {
      _setSelectVal('dataBitsSelect', sp.dataBits);
      _setSelectVal('stopBitsSelect', sp.stopBits);
      _setSelectVal('paritySelect', sp.parity);
    }
  }
}

function _setSelectVal(id, val) {
  if (val === undefined || val === null) return;
  const el = pageEl(id);
  if (!el) return;
  const str = String(val);
  if (el.tagName === 'INPUT') { el.value = str; return; }
  for (let i = 0; i < el.options.length; i++) {
    if (el.options[i].value === str) { el.value = str; return; }
  }
}

function handleDaemonOffline() {
  // Destroy all session tabs (keep settings and welcome tabs).
  // After daemon disconnect these tabs are dead — their sessionId is stale
  // and they cannot send/receive data.  Removing them avoids a confusing
  // UI where zombie tabs linger and later collide with freshly synced tabs.
  for (let i = state.tabs.length - 1; i >= 0; i--) {
    const t = state.tabs[i];
    if (t.type === 'settings') continue;
    if (t._page && t._page.isWelcome) continue;
    if (t.sessionId) {
      delete state.historyCache[t.sessionId];
      delete state.statsBase[t.sessionId];
    }
    if (t._page) {
      t._page.destroy();
      t._page = null;
    }
    state.tabs.splice(i, 1);
  }
  state.activeTab = 0;
  if (state.tabs.length === 0 && state._welcomePage) {
    state._welcomePage.show();
  }
  state.knownDaemonSessions = {};
  state.currentMode = 'single';
  updateEmptyState();
  renderTabs();
  updateModeUI();
  loadTabHistory();
}

function updateSyncIndicator() {
  const guiCount = stateClients.filter(c => c.source === 'gui').length;
  const cliCount = stateClients.filter(c => c.source === 'cli').length;
  const mcpCount = stateClients.filter(c => c.source === 'mcp').length;
  if (_statusBar) _statusBar.updateClients({gui: guiCount, cli: cliCount, mcp: mcpCount});
}

// Returns true when the active "tab" is a real session tab (not settings/welcome).
function _hasSessionTab() {
  const t = getActiveTab();
  return t && !t.type && !(t._page && t._page.isWelcome);
}

// Show/hide status-bar stats: visible only when daemon is online and the
// active tab is a real session tab (not settings, not welcome).
function _updateStatsVisible() {
  const vis = state.daemonOnline && _hasSessionTab();
  const el = document.getElementById('statusbar-stats');
  if (el) el.style.display = vis ? '' : 'none';
}

function updateDaemonUI() {
  const overlay = document.getElementById('offlineOverlay');
  const portSel = pageEl('portSelect');
  if (_statusBar) _statusBar.updateDaemon({online: state.daemonOnline, tabsCount: state.tabs.length});
  // Show overlay only when daemon is offline AND the active tab is not settings
  const isSettings = getActiveTab() && getActiveTab().type === 'settings';
  if (state.daemonOnline || isSettings) {
    overlay.classList.add('is-hidden');
    var mt = document.getElementById('menuToggleDaemon');
    if (mt) { var mts = mt.querySelector('.menu-text'); if (mts) mts.textContent = t('menu.stop_daemon', '停止守护进程'); }
  } else {
    overlay.classList.remove('is-hidden');
    var mt2 = document.getElementById('menuToggleDaemon');
    if (mt2) { var mts2 = mt2.querySelector('.menu-text'); if (mts2) mts2.textContent = t('menu.start_daemon', '启动守护进程'); }
    if (portSel) {
      state.lastPorts = '';
      portSel.innerHTML = '<option value="">' + t('offline.title', '-- 守护进程未运行 --') + '</option>';
    }
  }
  // Disable new-tab buttons when daemon is offline
  const addBtn = document.getElementById('tabAddBtn');
  if (addBtn) addBtn.disabled = !state.daemonOnline;
  const menuNew = document.getElementById('menuNewSession');
  if (menuNew) menuNew.disabled = !state.daemonOnline;
  _updateStatsVisible();
}

async function toggleDaemon() {
  if (state.daemonOnline) {
    await stopDaemon();
  } else {
    await startDaemon();
  }
}

async function startDaemon() {
  if (_connecting) return;
  _connecting = true;
  if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(true);
  stopOfflineCheck();
  try {
    await window.go.main.App.StartDaemon();
    state.processRunning = true;
    state.daemonOnline = true;
    state.connectFailures = 0;
    updateDaemonUI();
    updateOpenBtn();
    await syncDaemonSessions();
  } catch (e) {
    console.error('startDaemon failed:', e);
    state.daemonOnline = false;
    state.processRunning = false;
    updateDaemonUI();
    startOfflineCheck();
    showAlert(t('cli.error','错误'), t('serial.start_fail','启动失败: ') + e);
  }
  _connecting = false;
  if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(false);
}

async function stopDaemon() {
  showConfirm(t('menu.stop_daemon', '停止守护进程'), t('confirm.stop_daemon_msg', '确定要停止守护进程吗？\n所有串口连接将断开。'), async () => {
    if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(true);
    try {
      await window.go.main.App.StopDaemon();
    } catch {}
    // Clean up local state after requesting shutdown
    handleDaemonOffline();
    state.daemonOnline = false;
    state.processRunning = false;
  state.lastPorts = '';
  state.knownDaemonSessions = {};
  state.activeTab = 0;
  updateEmptyState();
  startOfflineCheck();
  updateDaemonUI();
  updateOpenBtn();
  updateSyncIndicator();
  renderTabs();
  clearDisplay();
  if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(false);
  }, 'confirm.ok_shutdown', 'confirm.cancel_shutdown');
}

// ---- serial ports ----
function _applyPortOptions(selId, optionsHtml, placeholder) {
  const sel = document.getElementById(selId);
  if (!sel) return;
  const curVal = sel.value;
  const hasPorts = optionsHtml !== '<option value="">-- 无可用串口 --</option>';
  sel.innerHTML = hasPorts ? `<option value="">${placeholder}</option>` + optionsHtml : optionsHtml;
  if (curVal) {
    const opt = sel.querySelector(`option[value="${curVal.replace(/"/g, '\\"')}"]`);
    if (opt) sel.value = curVal;
  }
}

function updatePortList(ports) {
  let html = '';
  if (!ports || ports.length === 0) {
    html = `<option value="">${t('status.no_ports', '-- 无可用串口 --')}</option>`;
  } else {
    ports.forEach(p => {
      const desc = (p.description && p.description !== p.port) ? ' — ' + p.description : '';
      html += `<option value="${escHtml(p.port)}">${escHtml(p.port + desc)}</option>`;
    });
  }
  if (html !== state.lastPorts) {
    state.lastPorts = html;
    _applyPortOptions('portSelect', html, t('status.select_port', '-- 选择端口 --'));
    _applyPortOptions('portSelectA', html, t('status.port1', '-- 端口1 --'));
    _applyPortOptions('portSelectB', html, t('status.port2', '-- 端口2 --'));
    _applyPortOptions('portSelectASettings', html, t('status.port1', '-- 端口1 --'));
    _applyPortOptions('portSelectBSettings', html, t('status.port2', '-- 端口2 --'));
  }
  updatePortOccupied();
}

async function refreshPorts() {
  if (!state.daemonOnline) return;
  try {
    const ports = await window.go.main.App.GetPorts();
    updatePortList(ports);
  } catch (e) {
    // Keep last known port list on error — don't overwrite
    console.error('refreshPorts:', e);
  }
}

async function refreshPortsForce() {
  if (!state.daemonOnline) return;
  try {
    const ports = await window.go.main.App.RefreshPorts();
    updatePortList(ports);
  } catch (e) {
    // Keep last known port list on error
    console.error('refreshPortsForce:', e);
  }
}

// ---- per-tab initialization ----
function initTabPage(pageRoot) {
  if (!pageRoot || !pageRoot.querySelectorAll) return;
  // Initialize custom selects
  pageRoot.querySelectorAll('select').forEach(function(s) {
    if (typeof initCustomSelect === 'function') initCustomSelect(s);
  });
  // Populate SVG icons
  if (typeof _populateIcons === 'function') _populateIcons();
  // Initialize send toolbar scroll
  if (typeof bindSendScroll === 'function') {
    var scrollArea = pageRoot.querySelector('.send-scroll-area');
    if (scrollArea) bindSendScroll(scrollArea);
  }
  // Populate port selects from cached port list (use pageRoot directly,
  // not pageEl(), because this tab may not be the active one yet)
  if (state.lastPorts) {
    var selP = pageRoot.querySelector('#portSelect');
    if (selP) _setPortOptions(selP, state.lastPorts, t('status.select_port', '-- 选择端口 --'));
    var selA = pageRoot.querySelector('#portSelectA');
    if (selA) _setPortOptions(selA, state.lastPorts, t('status.port1', '-- 端口1 --'));
    var selB = pageRoot.querySelector('#portSelectB');
    if (selB) _setPortOptions(selB, state.lastPorts, t('status.port2', '-- 端口2 --'));
  }
}

function _setPortOptions(sel, optionsHtml, placeholder) {
  var curVal = sel.value;
  var hasPorts = optionsHtml !== '<option value="">-- 无可用串口 --</option>';
  sel.innerHTML = hasPorts ? '<option value="">' + placeholder + '</option>' + optionsHtml : optionsHtml;
  if (curVal) {
    var opt = sel.querySelector('option[value="' + curVal.replace(/"/g, '\\"') + '"]');
    if (opt) sel.value = curVal;
  }
}

// ---- tabs ----
let _creatingTab = false;

async function addTab(switchTo) {
  // Cannot create tabs when daemon is offline — the tab would be a zombie
  // with no sessionId and no ability to send/receive data.
  if (!state.daemonOnline) return;
  // Create idle process on daemon, then add a local tab for it.
  // Use _creatingTab flag to prevent syncDaemonSessions from creating
  // a duplicate tab when the process-changed event fires mid-await.
  let sid = null;
  if (state.daemonOnline) {
    _creatingTab = true;
    try { sid = await window.go.main.App.CreateIdleProcess(); } catch {}
    _creatingTab = false;
  }
  // Check if syncDaemonSessions already created a tab for this process
  if (sid && state.tabs.some(t => t.sessionId === sid)) {
    if (switchTo !== false) {
      state.activeTab = state.tabs.findIndex(t => t.sessionId === sid);
      state.currentMode = 'single';
      updateModeUI();
      loadTabHistory();
    }
    updateEmptyState();
    renderTabs();
    updateOpenBtn();
    updateSyncIndicator();
    return;
  }
  var newTab2 = { id: state.tabCounter++, label: t('tab.idle_single', '单端口空闲'), sessionId: sid, portOpen: false, mode: 'single', source: 'gui', displayMode: 'text', txDisplayMode: 'text', p1DisplayMode: 'text', p2DisplayMode: 'text', scrollLocked: false, sendRatio: state.sendRatio || 0.3, quickPanelRatio: state.quickPanelRatio, quickPresets: [] };
  newTab2._page = new TabPage(state.tabs.length, newTab2);
  var mainContent2 = document.getElementById('mainContent');
  if (mainContent2) { mainContent2.appendChild(newTab2._page.root); initTabPage(newTab2._page.root); }
  state.tabs.push(newTab2);
  // Start per-tab Go decode worker
  if (sid && state.daemonOnline) {
    window.go.main.App.StartTabDecoder(sid);
    window.go.main.App.SetTabEncoding(sid, state.encoding);
  }
  if (switchTo !== false) {
    // Hide currently visible page before switching
    var oldPage = getActivePage();
    if (oldPage) oldPage.hide();
    if (state._welcomePage) state._welcomePage.hide();
    state.activeTab = state.tabs.length - 1;
    state.currentMode = 'single';
    // Show new tab's page
    if (newTab2._page) newTab2._page.show();
    updateModeUI();
    loadTabHistory();
  }
  updateEmptyState();
  renderTabs();
  updateOpenBtn();
  updateSyncIndicator();
}

function switchTab(idx) {
  var oldTab = getActiveTab();
  var newTab = state.tabs[idx];
  if (!newTab) return;

  // Settings tab (in either direction): just show/hide, no per-tab state
  if (newTab.type === 'settings' || (oldTab && oldTab.type === 'settings')) {
    if (oldTab && oldTab._page) oldTab._page.hide();
    state.activeTab = idx;
    if (newTab.type === 'settings') {
      if (newTab._page) { newTab._page.show(); renderSettingsThemeGrid(); renderSettingsIconGrid(); }
    } else if (newTab._page) {
      newTab._page.show();
      loadTabHistory();
      refreshStatsDOM();
    }
    updateModeUI();
    renderTabs();
    updateOpenBtn();
    updateEmptyState();
    updateSyncIndicator();
    _updateStatsVisible();
    updateDaemonUI();
    return;
  }

  // Save full per-tab state from old tab
  if (oldTab) {
    saveSendUI();
    oldTab.displayMode = state.displayMode;
    oldTab.txDisplayMode = state.txDisplayMode;
    oldTab.p1DisplayMode = state.p1DisplayMode;
    oldTab.p2DisplayMode = state.p2DisplayMode;
    oldTab.scrollLocked = state.scrollLocked;
    oldTab.sendRatio = state.sendRatio;
    oldTab.quickPanelRatio = state.quickPanelRatio;
    oldTab.quickPresets = state.quickPresets.slice();
    oldTab._qpCycleActive = state._qpCycleActive;
    // Save port selections
    var ps = pageEl('portSelect');
    if (ps) oldTab._portSelectVal = ps.value;
    var bs = pageEl('baudSelect');
    if (bs) oldTab._baudSelectVal = bs.value;
    if (oldTab._page) {
      oldTab._page.ui.sendFormat = getSendState().sendFormat;
      oldTab._page.ui.textSendBuf = pageEl('sendInput').value;
      oldTab._page.hide();
    }
  }

  state.activeTab = idx;

  // Restore per-tab state from new tab
  state.displayMode = newTab.displayMode || 'text';
  state.txDisplayMode = newTab.txDisplayMode || 'text';
  state.p1DisplayMode = newTab.p1DisplayMode || 'text';
  state.p2DisplayMode = newTab.p2DisplayMode || 'text';
  state.scrollLocked = !!newTab.scrollLocked;
  state.sendRatio = (typeof newTab.sendRatio === 'number') ? newTab.sendRatio : 0.3;
  state.quickPanelRatio = (typeof newTab.quickPanelRatio === 'number') ? newTab.quickPanelRatio : 0;
  state.quickPresets = (newTab.quickPresets && newTab.quickPresets.length) ? newTab.quickPresets.slice() : [];
  state._qpCycleActive = !!newTab._qpCycleActive;
  state.currentMode = (newTab && newTab.mode) ? newTab.mode : 'single';

  // Show new tab
  if (newTab._page) {
    newTab._page.show();
    // Restore port selections
    if (newTab._portSelectVal) {
      var ps2 = pageEl('portSelect');
      if (ps2) ps2.value = newTab._portSelectVal;
    }
    if (newTab._baudSelectVal) {
      var bs2 = pageEl('baudSelect');
      if (bs2) bs2.value = newTab._baudSelectVal;
    }
  }
  updateModeUI();
  renderTabs();
  updateOpenBtn();
  restoreSendUI();
  loadTabHistory();
  refreshStatsDOM();
  updatePortOccupied();
  applySendRatio();
  applyQuickPanelRatio();
  updateFwdBtns();
  updateScrollLockBtns();
  quickPanelRender();

  _updateStatsVisible();

  // Query auto-send status for this tab
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.daemonOnline) {
    window.go.main.App.GetAutoSendStatus(tab.sessionId).then((s) => {
      if (state.tabs[state.activeTab] !== tab) return;
      tab._autoSendActive = (s && s.enabled === true);
      if (state.tabs[state.activeTab] === tab) _setAutoSendBtn(!!tab._autoSendActive);
    }).catch(() => {});
    // Load multi-string entries from daemon for this tab
    if (!tab._multistrLoaded) {
      tab._multistrLoaded = true;
      window.go.main.App.MultistrLoad(tab.sessionId).then(function(json) {
        if (state.tabs[state.activeTab] !== tab) return;
        var entries = JSON.parse(json);
        if (Array.isArray(entries) && entries.length > 0) {
          state.quickPresets = entries.map(function(e) {
            return {
              enabled: e.enabled !== false,
              hex: !!e.hex,
              content: e.content || '',
              delay: e.delay || 1000,
              note: e.note || ''
            };
          });
          quickPanelRender();
        }
      }).catch(function(){});
    }
  }
}

function closeTab(idx) {
  const tab = state.tabs[idx];
  if (!tab) return;
  // Suppress auto-create in syncDaemonSessions while we intentionally destroy a process.
  // Otherwise the process-changed event from the destroy triggers immediate re-creation.
  state._suppressAutoCreate = (Date.now() + 2000);
  if (tab.sessionId && state.daemonOnline) {
    window.go.main.App.CloseSession(tab.sessionId).catch(() => {});
  }
  if (tab.sessionId) {
    delete state.historyCache[tab.sessionId];
    delete state.statsBase[tab.sessionId];
  }
  // Destroy TabPage DOM
  if (tab._page) {
    tab._page.destroy();
    tab._page = null;
  }
  state.tabs.splice(idx, 1);
  if (state.activeTab >= state.tabs.length) state.activeTab = Math.max(0, state.tabs.length - 1);
  const newTab = getActiveTab();
  // Show the new active tab page (or welcome page if no tabs remain)
  if (newTab && newTab._page) {
    newTab._page.show();
  } else if (state._welcomePage) {
    state._welcomePage.show();
  }
  state.currentMode = (newTab && newTab.mode) ? newTab.mode : 'single';
  updateModeUI();
  updateEmptyState();
  renderTabs();
  updateOpenBtn();
  updateSyncIndicator();
  _updateStatsVisible();
  updateDaemonUI();
  if (newTab) loadTabHistory();
  else clearDisplay();
}

function closeCurrentTab() { closeTab(state.activeTab); }

function renderTabs() {
  const scroll = document.getElementById('tabsScroll');
  if (!scroll) return;
  scroll.innerHTML = '';
  state.tabs.forEach((t, i) => {
    if (t._page && t._page.isWelcome) return;  // skip welcome tab
    const btn = document.createElement('button');
	    btn.className = 'tab-btn' + (i === state.activeTab ? ' is-active' : '') + (t.mode === 'forward' ? ' tab-forward' : '');
    if (!t.portOpen) btn.className += ' tab-idle';
    btn.draggable = true;
    btn.dataset.tabIdx = i;
    btn.onclick = () => switchTab(i);
    // drag handlers
    btn.ondragstart = (e) => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(i)); btn.classList.add('tab-dragging'); };
    btn.ondragend = () => { btn.classList.remove('tab-dragging'); _tabHideIndicator(); };
    const isFwd = (t.mode === 'forward');
    const isSettings = (t.type === 'settings');
    const tag = isSettings ? '' : (isFwd ? ' ↔' : '');
    // Status dot (gear icon for settings)
    const dot = document.createElement('span');
    if (isSettings) {
      dot.innerHTML = icon('settings', 14, 14);
      dot.style.cssText = 'display:flex;align-items:center;margin-right:2px;';
    } else {
      dot.className = 'tab-dot' + (t.portOpen ? ' live' : '');
    }
    btn.appendChild(dot);
    // Label text
    const labelSpan = document.createElement('span');
    labelSpan.textContent = t.label + tag;
    btn.appendChild(labelSpan);
    const v2 = t.viewers || {gui:0, cli:0, mcp:0};
    [['gui','#4a9eff'],['cli','#4caf50'],['mcp','#ff9800']].forEach(function(s) {
      const cnt = v2[s[0]] || 0;
      if (cnt > 0 && !(s[0] === 'gui' && cnt === 1)) {
        const vb = document.createElement("span"); vb.style.cssText = "color:"+s[1]+";font-size:11px;font-weight:bold"; vb.textContent = cnt; btn.appendChild(vb);
      }
    });
    if (t.syncedParams) {
      btn.title = `会话: ${t.label}
数据位:${t.syncedParams.dataBits} 停止位:${t.syncedParams.stopBits} 校验:${t.syncedParams.parity}`;
    } else {
      btn.title = t.portOpen ? `会话 #${t.id}` : window.t('tooltip.idle_process', '空闲进程');
    }
    {
      const span = document.createElement('span');
      span.className = 'close-tab';
      span.innerHTML = icon('close', 12, 12);
      span.onclick = (e) => { e.stopPropagation(); closeTab(i); };
      btn.appendChild(span);
    }
    scroll.appendChild(btn);
  });
  // Update add button i18n (static HTML, outside scroll wrapper)
  var addBtn = document.getElementById('tabAddBtn');
  if (addBtn) { addBtn.title = (typeof t === 'function') ? t('tooltip.new_tab', '新建会话') : '新建会话'; }

  // Ensure drag indicator exists in bar (not scroll, to avoid overflow clipping)
  if (!document.getElementById('tabDropIndicator')) {
    var indicator = document.createElement('div');
    indicator.id = 'tabDropIndicator';
    indicator.className = 'tab-drop-indicator';
    var bar = document.getElementById('tabsBar');
    if (bar) bar.appendChild(indicator);
  }
}

// ---- port open/close ----
function getActiveTab() {
  var tab = state.tabs[state.activeTab];
  // Skip welcome tab — find first real tab
  if (tab && tab._page && tab._page.isWelcome) {
    for (var i = 0; i < state.tabs.length; i++) {
      if (!(state.tabs[i]._page && state.tabs[i]._page.isWelcome)) return state.tabs[i];
    }
    return null;
  }
  return tab;
}

async function togglePort() {
  const tab = getActiveTab();
  if (tab.portOpen) { await closePort(); } else { await openPort(); }
}

async function openPort() {
  if (activeMode() === 'forward') {
    await openForwardPorts();
    return;
  }
  const port = pageEl('portSelect').value;
  if (!port) { showAlert(t('confirm.title','提示'), t('serial.select_port','请选择串口')); return; }

  const cfg = {
    port: port,
    baud: parseInt(pageEl('baudSelect').value),
    dataBits: parseInt((pageEl('dataBitsSelect')||{}).value) || 8,
    stopBits: (pageEl('stopBitsSelect')||{}).value || '1',
    parity: (pageEl('paritySelect')||{}).value || 'none',
  };

  try {
    let tab = getActiveTab();

    // If tab has no backing process, create an idle one first
    if (!tab.sessionId) {
      const sid = await window.go.main.App.CreateIdleProcess();
      tab.sessionId = sid;
    }

    // Connect the idle process to the selected port
    await window.go.main.App.ConnectSession(tab.sessionId, cfg);

    tab.portOpen = true;
    tab.label = `${cfg.port} @ ${cfg.baud}`;
    tab.syncedParams = { portName: cfg.port, baud: cfg.baud, dataBits: cfg.dataBits, stopBits: cfg.stopBits, parity: cfg.parity };

    updateOpenBtn();
    updateSyncIndicator();
    updatePortOccupied();
    renderTabs();
    state.statsBase[tab.sessionId] = { tx: 0, rx: 0 };
    await mergeRingBuffer(tab.sessionId);
    appendSystemMsg(`串口已打开: ${cfg.port} @ ${cfg.baud}`);
    saveSettings();
  } catch (e) {
    showAlert(t('cli.error','错误'), t('serial.open_fail','打开失败: ') + e);
  }
}

async function openForwardPorts() {
  const portA = pageEl('portSelectA').value;
  const portB = pageEl('portSelectB').value;
  if (!portA || !portB) { showAlert(t('confirm.title','提示'), t('serial.select_two','请选择两个端口')); return; }

  const cfgA = {
    port: portA,
    baud: parseInt(pageEl('baudSelectA').value),
    dataBits: parseInt((pageEl('dataBitsSelectA')||{}).value) || 8,
    stopBits: (pageEl('stopBitsSelectA')||{}).value || '1',
    parity: (pageEl('paritySelectA')||{}).value || 'none',
  };
  const cfgB = {
    port: portB,
    baud: parseInt(pageEl('baudSelectB').value),
    dataBits: parseInt((pageEl('dataBitsSelectB')||{}).value) || 8,
    stopBits: (pageEl('stopBitsSelectB')||{}).value || '1',
    parity: (pageEl('paritySelectB')||{}).value || 'none',
  };

  try {
    let tab = getActiveTab();

    if (!tab.sessionId) {
      const sid = await window.go.main.App.CreateIdleProcess();
      tab.sessionId = sid;
    }

    await window.go.main.App.ForwardConnect(tab.sessionId, cfgA, cfgB);

    tab.portOpen = true;
    tab.label = cfgA.port + ' ↔ ' + cfgB.port;
    tab.syncedParams = { portName: cfgA.port, baud: cfgA.baud, dataBits: cfgA.dataBits, stopBits: cfgA.stopBits, parity: cfgA.parity, forwardPortB: cfgB.port, forwardBaudB: cfgB.baud };

    updateOpenBtn();
    updateSyncIndicator();
    updatePortOccupied();
    renderTabs();
    state.statsBase[tab.sessionId] = { tx: 0, rx: 0, p1: 0, p2: 0 };
    await mergeRingBuffer(tab.sessionId);
    appendSystemMsg('端口转发已启动: ' + cfgA.port + ' ↔ ' + cfgB.port);
    saveSettings();
  } catch (e) {
    showAlert(t('cli.error','错误'), t('serial.open_fail','打开失败: ') + e);
  }
}

async function closePort() {
  const tab = getActiveTab();
  if (!tab || !tab.sessionId) return;
  try {
    if (state.daemonOnline) {
      await window.go.main.App.DisconnectSession(tab.sessionId);
    }
  } catch {}
  refreshStatsDOM();
  await mergeRingBuffer(tab.sessionId);
  appendSystemMsg(t('sys.port_closed_label', '串口已关闭: %s').replace('%s', tab.label));
  tab.portOpen = false;
  tab.label = tab.mode === 'forward' ? t('tab.idle_forward', '端口转发空闲') : t('tab.idle_single', '单端口空闲');
  tab.syncedParams = null;
  updateOpenBtn();
  updateSyncIndicator();
  updatePortOccupied();
  renderTabs();
}

function swapForwardPorts() {
  const pairs = [
    ['portSelectA', 'portSelectB'],
    ['baudSelectA', 'baudSelectB'],
    ['portSelectASettings', 'portSelectBSettings'],
    ['baudSelectASettings', 'baudSelectBSettings'],
    ['dataBitsSelectA', 'dataBitsSelectB'],
    ['stopBitsSelectA', 'stopBitsSelectB'],
    ['paritySelectA', 'paritySelectB'],
    ['flowControlSelectA', 'flowControlSelectB'],
  ];
  pairs.forEach(([idA, idB]) => {
    const elA = pageEl(idA);
    const elB = pageEl(idB);
    if (!elA || !elB) return;
    const tmp = elA.value;
    elA.value = elB.value;
    elB.value = tmp;
  });
}

function _syncSettingsToBar() {
  // Forward: sync settings popup → config bar
  if (activeMode() === 'forward') {
    const pairs = [
      ['portSelectASettings', 'portSelectA'],
      ['baudSelectASettings', 'baudSelectA'],
      ['dataBitsSelectA', 'dataBitsSelectA'],
      ['stopBitsSelectA', 'stopBitsSelectA'],
      ['paritySelectA', 'paritySelectA'],
      ['portSelectBSettings', 'portSelectB'],
      ['baudSelectBSettings', 'baudSelectB'],
      ['dataBitsSelectB', 'dataBitsSelectB'],
      ['stopBitsSelectB', 'stopBitsSelectB'],
      ['paritySelectB', 'paritySelectB'],
    ];
    pairs.forEach(([fromId, toId]) => {
      const from = pageEl(fromId);
      const to = pageEl(toId);
      if (from && to && from.value) to.value = from.value;
    });
  } else {
    // Single: sync settings popup → config bar
    const pairs = [
      ['baudSelect', 'baudSelect'],
      ['dataBitsSelect', 'dataBitsSelect'],
      ['stopBitsSelect', 'stopBitsSelect'],
      ['paritySelect', 'paritySelect'],
    ];
  }
}

function _syncBarToSettings() {
  // Forward: sync config bar → settings popup
  if (activeMode() === 'forward') {
    const pairs = [
      ['portSelectA', 'portSelectASettings'],
      ['baudSelectA', 'baudSelectASettings'],
      ['dataBitsSelectA', 'dataBitsSelectA'],
      ['stopBitsSelectA', 'stopBitsSelectA'],
      ['paritySelectA', 'paritySelectA'],
      ['portSelectB', 'portSelectBSettings'],
      ['baudSelectB', 'baudSelectBSettings'],
      ['dataBitsSelectB', 'dataBitsSelectB'],
      ['stopBitsSelectB', 'stopBitsSelectB'],
      ['paritySelectB', 'paritySelectB'],
    ];
    pairs.forEach(([fromId, toId]) => {
      const from = document.getElementById(fromId);
      const to = document.getElementById(toId);
      if (from && to && from.value) to.value = from.value;
    });
  }
}

function openSettingsPage() {
  // Check if settings tab already exists
  var existingIdx = state.tabs.findIndex(function(t) { return t.type === 'settings'; });
  if (existingIdx >= 0) {
    switchTab(existingIdx);
    return;
  }
  // Create settings tab page
  var page = document.createElement('div');
  page.className = 'tab-page';
  page.style.display = 'none';
  var inner = document.createElement('div');
  inner.className = 'settings-page-inner';
  inner.style.cssText = 'flex:1;min-height:0;';
  inner.appendChild(buildSettingsPage());
  page.appendChild(inner);

  // Hide current page
  var oldPage = getActivePage();
  if (oldPage) oldPage.hide();
  if (state._welcomePage) state._welcomePage.hide();

  var settingsTab = {
    id: state.tabCounter++,
    label: t('settings.title', '设置'),
    type: 'settings',
    portOpen: false,
    mode: 'single',
    source: 'gui',
    _page: { root: page, show: function() { page.style.display = ''; setTimeout(function() { if (typeof renderSettingsThemeGrid === 'function') { renderSettingsThemeGrid(); renderSettingsIconGrid(); } if (typeof refreshSettingsLangList === 'function') { refreshSettingsLangList(); } }, 50); }, hide: function() { page.style.display = 'none'; }, destroy: function() { page.remove(); } }
  };
  state.tabs.push(settingsTab);
  var mc = document.getElementById('mainContent');
  if (mc) mc.appendChild(page);

  state.activeTab = state.tabs.length - 1;
  state.currentMode = 'single';
  // Show settings page
  page.style.display = '';
  setTimeout(function() {
    renderSettingsThemeGrid();
    renderSettingsIconGrid();
  }, 50);
  renderTabs();
  updateOpenBtn();
  updateEmptyState();
  updateSyncIndicator();
  _updateStatsVisible();
  updateDaemonUI();
}
function showAbout() {
  openSettingsPage();
  // Switch to the about panel
  setTimeout(function() {
    var nav = document.querySelector('.settings-nav-item[data-nav="about"]');
    if (nav) nav.click();
  }, 60);
}
function closeSettingsPage() {
  // Close settings tab if active
  var idx = state.tabs.findIndex(function(t) { return t.type === 'settings'; });
  if (idx >= 0) closeTab(idx);
}

function toggleSettings() {
  const overlay = document.getElementById('settingsOverlay');
  if (!overlay) return;
  const wasHidden = overlay.classList.contains('is-hidden');
  if (wasHidden) {
    _syncBarToSettings();
  } else {
    _syncSettingsToBar();
  }
  overlay.classList.toggle('is-hidden');
}

function updateOpenBtn() {
  const btn = pageEl('btnOpen');
  if (!btn) return;
  const tab = getActiveTab();
  // Settings tab has no open/close button
  if (tab && tab.type === 'settings') {
    btn.disabled = true; btn.style.display = 'none';
    var sendBtn = pageEl('btnSend');
    if (sendBtn) sendBtn.disabled = true;
    return;
  }
  btn.style.display = '';
  const isFwd = activeMode() === 'forward';
  if (!state.daemonOnline) {
    btn.disabled = true; btn.textContent = isFwd ? t('btn.start_forward', '启动转发') : t('btn.open_port', '打开串口');
  } else if (tab && tab.portOpen) {
    btn.disabled = false;
    btn.textContent = isFwd ? t('btn.stop_forward', '停止转发') : t('btn.close_port', '关闭串口');
    btn.style.background = 'var(--red)'; btn.style.color = '#fff';
  } else {
    btn.disabled = false;
    btn.textContent = isFwd ? t('btn.start_forward', '启动转发') : t('btn.open_port', '打开串口');
    btn.style.background = ''; btn.style.color = '';
  }
  pageEl('btnSend').disabled = !(tab && tab.portOpen && activeMode() === 'single');
}

// ---- display ----
function toggleDisplayMode() {
  state.displayMode = state.displayMode === 'text' ? 'hex' : 'text';
  const btn = pageEl('btnToggleDisplay');
  if (!btn) return;
  if (state.displayMode === 'hex') {
    btn.textContent = '⇃ ' + t('display.rx_hex_short', 'H');
    btn.title = t('display.switch_to_text', '点击切换至文本显示');
    btn.classList.add('btn-hex');
  } else {
    btn.textContent = '⇃ ' + t('display.rx_text_short', '字');
    btn.title = t('display.switch_to_hex', '点击切换至Hex显示');
    btn.classList.remove('btn-hex');
  }
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
  saveSettings();
}

// ── toolbar horizontal scroll ──
let _sendScrollObserver = null;

function initSendScroll() {
  // Per-tab send scroll is bound via bindSendScroll() when each TabPage is created.
  // At startup there are no tabs, so this is intentionally a no-op.
}

function bindSendScroll(scrollArea) {
  if (!scrollArea) return;
  var wrap = scrollArea.querySelector('.send-scroll-wrap');
  var left = scrollArea.querySelector('.send-scroll-arrow--left');
  var right = scrollArea.querySelector('.send-scroll-arrow--right');
  if (!wrap || !left || !right) return;

  function updateArrows() {
    var canLeft = wrap.scrollLeft > 0;
    var canRight = wrap.scrollLeft < wrap.scrollWidth - wrap.clientWidth - 1;
    left.classList.toggle('is-hidden', !canLeft);
    right.classList.toggle('is-hidden', !canRight);
  }

  wrap.addEventListener('scroll', updateArrows, { passive: true });
  new ResizeObserver(updateArrows).observe(wrap);

  // ── drag to scroll ──
  var dragging = false, startX = 0, startScroll = 0;

  wrap.addEventListener('mousedown', function(e) {
    if (e.button !== 0) return;
    if (e.target.closest('input, select, textarea, button, label')) return;
    dragging = true; startX = e.pageX; startScroll = wrap.scrollLeft;
    wrap.classList.add('is-dragging');
    e.preventDefault();
  });
  window.addEventListener('mousemove', function(e) {
    if (!dragging) return;
    wrap.scrollLeft = startScroll - (e.pageX - startX);
  });
  window.addEventListener('mouseup', function() {
    if (!dragging) return;
    dragging = false;
    wrap.classList.remove('is-dragging');
  });

  updateArrows();
}

function toggleScrollLock() {
  state.scrollLocked = !state.scrollLocked;
  updateScrollLockBtns();
  // If unlocking, scroll to latest data
  if (!state.scrollLocked) {
    const display = pageEl('displayContent');
    if (display) display.scrollTop = display.scrollHeight;
  }
}

function updateScrollLockBtns() {
  const btn = pageEl('btnScrollLock');
  if (!btn) return;
  if (state.scrollLocked) {
    btn.textContent = t('btn.unlock_scroll', '锁定滚动');
    btn.classList.add('is-locked');
    btn.title = t('btn.unlock_scroll', '解锁滚动');
  } else {
    btn.textContent = t('btn.lock_scroll', '自动滚动');
    btn.classList.remove('is-locked');
    btn.title = t('btn.lock_scroll', '自动滚动');
  }
}

function toggleP1Display() {
  state.p1DisplayMode = state.p1DisplayMode === 'text' ? 'hex' : 'text';
  const btn = pageEl('btnP1Display');
  if (btn) {
    if (state.p1DisplayMode === 'hex') {
      btn.textContent = 'P1 H';
      btn.title = t('display.switch_to_text', '点击切换至文本显示');
      btn.classList.add('btn-hex');
    } else {
      btn.textContent = 'P1 文';
      btn.title = t('display.switch_to_hex', '点击切换至Hex显示');
      btn.classList.remove('btn-hex');
    }
  }
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
}

function toggleP2Display() {
  state.p2DisplayMode = state.p2DisplayMode === 'text' ? 'hex' : 'text';
  const btn = pageEl('btnP2Display');
  if (btn) {
    if (state.p2DisplayMode === 'hex') {
      btn.textContent = 'P2 H';
      btn.title = t('display.switch_to_text', '点击切换至文本显示');
      btn.classList.add('btn-hex');
    } else {
      btn.textContent = 'P2 文';
      btn.title = t('display.switch_to_hex', '点击切换至Hex显示');
      btn.classList.remove('btn-hex');
    }
  }
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
}

function toggleSendFormat() {
  const ss = getSendState();
  if (ss.sendFormat === 'text') ss.sendTextBuf = pageEl('sendInput').value;
  else ss.sendHexBuf = pageEl('sendInput').value;
  ss.sendFormat = ss.sendFormat === 'text' ? 'hex' : 'text';
  pageEl('sendInput').value = ss.sendFormat === 'hex' ? ss.sendHexBuf : ss.sendTextBuf;
  _syncSendMirror(pageEl('sendInput'));
  updateSendQuickBtn();
  updateSendInfo();
}

function toggleTxDisplay() {
  state.txDisplayMode = state.txDisplayMode === 'text' ? 'hex' : 'text';
  var btn = pageEl('btnToggleSend');
  if (!btn) return;
  if (state.txDisplayMode === 'hex') {
    btn.textContent = '↾ ' + t('display.tx_hex_short', 'H');
    btn.title = t('display.switch_to_text', '点击切换至文本显示');
    btn.classList.add('btn-hex');
  } else {
    btn.textContent = '↾ ' + t('display.tx_text_short', '字');
    btn.title = t('display.switch_to_hex', '点击切换至Hex显示');
    btn.classList.remove('btn-hex');
  }
  var tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
  saveSettings();
}

function updateSendQuickBtn() {
  var btn = pageEl('btnSendQuick');
  if (!btn) return;
  var ss = getSendState();
  if (ss.sendFormat === 'hex') {
    btn.innerHTML = icon('send', 14, 14) + ' ' + t('send.tx_hex_short', 'H');
    btn.title = t('send.switch_to_text', '点击切换至文本发送');
    btn.classList.add('btn-hex');
  } else {
    btn.innerHTML = icon('send', 14, 14) + ' ' + t('send.tx_text_short', '字');
    btn.title = t('send.switch_to_hex', '点击切换至Hex发送');
    btn.classList.remove('btn-hex');
  }
}

function setStatArrows(isFwd) {
  if (_statusBar) _statusBar.setStatsMode(isFwd);
}

// ---- stats (daemon subscription events) ----
function onStatsCount(data) {
  const tab = getActiveTab();
  if (!tab || !tab.sessionId || data.processId !== tab.sessionId) return;
  const sid = tab.sessionId;
  if (!state.statsBase[sid]) { state.statsBase[sid] = { tx: 0, rx: 0, p1: 0, p2: 0 }; }
  const b = state.statsBase[sid];
  const isFwd = activeMode() === 'forward';
  if (isFwd) {
    b.p1 = data.bytesPort1 || 0;
    b.p2 = data.bytesPort2 || 0;
  } else {
    b.tx = data.bytesWritten || 0;
    b.rx = data.bytesRead || 0;
  }
  if (_statusBar) {
    _statusBar.updateStatsBytes({tx: b.tx, rx: b.rx, p1: b.p1, p2: b.p2});
    _statusBar.setStatsMode(isFwd);
  }
}

function onStatsRate(data) {
  const tab = getActiveTab();
  if (!tab || !tab.sessionId || data.processId !== tab.sessionId) return;
  const isFwd = activeMode() === 'forward';
  if (_statusBar) {
    _statusBar.setStatsMode(isFwd);
    _statusBar.updateStatsRates({
      txRate: data.writeRateBps || 0,
      rxRate: data.readRateBps || 0,
      p1Rate: data.writeRateBps || 0,
      p2Rate: data.readRateBps || 0,
      maxBps: (tab && tab.syncedParams && tab.syncedParams.baud) ? tab.syncedParams.baud / 10 : 0
    });
  }
}

function refreshStatsDOM() {
  const tab = getActiveTab();
  const isFwd = activeMode() === 'forward';
  if (tab && tab.sessionId) {
    if (!state.statsBase[tab.sessionId]) {
      // No local stats yet — fetch from daemon asynchronously
      state.statsBase[tab.sessionId] = { tx: 0, rx: 0, p1: 0, p2: 0 };
      if (state.daemonOnline) {
        window.go.main.App.GetSessionStats(tab.sessionId).then(function(result) {
          if (!result || !result.stats) return;
          const s = result.stats;
          const b = state.statsBase[tab.sessionId];
          if (!b) return;
          b.tx = s.bytesWritten || 0;
          b.rx = s.bytesRead || 0;
          b.p1 = s.bytesPort1 || 0;
          b.p2 = s.bytesPort2 || 0;
          if (getActiveTab() === tab) {
            if (_statusBar) {
              _statusBar.updateStatsBytes({tx: b.tx, rx: b.rx, p1: b.p1, p2: b.p2});
              _statusBar.setStatsMode(isFwd);
            }
          }
        }).catch(function() {});
      }
    }
    const b = state.statsBase[tab.sessionId];
    if (b && _statusBar) {
      _statusBar.updateStatsBytes({tx: b.tx || 0, rx: b.rx || 0, p1: b.p1 || 0, p2: b.p2 || 0});
      _statusBar.setStatsMode(isFwd);
    }
  } else {
    if (_statusBar) _statusBar.resetStats();
  }
}

function fmtBytes(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(3) + 'M';
  if (n >= 1024) return (n / 1024).toFixed(2) + 'k';
  return Math.round(n).toString();
}

function fmtSpeed(bps, tab) {
  let unit = 'B/s';
  let val = bps;
  if (bps >= 1048576) { val = bps / 1048576; unit = 'MB/s'; }
  else if (bps >= 1024) { val = bps / 1024; unit = 'kB/s'; }
  let s = val.toFixed(1) + ' ' + unit;
  let pct = 0;
  let colorClass = '';
  if (tab && tab.syncedParams && tab.syncedParams.baud) {
    const maxBps = tab.syncedParams.baud / 10;
    if (maxBps > 0) {
      pct = Math.round(bps / maxBps * 100);
      if (pct >= 90) colorClass = 'rate-red';
      else if (pct >= 80) colorClass = 'rate-orange';
      s += '(' + pct + '%)';
    }
  } else {
    s += '(0%)';
  }
  return { text: s, cls: colorClass };
}

function toggleHexPrefix() {
  state.hexPrefix = !state.hexPrefix;
  updateHexMenu();
  refreshHexDisplay();
  saveSettings();
}

function toggleHexCase() {
  state.hexCase = state.hexCase === 'upper' ? 'lower' : 'upper';
  updateHexMenu();
  refreshHexDisplay();
  saveSettings();
}

function setHexSep(val) {
  if (state.hexSep === val) return;
  state.hexSep = val;
  updateHexMenu();
  refreshHexDisplay();
  saveSettings();
}

function updateHexMenu() {
  const setChk = (id, on) => {
    const btn = document.getElementById(id);
    if (!btn) return;
    const chk = btn.querySelector('.chk');
    if (chk) chk.classList.toggle('on', on);
  };
  setChk('menuHexPrefix', state.hexPrefix);
  setChk('menuHexLower', state.hexCase === 'lower');
  setChk('menuHexSepN', state.hexSep === 'none');
  setChk('menuHexSepS', state.hexSep === 'space');
  setChk('menuHexSepC', state.hexSep === 'comma');
}

function toggleCrVisible() {
  state.crVisible = !state.crVisible;
  updateTextMenu();
  // Re-render to show/hide CR bubbles
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
  saveSettings();
}

function updateTextMenu() {
  const setChk = (id, on) => {
    const btn = document.getElementById(id);
    if (!btn) return;
    const chk = btn.querySelector('.chk');
    if (chk) chk.classList.toggle('on', on);
  };
  setChk('menuCrVisible', state.crVisible);
  setChk('menuHexEscShow', state.hexEscapeMode === 'show');
  setChk('menuHexEscHide', state.hexEscapeMode === 'hide');
  setChk('menuHexEscRaw', state.hexEscapeMode === 'raw');
}

function setHexEscapeMode(mode) {
  if (state.hexEscapeMode === mode) return;
  state.hexEscapeMode = mode;
  _updateHexEscFmtRow();
  updateTextMenu();
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
  saveSettings();
}

function setHexEscapeFormat(fmt) {
  if (state.hexEscapeFormat === fmt) return;
  state.hexEscapeFormat = fmt;
  const tab = getActiveTab();
  if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
    renderHistoryLines(state.historyCache[tab.sessionId]);
  }
  saveSettings();
}

function _updateHexEscFmtRow() {
  var row = document.getElementById('hexEscFmtRow');
  if (row) row.classList.toggle('is-hidden', state.hexEscapeMode !== 'show');
}

function refreshHexDisplay() {
  // Only refresh if in hex display mode
  if (activeMode() === 'forward') {
    if (state.p1DisplayMode === 'hex' || state.p2DisplayMode === 'hex') {
      const tab = getActiveTab();
      if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
        renderHistoryLines(state.historyCache[tab.sessionId]);
      }
    }
  } else if (state.displayMode === 'hex') {
    const tab = getActiveTab();
    if (tab && tab.sessionId && state.historyCache[tab.sessionId]) {
      renderHistoryLines(state.historyCache[tab.sessionId]);
    }
  }
}

// ---- threads ----
async function viewThreads() {
  if (!state.daemonOnline) { showAlert(t('confirm.title','提示'), t('cli.daemon_not_running','守护进程未运行')); return; }
  try {
    const data = await window.go.main.App.GetThreads();
    const sessions = data.sessions || [];
    const lines = [`goroutines: ${data.goroutines}`, `活动会话: ${sessions.length}`, ''];
    if (sessions.length > 0) {
      sessions.forEach((s, i) => {
        const src = state.tabs.find(t => t.sessionId === s.id);
        const tag = '';
        lines.push(`[${i+1}]${tag} ${s.portName} @ ${s.baud} | 数据位:${s.dataBits} 停止位:${s.stopBits} 校验:${s.parity} (id:${s.id})`);
      });
    } else {
      lines.push('无活动会话');
    }
    showAlert(t('menu.view_threads','线程信息'), lines.join('\n'));
  } catch {
    showAlert(t('cli.error','错误'), t('serial.get_fail','获取失败'));
  }
}

// ---- client details ----
let stateClients = [];

function onClientsChanged(data) {
  if (data && data.clients) {
    stateClients = data.clients;
  }
  updateSyncIndicator();
}

async function viewClients() {
  // Use pushed data if available, otherwise fetch
  let clients = stateClients;
  if ((!clients || clients.length === 0) && state.daemonOnline) {
    try { clients = await window.go.main.App.GetClients(); } catch { clients = []; }
  }
  if (!clients || clients.length === 0) {
    showAlert(t('confirm.title','提示'), t('cli.no_connected','没有已连接的进程'));
    return;
  }
  const lines = [`已连接客户端: ${clients.length}`, ''];
  clients.forEach((c, i) => {
    const src = c.source === 'gui' ? 'GUI' : c.source === 'cli' ? 'CLI' : c.source === 'mcp' ? 'MCP' : c.source;
    const subs = (c.subs || []).join(', ');
    lines.push(`[${i + 1}] ${src} | ${c.clientId}`);
    lines.push(`    PID: ${c.pid} | 请求数: ${c.reqCount} | 连接时间: ${c.connectTime}`);
    lines.push(`    订阅: ${subs || '(无)'}`);
  });
  showAlert(t('menu.view_clients','已连接客户端'), lines.join('\n'));
}

// ---- send ----
function onAutoToggle() {
  var tab = getActiveTab();
  if (tab && tab._autoSendActive) {
    if (tab.sessionId) window.go.main.App.StopAutoSend(tab.sessionId).catch(() => {});
    tab._autoSendActive = false;
    _setAutoSendBtn(false);
    return;
  }
  if (!tab || !tab.portOpen || !tab.sessionId) return;
  startAutoSend();
}

function onIntervalChange() {
  const tab = getActiveTab();
  if (!tab || !tab._autoSendActive) return;
  const intervalMs = Math.max(1, parseInt(pageEl('sendInterval').value) || 1000);
  window.go.main.App.SetAutoSendInterval(tab.sessionId, intervalMs).catch(() => {});
}

async function startAutoSend() {
  const tab = getActiveTab();
  if (!tab || !tab.portOpen || !tab.sessionId) return;
  const ss = getSendState();

  const input = pageEl('sendInput');
  let data = input.value;
  if (data === '') return;

  let fmt = ss.sendFormat;
  if (fmt === 'hex') {
    data = data.replace(/\s/g, '');
    const ab = getAppendBytes('hex');
    if (ab.hex) data += ab.hex;
  } else {
    const ab = getAppendBytes('text');
    if (ab.text) data += ab.text;
    data = await encodeTextForSend(data);
    fmt = 'hex';
  }

  const intervalMs = ss.sendInterval;
  try {
    await window.go.main.App.StartAutoSend(tab.sessionId, data, fmt, intervalMs, 'single', '\n');
    tab._autoSendActive = true;
    _setAutoSendBtn(true);
  } catch (e) {
    _setAutoSendBtn(false);
    showAlert(t('cli.error','错误'), t('serial.start_fail','启动失败: ') + e);
  }
}

function _setAutoSendBtn(active) {
  var btn = pageEl('autoSend');
  if (!btn) return;
  if (active) {
    btn.classList.add('is-active');
    btn.textContent = t('send.auto_stop', '停止循环');
  } else {
    btn.classList.remove('is-active');
    btn.textContent = t('send.auto_send', '自动发送');
  }
}
function updateAutoSendUI() {
  var tab = getActiveTab();
  _setAutoSendBtn(!!(tab && tab._autoSendActive));
}

let _autoSendDebounce = null;

function getAppendBytes(fmt) {
	const sel = pageEl('appendSelect');
	const val = sel ? sel.value : '';
	if (!val) return { text: '', hex: '' };
	if (val === 'cr') return { text: '\r', hex: '0D' };
	if (val === 'lf') return { text: '\n', hex: '0A' };
	return { text: '\r\n', hex: '0D0A' };
}

function updateSendInfo() {
	const el = pageEl('sendInfo');
	if (!el) return;
	const input = pageEl('sendInput');
	const ss = getSendState();
	let text = input ? input.value : '';
	if (!text) { el.textContent = ''; return; }
	let byteSize;
	if (ss.sendFormat === 'hex') {
		text = text.replace(/\s/g, '');
		byteSize = Math.floor(text.length / 2);
	} else {
		byteSize = getByteSizeForText(text);
	}
	const baud = parseInt(pageEl('baudSelect').value) || 115200;
	const ms = byteSize * 10 / baud * 1000;
	let timeStr;
	if (ms >= 1000) timeStr = (ms / 1000).toFixed(2) + 's';
	else if (ms >= 1) timeStr = ms.toFixed(1) + 'ms';
	else timeStr = (ms * 1000).toFixed(0) + 'µs';
		var sizeStr;
		if (byteSize >= 1000000) sizeStr = (byteSize / 1000000).toFixed(1) + 'MB';
		else if (byteSize >= 1000) sizeStr = (byteSize / 1000).toFixed(2) + 'kB';
		else sizeStr = byteSize + 'B';
		el.textContent = sizeStr + '  (≈' + timeStr + ')';

	// When auto-send is active, write changed input to ring so next tick uses new data
	const tab = getActiveTab();
	if (tab && tab._autoSendActive && tab.sessionId) {
			_autoSendDebounce = setTimeout(async () => {
				const cur = pageEl("sendInput");
				if (!cur || !tab._autoSendActive) return;
				let data2 = cur.value;
				if (!data2) return;
				let fmt2 = ss.sendFormat;
				if (fmt2 === "hex") {
					data2 = data2.replace(/0x/gi, "").replace(/[\s,]+/g, "");
					const ab3 = getAppendBytes("hex");
					if (ab3.hex) data2 += ab3.hex;
				} else {
					const ab3 = getAppendBytes("text");
					if (ab3.text) data2 += ab3.text;
					data2 = await encodeTextForSend(data2);
					fmt2 = "hex";
				}
				window.go.main.App.WriteSendRing(tab.sessionId, data2, fmt2).catch(() => {});
			}, 300);
	}
}

async function sendData() {
  const tab = getActiveTab();
  if (!tab || !tab.portOpen || !tab.sessionId) { showAlert(t('confirm.title','提示'), t('serial.open_first','请先打开串口')); return; }
  const ss = getSendState();

  const input = pageEl('sendInput');
  let data = input.value;
  if (data === '') return;

  let fmt = ss.sendFormat;
  if (fmt === 'hex') {
    data = data.replace(/0x/gi, '').replace(/[\s,]+/g, '');
    const ab2 = getAppendBytes('hex');
    if (ab2.hex) data += ab2.hex;
  } else {
    const ab2 = getAppendBytes('text');
    if (ab2.text) data += ab2.text;
    data = await encodeTextForSend(data);
    fmt = 'hex';
  }

  try {
    const running = (tab._autoSendActive === true);
    if (running) {
      await window.go.main.App.WriteSendRing(tab.sessionId, data, fmt);
    } else {
      await window.go.main.App.SendDataShm(tab.sessionId, data, fmt);
    }
  } catch {}
}

function openDevTools() {
  window.go.main.App.OpenDevTools();
}

// ---- quick panel presets ----
function quickPanelAdd() {
  state.quickPresets.push({ enabled: false, content: '', note: '', hex: false, delay: 1000 });
  quickPanelRender();
  saveSettings();
}
function quickPanelDelete(idx) {
  state.quickPresets.splice(idx, 1);
  quickPanelRender();
  saveSettings();
}
function quickPanelClearAll() {
  showConfirm(t('quick_panel.clear_all_title', '清空全部'), t('quick_panel.clear_all_msg', '确定要清空所有预设条目吗？此操作不可恢复。'), function() {
    state.quickPresets = [];
    quickPanelRender();
    saveSettings();
  }, 'confirm.ok_clear', 'confirm.cancel_clear');
}
var _qpDragSide = 'above';

function qpDragStart(e, idx) {
  e.dataTransfer.effectAllowed = 'move';
  e.dataTransfer.setData('text/plain', String(idx));
}

function qpDragEnd(e) {
  var page = getActivePage();
  var rows = page && page.quickPanel ? page.quickPanel.querySelector('.qp-rows') : null;
  if (!rows) return;
  rows.querySelectorAll('.qp-gap-before,.qp-gap-after').forEach(function(el) { el.classList.remove('qp-gap-before','qp-gap-after'); });
}

function qpDragOver(e) {
  e.preventDefault();
  e.dataTransfer.dropEffect = 'move';
  var row = e.currentTarget;
  var rect = row.getBoundingClientRect();
  var pct = (e.clientY - rect.top) / rect.height;
  // Remove old gap classes first
  var rows = (getActivePage()&&getActivePage().quickPanel?getActivePage().quickPanel.querySelector('.qp-rows'):null);
  if (rows) rows.querySelectorAll('.qp-gap-before,.qp-gap-after').forEach(function(el) { el.classList.remove('qp-gap-before','qp-gap-after'); });
  if (pct < 0.5) {
    row.classList.add('qp-gap-before');
    _qpDragSide = 'above';
  } else {
    row.classList.add('qp-gap-after');
    _qpDragSide = 'below';
  }
}

function qpDragLeave(e) {
  e.currentTarget.classList.remove('qp-gap-before','qp-gap-after');
}

function qpDrop(e, toIdx) {
  e.preventDefault();
  e.currentTarget.classList.remove('qp-gap-before','qp-gap-after');
  var fromIdx = parseInt(e.dataTransfer.getData('text/plain'));
  if (isNaN(fromIdx)) return;
  var rect = e.currentTarget.getBoundingClientRect();
  var pct = (e.clientY - rect.top) / rect.height;
  var gap = (pct >= 0.5) ? toIdx + 1 : toIdx;
  if (fromIdx === gap || fromIdx + 1 === gap) return;
  var moved = state.quickPresets.splice(fromIdx, 1)[0];
  var insertAt = (fromIdx < gap) ? gap - 1 : gap;
  state.quickPresets.splice(insertAt, 0, moved);
  quickPanelRender();
  saveSettings();
}
function quickPanelSend(idx) {
  var preset = state.quickPresets[idx];
  if (!preset || !preset.enabled || !preset.content) return;
  var tab = getActiveTab();
  if (!tab || !tab.sessionId) return;
  // Write single entry to sendq then trigger
  var entry = [{enabled: true, hex: preset.hex, content: preset.content, delay: preset.delay || 1000, note: preset.note || ''}];
  window.go.main.App.WriteMultistrEntries(tab.sessionId, JSON.stringify(entry)).then(function() {
    return window.go.main.App.TriggerSend(tab.sessionId);
  }).catch(function(){});
}
function quickPanelUpdate(idx, field, value) {
  if (idx >= 0 && idx < state.quickPresets.length) {
    state.quickPresets[idx][field] = value;
    if (field === 'enabled') {
      var rows = (getActivePage()&&getActivePage().quickPanel?getActivePage().quickPanel.querySelector('.qp-rows'):null);
      if (rows) {
        var row = rows.querySelector('.qp-row[data-idx="' + idx + '"]');
        if (row) {
          var btn = row.querySelector('.qp-btn-send');
          if (btn) btn.disabled = !value;
        }
      }
    }
    // If daemon is looping, sync changed entries to sendq (debounced)
    if (state._qpCycleActive) {
      clearTimeout(_syncQuickPresetsTimer);
      _syncQuickPresetsTimer = setTimeout(function() {
        var tab = getActiveTab();
        if (tab && tab.sessionId && state._qpCycleActive) {
          syncQuickPresetsToDaemon(tab.sessionId);
        }
      }, 200);
    }
    saveSettings();
  }
}
function quickPanelRender() {
  var page = getActivePage();
  if (!page || !page.quickPanel) return;
  var rows = page.quickPanel.querySelector('.qp-rows');
  if (!rows) return;
  var presets = state.quickPresets;
  if (!presets || presets.length === 0) {
    rows.innerHTML = '<div class="qp-empty">' + t('quick_panel.empty', '暂无预设，点击上方「+ 添加」新建') + '</div>';
    return;
  }
  var h = '';
  for (var i = 0; i < presets.length; i++) {
    var p = presets[i];
    var en = p.enabled !== false ? ' checked' : '';
    var hexChk = p.hex ? ' checked' : '';
    var cnt = escHtml(p.content || '');
    var sendDisabled = p.enabled === false ? ' disabled' : '';
    var note = escHtml(p.note || '');
    var dly = (p.delay != null) ? p.delay : 1000;
    var noteCls = p.note ? ' qp-note has-content' : ' qp-note';
    h += '<div class="qp-row" data-idx="' + i + '"' +
      ' ondragover="qpDragOver(event)"' +
      ' ondragleave="qpDragLeave(event)"' +
      ' ondrop="qpDrop(event,' + i + ')">' +
      '<span class="qp-drag" draggable="true" title="' + t('quick_panel.drag', '拖动排序') + '"' +
      ' ondragstart="qpDragStart(event,' + i + ')"' +
      ' ondragend="qpDragEnd(event)">⋮⋮</span>' +
      '<input type="checkbox" id="qp-enable-' + i + '" name="qp-enable-' + i + '"' + en + ' onchange="quickPanelUpdate(' + i + ',&quot;enabled&quot;,this.checked)">' +
      '<input type="checkbox" id="qp-hex-' + i + '" name="qp-hex-' + i + '"' + hexChk + ' onchange="quickPanelUpdate(' + i + ',&quot;hex&quot;,this.checked)">' +
      '<input type="text" id="qp-content-' + i + '" name="qp-content-' + i + '" value="' + cnt + '" onchange="quickPanelUpdate(' + i + ',&quot;content&quot;,this.value)" placeholder="' + t('quick_panel.content_placeholder', '请输入内容') + '">' +
      '<span class="qp-delay-wrap"><input type="number" id="qp-delay-' + i + '" name="qp-delay-' + i + '" value="' + dly + '" min="1" size="4" onchange="quickPanelUpdate(' + i + ',&quot;delay&quot;,Math.max(1,parseInt(this.value)||1))"></span>' +
      '<button class="qp-btn-send"' + sendDisabled + ' onclick="quickPanelSend(' + i + ')" title="' + t('quick_panel.send', '发送') + '">' + t('quick_panel.send', '发送') + '</button>' +
      '<button class="qp-btn-del" onclick="quickPanelDelete(' + i + ')" title="' + t('quick_panel.delete', '删除') + '">' + icon('close', 16, 16) + '</button>' +
      '<input type="text" class="' + noteCls + '" id="qp-note-' + i + '" name="qp-note-' + i + '" value="' + note + '" onchange="quickPanelUpdate(' + i + ',&quot;note&quot;,this.value)" placeholder="' + t('quick_panel.note_placeholder', '点击添加备注') + '">' +
      '</div>';
      if (i < presets.length - 1) h += '<div class="qp-divider"></div>';
  }
  rows.innerHTML = h;

  // Debounced sync to daemon sendq
  clearTimeout(_syncQuickPresetsTimer);
  _syncQuickPresetsTimer = setTimeout(function() {
    var tab = getActiveTab();
    if (tab && tab.sessionId) {
      syncQuickPresetsToDaemon(tab.sessionId);
    }
  }, 300);
}

function qpEnableAll() {
  state.quickPresets.forEach(function(p) { p.enabled = true; });
  quickPanelRender();
  saveSettings();
}
function qpDisableAll() {
  state.quickPresets.forEach(function(p) { p.enabled = false; });
  quickPanelRender();
  saveSettings();
}
function qpSendEnabled() {
  var list = state.quickPresets;
  if (!list.length) return;
  var tab = getActiveTab();
  if (!tab || !tab.sessionId) return;
  // Sync entries to sendq, then start one-shot queue send
  syncQuickPresetsToDaemon(tab.sessionId, function() {
    window.go.main.App.StartQueueSend(tab.sessionId, false, 0).catch(function(){});
  });
}
function qpCycleToggle() {
  var page2 = getActivePage();
  var btn = page2 && page2.quickPanel ? page2.quickPanel.querySelector('#qpCycleSend') : null;
  if (!btn) return;
  var tab = getActiveTab();
  if (!tab || !tab.sessionId) return;
  if (state._qpCycleActive) {
    window.go.main.App.StopAutoSend(tab.sessionId).catch(function(){});
    _setQpCycleBtn(false);
    state._qpCycleActive = false;
  } else {
    var ci = parseInt(document.getElementById('qpCycleInterval').value) || 1000;
    syncQuickPresetsToDaemon(tab.sessionId, function() {
      window.go.main.App.StartQueueSend(tab.sessionId, true, ci).catch(function(){});
    });
    _setQpCycleBtn(true);
    state._qpCycleActive = true;
  }
}

function _setQpCycleBtn(active) {
  var page2 = getActivePage();
  var btn = page2 && page2.quickPanel ? page2.quickPanel.querySelector('#qpCycleSend') : null;
  if (!btn) return;
  if (active) {
    btn.classList.add('is-active');
    btn.textContent = t('quick_panel.cycle_stop', '停止循环');
  } else {
    btn.classList.remove('is-active');
    btn.textContent = t('quick_panel.cycle_send', '循环发送');
  }
}
function qpImport() {
  var input = document.createElement('input');
  input.type = 'file'; input.accept = '.json';
  input.onchange = function() {
    var file = input.files[0];
    if (!file) return;
    var reader = new FileReader();
    reader.onload = function() {
      try {
        var data = JSON.parse(reader.result);
        if (Array.isArray(data)) { state.quickPresets = data; quickPanelRender(); saveSettings(); }
      } catch(e) { showAlert(t('quick_panel.import_error_title', '导入失败'), t('quick_panel.import_error_msg', '文件格式不正确')); }
    };
    reader.readAsText(file);
  };
  input.click();
}
function qpExport() {
  var blob = new Blob([JSON.stringify(state.quickPresets, null, 2)], {type: 'application/json'});
  var a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = 'quick_presets.json';
  a.click();
  URL.revokeObjectURL(a.href);
}
// Rebuild a tab's label from its stored data (for i18n language switch).
function _rebuildTabLabel(tab) {
  if (!tab.syncedParams) return;
  var sp = tab.syncedParams;
  var isConnected = tab.portOpen;
  var isForward = tab.mode === 'forward';
  var hasDeclared = !!sp.portName;
  if (isForward) {
    tab.label = isConnected ? sp.portName : (hasDeclared ? `${sp.portName} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_forward', '端口转发空闲'));
  } else {
    tab.label = isConnected ? `${sp.portName} @ ${sp.baud}` : (hasDeclared ? `${sp.portName} @ ${sp.baud} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_single', '单端口空闲'));
  }
}

function qpRestoreDefaults() {
  showConfirm(t('quick_panel.restore_defaults_title', '恢复默认'), t('quick_panel.restore_defaults_msg', '确定要恢复默认设置吗？当前所有预设将被清空。'), function() {
    state.quickPresets = [];
    quickPanelRender();
    saveSettings();
  }, 'confirm.ok_restore', 'confirm.cancel_restore');
}

// Sync state.quickPresets to daemon sendq, then call callback.
function syncQuickPresetsToDaemon(processId, cb) {
  var entries = state.quickPresets.map(function(p) {
    return {
      enabled: p.enabled !== false,
      hex: !!p.hex,
      content: p.content || '',
      delay: p.delay || 1000,
      note: p.note || ''
    };
  });
  window.go.main.App.WriteMultistrEntries(processId, JSON.stringify(entries)).then(function() {
    if (cb) cb();
  }).catch(function(){});
}

// Handle multistr-changed events from daemon.
function onMultistrChanged(params) {
  // Update toolbar loop button state
  if (params.state === 'looping') {
    _setQpCycleBtn(true);
    state._qpCycleActive = true;
  } else if (params.state === 'idle') {
    _setQpCycleBtn(false);
    state._qpCycleActive = false;
  }
  // Update round count display if visible
  var roundEl = document.getElementById('qpRoundCount');
  if (roundEl && params.roundCount !== undefined) {
    roundEl.textContent = params.roundCount;
  }
}

function toggleQuickPanel() {
  if (state.quickPanelRatio > 0.01) {
    state.quickPanelRatio = 0;
  } else {
    state.quickPanelRatio = 0.25;
  }
  applyQuickPanelRatio();
  updateQuickPanelCheck();
  saveSettings();
}

function updateQuickPanelCheck() {
  const btn = document.getElementById('menuQuickPanel');
  if (!btn) return;
  const chk = btn.querySelector('.chk');
  if (chk) {
    if (state.quickPanelRatio > 0.01) chk.classList.add('on');
    else chk.classList.remove('on');
  }
}

async function toggleAutoSaveHistory() {
  const cur = await goGetHistoryStatus();
  const next = !cur;
  try {
    await window.go.main.App.SetHistoryEnabled(next);
    updateAutoSaveCheck(next);
  } catch (e) { /* ignore */ }
}

async function goGetHistoryStatus() {
  try {
    return await window.go.main.App.GetHistoryStatus();
  } catch (e) { return true; }
}

function updateAutoSaveCheck(on) {
  const btn = document.getElementById('menuAutoSaveHistory');
  if (!btn) return;
  const chk = btn.querySelector('.chk');
  if (chk) {
    if (on) chk.classList.add('on');
    else chk.classList.remove('on');
  }
}

function toggleAutoCreateSession() {
  state.autoCreateSession = !state.autoCreateSession;
  updateAutoCreateCheck(state.autoCreateSession);
  saveSettings();
}

function updateAutoCreateCheck(on) {
  const btn = document.getElementById('menuAutoCreateSession');
  if (!btn) return;
  const chk = btn.querySelector('.chk');
  if (chk) {
    if (on) chk.classList.add('on');
    else chk.classList.remove('on');
  }
}

function updateEmptyState() {
  // Count real (non-welcome) tabs
  var realTabs = state.tabs.filter(function(t) { return !(t._page && t._page.isWelcome); });
  var hasRealTabs = realTabs.length > 0;
  if (_statusBar) _statusBar.updateDaemon({online: state.daemonOnline, tabsCount: realTabs.length});
  // Show/hide welcome page
  if (state._welcomePage) {
    if (hasRealTabs) {
      state._welcomePage.hide();
    } else {
      state._welcomePage.show();
    }
  }
  _updateStatsVisible();
}

function quitApp() {
  if (confirm(t('confirm.quit_msg', 'Are you sure you want to quit?'))) {
    try { window.runtime.Quit(); } catch {}
  }
}

// ---- quick panel drag (event delegation on #mainContent) ----
var _qpDragState = null;
function initQuickPanelDrag() {
	applyQuickPanelRatio();
	var mc = document.getElementById('mainContent');
	if (!mc) return;
	mc.addEventListener('mousedown', function(e) {
		var qd = e.target.closest('.o-divider--quick');
		if (!qd) return;
		var page = qd.closest('.tab-page');
		if (!page) return;
		_qpDragState = { page: page, qd: qd };
		qd.classList.add('is-active');
		document.body.style.cursor = 'ew-resize';
		document.body.style.userSelect = 'none';
		e.preventDefault();
	});
	document.addEventListener('mousemove', function(e) {
		if (!_qpDragState) return;
		var cr = _qpDragState.page.querySelector('.content-row');
		if (!cr) return;
		var crRect = cr.getBoundingClientRect();
		var isRTL = document.documentElement.dir === 'rtl';
		var panelW = isRTL ? (e.clientX - crRect.left) : (crRect.right - e.clientX);
		var avail = crRect.width - 5;
		var ratio = avail > 0 ? Math.max(0, Math.min(0.8, panelW / avail)) : 0;
		state.quickPanelRatio = ratio;
		applyQuickPanelRatio();
	});
	document.addEventListener('mouseup', function() {
		if (!_qpDragState) return;
		_qpDragState.qd.classList.remove('is-active');
		_qpDragState = null;
		document.body.style.cursor = '';
		document.body.style.userSelect = '';
		saveSettings();
	});
	window.addEventListener('resize', function() { applyQuickPanelRatio(); });
}

function applyQuickPanelRatio() {
	var page = getActivePage();
	if (!page) return;
	var leftArea = page.leftArea;
	var quickPanel = page.quickPanel;
	var qDivider = page.quickDivider;
	if (!leftArea || !quickPanel || !qDivider) return;
	if (activeMode() === 'forward') {
		leftArea.style.flex = '1 1 0px';
		quickPanel.style.display = 'none';
		qDivider.style.display = 'none';
		return;
	}
	var r = state.quickPanelRatio || 0;
	leftArea.style.flex = (1 - r) + ' 1 0px';
	quickPanel.style.flex = r + ' 1 0px';
	qDivider.style.display = '';
	if (r <= 0.01) {
		quickPanel.style.display = 'none';
	} else {
		quickPanel.style.display = '';
	}
	updateQuickPanelCheck();
}

// ---- divider drag (event delegation on #mainContent) ----

function initHistoryScroll() {
  // Scroll-to-expand removed — all history is always rendered
}

var _divDragState = null;
function initDividerDrag() {
  applySendRatio();
  var mc = document.getElementById('mainContent');
  if (!mc) return;
  mc.addEventListener('mousedown', function(e) {
    var div = e.target.closest('.o-divider');
    if (!div || div.classList.contains('o-divider--quick')) return;
    var page = div.closest('.tab-page');
    if (!page) return;
    _divDragState = { page: page, div: div };
    div.classList.add('is-active');
    document.body.style.cursor = 'ns-resize';
    document.body.style.userSelect = 'none';
    e.preventDefault();
  });
  document.addEventListener('mousemove', function(e) {
    if (!_divDragState) return;
    var appRect = document.getElementById('app').getBoundingClientRect();
    var topH = 26 + 36 + 40;
    var bottomH = 24;
    var dividerH = 5;
    var yMin = topH;
    var yMax = appRect.height - bottomH - dividerH;
    var avail = yMax - yMin;
    var clampedY = Math.max(yMin, Math.min(yMax, e.clientY - appRect.top));
    var sendH = yMax - clampedY;
    state.sendRatio = avail > 0 ? sendH / avail : 0.3;
    applySendRatio();
  });
  document.addEventListener('mouseup', function() {
    if (!_divDragState) return;
    _divDragState.div.classList.remove('is-active');
    _divDragState = null;
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
    saveSettings();
  });
  window.addEventListener('resize', function() { applySendRatio(); });
}

function applySendRatio() {
  var page = getActivePage();
  if (!page) return;
  var displayArea = page.displayArea;
  var sendArea = page.sendArea;
  var divider = page.divider;
  if (!displayArea || !sendArea) return;
  if (activeMode() === 'forward') {
    displayArea.style.flex = '1 1 0px';
    displayArea.style.minHeight = '';
    displayArea.style.overflow = '';
    sendArea.style.display = 'none';
    if (divider) divider.style.display = 'none';
    return;
  }
  var r = state.sendRatio;
  displayArea.style.flex = (1 - r) + ' 1 0px';
  displayArea.style.minHeight = '40px';
  displayArea.style.overflow = r >= 0.97 ? 'is-hidden' : '';
  sendArea.style.display = '';
  sendArea.style.flex = r + ' 1 0px';
  sendArea.style.minHeight = '0px';
  sendArea.style.overflow = r <= 0.01 ? 'is-hidden' : '';
  if (divider) divider.style.display = '';
}
