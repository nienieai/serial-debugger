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
  stlogShowYear: true,   // .stlog 文件页行时间戳是否带年份（设置持久化）
  p1DisplayMode: 'text',
  p2DisplayMode: 'text',
  currentMode: 'single',
  lastPorts: '',
  knownDaemonSessions: {},
  // 正在等会话页的工程路径（session.open 之后、syncDaemonSessions 建页之前）
  _pendingProjectPaths: {},
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
  // 历史渲染窗口行数：null = 按视口推导（推荐）；数字 = 设置里手动指定
  historyWindowRows: null,
  // 往上翻到顶时自动回补更早的历史（缓存 → 守护进程环 → 磁盘提示）。
  // 关掉后只摆一个「↑ 向上滚动加载更多」的提示，由用户点了才取。
  autoRefillHistory: true,
  statsBase: {},
  autoCreateSession: false,
  // 「默认自动保存历史」：默认关闭，只有用户显式打开才落盘。
  autoSaveHistory: false,
  // COM 口 → 设备描述（「USB-Enhanced-SERIAL CH343」这种）。端口下拉用完就丢，
  // 标签浮窗要显示它，所以在 updatePortList 里留一份。
  portDescs: {},

  // ── 标签栏外观（设置 → 标签页；全部持久化到 gui.ini）──
  tabTwoLines: false,        // 双行标签：高度 ×1.5
  tabSubLine: 'a',           // 第二行内容：a 端口·参数 / b 参数·速率 / c 保存·客户端
  tabShowClients: true,      // 显示客户端数量
  tabClientStyle: 'source',  // source 分来源彩色（0.8.3 原样）/ total 只显示总数
  tabShowDot: true,          // 图标右下角的状态点
  tabNameMax: 170,           // 线程名最大宽度（px）
  tabCollapse: 'sub',        // 拥挤时先牺牲谁：sub 先收第二行 / clients 先收数字
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
      stlogShowYear: state.stlogShowYear !== false,
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
      autoSaveHistory: state.autoSaveHistory === true,
      tabTwoLines: state.tabTwoLines === true,
      tabSubLine: state.tabSubLine || 'a',
      tabShowClients: state.tabShowClients !== false,
      tabClientStyle: state.tabClientStyle || 'source',
      tabShowDot: state.tabShowDot !== false,
      tabNameMax: state.tabNameMax || 170,
      tabCollapse: state.tabCollapse || 'sub',
      historyWindowRows: state.historyWindowRows,
      autoRefillHistory: state.autoRefillHistory,
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
      // 「默认自动保存历史」：默认关。这里只读本地设置，连上守护进程后再推给它。
      state.autoSaveHistory = s.autoSaveHistory === true;
      updateAutoSaveCheck(state.autoSaveHistory);
      if (typeof s.stlogShowYear === 'boolean') state.stlogShowYear = s.stlogShowYear;
      // ── 标签栏外观（设置 → 标签页）──
      // 每一项都先判断类型：gui.ini 可能是旧版本写的，缺键时保持默认值。
      if (typeof s.tabTwoLines === 'boolean') state.tabTwoLines = s.tabTwoLines;
      if (s.tabSubLine === 'a' || s.tabSubLine === 'b' || s.tabSubLine === 'c') state.tabSubLine = s.tabSubLine;
      if (typeof s.tabShowClients === 'boolean') state.tabShowClients = s.tabShowClients;
      if (s.tabClientStyle === 'source' || s.tabClientStyle === 'total') state.tabClientStyle = s.tabClientStyle;
      if (typeof s.tabShowDot === 'boolean') state.tabShowDot = s.tabShowDot;
      if (typeof s.tabNameMax === 'number' && s.tabNameMax >= 120 && s.tabNameMax <= 280) state.tabNameMax = s.tabNameMax;
      if (s.tabCollapse === 'sub' || s.tabCollapse === 'clients') state.tabCollapse = s.tabCollapse;
      if (typeof _applyTabDisplaySettings === 'function') _applyTabDisplaySettings();
      if (s.language && typeof s.language === 'string') state.language = s.language;
      if (typeof s.displayFontFamily === 'string') state.displayFontFamily = s.displayFontFamily;
      if (typeof s.displayFontSize === 'number') state.displayFontSize = s.displayFontSize;
      if (typeof s.tabSize === 'number') state.tabSize = s.tabSize;
      if (typeof s.eolSequence === 'string') state.eolSequence = s.eolSequence;
      // 历史渲染窗口：null/缺失 = 按视口推导
      if (typeof s.historyWindowRows === 'number' && s.historyWindowRows > 0) {
        state.historyWindowRows = s.historyWindowRows;
      } else if (s.historyWindowRows === null) {
        state.historyWindowRows = null;
      }
      if (typeof s.autoRefillHistory === 'boolean') state.autoRefillHistory = s.autoRefillHistory;
      _applyDisplayStyle();
      if (_statusBar) _statusBar.updateEol();
      // Display color overrides
      if (s.displayColors) {
        try { state.displayColors = JSON.parse(s.displayColors); } catch(e) {}
      }
      if (!state.displayColors) state.displayColors = _defaultDisplayColors();
      else _migrateColorDefaults();
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
    ph.textContent = t('send.placeholder', '输入要发送的数据...');
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
    ph.textContent = t('send.placeholder', '输入要发送的数据...');
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
    } else if (ch === '0' && i + 1 < text.length && (text[i+1] === 'x' || text[i+1] === 'X')) {
      // 0x / 0X — push as display token preserving case
      // 边界是 i+1（不是 i+2）：原写成 i+2 < length 时，紧贴末尾的「AA0x」里的
      // 0x 不被当作前缀，整段并进 hex token → 含 x 判非法 → 整段标红（假红灯），
      // 而 _normalizeHexInput 会把 0x 剥掉、实际能发出去（TODO #48）。
      if (cur) { tokens.push(cur); cur = ''; }
      tokens.push(text[i+1] === 'X' ? '0X' : '0x');
      i += 1; // skip the x/X
    } else {
      cur += ch;
    }
  }
  if (cur) tokens.push(cur);
  // Render tokens
  // 注意循环变量不能叫 t：var 会提升到整个函数作用域，把上面的 i18n t() 顶掉，
  // 于是「内容为空 → 显示占位符」那条分支会抛 TypeError: t is not a function，
  // 表现就是切到 Hex 后镜像层不刷新、按钮文案也不更新（旧文本残留）。
  for (var ti = 0; ti < tokens.length; ti++) {
    var tok = tokens[ti];
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
  el.textContent = '.display-content,#sendInput,#sendMirror,.c-color-preview{font-family:"' + state.displayFontFamily + '",monospace;font-size:' + state.displayFontSize + 'px;tab-size:' + state.tabSize + ';}';
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
  _rerenderHistoryKeepingView();
}

// ---- alert helper ----
var _alertCallback = null;

// _resetAlertButtons 把弹窗底部的按钮区恢复成「取消 + 确定」。
//
// showChoice 会临时藏起这两个、塞进几个并列选项；下一个弹窗如果不复原，
// 就会只剩「确定」或者残留上一次的选项按钮。
function _resetAlertButtons() {
  var ov = document.getElementById('alertOverlay');
  if (!ov) return;
  var ok = document.getElementById('alertOkBtn');
  if (ok) ok.classList.remove('is-hidden');
  var btns = ov.querySelector('.alert-btns');
  if (!btns) return;
  Array.prototype.slice.call(btns.querySelectorAll('.alert-choice')).forEach(function (b) { b.remove(); });
}

// okKey / cancelKey: optional i18n keys for context-specific button labels
// mono: 等宽 + 保留换行，用于多行或按列对齐的文本（如 .stlog 摘要）
function showAlert(title, message, okKey, mono) {
  _resetAlertButtons();
  _alertCallback = null;
  document.getElementById('alertTitle').textContent = title;
  var body = document.getElementById('alertBody');
  body.textContent = message;
  body.classList.toggle('t-mono', !!mono);
  document.getElementById('alertCancelBtn').classList.add('is-hidden');
  document.getElementById('alertOkBtn').textContent = t(okKey || 'confirm.ok', '确定');
  document.getElementById('alertOverlay').classList.remove('is-hidden');
}

function showConfirm(title, message, onOk, okKey, cancelKey) {
  _resetAlertButtons();
  _alertCallback = onOk;
  document.getElementById('alertTitle').textContent = title;
  var body = document.getElementById('alertBody');
  body.textContent = message;
  body.classList.remove('t-mono'); // 共用同一个 body 元素，别让上个弹窗的等宽类漏过来
  document.getElementById('alertCancelBtn').classList.remove('is-hidden');
  document.getElementById('alertOkBtn').textContent = t(okKey || 'confirm.ok', '确定');
  document.getElementById('alertCancelBtn').textContent = t(cancelKey || 'confirm.cancel', '取消');
  document.getElementById('alertOverlay').classList.remove('is-hidden');
}

// showChoice 用同一个弹窗摆两个并列选项（不是「确定/取消」）。
//
// 只有「打开工程目录 / 打开 .stlog 文件」用它：拿确认框做二选一的话，取消键
// 就变成了第二个选项，语义是歪的。
function showChoice(title, message, options) {
  _resetAlertButtons();
  _alertCallback = null;
  document.getElementById('alertTitle').textContent = title;
  var body = document.getElementById('alertBody');
  body.textContent = message;
  body.classList.remove('t-mono');
  document.getElementById('alertCancelBtn').classList.add('is-hidden');
  document.getElementById('alertOkBtn').classList.add('is-hidden');
  var ov = document.getElementById('alertOverlay');
  var btns = ov.querySelector('.alert-btns');
  (options || []).forEach(function (opt) {
    var b = document.createElement('button');
    b.className = 'alert-choice';
    b.textContent = opt.label;
    b.addEventListener('click', function () {
      closeAlert(null);
      if (opt.onPick) opt.onPick();
    });
    btns.appendChild(b);
  });
  ov.classList.remove('is-hidden');
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

// 主题变量只写行内 style，缺的键保持不写 —— 于是自动落回 CSS 里的默认值。
// 这里记录"实际写过哪些键"，切换主题时按记录清，不再维护一份硬编码白名单
// （新增变量忘了加进白名单就会残留到下一个主题）。
var _themeAppliedKeys = [];

function clearThemeVars() {
  var root = document.documentElement;
  for (var i = 0; i < _themeAppliedKeys.length; i++) root.style.removeProperty(_themeAppliedKeys[i]);
  _themeAppliedKeys = [];
}

function applyThemeVars(vars) {
  var root = document.documentElement;
  for (var k in vars) {
    var v = vars[k];
    if (v === null || v === undefined || v === '') continue;
    root.style.setProperty(k, String(v));
    if (_themeAppliedKeys.indexOf(k) < 0) _themeAppliedKeys.push(k);
  }
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
    // 内置图标集合由 icons.js 启动时异步拉取；先等它，否则首次 applyIconTheme
    // 叠上去的底是空的，内置图标会整批消失。
    try { await _iconsReady; } catch (e) {}
    var external = await window.go.main.App.ListExternalIconThemes();
    var all = [];
    // 内置图标包：config/icons/<pack>/ 随二进制嵌入，system 包对外仍叫「内置默认」
    var builtinPacks = [];
    try { builtinPacks = await window.go.main.App.ListBuiltinIconThemes() || []; } catch (e) { builtinPacks = []; }
    if (builtinPacks.length) {
      for (var b = 0; b < builtinPacks.length; b++) {
        if (builtinPacks[b].id === 'system') {
          all.push({ id: '', pack: 'system', name: builtinPacks[b].name, _i18nKey: 'theme.icons_default', builtin: true });
        } else {
          all.push({ id: builtinPacks[b].id, pack: builtinPacks[b].id, name: builtinPacks[b].name, builtin: true });
        }
      }
    } else {
      all.push({ id: '', name: '', _i18nKey: 'theme.icons_default', builtin: true });
    }
    if (external) {
      for (var j = 0; j < external.length; j++) {
        all.push({ id: external[j].id, name: external[j].name, builtin: false });
      }
    }
    _iconThemesAll = all;
    state.iconThemes = all;
    var saved = state.iconThemeId;
    if (saved) {
      var _isBuiltin = false;
      for (var bi = 0; bi < all.length; bi++) { if (all[bi].id === saved && all[bi].builtin) { _isBuiltin = true; break; } }
      var ext = _isBuiltin
        ? await window.go.main.App.GetBuiltinIconTheme(saved)
        : await window.go.main.App.GetExternalIconTheme(saved);
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
  var isBuiltin = false;
  var list = state.iconThemes || [];
  for (var i = 0; i < list.length; i++) { if (list[i].id === id && list[i].builtin) { isBuiltin = true; break; } }
  var req = isBuiltin
    ? window.go.main.App.GetBuiltinIconTheme(id)
    : window.go.main.App.GetExternalIconTheme(id);
  req.then(function(ext) {
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
    ctrlFg:  { dark: '#8a8a8a', light: '#bbbbbb', bgDark: T, bgLight: T, bold: false, italic: false },
    stat:    { dark: '#888888', light: '#999999', bgDark: T, bgLight: T, bold: false, italic: false },
    fhexEsc: { dark: '#e91e63', light: '#c2185b', bgDark: T, bgLight: T, bold: false, italic: false },
    fctrlBg: { dark: T, light: T, bgDark: T, bgLight: T, bold: false, italic: false },
    fctrlFg: { dark: '#8a8a8a', light: '#bbbbbb', bgDark: T, bgLight: T, bold: false, italic: false },
    fstat:   { dark: '#888888', light: '#999999', bgDark: T, bgLight: T, bold: false, italic: false },
    // ── 发送框（输入区）独立配色 ──
    // 0.8.4 之前发送框没有自己的键，全是借历史流的：hex 字节吃 --dc-hexTx、
    // 非法字节吃 --dc-hexInv、控制字符吃 --dc-ctrlFg/Bg、普通字符吃 --fg。
    // 于是「改发送框 Hex 颜色」会连带改掉记录区里 Tx 行的 Hex，反过来也一样。
    sendText:   { dark: '#ffffff', light: '#000000', bgDark: T, bgLight: T, bold: false, italic: false },
    sendHex:    { dark: '#f59e0b', light: '#d97706', bgDark: T, bgLight: T, bold: false, italic: false },
    sendHexBad: { dark: '#ff5c5c', light: '#dc2626', bgDark: T, bgLight: T, bold: false, italic: false },
    sendPrefix: { dark: '#808080', light: '#9a9a9a', bgDark: T, bgLight: T, bold: false, italic: false },
    sendCtrlBg: { dark: T, light: T, bgDark: T, bgLight: T, bold: false, italic: false },
    sendCtrl:   { dark: '#8a8a8a', light: '#bbbbbb', bgDark: T, bgLight: T, bold: false, italic: false },
    sendPh:     { dark: '#808080', light: '#9a9a9a', bgDark: T, bgLight: T, bold: false, italic: false }
  };
}

// 0.8.4：控制字符的深色默认值原来是 #cccccc —— 在 #1e1e1e 背景上跟正文白字
// （#ffffff）几乎同亮度，`·` `←↵` `→` 这些标记看着就像普通字符。改暗一档。
// 改默认值只对「没保存过的配置」生效，所以已经写进 gui.ini 的旧值要迁移 ——
// 但只迁移「值仍是旧默认」的那些，用户自己调过的颜色不动。
var _CTRL_FG_OLD_DARK = '#cccccc';
function _migrateColorDefaults() {
  var dc = state.displayColors;
  if (!dc) return;
  var dflt = _defaultDisplayColors();
  ['ctrlFg', 'fctrlFg', 'sendCtrl'].forEach(function(k) {
    var v = dc[k];
    if (v && typeof v.dark === 'string' && v.dark.toLowerCase() === _CTRL_FG_OLD_DARK) {
      v.dark = dflt[k].dark;
    }
  });
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
  // 提示文字画在镜像层里（textarea 的 ::placeholder 是透明的），切语言后要重绘
  if (si) _syncSendMirror(si);
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
  _relabelIdleTabs();
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

function updateModeUI() {
  var activeTab = getActiveTab();
  // 设置页 / 新建选择页都没有端口 UI：直接返回，避免 pageEl 走到全局回退
  // 去操作第一个标签页的元素（见 TODO #64 / #68）。
  if (activeTab && !isSessionTab(activeTab)) return;

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

// 下拉内容签名：值 + 显示文字 + option 上的类（占用高亮）+ 当前选中项。
// 只用来判断「要不要重建」，所以用拼接字符串即可，N 只有几个到几十个。
function _csOptionsSig(sel) {
  var parts = [String(sel.selectedIndex)];
  for (var i = 0; i < sel.options.length; i++) {
    var o = sel.options[i];
    parts.push(o.value + '\u0001' + (o.textContent || o.text || '') + '\u0001' + o.className);
  }
  return parts.join('\u0002');
}

// 刷新自定义下拉。
//
// 原实现每次调用都重拼 dd.innerHTML 并给每个 .cs-option 重新绑 mousedown，
// 而这个函数在每个 <select> 上挂着 MutationObserver（childList+subtree+attributes），
// 于是「切标签 / 开完串口 / 端口列表刷新」这类路径会放大成 3×N 次整表重建
// （TODO #79 ①）。现在：① 内容签名没变就直接返回（只更新触发器文字）；
// ② 点击用事件委托，一次绑定，重建后不用再绑。
function _csRefresh(wrap) {
  var sel = wrap.querySelector('select');
  var trigText = wrap.querySelector('.cs-text');
  if (!sel || !trigText) return;
  var dd = wrap.querySelector('.cs-dropdown');
  // 下拉打开时会被 portal 到 body；只有它确实属于这个 wrap 才认
  if (!dd && _csOpen === wrap) dd = document.querySelector('.cs-dropdown.is-portalled');
  var sig = _csOptionsSig(sel);
  if (dd && dd._csSig !== sig) {
    var html = '';
    for (var i = 0; i < sel.options.length; i++) {
      var opt = sel.options[i];
      var cls = 'cs-option';
      if (i === sel.selectedIndex) cls += ' is-selected';
      if (opt.className) cls += ' ' + opt.className;
      html += '<div class="' + cls + '" data-val="' + escHtml(opt.value) + '">' + escHtml(opt.textContent || opt.text) + '</div>';
    }
    dd.innerHTML = html;
    dd._csSig = sig;
  }
  if (sel.selectedIndex >= 0) {
    trigText.textContent = sel.options[sel.selectedIndex].textContent || sel.options[sel.selectedIndex].text;
  }
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
  // 选中项走事件委托：下拉重建（dd.innerHTML=…）之后不用再逐项绑 mousedown，
  // 监听器挂在 dd 上跟着元素走（portal 到 body 也照样生效）。
  dd.addEventListener('mousedown', function(e) {
    var optEl = e.target && e.target.closest ? e.target.closest('.cs-option') : null;
    if (!optEl || !dd.contains(optEl)) return;
    e.preventDefault();
    sel.value = optEl.dataset.val;
    sel.dispatchEvent(new Event('change', { bubbles: true }));
    _csClose(wrap);
  });

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

// ---- Tab drag：pointer 事件自实现（拖动时实时补位）----
//
// 为什么不用 HTML5 拖放：dragstart 里把源元素改成 position:absolute / display:none，
// 浏览器会判定「拖动源消失」直接把这次拖动取消掉 —— 表现就是按住标签拖不动。
// pointer 事件全程可控：
//   1. 按住标签、移动超过 4px 才算拖动，否则照常点击切换
//   2. 拖动开始：**原标签自己**改成 position:fixed + 压暗，在标签栏这一行里左右滑。
//      不克隆、不多出一个方块，样子天然就是原样；原位插一个等宽占位块，占位块只
//      占空间不画出来，靠其余标签让位来提示落点（也会把「+」一起推着走）。
//   3. setPointerCapture 咬住这个指针：拖到标签栏外甚至窗口外，move/up 仍会派发
//      回来，不会卡在「拖出去就松不开」的状态
//   4. 松手按占位块在 DOM 里的位置结算 state.tabs，再 renderTabs()；Esc 取消
var _tabDrag = null;   // { btn, idx, started, ph, pid, ... }

function _tabDragDown(e) {
  if (e.button !== 0) return;
  var btn = e.currentTarget;
  if (e.target && e.target.closest && e.target.closest('.close-tab')) return;
  var idx = parseInt(btn.dataset.tabIdx, 10);
  if (isNaN(idx)) return;
  var r = btn.getBoundingClientRect();
  _tabDrag = {
    btn: btn, idx: idx, started: false, pid: e.pointerId,
    startX: e.clientX, startY: e.clientY,
    offX: e.clientX - r.left,
    w: r.width, h: r.height, ph: null,
  };
  try { btn.setPointerCapture(e.pointerId); } catch (_) {}
}

function _tabDragStart(st) {
  st.started = true;
  _tabPopClose();   // 拖标签时浮窗跟着飘很怪
  var r = st.btn.getBoundingClientRect();
  var ph = document.createElement('div');
  ph.className = 'tab-placeholder';
  ph.style.width = st.w + 'px';
  st.btn.parentNode.insertBefore(ph, st.btn);
  st.ph = ph;

  // 原标签自己变成「在标签栏这一行里左右滑的压暗标签」
  st.btn.classList.add('tab-dragging');
  st.btn.style.width = st.w + 'px';
  st.btn.style.height = st.h + 'px';
  st.btn.style.left = r.left + 'px';
  st.btn.style.top = r.top + 'px';
  document.body.classList.add('is-tab-dragging');

  // 「+」的基础位置记下来：拖动时它会被标签顶着往右让，最多让到窗口边界
  var addBtn = document.getElementById('tabAddBtn');
  if (addBtn) {
    var ar = addBtn.getBoundingClientRect();
    st.addBtn = addBtn;
    st.addLeft = ar.left;
    st.addW = ar.width;
  }

  st.lastX = st.startX;
  _tabDragStartAutoScroll();
}

// 拖到标签栏左右边缘时自动滚动：标签多到一屏放不下时，靠这个才能把它拖到
// 看得见区域以外的位置。越靠边滚得越快，松手/取消就停。
var _tabDragRaf = null;

function _tabDragStartAutoScroll() {
  if (_tabDragRaf) return;
  _tabDragRaf = requestAnimationFrame(_tabDragAutoScrollStep);
}

function _tabDragStopAutoScroll() {
  if (_tabDragRaf) {
    cancelAnimationFrame(_tabDragRaf);
    _tabDragRaf = null;
  }
}

function _tabDragAutoScrollStep() {
  _tabDragRaf = null;
  var st = _tabDrag;
  if (!st || !st.started) return;
  var scroll = document.getElementById('tabsScroll');
  if (!scroll) return;

  var r = scroll.getBoundingClientRect();
  var EDGE = 44;      // 进入边缘这个距离内开始滚
  var MAX = 16;       // 每帧最多滚这么多像素
  var x = st.lastX;
  var delta = 0;
  if (x > r.right - EDGE) {
    delta = Math.ceil(Math.min(MAX, 2 + (x - (r.right - EDGE)) / EDGE * MAX));
  } else if (x < r.left + EDGE) {
    delta = -Math.ceil(Math.min(MAX, 2 + ((r.left + EDGE) - x) / EDGE * MAX));
  }
  if (delta) {
    var before = scroll.scrollLeft;
    scroll.scrollLeft = before + delta;
    if (scroll.scrollLeft !== before) {
      _tabDragPlace(st, x);           // 滚动后可见标签变了，落点跟着重算
      if (typeof _updateTabArrows === 'function') _updateTabArrows();
    }
  }
  _tabDragRaf = requestAnimationFrame(_tabDragAutoScrollStep);
}

// 落点 = 第一个「鼠标还在它左半区」的标签之前；都没有就排到最后
function _tabDragPlace(st, clientX) {
  var scroll = document.getElementById('tabsScroll');
  if (!scroll || !st.ph) return;
  var before = null;
  var tabs = scroll.querySelectorAll('.tab-btn:not(.tab-new)');
  for (var j = 0; j < tabs.length; j++) {
    var t = tabs[j];
    if (t === st.btn) continue;
    var r = t.getBoundingClientRect();
    if (clientX < r.left + r.width / 2) { before = t; break; }
  }
  var kids = Array.prototype.filter.call(scroll.children, function(el) { return el !== st.btn; });
  var target = before ? kids.indexOf(before) : kids.length;
  if (kids.indexOf(st.ph) === target) return;

  // FLIP：先记下每个格子的旧位置，挪完再按差值反向位移、过渡回 0，
  // 这样「让位」是滑过去的，不是瞬间跳一格。
  var prev = [];
  for (var i = 0; i < kids.length; i++) prev.push(kids[i].getBoundingClientRect().left);

  var ref = target < kids.length ? kids[target] : null;
  if (ref) scroll.insertBefore(st.ph, ref); else scroll.appendChild(st.ph);

  for (var k = 0; k < kids.length; k++) {
    var dx = prev[k] - kids[k].getBoundingClientRect().left;
    if (dx) _flipShift(kids[k], dx, 0);
  }
}

// FLIP 通用件：反向位移 → 下一帧过渡回 0；动画结束后清掉内联样式，别影响下次布局
function _flipShift(el, dx, dy) {
  el.style.transition = 'none';
  el.style.transform = 'translate(' + (dx || 0) + 'px, ' + (dy || 0) + 'px)';
  void el.offsetWidth;   // 强制回流，让上面的反向位移先落地
  el.style.transition = 'transform .16s cubic-bezier(.2, .8, .3, 1)';
  el.style.transform = '';
  if (el._flipTimer) clearTimeout(el._flipTimer);
  el._flipTimer = setTimeout(function() {
    el.style.transition = '';
    el.style.transform = '';
    el._flipTimer = null;
  }, 220);
}

function _tabDragMove(e) {
  var st = _tabDrag;
  if (!st) return;
  if (!st.started) {
    if (Math.abs(e.clientX - st.startX) < 4 && Math.abs(e.clientY - st.startY) < 4) return;
    _tabDragStart(st);
  }
  if (e.cancelable) e.preventDefault();
  st.lastX = e.clientX;
  // 只跟水平方向：压暗的那个标签留在标签栏这一行里左右滑，不跟着鼠标上下跑，
  // 也不在光标下面变成一块浮起来的方块。右边界要给「+」留出位置 —— 推到最右时
  // 是「标签 + 加号」一起顶到窗口边，不能让标签把加号压在身下。
  var gx = e.clientX - st.offX;
  var bar = document.getElementById('tabsBar');
  if (bar) {
    var br = bar.getBoundingClientRect();
    var maxGx = br.right - st.w - 4;
    if (st.addBtn) maxGx = Math.min(maxGx, br.right - st.w - st.addW - 14);
    gx = Math.max(br.left + 4, Math.min(gx, maxGx));
  }
  st.btn.style.left = gx + 'px';

  // 把「+」往右顶：被拖标签的右缘贴上去就推着它走，最多推到窗口边界为止。
  if (st.addBtn) {
    var want = gx + st.w + 6 - st.addLeft;
    var maxPush = Math.max(0, window.innerWidth - 4 - (st.addLeft + st.addW));
    var push = Math.max(0, Math.min(want, maxPush));
    st.addBtn.style.transform = push > 0.5 ? 'translateX(' + push + 'px)' : '';
  }
  _tabDragPlace(st, e.clientX);
}

// commit=false 表示取消（Esc / pointercancel）：只清理，不改顺序
function _tabDragFinish(commit) {
  var st = _tabDrag;
  if (!st) return;
  _tabDrag = null;
  _tabDragStopAutoScroll();
  try { st.btn.releasePointerCapture(st.pid); } catch (_) {}
  document.body.classList.remove('is-tab-dragging');
  if (!st.started) return;

  st.btn.classList.remove('tab-dragging');
  st.btn.style.left = '';
  st.btn.style.top = '';
  st.btn.style.width = '';
  st.btn.style.height = '';
  if (st.addBtn) st.addBtn.style.transform = '';

  var scroll = document.getElementById('tabsScroll');
  var kids = scroll ? Array.prototype.filter.call(scroll.children, function(el) { return el !== st.btn; }) : [];
  var target = st.ph ? kids.indexOf(st.ph) : -1;
  if (st.ph && st.ph.parentNode) st.ph.parentNode.removeChild(st.ph);

  if (commit && target >= 0 && target !== st.idx) {
    var moved = state.tabs.splice(st.idx, 1)[0];
    state.tabs.splice(target, 0, moved);
    if (state.activeTab === st.idx) state.activeTab = target;
    else if (st.idx < state.activeTab && target >= state.activeTab) state.activeTab--;
    else if (st.idx > state.activeTab && target <= state.activeTab) state.activeTab++;
    // 拖动结束后浏览器还会补一个 click，别让它把标签又切回去
    window._tabClickSuppress = true;
    setTimeout(function() { window._tabClickSuppress = false; }, 0);
  }
  renderTabs();
}

function _tabDragFinishAll(commit) {
  if (_tabDrag) _tabDragFinish(commit);
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
    // 标签/多字符串拖动：pointer 事件 + setPointerCapture（拖到窗口外也收得到 up）
    document.addEventListener('pointermove', _tabDragMove);
    document.addEventListener('pointermove', _qpDragMove);
    document.addEventListener('pointerup', function() { _tabDragFinishAll(true); _qpDragFinishAll(true); });
    document.addEventListener('pointercancel', function() { _tabDragFinishAll(false); _qpDragFinishAll(false); });
    // 兜底：切走窗口 / 页面隐藏时指针事件可能收不全，直接按取消收拾干净，
    // 免得标签停在「压暗固定」或行停在固定定位的状态里下不来。
    window.addEventListener('blur', function() { _tabDragFinishAll(false); _qpDragFinishAll(false); });
    document.addEventListener('visibilitychange', function() {
      if (document.hidden) { _tabDragFinishAll(false); _qpDragFinishAll(false); }
    });
    document.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') { _tabDragFinishAll(false); _qpDragFinishAll(false); _tabPopClose(); }
    });
    // Mouse wheel horizontal scroll for tab bar
    bar.addEventListener('wheel', function(e) {
      var scroll = document.getElementById('tabsScroll');
      if (scroll) { scroll.scrollLeft += e.deltaY; e.preventDefault(); }
    }, { passive: false });
  }

  initStatusBar();
  bindTabsScroll();
  initTabPopup();
  // Fetch version once
  window.go.main.App.GetVersion().then(function(v) { window._appVersion = v || '0.6.1'; document.title = '串口调试工具 v' + window._appVersion; }).catch(function() { window._appVersion = '0.6.1'; });
  // Create global overlays (moved from static HTML to JS)
  createOfflineOverlay();
  createAlertOverlay();
  createThemeOverlay();
  if (typeof createSettingsOverlay === 'function') {
  createSettingsOverlay();
  // 弹窗里的端口 / 波特率一改就同步到工具栏
  _bindSettingsPortSync();
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
    // 守护进程就绪（管道可连接）时立刻连；离线时不再有轮询，靠这个事件重连
    window.runtime.EventsOn('daemon-ready', () => {
      _tlog('事件 daemon-ready', undefined, 'online=' + state.daemonOnline);
      if (!state.daemonOnline) tryConnect();
    });
    window.runtime.EventsOn('process-changed', (data) => syncDaemonSessions(data));
    window.runtime.EventsOn('stats-count', (data) => onStatsCount(data));
    window.runtime.EventsOn('stats-rate', (data) => onStatsRate(data));
    window.runtime.EventsOn('clients-changed', (data) => onClientsChanged(data));
    window.runtime.EventsOn('multistr-changed', (data) => onMultistrChanged(data));
  } catch {}

  initDividerDrag();
  startClientsRefresh();
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

// ---- 连接状态 ----
//
// 上线只有两条路：
//   ① 启动时主动连一次（init → tryConnect）
//   ② 守护进程就绪事件 daemon-ready（Go 侧每 3s 重发一次，丢一次也能自愈）
//
// 掉线只有两条路：
//   ① 守护进程正常关闭 → daemon-shutdown（即时广播，不依赖心跳）
//   ② 意外退出 → Go 侧心跳 2s×3 + 一次重连确认，真的连不上才发 daemon-offline
//
// 这里**不再有离线轮询**。轮询曾经和用户点「启动守护进程」抢同一把锁，把点击
// 静默吞掉——「先开 GUI 后开守护进程，大概率连不上」就是这么来的。

// ---- 连接耗时诊断 ----
//
// 时间戳格式与 daemon.log 一致（HH:MM:SS.mmm），可以直接对照。
// 只是诊断输出，不影响任何控制流。
function _nowStamp() {
  var d = new Date();
  return d.toTimeString().slice(0, 8) + '.' + String(d.getMilliseconds()).padStart(3, '0');
}
function _tlog(name, ms, extra) {
  if (!window.console) return;
  console.log('[' + _nowStamp() + '] [daemon] ' + name +
    (ms === undefined ? '' : ' ' + ms + 'ms') + (extra ? ' ' + extra : ''));
}

// _connecting 只保护后台探测（daemon-ready 每 3s 重发，不能被叠加）；
// 用户主动操作（startDaemon）走自己的 _starting，两者互不吞。
var _connecting = false;

async function tryConnect() {
  if (state.daemonOnline) return;
  if (_connecting) { _tlog('tryConnect 跳过：上一次还没结束'); return; }
  _connecting = true;
  var tConn = performance.now();
  try {
    const ok = await window.go.main.App.CheckDaemonStatus();
    _tlog('CheckDaemonStatus', Math.round(performance.now() - tConn), 'ok=' + ok);
    if (ok && !state.daemonOnline) {
      state.daemonOnline = true;
      state.processRunning = true;
      state.connectFailures = 0;
      updateDaemonUI();
      updateOpenBtn();
      await syncDaemonSessions();
      // GUI 的「默认自动保存」存在 gui.ini，是本地真相：连上后推给守护进程，
      // 而不是反过来读守护进程的值（否则 CLI 改过默认值就会覆盖 GUI 设置）。
      window.go.main.App.SetHistoryEnabled(state.autoSaveHistory === true).catch(function() {});
      updateAutoSaveCheck(state.autoSaveHistory === true);

      const t = getActiveTab();
      state.currentMode = (t && t.mode) ? t.mode : 'single';
      updateModeUI();
      refreshPortsForce().then(() => _syncPortSelectsFromTab());
      applyI18n();
    }
  } catch (e) {
    _tlog('tryConnect 异常', Math.round(performance.now() - tConn), String(e));
  } finally {
    _connecting = false;
  }
}

function onDaemonGone(reason) {
  _tlog('onDaemonGone', undefined, 'reason=' + reason);
  if (!state.daemonOnline) return;
  state.daemonOnline = false;
  state.connectFailures = 0;
  updateDaemonUI();
  handleDaemonOffline();
  // 不再启动离线轮询：重连交给 daemon-ready 事件（Go 侧每 3s 重发一次）
}

// ---- session sync ----
let syncPending = false;

// 事件载荷用 "processId"，GetSessions 用 "id"；统一成 "id"。
function _normalizeSessions(list) {
  return (list || []).map(p => {
    const e = Object.assign({}, p);
    if (e.processId !== undefined && e.id === undefined) e.id = e.processId;
    return e;
  });
}

// _daemonTabLabel 算会话标签的文案。
//
// 工程会话（sessionPath 非空）单独处理：记了端口就显示端口，没记端口就显示工程
// 目录名 —— 否则一个「单端口空闲」根本看不出打开的是哪个工程。
function _daemonTabLabel(ds) {
  const isConnected = ds.status === 'connected';
  const isForward = ds.mode === 'forward';
  const hasDeclared = !!ds.portName;
  if (ds.sessionPath) {
    if (isConnected) {
      return isForward ? ds.portName : `${ds.portName} @ ${ds.baud}`;
    }
    if (hasDeclared) {
      return isForward
        ? `${ds.portName} [` + t('tab.declared', '已声明') + `]`
        : `${ds.portName} @ ${ds.baud} [` + t('tab.declared', '已声明') + `]`;
    }
    return stlogBaseName(ds.sessionPath) + ' [' + t('tab.project', '工程') + ']';
  }
  return isForward
    ? (isConnected ? ds.portName : (hasDeclared ? `${ds.portName} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_forward', '端口转发空闲')))
    : (isConnected ? `${ds.portName} @ ${ds.baud}` : (hasDeclared ? `${ds.portName} @ ${ds.baud} [` + t('tab.declared', '已声明') + `]` : t('tab.idle_single', '单端口空闲')));
}

// 把守护进程会话的当前状态写到一个标签页上（标签、在线状态、参数、查看者）。
// 「找回原有标签页」与「新建标签页」两条路共用，避免两处判断漂移。
// （第 2 步「更新已有标签页」是它的增量版本：只在实际变化时打 tabsChanged。）
function _applyDaemonSessionToTab(tab, ds) {
  const isConnected = ds.status === 'connected';
  const hasDeclared = !!ds.portName;
  tab.label = _daemonTabLabel(ds);
  // 线程名：守护进程说了算（GUI 不再自己拼文案）。nameCustom 只是标记名字是
  // 用户起的还是自动算的，右键菜单据此显示「恢复自动名」。
  tab.name = ds.name || '';
  tab.nameCustom = !!ds.nameCustom;
  tab.portOpen = isConnected;
  tab.mode = ds.mode || 'single';
  tab.viewers = ds.viewers || { gui: 0, cli: 0, mcp: 0 };
  // 工程会话才有值：标签上要显示工程目录名（而不是「单端口空闲」）
  tab.sessionPath = ds.sessionPath || '';
  if (isConnected || hasDeclared) {
    tab.syncedParams = {
      portName: ds.portName, baud: ds.baud,
      dataBits: ds.dataBits, stopBits: ds.stopBits, parity: ds.parity,
      forwardPortB: ds.forwardPortB, forwardBaudB: ds.forwardBaudB,
    };
  } else {
    tab.syncedParams = null;
  }
}

async function syncDaemonSessions(procData) {
  if (syncPending) return;
  syncPending = true;
  try {
  // 守护进程事件可能比语言包先到：这时建出来的标签页/快捷面板会拿到空文案
  // （t() 缺键给空串），而且之后没有任何补译路径。先把语言包装好再建 UI。
  if (!_i18nLoaded) {
    try { await loadI18n(state.language || 'zh'); } catch (e) {}
  }
  if (!state.daemonOnline) return;

  let daemonSessions;
  // Accept process-changed push data or fetch explicitly
  if (procData && procData.processes) {
    daemonSessions = _normalizeSessions(procData.processes);
  } else {
    try {
      daemonSessions = _normalizeSessions(await window.go.main.App.GetSessions());
    } catch { return; }
  }
  if (!daemonSessions) daemonSessions = [];

  // ⚠️ 事件载荷可能是**陈旧**的：它可能是在某个进程创建之前发出的（实测出现过
  // `processes: []` 落在某个标签页刚拿到 sessionId 之后）。仅凭这样一份载荷就把
  // 活着的会话清掉，后果是连锁的：
  //   sessionId 被清 → 标签页看起来是空闲的 → 点「打开串口」时**又建一个进程** →
  //   旧进程随后被第 1 步补成一个多余标签页（用户实测的「启动串口后自动多了一个
  //   标签页」，daemon.log 里是 create idle #1 → create idle #2 → connect #2）。
  // 所以只要这份载荷会**移除**某个在线标签页的会话，就先向守护进程核实一次。
  if (procData && procData.processes) {
    const payloadIds = new Set(daemonSessions.map(s => s.id));
    const wouldDrop = state.tabs.some(t => t.sessionId && !payloadIds.has(t.sessionId));
    if (wouldDrop) {
      try {
        const fresh = _normalizeSessions(await window.go.main.App.GetSessions());
        if (fresh && fresh.length >= 0) daemonSessions = fresh;
      } catch {}
    }
  }

  // Auto-create idle process if daemon just came online with no processes.
  // Skip while a tab is being intentionally closed (suppress window to avoid
  // the process-changed event from the destroy triggering immediate re-creation).
  const suppress = state._suppressAutoCreate && Date.now() < state._suppressAutoCreate;
  // autoCreateSession: add a local tab (no daemon process) when no tabs exist
  if (!suppress && state.autoCreateSession && daemonSessions.length === 0 && state.tabs.length === 0) {
    await autoCreateIdleSession();  // 自动补一个单端口空闲会话（不弹选择页）
    // ⚠️ 刚建出来的进程**不在**这次同步开头取到的那份快照里。此处此前写的是
    // `daemonSessions = []`，本意是「别为它再建一个标签页」，但第 3 步正是用这份
    // 列表判断「会话还在不在」—— 列表一空，刚挂上去的 sessionId 立刻被当成已消失
    // 清掉（而 tab.connectedAt 又没人写，3 秒宽限期不生效）。标签页于是变成
    // 「看起来空闲」，用户点「打开串口」时**又建一个进程**，旧进程随后被第 1 步补成
    // 一个多余标签页。实测日志（0.7.5.8，用户机器）：
    //   r4 create idle #1 → 5.2s → r8 create idle #2 → r9 history #1
    // 正确做法：重新取一份权威列表，让第 1/3 步都看到真实状态。
    try {
      const refreshed = _normalizeSessions(await window.go.main.App.GetSessions());
      if (refreshed && refreshed.length > 0) daemonSessions = refreshed;
    } catch {}
  }

  const dsMap = {};
  daemonSessions.forEach(s => { dsMap[s.id] = s; });

  const guiSessionIds = new Set();
  state.tabs.forEach(t => { if (t.sessionId) guiSessionIds.add(t.sessionId); });

  let tabsChanged = false;

  // 1. Create tabs for new daemon sessions not tracked in any GUI tab
  daemonSessions.forEach(ds => {
    if (ds.sessionPath) {
      // 文件会话进程（session.open 建出来的）默认不建标签页 —— 有人正在等它的
      // 会话页（_pendingProjectPaths 里有这条路径）才往下建，建完把待办划掉。
      if (!state._pendingProjectPaths || !state._pendingProjectPaths[ds.sessionPath]) return;
      delete state._pendingProjectPaths[ds.sessionPath];
    }
    if (!guiSessionIds.has(ds.id)) {
          // 先看有没有「本来就是这个会话」的标签页：第 3 步在守护进程列表一时
          // 不含该会话时会把 sessionId 清掉（连接/重建的窗口期）。等它回到列表里
          // 若直接按「未登记」新建，同一个会话就会多出一个标签页 —— 用户实测到的
          // 「启动串口后多了一个新建标签页、原来那个跑到后面」正是这条。
          const orphan = state.tabs.find(t => isSessionTab(t) && !t.sessionId && t._orphanSid === ds.id);
          if (orphan) {
            _applyDaemonSessionToTab(orphan, ds);
            orphan.sessionId = ds.id;
            orphan._orphanSid = null;
            orphan.connectedAt = Date.now();   // 让下面的「刚挂上会话」宽限期真正生效
            guiSessionIds.add(ds.id);
            if (ds.id && state.daemonOnline) {
              window.go.main.App.StartTabDecoder(ds.id);
              window.go.main.App.SetTabEncoding(ds.id, state.encoding);
            }
            tabsChanged = true;
            return;
          }
          // Skip unmatched idle processes while GUI is creating one (addTab in flight)
          if (_creatingTab && ds.status !== 'connected') return;
	      const isConnected = ds.status === 'connected';
        const hasDeclared = !!ds.portName;
        const label = _daemonTabLabel(ds);
      var newTab = {
        id: state.tabCounter++,
        label: label,
        sessionId: ds.id,
        // 工程会话记下工程路径：同一个工程再点一次要能切回这一页，而不是再建一个
        _projectPath: ds.sessionPath || null,
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
        const newLabel = _daemonTabLabel(ds);
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
      // 刚挂上会话的这几秒内一律不动它：连接/重连的窗口期里，事件载荷可能还没有
      // 这个会话（见上面「陈旧载荷」的说明）。注意 connectedAt 此前**从来没人写过**，
      // 这道宽限期一直是死代码。
      if (tab.connectedAt && (Date.now() - tab.connectedAt) < 3000) {
        return;
      }
      // 记住它是谁：下一次列表里这个会话回来时要能找回同一个标签页，
      // 而不是新建一个（见第 1 步的 orphan 分支）。
      tab._orphanSid = tab.sessionId;
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
    // 只数「会话标签页」：设置页不占会话位。此前用的是 state.tabs.length，
    // 于是**只要开着设置页**，单独一个空闲会话标签页就会被判成「还有别的标签页」
    // 而销毁 —— 它在标签栏里的位置随之消失，下一次同步又会在末尾补一个新的。
    let sessionTabCount = state.tabs.filter(t => isSessionTab(t)).length;
    for (let i = state.tabs.length - 1; i >= 0; i--) {
      const t = state.tabs[i];
      if (!isSessionTab(t)) continue;
      if (t.source === 'gui' && !t.sessionId && sessionTabCount > 1) {
        if (t._page) t._page.destroy();
        state.tabs.splice(i, 1);
        sessionTabCount--;
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

// 标注「已被其它标签页占用」的端口选项。
// sel 是本页的 <select> 元素（不能用 id 查：多标签时每个页面各有一份同 id），
// owner 是这个下拉所属的标签页 —— 前向模式下「自己另一个端口」不能算被别人占用。
function _markOccupied(sel, owner) {
  if (!sel) return;
  const tab = owner || getActiveTab();
  // Extract individual ports for this page's forward tab
  const myPorts = tab && tab.mode === 'forward' && tab.portOpen ? _forwardPorts(tab) : null;
  const selId = sel.id;
  let changed = false;
  for (let i = 0; i < sel.options.length; i++) {
    const opt = sel.options[i];
    const portName = opt.value;
    let occupied = false;
    if (portName) {
      for (let j = 0; j < state.tabs.length; j++) {
        const t = state.tabs[j];
        if (t === tab) continue;
        if (!t.portOpen || !t.syncedParams) continue;
        if (_tabHasPort(t, portName)) { occupied = true; break; }
      }
      // In forward mode, mark own other port as occupied to prevent self-duplicate
      if (!occupied && myPorts) {
        if ((selId === 'portSelectA' && portName === myPorts[1]) ||
            (selId === 'portSelectB' && portName === myPorts[0])) {
          occupied = true;
        }
      }
    }
    const want = occupied ? 'port-occupied' : '';
    // 只在真的变了时写：这个 select 上挂着 MutationObserver（attributes: true），
    // 无条件写 className 每次都会把整份下拉重建一遍（TODO #79 ①）。
    if (opt.className !== want) {
      opt.className = want;
      changed = true;
    }
  }
  // 已渲染的下拉是一份 <div class="cs-option"> 副本，标完要同步过去，
  // 否则得重新打开下拉才看得见（旧 bug 的次要原因）。
  if (changed) _syncOccupiedDropdown(sel);
}

// 把 select 上的占用类同步到已渲染的 .cs-option 副本（按 option 值对齐，不重建 DOM）。
function _syncOccupiedDropdown(sel) {
  const wrap = sel.closest ? sel.closest('.cs-wrap') : null;
  if (!wrap) return;
  let dd = wrap.querySelector('.cs-dropdown');
  // 下拉打开时会被 portal 到 body 上
  if (!dd && _csOpen === wrap) dd = document.querySelector('.cs-dropdown.is-portalled');
  if (!dd) return;
  dd.querySelectorAll('.cs-option').forEach(function(item) {
    let opt = null;
    for (let i = 0; i < sel.options.length; i++) {
      if (sel.options[i].value === item.dataset.val) { opt = sel.options[i]; break; }
    }
    const want = !!opt && opt.className.indexOf('port-occupied') >= 0;
    if (item.classList.contains('port-occupied') !== want) {
      item.classList.toggle('port-occupied', want);
    }
  });
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

// 每个标签页都有自己的三份端口下拉（同 id），必须按页 root 逐份标注。
function updatePortOccupied() {
  state.tabs.forEach(function(tb) {
    if (!tb || !tb._page || !tb._page.root || !tb._page.root.querySelector) return;
    ['#portSelect', '#portSelectA', '#portSelectB'].forEach(function(spec) {
      _markOccupied(tb._page.root.querySelector(spec), tb);
    });
  });
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
    // .stlog 文件页读的是本地磁盘，跟守护进程没关系：别跟着一起销毁。
    if (t.type === 'stlog') continue;
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
  state._pendingProjectPaths = {};
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
  // 悬停能看到具体是哪些会话，排查「计数和标签页对不上」时有用
  var guiBadge = document.querySelector('.gui-badge');
  if (guiBadge) {
    guiBadge.title = stateClients
      .filter(function(c) { return c.source === 'gui'; })
      .map(function(c) { return (c.clientId || '?') + ' (PID ' + (c.pid || '?') + ')'; })
      .join('\n');
  }
}

// 客户端列表的权威刷新：clients-changed 是推送，个别情况下会漏（例如订阅那
// 一刻的竞态），此时状态栏会一直显示旧的计数。每 5 秒拉一次兜底。
function startClientsRefresh() {
  setInterval(function() {
    if (!state.daemonOnline) return;
    try {
      window.go.main.App.GetClients().then(function(list) {
        if (Array.isArray(list)) {
          stateClients = list;
          updateSyncIndicator();
        }
      }).catch(function() {});
    } catch (e) {}
  }, 5000);
}

// 状态栏统计在会话页上**始终显示**（空闲会话显示 0，不隐藏）；只有设置页 /
// 欢迎页 / 守护进程离线时才整块隐藏。
// 数字是否可信交给 refreshStatsDOM：端口没连上就拿 0 顶掉，避免把上一次连接的
// 数字（以及最后一次 stats-rate 事件的速率）一直挂在下面。
function _updateStatsVisible() {
  const tab = getActiveTab();
  const vis = state.daemonOnline && isSessionTab(tab);
  const el = document.getElementById('statusbar-stats');
  if (el) el.style.display = vis ? '' : 'none';
  // 藏起来的同时清零：否则下一次显示会先闪一遍上次会话的残留数字
  if (!vis && _statusBar) _statusBar.resetStats();
}

function updateDaemonUI() {
  const overlay = document.getElementById('offlineOverlay');
  const portSel = pageEl('portSelect');
  if (_statusBar) _statusBar.updateDaemon({online: state.daemonOnline, tabsCount: state.tabs.length});
  // Show overlay only when daemon is offline AND the active tab is a session tab.
  // 设置页 / .stlog 文件页 / 新建选择页都不需要守护进程，被遮住等于把「离线也能
  // 看文件」这条路堵死（实测拖入 .stlog 后整页被「守护进程未启用」盖住）。
  const at = getActiveTab();
  const isNonSession = !!at && (at.type === 'settings' || at.type === 'stlog' || at.type === 'new');
  if (state.daemonOnline || isNonSession) {
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
  // 「+」和「文件 → 新建会话」不再随守护进程状态置灰：离线也能进选择页，
  // 页内再按能力置灰（串口卡禁用、从文件打开可用）。
  state.tabs.forEach(function (t) {
    if (t.type === 'new' && t._page && t._page.root && typeof _syncChooserAvailability === 'function') {
      _syncChooserAvailability(t._page.root);
    }
    // 只读工程页的「推送到守护进程」按钮跟着守护进程状态亮/灭
    if (t.type === 'stlog' && t._stlog && typeof stlogSyncPushBtn === 'function') stlogSyncPushBtn(t);
  });
  _updateStatsVisible();
  // 引导页的显隐也依赖 daemonOnline，这里跟着刷一次，别等 updateEmptyState 的调用方
  _syncWelcome();
}

async function toggleDaemon() {
  if (state.daemonOnline) {
    await stopDaemon();
  } else {
    await startDaemon();
  }
}

// _starting 是「用户主动启动」的互斥，和后台探测的 _connecting 分开 ——
// 后台探测再频繁也不该把用户的点击吞掉（那正是「先开 GUI 后开守护进程连不上」的成因）。
var _starting = false;

async function startDaemon() {
  if (_starting) { _tlog('startDaemon 被跳过：上一次启动还没结束'); return; }
  _starting = true;
  if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(true);
  var tStart = performance.now();
  try {
    await window.go.main.App.StartDaemon();
    _tlog('App.StartDaemon', Math.round(performance.now() - tStart));
    state.processRunning = true;
    state.daemonOnline = true;
    state.connectFailures = 0;
    updateDaemonUI();
    updateOpenBtn();
    await syncDaemonSessions();
    _tlog('syncDaemonSessions 完成', Math.round(performance.now() - tStart));
  } catch (e) {
    _tlog('startDaemon 失败', Math.round(performance.now() - tStart), String(e));
    console.error('startDaemon failed:', e);
    state.daemonOnline = false;
    state.processRunning = false;
    updateDaemonUI();
    showAlert(t('cli.error','错误'), t('serial.start_fail','启动失败: ') + e);
  } finally {
    _starting = false;
    if (_statusBar && _statusBar.daemon) _statusBar.daemon.setBusy(false);
  }
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
  // 顺手缓存「COM → 设备描述」：标签浮窗（c-tabpop）要显示串口名，
  // 而这里刷出来的描述原本只用于拼下拉框文案，用完就丢。
  state.portDescs = {};
  if (ports) {
    for (let i = 0; i < ports.length; i++) {
      const p = ports[i];
      if (!p || !p.port) continue;
      state.portDescs[p.port] = (p.description && p.description !== p.port) ? p.description : '';
    }
  }
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
    // 三个工具栏下拉不在这里填了：它们走 document.getElementById，多标签时只能
    // 命中文档里**第一个**同名元素，于是除第一个标签页以外，其余页面的端口下拉
    // 永远只剩占位符。统一交给下面的 _populateTabPortSelects 按页 root 填 ——
    // 顺带省掉「第一页被填两遍」的那次多余重建（TODO #79 ①）。
    // 设置弹窗里的两份是全文档唯一实例，仍用 id 定位。
    _applyPortOptions('portSelectASettings', html, t('status.port1', '-- 端口1 --'));
    _applyPortOptions('portSelectBSettings', html, t('status.port2', '-- 端口2 --'));
  }
  state.tabs.forEach(function(tb) {
    if (tb && tb._page && tb._page.root) _populateTabPortSelects(tb._page.root);
  });
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
  // 端口下拉按本页 root 填充（不能用 getElementById：每个标签页都有一份同 id）
  _populateTabPortSelects(pageRoot);
  // 自绘滚动条（原生条已隐藏），只反映真实内容
  if (typeof initDisplayScrollbar === 'function') {
    initDisplayScrollbar(pageRoot.querySelector('#displayContent'));
  }
  if (typeof initSendScrollbar === 'function') {
    initSendScrollbar(pageRoot.querySelector('#sendInput'));
  }
}

// 按 root 作用域填充端口下拉：portSelect / portSelectA / portSelectB。
// 多标签时每个页面各有一份同 id 的 <select>，getElementById 只能拿到第一个，
// 所以必须限定在 root 里查。
function _populateTabPortSelects(root) {
  if (!root || !root.querySelector || !state.lastPorts) return;
  [
    ['#portSelect', t('status.select_port', '-- 选择端口 --')],
    ['#portSelectA', t('status.port1', '-- 端口1 --')],
    ['#portSelectB', t('status.port2', '-- 端口2 --')],
  ].forEach(function(spec) {
    var sel = root.querySelector(spec[0]);
    if (!sel) return;
    _setPortOptions(sel, state.lastPorts, spec[1]);
    _refreshCustomSelectFor(sel);
  });
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

// 新建会话：先开一个「选择页」标签（两张调色盘卡片），选定模式之后才在守护
// 进程里建空闲进程，并把这个标签页**原地**换成会话页 —— 所以直接关掉选择页
// 不会留下孤儿进程（旧流程先把进程建出来再建页，关掉就漏了）。
function openNewSessionTab() {
  // 守护进程没起来也照样能进来：页内的串口卡会置灰，「从文件打开」始终可用。
  // 已经有一个选择页就切过去，不重复开
  var existing = state.tabs.findIndex(function(x) { return x.type === 'new'; });
  if (existing >= 0) { switchTab(existing); return; }

  var tab = {
    id: state.tabCounter++,
    label: t('newtab.title', '新建会话'),
    type: 'new',
    portOpen: false,
    mode: 'single',
    source: 'gui'
  };
  tab._page = createModeChooserPage();
  var mc = document.getElementById('mainContent');
  if (mc) mc.appendChild(tab._page.root);

  var oldPage = getActivePage();
  if (oldPage) oldPage.hide();
  if (state._welcomePage) state._welcomePage.hide();

  state.tabs.push(tab);
  state.activeTab = state.tabs.length - 1;
  tab._page.show();

  renderTabs();
  updateEmptyState();
  updateOpenBtn();
  updateSyncIndicator();
  _updateStatsVisible();
  updateDaemonUI();
}

// 在守护进程里建空闲进程；转发模式再把进程切成 forward。
// 失败时把可能已经建出来的进程收掉，不留孤儿。
async function _createIdleProcessOfMode(mode) {
  var sid = null;
  _creatingTab = true;
  try {
    sid = await window.go.main.App.CreateIdleProcess();
    if (sid && mode === 'forward') {
      await window.go.main.App.SetProcessMode(sid, 'forward');
    }
    return sid;
  } catch (e) {
    if (sid) { try { await window.go.main.App.CloseSession(sid); } catch (_) {} }
    showAlert(t('cli.error', '错误'), t('serial.mode_fail', '切换模式失败: ') + e);
    return null;
  } finally {
    _creatingTab = false;
  }
}

// 会话标签页的公共字段（选择页转换与自动建会话共用）。
function _sessionTabDefaults(mode, id) {
  var fwd = (mode === 'forward');
  var tabId = (typeof id === 'number') ? id : state.tabCounter++;
  return {
    id: tabId,
    label: fwd ? t('tab.idle_forward', '端口转发空闲') : t('tab.idle_single', '单端口空闲'),
    portOpen: false,
    mode: fwd ? 'forward' : 'single',
    source: 'gui',
    // 新页沿用当前全局的显示模式（否则一旦发生 tabsChanged，同步会拿这里的
    // 'text' 回填全局，把用户选好的 Hex 顶掉）
    displayMode: state.displayMode || 'text',
    txDisplayMode: state.txDisplayMode || 'text',
    p1DisplayMode: state.p1DisplayMode || 'text',
    p2DisplayMode: state.p2DisplayMode || 'text',
    scrollLocked: false,
    sendRatio: state.sendRatio || 0.3,
    quickPanelRatio: state.quickPanelRatio,
    quickPresets: []
  };
}

// 选择页卡片被点：建进程 → 原地换成会话页。
async function chooseNewSessionMode(mode) {
  // 「从文件打开」不碰守护进程，走自己的分支。
  if (mode === 'file') { chooseOpenFile(); return; }
  var tab = getActiveTab();
  if (!tab || tab.type !== 'new') return;
  if (!state.daemonOnline) {
    showAlert(t('confirm.title', '提示'),
      t('newtab.need_daemon', '这个模式需要守护进程，请先启动它（菜单 守护进程 → 启停），或改用「从文件打开」'));
    return;
  }
  if (tab._choosing) return;   // 防连点：两次点击只建一个进程
  tab._choosing = true;
  _setChooserBusy(tab, true);

  var sid = await _createIdleProcessOfMode(mode);
  if (!sid) {
    tab._choosing = false;
    _setChooserBusy(tab, false);
    return;
  }
  _convertChooserToSession(tab, sid, mode);
}

function _setChooserBusy(tab, busy) {
  if (!tab || !tab._page || !tab._page.root) return;
  tab._page.root.classList.toggle('is-busy', !!busy);
  var cards = tab._page.root.querySelectorAll('.c-mode-card');
  for (var i = 0; i < cards.length; i++) {
    // 「从文件打开」不吃这个忙碌态：它跟守护进程里的建进程无关
    if (cards[i].getAttribute('data-mode') === 'file') continue;
    cards[i].disabled = !!busy;
  }
}

// 选择页 → 会话页：同一个标签、同一个下标，不新增标签页。
function _convertChooserToSession(tab, sid, mode) {
  var idx = state.tabs.indexOf(tab);
  if (idx < 0) return;

  var keepId = tab.id;
  if (tab._page) { tab._page.hide(); tab._page.destroy(); }

  var defaults = _sessionTabDefaults(mode, keepId);
  for (var k in defaults) {
    if (Object.prototype.hasOwnProperty.call(defaults, k)) tab[k] = defaults[k];
  }
  delete tab.type;
  delete tab._choosing;
  tab.sessionId = sid;
  // 刚挂上会话的时刻：同步第 3 步靠它避开「列表还没包含这个会话」的窗口期（TODO #74）
  tab.connectedAt = Date.now();
  tab._page = new TabPage(idx, tab);

  var mc = document.getElementById('mainContent');
  if (mc) { mc.appendChild(tab._page.root); initTabPage(tab._page.root); }
  tab._page.show();

  if (state.daemonOnline) {
    try { window.go.main.App.StartTabDecoder(sid); } catch (e) {}
    try { window.go.main.App.SetTabEncoding(sid, state.encoding); } catch (e) {}
  }

  state.activeTab = idx;
  state.currentMode = tab.mode;
  updateModeUI();
  renderTabs();
  updateOpenBtn();
  updateSyncIndicator();
  updateEmptyState();
  loadTabHistory();
}

// 守护进程刚上线且没有任何会话时自动补一个单端口空闲会话（不弹选择页，
// 这个时机用户不在交互中）。
async function autoCreateIdleSession() {
  if (!state.daemonOnline) return;
  var sid = await _createIdleProcessOfMode('single');
  if (!sid) return;
  if (state.tabs.some(function(x) { return x.sessionId === sid; })) return;

  var tab = _sessionTabDefaults('single');
  tab.sessionId = sid;
  tab.connectedAt = Date.now();
  tab._page = new TabPage(state.tabs.length, tab);
  var mc = document.getElementById('mainContent');
  if (mc) { mc.appendChild(tab._page.root); initTabPage(tab._page.root); }
  state.tabs.push(tab);

  if (state.daemonOnline) {
    try { window.go.main.App.StartTabDecoder(sid); } catch (e) {}
    try { window.go.main.App.SetTabEncoding(sid, state.encoding); } catch (e) {}
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
  // 切到这一页时补齐端口下拉（页面可能在端口列表到达之前就建好了）
  if (newTab._page && newTab._page.root) _populateTabPortSelects(newTab._page.root);

  // 非会话标签页（设置页 / 新建选择页，任一方向）：只显示隐藏，不碰会话状态
  if (!isSessionTab(newTab) || (oldTab && !isSessionTab(oldTab))) {
    if (oldTab && oldTab._page) oldTab._page.hide();
    state.activeTab = idx;
    if (newTab.type === 'settings') {
      if (newTab._page) { newTab._page.show(); renderSettingsThemeGrid(); renderSettingsIconGrid(); }
    } else if (newTab._page) {
      newTab._page.show();
      if (isSessionTab(newTab)) {
        loadTabHistory();
        refreshStatsDOM();
      }
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

// 空闲标签页的标题是 `t('tab.idle_*')` 生成的，切语言后要重新算一遍，
// 否则标签栏会一直停在上一种语言（实测俄语界面里标签还是中文）。
function _relabelIdleTabs() {
  var list = state.tabs || [];
  for (var i = 0; i < list.length; i++) {
    var tb = list[i];
    if (!tb || tb.portOpen || tb.type === 'settings' || tb.type === 'new') continue;
    if (tb.syncedParams && tb.syncedParams.portName) continue;  // 已声明端口，标题里带着端口名
    tb.label = tb.mode === 'forward' ? t('tab.idle_forward', '端口转发空闲') : t('tab.idle_single', '单端口空闲');
  }
}

// 标签页标题：0.8.4 起标签只显示**线程名**（守护进程 process.list 的 name），
// 由 _tabNameOf() 从会话状态取。tab.label 仍然保留 —— 右键菜单、窗口标题、
// 文件标签的默认名还在用它。

// ---- 标签高级信息浮窗（设计稿 A：c-tabpop） ----
//
// 悬停标签时自绘浮窗，替代原生 title —— 原生 tooltip 只能放纯文本、不跟主题、
// 约 1s 才出现，也显示不了实时速率。数据来源：
//   端口 / 波特率 / 8N1   tab.syncedParams（app.js:3228、3280）
//   串口名                state.portDescs（updatePortList 缓存，来自 daemon 的 description）
//   速率 / 累计 / 错误    GetSessionStats（app.go:728）+ stats-rate / stats-count 事件
//   运行时长              stats.uptimeSec（daemon/serial.go:61）
var _tabPopEl = null;          // 浮窗 DOM 单例，挂在 body 下
var _tabPop = null;            // 当前打开状态：{ tabEl, tab, idx, sessionId, stats, model }
var _tabPopShowTimer = null;
var _tabPopHideTimer = null;
var _TABPOP_SHOW_DELAY = 140;
var _TABPOP_HIDE_DELAY = 160;

function _tabPopParityLetter(p) {
  switch (String(p || 'none')) {
    case 'odd': return 'O';
    case 'even': return 'E';
    case 'mark': return 'M';
    case 'space': return 'S';
    default: return 'N';
  }
}

function _tabPopRateText(bps) {
  if (!bps) return '0.0 B/s';
  if (bps >= 1048576) return (bps / 1048576).toFixed(1) + ' MB/s';
  if (bps >= 1024) return (bps / 1024).toFixed(1) + ' kB/s';
  return bps.toFixed(1) + ' B/s';
}

function _tabPopDuration(sec) {
  if (!sec || sec < 0) return '—';
  var h = Math.floor(sec / 3600);
  var m = Math.floor((sec % 3600) / 60);
  var s = Math.floor(sec % 60);
  return h + ':' + (m < 10 ? '0' : '') + m + ':' + (s < 10 ? '0' : '') + s;
}

// 从标签自身状态推出浮窗要显示的东西（不依赖统计接口）
function _tabPopModel(tab) {
  var sp = tab.syncedParams || null;
  var isFwd = tab.mode === 'forward';
  var ports = [];
  var baud = 0;
  if (sp) {
    baud = sp.baud || 0;
    if (isFwd) {
      // 已连接时 portName 是 "COM4 ↔ COM14"；未连接时只有 portName + forwardPortB
      var parts = String(sp.portName || '').split(' ↔ ');
      if (parts[0]) ports.push({ tag: 'A', port: parts[0] });
      var pb = sp.forwardPortB || parts[1] || '';
      if (pb) ports.push({ tag: 'B', port: pb });
    } else if (sp.portName) {
      ports.push({ tag: '', port: sp.portName });
    }
  }
  var params = '';
  if (sp) {
    var fmt = (sp.dataBits || 8) + _tabPopParityLetter(sp.parity) + (sp.stopBits || 1);
    params = (isFwd && sp.forwardBaudB && sp.forwardBaudB !== baud)
      ? (baud + ' ⇄ ' + sp.forwardBaudB + ' · ' + fmt)
      : (baud + ' · ' + fmt);
  }
  return { ports: ports, params: params, declared: !!(sp && sp.portName), baud: baud, fwd: isFwd };
}

// _tabPopValues 取浮窗的数值。两个概念必须分开：
//
//	live    —— 端口开着：速率、条形、运行时长才有意义
//	hasData —— 拿得到统计：累计收发/错误/比例就该显示
//
// 以前只有一个 on = portOpen && stats，于是"关掉串口但明明收过 52 KB"整块显示成 —。
// daemon 侧的累计计数是进程生命期的，断开后照样能读（ProcessManager.GetStats 只看
// 进程是否存在），所以断开后仍然要把累计值显示出来，只有速率/条形归零。
function _tabPopValues() {
  var tab = _tabPop.tab;
  var s = _tabPop.stats;
  var isFwd = tab.mode === 'forward';
  var live = !!tab.portOpen && !!s;
  if (!s) {
    return {
      rxRate: 0, txRate: 0, rxTotal: 0, txTotal: 0, errRx: 0, errTx: 0,
      uptime: '—', live: false, hasData: false,
    };
  }
  // uptimeSec 只在 GetSessionStats 里给一次，事件不带它。连着的时候按取到的那一刻
  // 往后推算（每秒 tick 读一次）；断开后必须冻结，否则"运行"会一直跳。
  var upSec = s.uptimeSec || 0;
  if (live && _tabPop.uptimeAt) upSec += (Date.now() - _tabPop.uptimeAt) / 1000;
  var hasUptime = (s.uptimeSec || 0) > 0 || (live && _tabPop.uptimeAt);
  return {
    rxRate: live ? (s.readRateBps || 0) : 0,
    txRate: live ? (s.writeRateBps || 0) : 0,
    rxTotal: isFwd ? (s.bytesPort1 || 0) : (s.bytesRead || 0),
    txTotal: isFwd ? (s.bytesPort2 || 0) : (s.bytesWritten || 0),
    errRx: s.readErrors || 0,
    errTx: s.writeErrors || 0,
    uptime: hasUptime ? _tabPopDuration(upSec) : '—',
    live: live,
    hasData: true,
  };
}

// 条形颜色：>=80% 黄、>=90% 红（与状态栏 rate-orange / rate-red 同阈值）
function _tabPopBarColor(pct, baseColor, live) {
  if (!live) return baseColor;
  if (pct >= 90) return 'var(--red)';
  if (pct >= 80) return 'var(--yellow)';
  return baseColor;
}

// 技术性取值（端口号、速率、累计、比例…）一律按 LTR 隔离：阿拉伯语界面是 RTL，
// 不隔离的话 "115200 · 8N1" 会变成 "8N1 · 115200"、"⇃ RX" 变成 "RX ⇃"。
// 设备名可能是阿拉伯语，用 <bdi> 让它按内容自己判定方向。
function _tabPopIsRTL() {
  return document.documentElement.dir === 'rtl';
}

function _tabPopLaneHTML(tag, color, totalText, rateText, pct, live, idx) {
  return '<div class="c-tabpop__lane">' +
    '<span class="c-tabpop__tag" style="color:' + color + '" dir="ltr">' + tag + '</span>' +
    '<span class="c-tabpop__total" data-role="total' + idx + '" dir="ltr">' + totalText + '</span>' +
    '<span class="c-tabpop__rate" data-role="rate' + idx + '" dir="ltr">' + rateText + '</span>' +
    '<span class="c-tabpop__bar">' +
      '<i data-role="bar' + idx + '" style="width:' + pct + '%;background:' + _tabPopBarColor(pct, color, live) + '"></i>' +
    '</span>' +
  '</div>';
}

// RTL 下条形从右往左排（RX 在右），百分数也跟着从右往左读，
// 否则左边数字配右边色块会对不上。
function _tabPopRatioValHTML(hasRatio, rp, cRx, cTx) {
  if (!hasRatio) return '<span class="c-tabpop__rval-dim">—</span>';
  var firstPct = _tabPopIsRTL() ? (100 - rp) : rp;
  var secondPct = 100 - firstPct;
  var firstColor = _tabPopIsRTL() ? cTx : cRx;
  var secondColor = _tabPopIsRTL() ? cRx : cTx;
  return '<b style="color:' + firstColor + '">' + firstPct + '%</b> / ' +
         '<b style="color:' + secondColor + '">' + secondPct + '%</b>';
}

// 结构性内容（连接状态 / 模式 / 客户端 / 端口）变了才整块重画，其余只改数值，
// 条形才能靠 CSS transition 平滑地动起来。
function _tabPopSig() {
  var tb = _tabPop.tab, m = _tabPop.model, vw = tb.viewers || {};
  return [
    // 统计拿到没有也要算进签名：徽标（已连接/已声明）跟着它变
    _tabPop.stats ? 1 : 0,
    tb.portOpen ? 1 : 0,
    tb.mode,
    (vw.gui || 0) + ',' + (vw.cli || 0) + ',' + (vw.mcp || 0),
    m.params,
    m.ports.map(function (p) { return p.port; }).join(','),
    // 线程名与保存开关也进签名：改名后卡片标题要跟着换
    tb.name || '',
    tb.histOn === true ? 'save' : (tb.histOn === false ? 'nosave' : '?')
  ].join('|');
}

// 语言包还没跟上新键时（比如只换了前端、没重编 exe）不要显示空白：
// t() 在语言包装好之后遇到缺键一律返回空串，浮窗这里兜一层中文。
function _tabPopT(key, fb) {
  var v = t(key, fb);
  return v || fb;
}

// 文件标签的浮窗。文件标签没有会话概念（没 sessionId、没端口），套会话卡片只会
// 得到一片 —，所以单独画：文件名 / 路径 / 大小 / 收发 / 时间范围 / 来源。
// 数据来自 tab._stlog.info（stlogLoad 每次读回来都会存一份）。
function _tabPopRenderFile() {
  var el = _tabPopEl;
  var tab = _tabPop.tab;
  var st = tab._stlog || {};
  var info = st.info || {};
  var path = tab.path || '';
  var fwd = info.forward === true;
  var set = info.settings || {};
  var portA = set.port || '';
  var portB = info.portB || '';

  // 端口：文件头只存口名（不存设备描述），转发录制 A/B 各一行
  var portsHTML = '';
  if (fwd && portB) {
    portsHTML = '<div class="c-tabpop__ports">' +
      '<div class="c-tabpop__port"><span class="c-tabpop__ptag">A</span>' +
        '<span class="c-tabpop__pname" dir="ltr">' + escHtml(portA) + '</span></div>' +
      '<div class="c-tabpop__port"><span class="c-tabpop__ptag">B</span>' +
        '<span class="c-tabpop__pname" dir="ltr">' + escHtml(portB) + '</span></div>' +
    '</div>';
  } else if (portA) {
    portsHTML = '<div class="c-tabpop__ports">' +
      '<div class="c-tabpop__port"><span class="c-tabpop__pname" dir="ltr">' + escHtml(portA) + '</span></div>' +
    '</div>';
  }

  // 参数：文件头只记了波特率。帧格式（数据位/停止位/校验）没有入档，
  // 所以这里只写波特率，不编造「8N1」——设计稿假设它有，实际没有。
  var paramsHTML = '';
  if (set.baud > 0) {
    paramsHTML = '<div class="c-tabpop__meta" dir="ltr">' + set.baud + '</div>';
  }

  // 收发两行：与会话卡同构。文件没有「当前速率」这一列，右列直接放累计字节。
  function lane(tag, color, bytes) {
    return '<div class="c-tabpop__lane">' +
      '<span class="c-tabpop__tag" style="color:' + color + '" dir="ltr">' + tag + '</span>' +
      '<span class="c-tabpop__rate" dir="ltr">' + fmtBytes(bytes || 0) + '</span>' +
    '</div>';
  }
  var lanesHTML = fwd
    ? lane('A→B', 'var(--fwd-p1)', info.bytesAB) + lane('B→A', 'var(--fwd-p2)', info.bytesBA)
    : lane('⇃ RX', 'var(--green)', info.rxTotal) + lane('↾ TX', 'var(--accent)', info.txTotal);

  // 比例条：单口是收发比（绿/蓝），转发是分流比（P1/P2 两色）
  var cRx = fwd ? 'var(--fwd-p1)' : 'var(--green)';
  var cTx = fwd ? 'var(--fwd-p2)' : 'var(--accent)';
  var partA = fwd ? (info.bytesAB || 0) : (info.rxTotal || 0);
  var partB = fwd ? (info.bytesBA || 0) : (info.txTotal || 0);
  var sum = partA + partB;
  var hasRatio = sum > 0;
  var rp = hasRatio ? Math.round(partA / sum * 100) : 0;
  var ratioHTML = '<div class="c-tabpop__ratio">' +
    '<span class="c-tabpop__rlbl">' +
      (fwd ? _tabPopT('tabpop.ratio_fwd', '分流比') : _tabPopT('tabpop.ratio', '收发比')) + '</span>' +
    '<span class="c-tabpop__rbar">' +
      '<i style="width:' + rp + '%;background:' + cRx + '"></i>' +
      '<i style="width:' + (hasRatio ? (100 - rp) : 0) + '%;background:' + cTx + '"></i>' +
    '</span>' +
    '<span class="c-tabpop__rval" dir="ltr">' + _tabPopRatioValHTML(hasRatio, rp, cRx, cTx) + '</span>' +
  '</div>';

  // 页脚三项：帧（已载入/总数）、时长、来源数。绝对时间范围退到 title。
  var loaded = (st.frames && st.frames.length) ? st.frames.length : 0;
  var totalFrames = (typeof info.total === 'number') ? info.total : 0;
  var srcCount = (info.origins && info.origins.length) ? info.origins.length : 0;
  var range = stlogRangeText(info.firstTs, info.lastTs) || '';

  el.innerHTML =
    '<div class="c-tabpop__head">' +
      '<span class="c-tabpop__title">' + escHtml(stlogBaseName(path)) + '</span>' +
      '<span class="c-tabpop__badge is-dim"><i class="c-tabpop__dot"></i>' +
        _tabPopT('tabpop.readonly', '只读') + '</span>' +
    '</div>' +
    portsHTML +
    paramsHTML +
    '<div class="c-tabpop__sep"></div>' +
    lanesHTML +
    ratioHTML +
    '<div class="c-tabpop__sep"></div>' +
    '<div class="c-tabpop__foot"' + (range ? ' title="' + escHtml(range) + '"' : '') + '>' +
      '<span>' + _tabPopT('tabpop.frames', '帧') + ' <b>' + loaded + '/' + totalFrames + '</b></span>' +
      '<span>' + _tabPopT('tabpop.span', '时长') + ' <b>' + _tabPopSpanText(info.firstTs, info.lastTs) + '</b></span>' +
      '<span>' + _tabPopT('tabpop.sources', '来源') + ' <b>' +
        (srcCount ? srcCount : _tabPopT('tabpop.none', '无')) + '</b></span>' +
    '</div>';
  _tabPop.fileSig = _tabPopFileSig();
  _tabPop.lastH = el.offsetHeight;
}

// 时长：lastTs − firstTs → H:MM:SS（文件卡页脚用）
function _tabPopSpanText(firstTs, lastTs) {
  if (typeof firstTs !== 'number' || typeof lastTs !== 'number' || lastTs < firstTs) return '—';
  var s = Math.round((lastTs - firstTs) / 1000);
  var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  function p2(n) { return n < 10 ? '0' + n : '' + n; }
  return h + ':' + p2(m) + ':' + p2(sec);
}

// 文件浮窗只需要在"已载入帧数"变化时重画（其余字段是静态的）
function _tabPopFileSig() {
  var st = (_tabPop && _tabPop.tab && _tabPop.tab._stlog) || {};
  var info = st.info || {};
  return ['file', info.path || '', info.total || 0, (st.frames ? st.frames.length : 0)].join('|');
}

function _tabPopRender() {
  if (!_tabPop || !_tabPopEl) return;
  // 文件标签没有会话概念，走专门的卡片（否则就是一片 —，见 _tabPopRenderFile）
  if (_tabPop.tab && _tabPop.tab.type === 'stlog') { _tabPopRenderFile(); return; }
  var el = _tabPopEl;
  var tab = _tabPop.tab;
  var m = _tabPop.model;
  var v = _tabPopValues();
  var isFwd = tab.mode === 'forward';
  var on = v.live;           // 徽标 / 速率 / 条形
  var hasData = v.hasData;   // 累计 / 错误 / 收发比

  var badgeCls = on ? 'is-ok' : (m.declared ? 'is-warn' : 'is-dim');
  var badgeTxt = on ? _tabPopT('tabpop.state.connected', '已连接')
    : (m.declared ? _tabPopT('tabpop.state.declared', '已声明') : _tabPopT('tabpop.state.idle', '未连接'));

  var portsHTML = '';
  for (var i = 0; i < m.ports.length; i++) {
    var p = m.ports[i];
    var dev = state.portDescs[p.port] || '';
    portsHTML += '<div class="c-tabpop__port">' +
      (p.tag ? '<span class="c-tabpop__ptag" dir="ltr">' + p.tag + '</span>' : '') +
      '<span class="c-tabpop__pname" dir="ltr">' + escHtml(p.port) + '</span>' +
      (dev ? '<bdi class="c-tabpop__pdev">' + escHtml(dev) + '</bdi>' : '') +
    '</div>';
  }

  var base = m.baud ? m.baud / 10 : 0;
  var pctRx = (base && on) ? Math.min(100, Math.round(v.rxRate / base * 100)) : 0;
  var pctTx = (base && on) ? Math.min(100, Math.round(v.txRate / base * 100)) : 0;
  var totalRx = hasData ? fmtBytes(v.rxTotal) : '—';
  var totalTx = hasData ? fmtBytes(v.txTotal) : '—';
  var rateRx = on ? _tabPopRateText(v.rxRate) : '—';
  var rateTx = on ? _tabPopRateText(v.txRate) : '—';

  var lanes = '';
  if (isFwd) {
    lanes += _tabPopLaneHTML('A→B', 'var(--fwd-p1)', totalRx, rateRx, pctRx, on, 0);
    lanes += _tabPopLaneHTML('B→A', 'var(--fwd-p2)', totalTx, rateTx, pctTx, on, 1);
  } else {
    lanes += _tabPopLaneHTML('⇃ RX', 'var(--green)', totalRx, rateRx, pctRx, on, 0);
    lanes += _tabPopLaneHTML('↾ TX', 'var(--accent)', totalTx, rateTx, pctTx, on, 1);
  }

  // 收发比一直画：没数据时是一条空轨道 + 破折号，不整行消失
  var cRx = isFwd ? 'var(--fwd-p1)' : 'var(--green)';
  var cTx = isFwd ? 'var(--fwd-p2)' : 'var(--accent)';
  var sum = v.rxTotal + v.txTotal;
  var hasRatio = hasData && sum > 0;
  var rp = hasRatio ? Math.round(v.rxTotal / sum * 100) : 0;
  var ratioHTML = '<div class="c-tabpop__ratio">' +
    '<span class="c-tabpop__rlbl">' + (isFwd ? _tabPopT('tabpop.ratio_fwd', '分流比') : _tabPopT('tabpop.ratio', '收发比')) + '</span>' +
    '<span class="c-tabpop__rbar">' +
      '<i data-role="ratio-rx" style="width:' + rp + '%;background:' + cRx + '"></i>' +
      '<i data-role="ratio-tx" style="width:' + (hasRatio ? (100 - rp) : 0) + '%;background:' + cTx + '"></i>' +
    '</span>' +
    '<span class="c-tabpop__rval" data-role="ratio-val" dir="ltr">' + _tabPopRatioValHTML(hasRatio, rp, cRx, cTx) + '</span>' +
  '</div>';

  var viewers = tab.viewers || { gui: 0, cli: 0, mcp: 0 };
  var clients = (viewers.gui || 0) + (viewers.cli || 0) + (viewers.mcp || 0);

  el.innerHTML =
    '<div class="c-tabpop__head">' +
      // 标题用线程名：它是这个会话唯一的标识，会话号退到悬浮卡的页脚信息里
      '<span class="c-tabpop__title">' + escHtml(_tabNameOf(tab)) + '</span>' +
      '<span class="c-tabpop__badge ' + badgeCls + '"><i class="c-tabpop__dot"></i>' + badgeTxt + '</span>' +
    '</div>' +
    (portsHTML ? '<div class="c-tabpop__ports">' + portsHTML + '</div>' : '') +
    (m.params ? '<div class="c-tabpop__meta" dir="ltr">' + m.params + '</div>' : '') +
    // 保存开关搬到这里：标签上已经不放磁盘图标了
    '<div class="c-tabpop__save">' + _tabPopT('tabpop.save', '保存') + ' ' +
      '<b data-role="save">' + (tab.histOn === true ? _tabPopT('tabpop.on', '开') : (tab.histOn === false ? _tabPopT('tabpop.off', '关') : '—')) + '</b>' +
      '<span class="c-tabpop__sid" dir="ltr">#' + escHtml(String(tab.id)) + '</span>' +
    '</div>' +
    '<div class="c-tabpop__sep"></div>' +
    lanes +
    ratioHTML +
    '<div class="c-tabpop__sep"></div>' +
    '<div class="c-tabpop__foot">' +
      '<span>' + _tabPopT('tabpop.clients', '客户端') + ' <b dir="ltr">' + clients + '</b></span>' +
      '<span>' + _tabPopT('tabpop.errors', '错误') + ' <b dir="ltr" data-role="errors">' + (hasData ? (v.errRx + '/' + v.errTx) : '0/0') + '</b></span>' +
      '<span>' + _tabPopT('tabpop.uptime', '运行') + ' <b dir="ltr" data-role="uptime">' + v.uptime + '</b></span>' +
    '</div>' +
    ((!on && m.declared) ? '<div class="c-tabpop__hint">' + _tabPopT('tabpop.connect_hint', '点击标签连接此端口') + '</div>' : '');
  _tabPop.sig = _tabPopSig();
  _tabPop.lastH = el.offsetHeight;
}

// 只更新数值：不动 DOM 结构，条形靠 transition 平滑跟随
function _tabPopUpdate() {
  if (!_tabPop || !_tabPopEl) return;
  // 文件标签：信息是静态的，只有「已载入帧数」会变，签名一变重画一次就够
  if (_tabPop.tab && _tabPop.tab.type === 'stlog') {
    if (_tabPop.fileSig !== _tabPopFileSig()) _tabPopRenderFile();
    return;
  }
  var el = _tabPopEl;
  if (_tabPop.sig !== _tabPopSig()) {
    _tabPopRender();
    _tabPopPlace(_tabPop.tabEl);
    return;
  }
  var tab = _tabPop.tab, m = _tabPop.model;
  var v = _tabPopValues();
  var isFwd = tab.mode === 'forward';
  var on = v.live;
  var hasData = v.hasData;
  var base = m.baud ? m.baud / 10 : 0;
  var pctRx = (base && on) ? Math.min(100, Math.round(v.rxRate / base * 100)) : 0;
  var pctTx = (base && on) ? Math.min(100, Math.round(v.txRate / base * 100)) : 0;
  var cRx = isFwd ? 'var(--fwd-p1)' : 'var(--green)';
  var cTx = isFwd ? 'var(--fwd-p2)' : 'var(--accent)';

  function q(role) { return el.querySelector('[data-role="' + role + '"]'); }
  function setText(role, txt) { var n = q(role); if (n) n.textContent = txt; }
  function setBar(role, pct, color) {
    var n = q(role);
    if (!n) return;
    n.style.width = pct + '%';
    n.style.background = color;
  }

  setText('total0', hasData ? fmtBytes(v.rxTotal) : '—');
  setText('rate0', on ? _tabPopRateText(v.rxRate) : '—');
  setBar('bar0', pctRx, _tabPopBarColor(pctRx, cRx, on));
  setText('total1', hasData ? fmtBytes(v.txTotal) : '—');
  setText('rate1', on ? _tabPopRateText(v.txRate) : '—');
  setBar('bar1', pctTx, _tabPopBarColor(pctTx, cTx, on));

  var sum = v.rxTotal + v.txTotal;
  var hasRatio = hasData && sum > 0;
  var rp = hasRatio ? Math.round(v.rxTotal / sum * 100) : 0;
  setBar('ratio-rx', rp, cRx);
  setBar('ratio-tx', hasRatio ? (100 - rp) : 0, cTx);
  var rv = q('ratio-val');
  if (rv) rv.innerHTML = _tabPopRatioValHTML(hasRatio, rp, cRx, cTx);

  setText('errors', hasData ? (v.errRx + '/' + v.errTx) : '0/0');
  setText('uptime', v.uptime);

  // 文本长度变了导致高度变化时重新摆一次
  var h = el.offsetHeight;
  if (_tabPop.lastH !== h) {
    _tabPop.lastH = h;
    _tabPopPlace(_tabPop.tabEl);
  }
}

function _tabPopEnsure() {
  if (_tabPopEl) return _tabPopEl;
  var el = document.createElement('div');
  el.className = 'c-tabpop is-hidden';
  el.setAttribute('aria-hidden', 'true');
  document.body.appendChild(el);
  _tabPopEl = el;
  return el;
}

function _tabPopPlace(tabEl) {
  var el = _tabPopEl;
  if (!el || !tabEl) return;
  var r = tabEl.getBoundingClientRect();
  var w = el.offsetWidth, h = el.offsetHeight;
  var left;
  if (_tabPopIsRTL()) {
    // RTL：默认让浮窗右缘对齐标签右缘（视觉上与标签「同侧起」），左边放不下再改左缘
    left = r.right - w;
    if (left < 10) left = Math.min(window.innerWidth - w - 10, r.left);
  } else {
    left = r.left;
    if (left + w > window.innerWidth - 10) left = Math.max(10, r.right - w);
  }
  left = Math.max(10, Math.min(Math.max(10, window.innerWidth - w - 10), left));
  var top = r.bottom + 8;
  if (top + h > window.innerHeight - 10) {
    top = Math.max(10, r.top - h - 8);
  }
  el.style.left = left + 'px';
  el.style.top = top + 'px';
}

function _tabPopOpen(tabEl, idx) {
  var tab = state.tabs[idx];
  if (!tab) return;
  var el = _tabPopEnsure();
  _tabPop = {
    tabEl: tabEl, tab: tab, idx: idx,
    sessionId: tab.sessionId || '',
    stats: null,
    uptimeAt: 0,
    model: _tabPopModel(tab),
  };
  el.classList.remove('is-hidden');
  _tabPopRender();
  _tabPopPlace(tabEl);
  requestAnimationFrame(function () { if (_tabPop) el.classList.add('is-visible'); });
  _tabPopStartTick();
  if (!tab.sessionId) return;
  // 保存开关搬进了悬浮卡：本地的 tab.histOn 可能还不知道，开卡时补问一次。
  // 只问一次不会循环：拿到值就写回 tab，签名变了会重画一次卡。
  if (tab.histOn === undefined) {
    window.go.main.App.GetHistoryStatusFor(tab.sessionId).then(function (on) {
      tab.histOn = !!on;
      if (_tabPop && _tabPop.tab === tab) _tabPopUpdate();
    }).catch(function () {});
  }
  window.go.main.App.GetSessionStats(tab.sessionId).then(function (res) {
    if (!_tabPop || _tabPop.sessionId !== tab.sessionId) return;
    if (res && res.stats) {
      _tabPop.stats = res.stats;
      _tabPop.uptimeAt = Date.now();   // 从这一刻起本地推算运行时长
      _tabPopUpdate();
    }
  }).catch(function () {});
}

// 「运行」秒数要跳：uptimeSec 只在打开时取到一次，之后靠本地时钟补
var _tabPopTickTimer = null;

function _tabPopStartTick() {
  _tabPopStopTick();
  _tabPopTickTimer = setInterval(function () {
    if (!_tabPop) { _tabPopStopTick(); return; }
    _tabPopUpdate();
  }, 1000);
}

function _tabPopStopTick() {
  if (_tabPopTickTimer) {
    clearInterval(_tabPopTickTimer);
    _tabPopTickTimer = null;
  }
}

function _tabPopClose() {
  clearTimeout(_tabPopShowTimer);
  clearTimeout(_tabPopHideTimer);
  _tabPopShowTimer = null;
  _tabPopHideTimer = null;
  _tabPopStopTick();
  _tabPop = null;
  if (!_tabPopEl) return;
  var el = _tabPopEl;
  el.classList.remove('is-visible');
  setTimeout(function () { if (!_tabPop) el.classList.add('is-hidden'); }, 130);
}

function _tabPopSchedule(tabEl, idx) {
  if (document.body.classList.contains('is-tab-dragging')) return;
  // 刚点过标签：切换会重绘按钮，指针下的新按钮会再收到一次 pointerenter，
  // 不挡一下的话浮窗会在点击后 140ms 又自己弹出来。
  if (Date.now() - (window._tabPopClickedAt || 0) < 400) return;
  clearTimeout(_tabPopHideTimer);
  clearTimeout(_tabPopShowTimer);
  if (_tabPop) {
    if (_tabPop.tabEl === tabEl) return;   // 已经在这枚标签上，别重建
    _tabPopClose();
  }
  _tabPopShowTimer = setTimeout(function () { _tabPopOpen(tabEl, idx); }, _TABPOP_SHOW_DELAY);
}

function _tabPopScheduleClose() {
  clearTimeout(_tabPopShowTimer);
  clearTimeout(_tabPopHideTimer);
  _tabPopHideTimer = setTimeout(_tabPopClose, _TABPOP_HIDE_DELAY);
}

// 滚动 / 缩放时浮窗要么跟着锚点走，要么（标签已经滚出可视区）收起。
// 不能一见 scroll 就关：hover 本身可能带一次 scrollIntoView，会把自己的浮窗打掉。
function _tabPopReposition() {
  if (!_tabPop || !_tabPop.tabEl) return;
  var r = _tabPop.tabEl.getBoundingClientRect();
  var bar = document.getElementById('tabsBar');
  if (bar) {
    var br = bar.getBoundingClientRect();
    if (r.right <= br.left || r.left >= br.right) { _tabPopClose(); return; }
  }
  _tabPopPlace(_tabPop.tabEl);
}

// 滚动 / 缩放 / 换语言时锚点位置会变，跟着挪
function initTabPopup() {
  window.addEventListener('resize', _tabPopReposition);
  var scroll = document.getElementById('tabsScroll');
  if (scroll) scroll.addEventListener('scroll', _tabPopReposition, { passive: true });
}

// ---- 标签右键菜单（自绘，复用顶部菜单的 .menu-dropdown 皮肤） ----
//
// 为什么不用原生菜单：Wails 的菜单是应用级菜单栏，不能按标签弹出；原生
// contextmenu 又会盖住自绘菜单并且样式不可控。这里直接在 body 下挂一个
// fixed 浮层，跟随鼠标、贴边翻转，点空白 / Esc / 滚轮 / 失焦都会关。
var _tabMenuEl = null;

function _tabMenuOnDown(e) {
  if (_tabMenuEl && !_tabMenuEl.contains(e.target)) _tabMenuClose();
}

function _tabMenuOnKey(e) {
  if (e.key === 'Escape') _tabMenuClose();
}

function _tabMenuClose() {
  if (!_tabMenuEl) return;
  var el = _tabMenuEl;
  _tabMenuEl = null;
  if (el.parentNode) el.parentNode.removeChild(el);
  document.removeEventListener('pointerdown', _tabMenuOnDown, true);
  document.removeEventListener('keydown', _tabMenuOnKey, true);
  document.removeEventListener('scroll', _tabMenuClose, true);
  window.removeEventListener('blur', _tabMenuClose);
  window.removeEventListener('resize', _tabMenuClose);
}

// 菜单项沿用顶部菜单的四栏网格：勾选位 | 文本 | 快捷键 | 箭头。
function _tabMenuItem(label, opts) {
  opts = opts || {};
  var btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'tab-menu-item';
  btn.setAttribute('role', 'menuitem');
  var chk = document.createElement('span');
  chk.className = 'menu-chk chk' + (opts.checked ? ' on' : '');
  btn.appendChild(chk);
  var txt = document.createElement('span');
  txt.className = 'menu-text';
  txt.textContent = label;
  btn.appendChild(txt);
  var shortcut = document.createElement('span');
  shortcut.className = 'menu-shortcut';
  shortcut.textContent = opts.shortcut || '';
  btn.appendChild(shortcut);
  var arrow = document.createElement('span');
  arrow.className = 'menu-arrow';
  btn.appendChild(arrow);
  btn.addEventListener('click', function (e) {
    e.stopPropagation();
    _tabMenuClose();
    if (opts.onClick) opts.onClick();
  });
  return btn;
}

// ---- 标签就地改名 ----
//
// 标签上只有「图标 + 状态点 + 线程名」，所以改名就地把名字那段换成 input，
// 不再另开对话框：
//   回车 / 失焦 → 提交；Esc → 取消
//   会话标签 → RenameSession（守护进程广播 process-changed 后重画）
//   文件标签 → RenameStlogFile（改磁盘上的 .stlog 目录/文件名）
var _tabRenaming = false;
function _tabRenameStart(btn, idx) {
  if (_tabRenaming) return;
  var tab = state.tabs[idx];
  if (!tab || tab.type === 'settings' || tab.type === 'new') return;
  // 双击的第一下是「切标签」，switchTab 会重绘整个标签栏，传进来的按钮可能已经
  // 不在文档里了。重新按索引找一枚当前挂在 DOM 上的。
  if (!btn || !document.contains(btn)) {
    btn = document.querySelector('.tab-btn[data-tab-idx="' + idx + '"]');
  }
  if (!btn) return;
  var nameEl = btn.querySelector('.tab-port');
  if (!nameEl || !nameEl.parentNode) return;
  if (tab.type === 'stlog' && !tab.path) return;

  _tabRenaming = true;
  _tabPopClose();          // 悬停卡别挡住输入
  _tabMenuClose();
  var old = nameEl.textContent;
  var input = document.createElement('input');
  input.className = 'tab-rename';
  input.type = 'text';
  input.value = old;
  input.setAttribute('spellcheck', 'false');
  nameEl.parentNode.replaceChild(input, nameEl);
  input.focus();
  input.select();

  var finished = false;
  function restore() {
    if (input.parentNode) input.parentNode.replaceChild(nameEl, input);
    nameEl.textContent = old;
  }
  function finish(commit) {
    if (finished) return;
    finished = true;
    _tabRenaming = false;
    var val = String(input.value || '').trim();
    if (!commit || val === old) { restore(); return; }

    if (tab.type === 'stlog') {
      // 文件标签：改磁盘名。失败要还原并把原因说出来（比如被占用 / 同名已存在）
      window.go.main.App.RenameStlogFile(tab.path, val).then(function (newPath) {
        if (!newPath) { restore(); return; }
        tab.path = newPath;
        tab.label = stlogBaseName(newPath);
        if (tab._page && tab._page.root) {
          var nameNode = tab._page.root.querySelector('.t-stlog-name');
          var metaNode = tab._page.root.querySelector('.t-stlog-meta');
          if (nameNode) nameNode.textContent = tab.label;
          if (metaNode) metaNode.textContent = newPath;
        }
        renderTabs();
      }).catch(function (err) {
        restore();
        showAlert(t('tabmenu.rename', '重命名'), String(err && err.message ? err.message : err));
      });
      return;
    }

    // 会话标签：名字归守护进程；本地先乐观更新，广播回来会再覆盖一次
    tab.name = val;
    tab.nameCustom = true;
    renderTabs();
    window.go.main.App.RenameSession(tab.sessionId, val).catch(function (err) {
      // 守护进程拒绝（比如名字太长）就把本地乐观值撤回，别让界面显示一个没生效的名字
      tab.name = old;
      tab.nameCustom = false;
      renderTabs();
      showAlert(t('tabmenu.rename', '重命名'), String(err && err.message ? err.message : err));
    });
  }

  input.addEventListener('keydown', function (e) {
    e.stopPropagation();   // 别让全局快捷键（删除标签等）在输入时触发
    if (e.key === 'Enter') { e.preventDefault(); finish(true); }
    else if (e.key === 'Escape') { e.preventDefault(); finish(false); }
  });
  input.addEventListener('blur', function () { finish(true); });
}

function _tabMenuSep() {
  var sep = document.createElement('div');
  sep.className = 'menu-sep';
  return sep;
}

function _tabMenuCopy(text) {
  if (!text) return;
  try {
    if (window.runtime && typeof window.runtime.ClipboardSetText === 'function') {
      window.runtime.ClipboardSetText(String(text));
      return;
    }
  } catch (e) { /* 回退到浏览器剪贴板 */ }
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(String(text));
  } catch (e) { /* 忽略 */ }
}

function _closeOtherTabs(keepTab) {
  var guard = state.tabs.length + 2;   // closeTab 会重排数组，用对象引用定位要保留的标签
  while (state.tabs.length > 1 && guard-- > 0) {
    var j = -1;
    for (var i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i] !== keepTab) { j = i; break; }
    }
    if (j < 0) break;
    closeTab(j);
  }
}

function _tabMenuItemT(key, fb) {
  var v = (typeof t === 'function') ? t(key, fb) : '';
  return v || fb;
}

function _tabMenuOpen(clientX, clientY, idx) {
  _tabMenuClose();
  if (typeof _tabPopClose === 'function') _tabPopClose();   // 悬浮卡片让位给菜单
  var tab = state.tabs[idx];
  if (!tab) return;

  var isSession = tab.type !== 'settings' && tab.type !== 'new' && !!tab.sessionId;
  var isFileTab = tab.type === 'stlog';
  var menu = document.createElement('div');
  menu.className = 'menu-dropdown tab-menu';
  menu.setAttribute('role', 'menu');

  // 「重命名」：会话改守护进程里的线程名，文件标签改磁盘文件名
  if (isSession || isFileTab) {
    menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.rename', '重命名'), {
      onClick: function () {
        var btn = document.querySelector('.tab-btn[data-tab-idx="' + idx + '"]');
        if (btn) _tabRenameStart(btn, idx);
      }
    }));
    if (isSession && tab.nameCustom) {
      menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.reset_name', '恢复自动名'), {
        onClick: function () {
          window.go.main.App.RenameSession(tab.sessionId, '').catch(function () {});
        }
      }));
    }
    menu.appendChild(_tabMenuSep());
  }

  if (isSession) {
    // 「保存历史」按进程生效（history.enable/status + processId）：勾选态先按
    // 未知渲染，异步取到真实值再点亮，避免右键时卡一个 IPC 往返。
    var saveOn = false;
    var saveItem = _tabMenuItem(_tabMenuItemT('tabmenu.save_history', '保存历史'), {
      checked: false,
      onClick: function () {
        saveOn = !saveOn;
        // 标签上已经不再显示保存标记（它搬进悬浮卡了），这里只更新缓存，
        // 下次打开卡片能立刻显示正确的开/关。
        tab.histOn = saveOn;
        window.go.main.App.SetHistoryEnabledFor(tab.sessionId, saveOn).catch(function () {});
      }
    });
    menu.appendChild(saveItem);
    window.go.main.App.GetHistoryStatusFor(tab.sessionId).then(function (on) {
      saveOn = !!on;
      tab.histOn = saveOn;
      var chk = saveItem.querySelector('.menu-chk');
      if (chk) chk.classList.toggle('on', saveOn);
    }).catch(function () {});

    menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.copy_session', '复制会话号'), {
      onClick: function () { _tabMenuCopy(tab.sessionId); }
    }));
    var portName = tab.syncedParams && tab.syncedParams.portName;
    if (portName) {
      menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.copy_port', '复制端口名'), {
        onClick: function () { _tabMenuCopy(portName); }
      }));
    }
    menu.appendChild(_tabMenuSep());
  }

  menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.close', '关闭标签'), {
    onClick: function () { closeTab(idx); }
  }));
  menu.appendChild(_tabMenuItem(_tabMenuItemT('tabmenu.close_others', '关闭其他标签'), {
    onClick: function () { _closeOtherTabs(tab); }
  }));

  document.body.appendChild(menu);
  // 贴边翻转：右侧/底部放不下就翻到鼠标另一侧，并至少留 4px 边距。
  var mw = menu.offsetWidth || 176;
  var mh = menu.offsetHeight || 40;
  var x = clientX, y = clientY;
  if (x + mw + 4 > window.innerWidth) x = Math.max(4, x - mw);
  if (y + mh + 4 > window.innerHeight) y = Math.max(4, y - mh);
  menu.style.left = Math.round(x) + 'px';
  menu.style.top = Math.round(y) + 'px';

  _tabMenuEl = menu;
  document.addEventListener('pointerdown', _tabMenuOnDown, true);
  document.addEventListener('keydown', _tabMenuOnKey, true);
  document.addEventListener('scroll', _tabMenuClose, true);
  window.addEventListener('blur', _tabMenuClose);
  window.addEventListener('resize', _tabMenuClose);
}

// ---- 标签栏内容：类型图标 + 状态点 + 信息槽 ----
//
// 设计稿 0.8.4-dev/tab-icon-preview-v2.html 的 P4 方案。要解决的是「六套信号说三件事」：
// 以前一个标签上同时有 ●圆点、类型图标颜色、空闲斜体、转发绿加粗、未激活压暗、
// viewers 数字，可它们只表达「已连接 / 空闲 / 转发」。现在分工是：
//
//   类型   → 左侧 14px 图标，中性灰，靠形状（terminal / swap / file / folder / settings / plus）
//   状态   → 图标右下角 5px 点（绿=已连、紫=转发、半透明灰=空闲、空心环=只读）
//   信息   → 参数、保存、速率；不拥挤才显示，按速率→保存→参数顺序退让
//
// 唯一的例外是只读标签：文件（.stlog）和没推给守护进程的工程，状态点画成空心环，
// 另外整枚标签用虚线边框 —— 它们不是「会话状态」，不该占用彩色语义。

// 类型图标：只有这六种
function _tabIconKey(tb) {
  if (tb.type === 'settings') return 'settings';
  if (tb.type === 'new') return 'plus';
  if (tb.type === 'stlog') return (tb._stlog && tb._stlog.isDir) ? 'folder' : 'file';
  return tb.mode === 'forward' ? 'swap' : 'terminal';
}

// 状态：'' 表示不给点（设置页、新建页不是会话）
function _tabStateClass(tb) {
  if (tb.type === 'settings' || tb.type === 'new') return '';
  if (tb.type === 'stlog') return 'is-ro';
  if (!tb.portOpen) return 'is-idle';
  return tb.mode === 'forward' ? 'is-fwd' : 'is-live';
}

// 标签上只剩三段：类型图标 / 状态点 / 线程名。
//
// 线程名归守护进程所有（process.list 的 name 字段）：GUI 不再自己拼
// 「COM3 @ 115200」「端口转发空闲」这类文案，因为 CLI 和 MCP 也在看同一个名字，
// 各端各拼一份必然对不上。名字为空时退回端口名/会话号。
function _tabNameOf(tb) {
  if (tb.type === 'settings' || tb.type === 'new') return tb.label || '';
  if (tb.name) return tb.name;
  if (tb.type === 'stlog') return tb.label || stlogBaseName(tb.path || '');
  if (tb.sessionPath) return stlogBaseName(tb.sessionPath);
  var sp = tb.syncedParams;
  if (sp && sp.portName) return sp.portName;
  // 还没收到守护进程的 name（新建会话到第一条 process-changed 之间）：给个中性占位，
  // 别再显示「单端口空闲」那种状态文案 —— 状态由图标右下角那枚点负责。
  return '#' + tb.id;
}

// ---- 标签栏外观设置（设置 → 标签页）----
//
// 全部是 GUI 本地偏好（gui.ini），只影响标签栏怎么画。开关一变就调
// _applyTabDisplaySettings() 把状态写到 body 的属性上（CSS 负责画），
// 再重画标签 —— 第二行与客户端数字是 JS 拼出来的，必须重来一遍。

function _applyTabDisplaySettings() {
  var b = document.body;
  if (!b) return;
  b.setAttribute('data-tab-lines', state.tabTwoLines === true ? '2' : '1');
  b.setAttribute('data-tab-dot', state.tabShowDot === false ? 'off' : 'on');
  b.setAttribute('data-tab-cli', state.tabShowClients === false ? 'off' : 'on');
  b.setAttribute('data-tab-collapse', state.tabCollapse === 'clients' ? 'clients' : 'sub');
  b.style.setProperty('--tab-name-max', (state.tabNameMax || 170) + 'px');
}

// 客户端数字：0.8.3 原样是分来源彩色（GUI 蓝 / CLI 绿 / MCP 橙）。
// 只有 GUI 自己一个时整块省略 —— 那是「我在看自己的会话」，写个 1 只是噪声。
function _tabClientsNode(tb) {
  if (state.tabShowClients === false) return null;
  if (tb.type === 'settings' || tb.type === 'new') return null;
  var v = tb.viewers || {};
  var gui = v.gui || 0, cli = v.cli || 0, mcp = v.mcp || 0;
  var total = gui + cli + mcp;
  if (total <= 0) return null;
  if (total === 1 && gui === 1) return null;

  var el = document.createElement('span');
  el.className = 'tab-cli';
  el.setAttribute('dir', 'ltr');   // 阿拉伯语界面下数字顺序不能被翻转
  if (state.tabClientStyle === 'total') {
    el.textContent = String(total);
    return el;
  }
  el.classList.add('tab-cli--src');
  [['g', gui], ['c', cli], ['m', mcp]].forEach(function (pair) {
    if (!pair[1]) return;
    var i = document.createElement('i');
    i.className = 'tab-cli__' + pair[0];
    i.textContent = String(pair[1]);
    el.appendChild(i);
  });
  return el;
}

// 第二行明细（仅双行标签）。三种组合由「第二行内容」设置决定：
//   a 端口 · 参数    b 参数 · 速率    c 保存 · 客户端
function _tabSubText(tb) {
  if (state.tabTwoLines !== true) return '';
  if (tb.type === 'settings' || tb.type === 'new') return '';

  if (tb.type === 'stlog') {
    var st = tb._stlog || {};
    var info = st.info || {};
    if (st.isDir) {
      if (state.tabSubLine === 'b') {
        var set = info.settings || {};
        return set.port ? (set.port + (set.baud ? ' · ' + set.baud : '')) : _tabPopT('tab.sub_project', '工程');
      }
      if (state.tabSubLine === 'c') return _tabPopT('tab.sub_readonly', '只读') + ' · ' + _tabPopT('tab.sub_not_taken', '未接管');
      return _tabPopT('tab.sub_project', '工程') + ' · ' + _tabPopT('tab.sub_not_taken', '未接管');
    }
    // 单个 .stlog：帧数 + 时长（都在已读到的索引里，不额外读盘）
    var frames = st.total || 0;
    var dur = '';
    if (info.firstTs && info.lastTs && info.lastTs > info.firstTs) {
      dur = _tabPopDuration((info.lastTs - info.firstTs) / 1000);
    }
    if (state.tabSubLine === 'b') {
      var s2 = info.settings || {};
      return (s2.port || '') + (dur ? (s2.port ? ' · ' : '') + dur : '');
    }
    if (state.tabSubLine === 'c') return _tabPopT('tab.sub_readonly', '只读') + ' · ' + frames + ' ' + _tabPopT('tabpop.frames', '帧');
    return frames + ' ' + _tabPopT('tabpop.frames', '帧') + (dur ? ' · ' + dur : '');
  }

  var sp = tb.syncedParams;
  if (!sp || !sp.portName) {
    return state.tabSubLine === 'c'
      ? (_tabPopT('tabpop.save', '保存') + ' ' + _tabPopT('tab.sub_unset', '未配置'))
      : _tabPopT('tab.sub_unset', '未配置');
  }
  var name = _tabNameOf(tb);
  var port = sp.portName;
  var baud = sp.baud ? String(sp.baud) : '';
  var fmt = (sp.dataBits || 8) + _tabPopParityLetter(sp.parity) + (sp.stopBits || 1);
  var rate = '';
  var base = state.statsBase && tb.sessionId ? state.statsBase[tb.sessionId] : null;
  if (base) rate = _tabRateText((base.rxRate || 0) + (base.txRate || 0));
  var saveTxt = tb.histOn === true ? _tabPopT('tabpop.on', '开') : (tb.histOn === false ? _tabPopT('tabpop.off', '关') : '—');
  var cliTotal = 0;
  var vw = tb.viewers || {};
  cliTotal = (vw.gui || 0) + (vw.cli || 0) + (vw.mcp || 0);

  if (state.tabSubLine === 'b') {
    var parts2 = [baud ? (baud + (fmt ? ' ' + fmt : '')) : ''];
    if (rate) parts2.push(rate);
    return parts2.filter(Boolean).join(' · ');
  }
  if (state.tabSubLine === 'c') {
    // 「保存 · 客户端」：客户端数已经在这行里，就不再单独挂数字
    var parts3 = [_tabPopT('tabpop.save', '保存') + ' ' + saveTxt];
    if (state.tabShowClients !== false && cliTotal > 0) parts3.push(_tabPopT('tabpop.clients', '客户端') + ' ' + cliTotal);
    return parts3.join(' · ');
  }
  // a：端口 · 参数。名字本身就是端口时不重复
  var parts = [];
  if (port && port !== name) parts.push(port);
  if (baud) parts.push(baud + (fmt ? ' ' + fmt : ''));
  return parts.join(' · ');
}

// 紧凑速率：标签上一行放不下 "kB/s"，只留单位首字母
function _tabRateText(bps) {
  bps = Number(bps) || 0;
  if (bps <= 0) return '';
  if (bps >= 1048576) return (bps / 1048576).toFixed(1) + 'M';
  if (bps >= 1024) return (bps / 1024).toFixed(bps < 10240 ? 1 : 0) + 'k';
  return Math.round(bps) + 'B';
}

// 第二行是「参数 · 速率」时，速率每秒变：只改文本，不重建标签
function _updateTabSubTexts() {
  if (state.tabTwoLines !== true || state.tabSubLine !== 'b') return;
  var scroll = document.getElementById('tabsScroll');
  if (!scroll) return;
  var nodes = scroll.querySelectorAll('[data-role="sub"]');
  for (var i = 0; i < nodes.length; i++) {
    var btn = nodes[i].closest('.tab-btn');
    if (!btn) continue;
    var idx = parseInt(btn.getAttribute('data-tab-idx'), 10);
    var tb = state.tabs[idx];
    if (!tb) continue;
    var txt = _tabSubText(tb);
    if (nodes[i].textContent !== txt) nodes[i].textContent = txt;
  }
}

// 「保存」开关是按进程的，只有右键菜单里能改。第二行选「保存 · 客户端」时要在标签上
// 显示它，所以渲染后懒加载一次：拿到就写回 tab.histOn，签名变了自然会重画。
// 只在真的要用时才问 —— 否则每次 renderTabs 都发一批 IPC。
var _tabHistFetching = {};
function _ensureTabsHist() {
  if (state.tabTwoLines !== true || state.tabSubLine !== 'c') return;
  if (!state.daemonOnline) return;
  state.tabs.forEach(function (tb) {
    if (!tb || tb.type || !tb.sessionId) return;
    if (tb.histOn !== undefined || _tabHistFetching[tb.sessionId]) return;
    _tabHistFetching[tb.sessionId] = true;
    var sid = tb.sessionId;
    window.go.main.App.GetHistoryStatusFor(sid).then(function (on) {
      delete _tabHistFetching[sid];
      tb.histOn = !!on;
      if (state.tabs.indexOf(tb) >= 0) renderTabs();
    }).catch(function () {
      delete _tabHistFetching[sid];
      tb.histOn = false;
    });
  });
}

// 标签栏密度：放不下就逐级收内容（顺序由「拥挤时先收起」决定）。
// 尺子和 _updateTabArrows 同一把：scrollWidth vs clientWidth。
var _tabBarWidth = -1;
function _updateTabDensity(force) {
  var scroll = document.getElementById('tabsScroll');
  if (!scroll) return;
  if (!force && scroll.clientWidth === _tabBarWidth) return;
  _tabBarWidth = scroll.clientWidth;
  for (var lvl = 0; lvl < 4; lvl++) {
    scroll.setAttribute('data-lvl', String(lvl));
    if (scroll.scrollWidth <= scroll.clientWidth) return;
  }
  scroll.setAttribute('data-lvl', '4');
}

function renderTabs() {
  const scroll = document.getElementById('tabsScroll');
  if (!scroll) return;
  _tabMenuClose();   // 标签节点即将重建，菜单挂在旧位置没有意义
  scroll.innerHTML = '';
  state.tabs.forEach((t, i) => {
    if (t._page && t._page.isWelcome) return;  // skip welcome tab
    const btn = document.createElement('button');
    // 状态只体现在图标右下角那枚点上，颜色不再挂在文字上；只读标签另给虚线边框。
    const stateCls = _tabStateClass(t);
    btn.className = 'tab-btn' + (i === state.activeTab ? ' is-active' : '')
      + (stateCls === 'is-ro' ? ' is-ro' : '');
    btn.dataset.tabIdx = i;
    btn.onclick = () => {
      if (window._tabClickSuppress) return;
      window._tabPopClickedAt = Date.now();   // 重绘后别再自动弹出，见 _tabPopSchedule
      _tabPopClose();
      switchTab(i);
    };
    // 拖动排序走 pointer 事件（见 _tabDragDown）：HTML5 拖放在隐藏源元素时会被
    // 浏览器取消，拖不动。
    btn.onpointerdown = _tabDragDown;
    // 右键菜单：自绘，按标签类型给不同菜单项（会话 / 设置 / 新建）
    btn.oncontextmenu = (e) => {
      e.preventDefault();
      e.stopPropagation();
      _tabMenuOpen(e.clientX, e.clientY, i);
    };
    const isSettings = (t.type === 'settings');
    const isChooser = (t.type === 'new');
    // 会话标签：悬停出自绘浮窗（c-tabpop），不再挂原生 title
    if (!isSettings && !isChooser) {
      btn.addEventListener('pointerenter', function (e) {
        if (e.pointerType === 'touch') return;
        _tabPopSchedule(btn, i);
      });
      btn.addEventListener('pointerleave', function (e) {
        if (e.pointerType === 'touch') return;
        _tabPopScheduleClose();
      });
      btn.addEventListener('focus', function () {
        // 鼠标点出来的焦点不弹（不然点完标签浮窗又冒出来），键盘 Tab 才弹
        if (btn.matches && btn.matches(':focus-visible')) _tabPopSchedule(btn, i);
      });
      btn.addEventListener('blur', function () { _tabPopScheduleClose(); });
    }
    // 类型图标 + 状态点（同一枚元素，点画在 ::after 上）
    const ico = document.createElement('span');
    ico.className = 'tab-ico' + (stateCls ? ' ' + stateCls : '');
    ico.innerHTML = icon(_tabIconKey(t), 14, 14);
    btn.appendChild(ico);

    // 文字块：单行时只有名字；双行时名字 + 明细（客户端数字挂在明细末尾）。
    // 单行的客户端数字挂在名字右边（和 0.8.3 一样），所以它在 .tab-txt 外面。
    const txt = document.createElement('span');
    txt.className = 'tab-txt';
    const nameSpan = document.createElement('span');
    nameSpan.className = 'tab-port';
    nameSpan.textContent = _tabNameOf(t);
    txt.appendChild(nameSpan);

    const twoLines = state.tabTwoLines === true;
    if (twoLines) {
      const subEl = document.createElement('span');
      subEl.className = 'tab-sub';
      const subTxt = document.createElement('span');
      subTxt.className = 'tab-sub__txt';
      subTxt.setAttribute('data-role', 'sub');
      subTxt.textContent = _tabSubText(t);
      subEl.appendChild(subTxt);
      const cliInSub = _tabClientsNode(t);
      // 「保存 · 客户端」这一档已经写了数字，别重复
      if (cliInSub && !(state.tabSubLine === 'c' && !t.type)) subEl.appendChild(cliInSub);
      txt.appendChild(subEl);
    }
    btn.appendChild(txt);
    if (!twoLines) {
      const cli = _tabClientsNode(t);
      if (cli) btn.appendChild(cli);
    }
    // 双击改名：会话改守护进程里的线程名，文件标签改磁盘文件名
    btn.addEventListener('dblclick', function (e) {
      e.preventDefault();
      e.stopPropagation();
      _tabRenameStart(btn, i);
    });
    if (isChooser) {
      btn.title = window.t('newtab.hint', '选择一种模式开始');
    } else if (isSettings) {
      // 设置页不是「空闲进程」，别拿进程状态当提示
      btn.title = window.t('tooltip.settings', '详细设置');
    }
    // 会话标签这里原本挂三行原生 title（会话号 / 数据位 / 空闲进程），
    // 现在统一由 c-tabpop 浮窗承担，不再设置。
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

  // 重绘会把按钮换成新节点：浮窗开着的话，重新挂到同一个标签页的新按钮上，
  // 否则每次刷会话/读计数都会把浮窗闪没。标签被关掉就确实收起。
  if (_tabPop) {
    var keepIdx = state.tabs.indexOf(_tabPop.tab);
    var again = keepIdx >= 0
      ? scroll.querySelector('.tab-btn[data-tab-idx="' + keepIdx + '"]')
      : null;
    if (again) {
      _tabPop.idx = keepIdx;
      _tabPop.tabEl = again;
      _tabPopPlace(again);
    } else {
      _tabPopClose();
    }
  }

  // 重绘后：把外观设置写到 body（CSS 负责画），再按可用宽度定密度级别，
  // 最后重算左右箭头（ResizeObserver 不会因为子节点变多而触发）
  _applyTabDisplaySettings();
  _ensureTabsHist();
  _updateTabDensity(true);
  if (typeof _updateTabArrows === 'function') _updateTabArrows();
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

// 可以承载串口会话的标签页（排除设置页与欢迎页）。
//
// 为什么必须区分：设置标签页的 _page 是一个只有 show/hide 的壳，没有
// portSelect / btnOpen 这些元素，而 pageEl 在活动页找不到时会回退到
// document.getElementById —— 每个 TabPage 都有同名 id，于是那个回退交出去的
// 是**别的标签页**的元素。结果就是「在设置页上点打开串口」会读到另一个标签页
// 选的端口，再把会话挂到设置页对象上：串口真的开了，却没有任何界面能管它；
// 设置页对象还多了 sessionId，后续同步逻辑也跟着乱。
function isSessionTab(t) {
  return !!t && !t.type && !(t._page && t._page.isWelcome);
}

// 当前的会话标签页；停在设置页/欢迎页/无标签页时返回 null。
// 所有会碰串口、发送队列、历史缓存的入口都必须先过这一关。
function activeSessionTab() {
  var t = getActiveTab();
  return isSessionTab(t) ? t : null;
}

// 停在非会话标签页时给出解释（而不是静默失败或挂到别的标签页上）
function requireSessionTab(what) {
  var tab = activeSessionTab();
  if (tab) return tab;
  showAlert(t('confirm.title', '提示'),
    t('serial.need_session_tab', '请先切换到串口会话标签页（当前停在设置页）'));
  return null;
}

async function togglePort() {
  const tab = activeSessionTab();
  if (!tab) { requireSessionTab(); return; }
  if (tab.portOpen) { await closePort(); } else { await openPort(); }
}

async function openPort() {
  // 先确认站在会话标签页上：设置页没有端口选择与开关按钮，
  // 而 pageEl 的全局回退会交出别的标签页的元素（见 isSessionTab 的说明）。
  const tab = requireSessionTab();
  if (!tab) return;
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
    // If tab has no backing process, create an idle one first.
    // 建进程期间必须挡住 syncDaemonSessions：它收到 process-changed 时会为
    // 「GUI 尚未登记的进程」补一个标签页，而这里要过 await 才把 sessionId 记上，
    // 中间那一小段足够让它多建一个（addTab 一直有这个 _creatingTab 保护，
    // 这条路径此前没有 —— 「启动串口后多出一个新建标签页」就是这么来的）。
    if (!tab.sessionId) {
      _creatingTab = true;
      try { tab.sessionId = await window.go.main.App.CreateIdleProcess(); }
      finally { _creatingTab = false; }
    }
    // 记录「刚挂上会话」的时刻：同步的清理逻辑靠它避开连接窗口期的事件载荷
    tab.connectedAt = Date.now();

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
    refreshStatsDOM();
    _updateStatsVisible();
    await mergeRingBuffer(tab.sessionId);
    appendSystemMsg(`串口已打开: ${cfg.port} @ ${cfg.baud}`);
    saveSettings();
  } catch (e) {
    showAlert(t('cli.error','错误'), t('serial.open_fail','打开失败: ') + e);
  }
}

async function openForwardPorts() {
  const tab = requireSessionTab();
  if (!tab) return;
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
    // 同 openPort：建进程期间挡住 syncDaemonSessions，避免它替同一个进程再建一个标签页
    if (!tab.sessionId) {
      _creatingTab = true;
      try { tab.sessionId = await window.go.main.App.CreateIdleProcess(); }
      finally { _creatingTab = false; }
    }
    tab.connectedAt = Date.now();   // 同 openPort：给同步清理留出连接窗口期

    await window.go.main.App.ForwardConnect(tab.sessionId, cfgA, cfgB);

    tab.portOpen = true;
    tab.label = cfgA.port + ' ↔ ' + cfgB.port;
    tab.syncedParams = { portName: cfgA.port, baud: cfgA.baud, dataBits: cfgA.dataBits, stopBits: cfgA.stopBits, parity: cfgA.parity, forwardPortB: cfgB.port, forwardBaudB: cfgB.baud };

    updateOpenBtn();
    updateSyncIndicator();
    updatePortOccupied();
    renderTabs();
    state.statsBase[tab.sessionId] = { tx: 0, rx: 0, p1: 0, p2: 0 };
    refreshStatsDOM();
    _updateStatsVisible();
    await mergeRingBuffer(tab.sessionId);
    appendSystemMsg('端口转发已启动: ' + cfgA.port + ' ↔ ' + cfgB.port);
    saveSettings();
  } catch (e) {
    showAlert(t('cli.error','错误'), t('serial.open_fail','打开失败: ') + e);
  }
}

async function closePort() {
  const tab = activeSessionTab();
  if (!tab || !tab.sessionId) return;
  try {
    if (state.daemonOnline) {
      await window.go.main.App.DisconnectSession(tab.sessionId);
    }
  } catch {}
  await mergeRingBuffer(tab.sessionId);
  appendSystemMsg(t('sys.port_closed_label', '串口已关闭: %s').replace('%s', tab.label));
  tab.portOpen = false;
  tab.label = tab.mode === 'forward' ? t('tab.idle_forward', '端口转发空闲') : t('tab.idle_single', '单端口空闲');
  tab.syncedParams = null;
  // 会话还在（空闲进程），但字节/速率是那次连接的，重连会从 0 重新计
  delete state.statsBase[tab.sessionId];
  updateOpenBtn();
  updateSyncIndicator();
  updatePortOccupied();
  renderTabs();
  // 关掉端口后统计区不隐藏，但数字要归零（此前只刷新字节，速率一直停在
  // 最后一次 stats-rate 事件上，见 TODO #82）
  refreshStatsDOM();
  _updateStatsVisible();
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

// 串口详细设置 ↔ 工具栏 同步。
//
// 两边真正独立的只有转发的**端口与波特率**这四项：工具栏的转发行只有
// P1/P2 的端口 + 波特率；数据位 / 停止位 / 校验位只存在于弹窗里，由
// openForwardPorts 直接读弹窗元素，本来就不需要同步（同一份 DOM）。
//
// 赋值必须走 _applySelectValue：直接写 .value 既不是 attribute 变更、也不派发
// change，自定义下拉框触发器上显示的文字不会跟着变（TODO #79 的同一个坑）；
// 另外目标 <select> 里若没有这个选项，写 .value 会静默变成 selectedIndex=-1，
// 触发器文字同样纹丝不动 —— 这两条叠在一起就是「改了详细设置、主页面没反应」。
function _settingsSyncPairs() {
  return [
    ['portSelectASettings', 'portSelectA'],
    ['baudSelectASettings', 'baudSelectA'],
    ['portSelectBSettings', 'portSelectB'],
    ['baudSelectBSettings', 'baudSelectB'],
  ];
}

// 把值写进 <select>：缺选项就补一个，然后无条件刷新触发器文字。
function _applySelectValue(el, value) {
  if (!el || value === undefined || value === null) return false;
  var str = String(value);
  var has = false;
  for (var i = 0; i < el.options.length; i++) {
    if (el.options[i].value === str) { has = true; break; }
  }
  if (!has) {
    var opt = document.createElement('option');
    opt.value = str;
    opt.textContent = str;
    el.appendChild(opt);
  }
  if (el.value !== str) el.value = str;
  _refreshCustomSelectFor(el);
  return true;
}

function _setSelectValue(id, value) {
  var el = pageEl(id);
  if (!el) {
    // 只在真出问题时打一行：DevTools 在开发版里是开着的，便于定位
    console.warn('[settings-sync] 工具栏元素不存在:', id);
    return false;
  }
  return _applySelectValue(el, value);
}

function _refreshCustomSelectFor(el) {
  var wrap = (el && el.closest) ? el.closest('.cs-wrap') : null;
  if (wrap && typeof _csRefresh === 'function') _csRefresh(wrap);
}

// 弹窗 → 工具栏（关闭弹窗时兜底调用；改值时也会即时同步）
function _syncSettingsToBar() {
  if (activeMode() !== 'forward') return;
  _settingsSyncPairs().forEach(function(pair) {
    var from = document.getElementById(pair[0]);
    if (from && from.value) _setSelectValue(pair[1], from.value);
  });
}

// 工具栏 → 弹窗（打开弹窗时调用）
function _syncBarToSettings() {
  if (activeMode() !== 'forward') return;
  _settingsSyncPairs().forEach(function(pair) {
    var to = document.getElementById(pair[0]);
    var from = pageEl(pair[1]);
    if (!to || !from || !from.value) return;
    _applySelectValue(to, from.value);
  });
}

// 设置弹窗里改端口 / 波特率 / 数据位 / 停止位 / 校验位 → 立刻同步到工具栏
function _bindSettingsPortSync() {
  _settingsSyncPairs().forEach(function(pair) {
    var from = document.getElementById(pair[0]);
    if (!from) {
      console.warn('[settings-sync] 弹窗元素不存在:', pair[0]);
      return;
    }
    from.addEventListener('change', function() {
      if (!from.value) return;
      _setSelectValue(pair[1], from.value);
    });
  });
}

// 进设置页时静默重读一次语言包：改 i18n/*.json 后不用重启 exe。
// 只有内容真的变了才重建设置页，避免面板 / 滚动位置被无谓重置。
function _refreshSettingsI18n() {
  if (typeof loadI18n !== 'function') return;
  var before = (typeof _i18nSig === 'string') ? _i18nSig : '';
  loadI18n(state.language || 'zh').then(function() {
    if (typeof _i18nSig !== 'string' || _i18nSig === before) return;
    applyI18n();
    if (typeof refreshSettingsPage === 'function') refreshSettingsPage();
  }).catch(function() {});
}

function openSettingsPage() {
  // Check if settings tab already exists
  var existingIdx = state.tabs.findIndex(function(t) { return t.type === 'settings'; });
  if (existingIdx >= 0) {
    switchTab(existingIdx);
    _refreshSettingsI18n();
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
  _refreshSettingsI18n();
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
  const tab = getActiveTab();
  // Settings / welcome / no tab: there is no open-close button on this page.
  // 注意这里**不能**走 pageEl 的全局回退 —— 每个 TabPage 都有自己的 btnOpen，
  // 回退会把第一个标签页的按钮交出来，于是「切到设置页」会把第 1 个标签页的
  // 「打开串口」按钮隐藏掉（实测确认）。只动当前页自己的元素。
  if (!isSessionTab(tab)) {
    const page = getActivePage();
    if (page && page.btnOpen) { page.btnOpen.disabled = true; page.btnOpen.style.display = 'none'; }
    if (page && page.btnSend) page.btnSend.disabled = true;
    return;
  }
  const btn = pageEl('btnOpen');
  if (!btn) return;
  btn.style.display = '';
  const isFwd = activeMode() === 'forward';
  if (!state.daemonOnline) {
    btn.disabled = true; btn.textContent = isFwd ? t('btn.start_forward', '启动转发') : t('btn.open_port', '打开串口');
  } else if (tab.portOpen) {
    btn.disabled = false;
    btn.textContent = isFwd ? t('btn.stop_forward', '停止转发') : t('btn.close_port', '关闭串口');
    btn.style.background = 'var(--red)'; btn.style.color = '#fff';
  } else {
    btn.disabled = false;
    btn.textContent = isFwd ? t('btn.start_forward', '启动转发') : t('btn.open_port', '打开串口');
    btn.style.background = ''; btn.style.color = '';
  }
  pageEl('btnSend').disabled = !(tab.portOpen && activeMode() === 'single');
}

// 切换显示模式后重绘历史：冻结/回看时保持原来的窗口与滚动位置，
// 否则用户往回翻出来看的那一段会突然被拽回最新一条（未读提示也随之消失）。
function _rerenderHistoryKeepingView() {
  var tab = getActiveTab();
  if (!tab || !tab.sessionId || !state.historyCache[tab.sessionId]) return;
  var display = pageEl('displayContent');
  var keep = !!_frozen;
  var st = display ? display.scrollTop : 0;
  renderHistoryLines(state.historyCache[tab.sessionId], keep);
  if (display && keep) display.scrollTop = st;
}

// ---- display ----

// 把当前的显示模式写回标签页自己的副本。
//
// state.*DisplayMode 只是「当前活动页」的镜像，每个标签页另有一份 tab.*DisplayMode。
// 此前这份副本只在切标签时写（switchTab 的 oldTab.displayMode = ...），而
// syncDaemonSessions 在会话状态变化时会**用副本回填全局**（tabsChanged 分支）——
// 打开/关闭串口正好会让 portOpen / label 变化 -> tabsChanged -> 把用户刚切的
// 文本/Hex 覆盖回旧值（表现为开关串口时显示样式跳变）。所以每次切换都要落盘。
function _persistDisplayModes() {
  var tab = getActiveTab();
  if (!tab || !isSessionTab(tab)) return;
  tab.displayMode = state.displayMode;
  tab.txDisplayMode = state.txDisplayMode;
  tab.p1DisplayMode = state.p1DisplayMode;
  tab.p2DisplayMode = state.p2DisplayMode;
  tab.scrollLocked = state.scrollLocked;
}

function toggleDisplayMode() {
  state.displayMode = state.displayMode === 'text' ? 'hex' : 'text';
  _persistDisplayModes();
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
  _rerenderHistoryKeepingView();
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

var _updateTabArrows = null;

// 标签栏「还能往两边滚」的提示（TODO #26）。
// 滚轮映射与拖拽在 DOMContentLoaded 里另有绑定，这里只负责箭头显隐；箭头本身
// 复用发送区的 .send-scroll-arrow（绝对定位 + pointer-events:none，不吃布局）。
function bindTabsScroll() {
  var bar = document.getElementById('tabsBar');
  var scroll = document.getElementById('tabsScroll');
  if (!bar || !scroll) return;
  var left = bar.querySelector('.tabs-scroll-arrow--left');
  var right = bar.querySelector('.tabs-scroll-arrow--right');
  if (!left || !right) return;
  function update() {
    // 标签栏宽度变了，能显示到哪一级也要重算（内部按 clientWidth 做了缓存，
    // 滚动时不会反复重排）
    if (typeof _updateTabDensity === 'function') _updateTabDensity();
    var max = scroll.scrollWidth - scroll.clientWidth;
    left.classList.toggle('is-hidden', scroll.scrollLeft <= 1);
    right.classList.toggle('is-hidden', scroll.scrollLeft >= max - 1);
  }
  scroll.addEventListener('scroll', update, { passive: true });
  if (typeof ResizeObserver === 'function') new ResizeObserver(update).observe(scroll);
  window.addEventListener('resize', update);
  _updateTabArrows = update;
  update();
}

function toggleScrollLock() {
  state.scrollLocked = !state.scrollLocked;
  _persistDisplayModes();
  updateScrollLockBtns();
  // 冻结/解冻与未读占位块由历史层负责（锁定时视图冻结，但滚动条仍随未读增长）
  if (typeof onScrollLockChanged === 'function') onScrollLockChanged();
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
  _persistDisplayModes();
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
  _rerenderHistoryKeepingView();
}

function toggleP2Display() {
  state.p2DisplayMode = state.p2DisplayMode === 'text' ? 'hex' : 'text';
  _persistDisplayModes();
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
  _rerenderHistoryKeepingView();
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
  _persistDisplayModes();
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
  // 标签浮窗也吃这份事件：它不是活动标签，但也得实时跳数
  if (_tabPop && data && _tabPop.sessionId && data.processId === _tabPop.sessionId) {
    if (!_tabPop.stats) _tabPop.stats = {};
    if (_tabPop.tab.mode === 'forward') {
      _tabPop.stats.bytesPort1 = data.bytesPort1 || 0;
      _tabPop.stats.bytesPort2 = data.bytesPort2 || 0;
    } else {
      _tabPop.stats.bytesRead = data.bytesRead || 0;
      _tabPop.stats.bytesWritten = data.bytesWritten || 0;
    }
    _tabPopUpdate();
  }
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
    // 先切模式再写值：updateStatsBytes 按当前模式挑 p1/p2 还是 tx/rx，
    // 反过来写会拿单端口的数字去填 P1/P2（或反之），要等下一次事件才纠正
    _statusBar.setStatsMode(isFwd);
    _statusBar.updateStatsBytes({tx: b.tx, rx: b.rx, p1: b.p1, p2: b.p2});
  }
}

function onStatsRate(data) {
  // 浮窗按自己的 processId 匹配，和活动标签互不影响
  if (_tabPop && data && _tabPop.sessionId && data.processId === _tabPop.sessionId) {
    if (!_tabPop.stats) _tabPop.stats = {};
    _tabPop.stats.readRateBps = data.readRateBps || 0;
    _tabPop.stats.writeRateBps = data.writeRateBps || 0;
    _tabPopUpdate();
  }
  // 速率按会话缓存：以前只给活动标签记，于是标签栏根本拿不到非活动标签的速率。
  // 事件本来是按 processId 全量推送的，顺手给所有会话记一份。
  if (data && data.processId) {
    if (!state.statsBase) state.statsBase = {};
    const any = state.statsBase[data.processId]
      || (state.statsBase[data.processId] = { tx: 0, rx: 0, p1: 0, p2: 0 });
    any.txRate = data.writeRateBps || 0;
    any.rxRate = data.readRateBps || 0;
  }

  // 双行标签的第二行选了「参数 · 速率」时，速率每秒变：只改文本，不重建标签
  if (typeof _updateTabSubTexts === 'function') _updateTabSubTexts();

  const tab = getActiveTab();
  if (!tab || !tab.sessionId || !data || data.processId !== tab.sessionId) return;
  const sid = tab.sessionId;
  if (!state.statsBase[sid]) { state.statsBase[sid] = { tx: 0, rx: 0, p1: 0, p2: 0 }; }
  const b = state.statsBase[sid];
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
    // 端口没连上：一律显示 0（会话统计是按连接计的）。
    // 不去读缓存、也不去拉 daemon —— 空闲进程的计数没有意义，读出来只会让
    // 上一次连接的数字继续挂着（用户实测的那条 P2:389/s）。
    if (!tab.portOpen) {
      if (_statusBar) {
        // 模式也得定：转发页空闲时箭头仍应是 P1/P2（此前提前返回，把它留在
        // 上一个标签页的 TX/RX 上）
        _statusBar.setStatsMode(activeMode() === 'forward');
        _statusBar.resetStats();
      }
      return;
    }
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
              _statusBar.setStatsMode(isFwd);
              _statusBar.updateStatsBytes({tx: b.tx, rx: b.rx, p1: b.p1, p2: b.p2});
            }
          }
        }).catch(function() {});
      }
    }
    const b = state.statsBase[tab.sessionId];
    if (b && _statusBar) {
      // 同 onStatsCount：先定模式再写值，否则 P1/P2 会先显示单端口的数字
      _statusBar.setStatsMode(isFwd);
      _statusBar.updateStatsBytes({tx: b.tx || 0, rx: b.rx || 0, p1: b.p1 || 0, p2: b.p2 || 0});
      // 速率按会话缓存回放（没收到过就是 0）：否则切标签会留着上一个会话的速率
      _statusBar.updateStatsRates({
        txRate: b.txRate || 0,
        rxRate: b.rxRate || 0,
        p1Rate: b.txRate || 0,
        p2Rate: b.rxRate || 0,
        maxBps: (tab.syncedParams && tab.syncedParams.baud) ? tab.syncedParams.baud / 10 : 0
      });
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
  _rerenderHistoryKeepingView();
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
  _rerenderHistoryKeepingView();
  saveSettings();
}

function setHexEscapeFormat(fmt) {
  if (state.hexEscapeFormat === fmt) return;
  state.hexEscapeFormat = fmt;
  _rerenderHistoryKeepingView();
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
    data = _normalizeHexInput(data);
    const problem = _hexInputProblem(data);
    if (problem) { showSendHint(_hintText(problem)); return; }
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
    showAlert(t('cli.error','错误'), t('serial.start_fail','启动失败') + ': ' + e);
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

// Hex 发送输入的规范化：去掉 0x/0X 前缀、空白与逗号。
// 手动发送、自动发送、字节统计三条路径必须共用同一份规则，否则同一份输入
// 会出现「高亮全绿、手动能发、自动发送却报 invalid hex」这类自相矛盾的行为。
function _normalizeHexInput(s) {
	return String(s == null ? '' : s).replace(/0x/gi, '').replace(/[\s,]+/g, '');
}

// 判定规范化后的 hex 能否发出：可以返回 null，否则返回提示文案所需的键与参数。
// 与后端 hex.DecodeString 的接受条件一致：非空、偶数位、只含 0-9 A-F。
function _hexInputProblem(clean) {
	if (clean === '') return { key: 'send.hint_empty', fb: '没有要发送的数据' };
	if (clean.length % 2 !== 0) {
		return { key: 'send.hint_odd', fb: '十六进制位数必须是偶数（当前 %s 位）', arg: clean.length };
	}
	var m = clean.match(/[^0-9a-fA-F]/);
	if (m) return { key: 'send.hint_badchar', fb: "含非十六进制字符「%s」，只能是 0-9 与 A-F", arg: m[0] };
	return null;
}

function _hintText(problem) {
	var s = t(problem.key, problem.fb);
	return (problem.arg === undefined) ? s : s.replace('%s', problem.arg);
}

// 悬浮提示：显示在发送框右上角，不参与布局、不拦截点击，5 秒后自动消失。
var _sendHintTimer = null;

function showSendHint(msg) {
	if (!msg) return;
	var page = getActivePage();
	var el = page && page.sendHint;
	if (!el) return;
	el.textContent = msg;
	el.classList.remove('is-hidden');
	if (_sendHintTimer) clearTimeout(_sendHintTimer);
	_sendHintTimer = setTimeout(hideSendHint, 5000);
}

function hideSendHint() {
	if (_sendHintTimer) { clearTimeout(_sendHintTimer); _sendHintTimer = null; }
	var page = getActivePage();
	if (page && page.sendHint) page.sendHint.classList.add('is-hidden');
}

// 悬停发送框时，如果当前 hex 输入发不出去，就把原因显示出来。
function _hintCurrentSendInput() {
	var ss = getSendState();
	if (!ss || ss.sendFormat !== 'hex') return;
	var page = getActivePage();
	var ta = page && page.sendInput;
	if (!ta) return;
	var problem = _hexInputProblem(_normalizeHexInput(ta.value));
	if (problem) showSendHint(_hintText(problem));
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
		text = _normalizeHexInput(text);
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
					data2 = _normalizeHexInput(data2);
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
    data = _normalizeHexInput(data);
    // 先把「发不出去」的原因挡在这里并说清楚，不要丢给后端再被静默吞掉
    const problem = _hexInputProblem(data);
    if (problem) { showSendHint(_hintText(problem)); return; }
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
  } catch (e) {
    showSendHint(String(e));
  }
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
// ---- 多字符串（快速面板）拖动：pointer 事件 + 实时补位 ----
//
// 和标签栏那套同一观感：按住左侧 ⋮⋮ 上下拖，被拖的那行原样压暗、贴着列表上下滑；
// 原位由等高的占位块让开，其余行用 FLIP 平滑让位；松手按占位块位置结算顺序。
// 不用 HTML5 拖放，原因同标签栏（dragstart 里隐藏源元素会被浏览器取消拖动）。
var _qpDrag = null;   // { row, idx, started, ph, ... }

function qpDragDown(e, idx) {
  if (e.button !== 0) return;
  var row = e.currentTarget.closest('.qp-row');
  if (!row) return;
  var r = row.getBoundingClientRect();
  _qpDrag = {
    row: row, idx: idx, started: false, pid: e.pointerId,
    startY: e.clientY, offY: e.clientY - r.top,
    h: r.height, w: r.width, left: r.left, top: r.top,
    ph: null,
  };
  // 咬住指针：拖到面板外/窗口外时 up 也还会回来，不会卡住
  try { row.setPointerCapture(e.pointerId); } catch (_) {}
  if (e.cancelable) e.preventDefault();
}

function _qpDragBegin(st) {
  st.started = true;
  var rowsEl = st.row.parentNode;            // .qp-rows（display:contents，行是祖父网格的 item）
  var host = rowsEl.parentElement;           // .qp-scroll
  var table = host ? host.parentElement : null;  // .qp-table
  var cols = table ? getComputedStyle(table).gridTemplateColumns : '';
  st.host = host;
  st.table = table;
  st.addRow = table ? table.querySelector('.qp-add-row') : null;
  st.addPush = 0;

  st.row.classList.add('qp-src');
  st.row.style.left = st.left + 'px';
  st.row.style.top = st.top + 'px';
  st.row.style.width = st.w + 'px';
  st.row.style.height = st.h + 'px';
  if (cols) st.row.style.gridTemplateColumns = cols;

  var ph = document.createElement('div');
  ph.className = 'qp-placeholder';
  ph.style.height = st.h + 'px';
  rowsEl.insertBefore(ph, st.row);
  st.ph = ph;

  rowsEl.classList.add('is-dragging');
  document.body.classList.add('is-qp-dragging');

  st.lastY = st.top;
  _qpDragStartAutoScroll();
}

// 拖到列表上下边缘时自动滚动（和标签栏同一套思路，这里是纵向）
var _qpDragRaf = null;

function _qpDragStartAutoScroll() {
  if (_qpDragRaf) return;
  _qpDragRaf = requestAnimationFrame(_qpDragAutoScrollStep);
}

function _qpDragStopAutoScroll() {
  if (_qpDragRaf) {
    cancelAnimationFrame(_qpDragRaf);
    _qpDragRaf = null;
  }
}

function _qpDragAutoScrollStep() {
  _qpDragRaf = null;
  var st = _qpDrag;
  if (!st || !st.started) return;
  var rowsEl = st.row.parentNode;
  var scroll = rowsEl ? rowsEl.parentElement : null;   // .qp-scroll
  if (!scroll) return;
  var r = scroll.getBoundingClientRect();
  var EDGE = 40;
  var MAX = 12;
  var y = st.lastY;
  var delta = 0;
  if (y > r.bottom - EDGE) {
    delta = Math.ceil(Math.min(MAX, 2 + (y - (r.bottom - EDGE)) / EDGE * MAX));
  } else if (y < r.top + EDGE) {
    delta = -Math.ceil(Math.min(MAX, 2 + ((r.top + EDGE) - y) / EDGE * MAX));
  }
  if (delta) {
    var before = scroll.scrollTop;
    scroll.scrollTop = before + delta;
    if (scroll.scrollTop !== before) _qpDragPlace(st, y);
  }
  _qpDragRaf = requestAnimationFrame(_qpDragAutoScrollStep);
}

function _qpDragPlace(st, clientY) {
  var rowsEl = st.row.parentNode;
  if (!rowsEl || !st.ph) return;
  var rows = rowsEl.querySelectorAll('.qp-row');
  var before = null;
  for (var j = 0; j < rows.length; j++) {
    var r0 = rows[j];
    if (r0 === st.row) continue;
    var rc = r0.getBoundingClientRect();
    if (clientY < rc.top + rc.height / 2) { before = r0; break; }
  }
  var kids = Array.prototype.filter.call(rowsEl.children, function(el) { return el !== st.row; });
  var target = before ? kids.indexOf(before) : kids.length;
  if (kids.indexOf(st.ph) === target) return;

  var prev = [];
  for (var i = 0; i < kids.length; i++) prev.push(kids[i].getBoundingClientRect().top);
  var ref = target < kids.length ? kids[target] : null;
  if (ref) rowsEl.insertBefore(st.ph, ref); else rowsEl.appendChild(st.ph);
  for (var k = 0; k < kids.length; k++) {
    var dy = prev[k] - kids[k].getBoundingClientRect().top;
    if (dy) _flipShift(kids[k], 0, dy);
  }
}

function _qpDragMove(e) {
  var st = _qpDrag;
  if (!st) return;
  if (!st.started) {
    if (Math.abs(e.clientY - st.startY) < 4) return;
    _qpDragBegin(st);
  }
  if (e.cancelable) e.preventDefault();
  st.lastY = e.clientY;
  var rowsEl = st.row.parentNode;
  var top = e.clientY - st.offY;
  // .qp-rows 是 display:contents，自己的 getBoundingClientRect() 是空矩形，
  // 夹边界要用外层真正的容器。下限放到 .qp-table 底：这样被拖的行可以压到
  // 「+ 添加」那一行上，把添加行顶下去。
  var bounds = st.table ? st.table.getBoundingClientRect() : null;
  if (bounds) {
    top = Math.max(bounds.top, Math.min(top, bounds.bottom - st.h));
  }
  st.row.style.top = top + 'px';

  // 推挤「+ 添加」：行底部越过滚动区底边多少，就把它往下顶多少，最多顶到表格底。
  if (st.addRow && st.host) {
    var sr = st.host.getBoundingClientRect();
    var overflow = Math.max(0, (top + st.h) - sr.bottom);
    var ar = st.addRow.getBoundingClientRect();
    var baseBottom = ar.bottom - (st.addPush || 0);
    var space = Math.max(0, (bounds ? bounds.bottom : sr.bottom) - baseBottom);
    var push = Math.min(overflow, space);
    st.addPush = push;
    st.addRow.style.transform = push > 0.5 ? 'translateY(' + push + 'px)' : '';
  }
  _qpDragPlace(st, e.clientY);
}

function _qpDragFinish(commit) {
  var st = _qpDrag;
  if (!st) return;
  _qpDrag = null;
  _qpDragStopAutoScroll();
  try { st.row.releasePointerCapture(st.pid); } catch (_) {}
  if (!st.started) return;

  var rowsEl = st.row.parentNode;
  var kids = rowsEl ? Array.prototype.filter.call(rowsEl.children, function(el) { return el !== st.row; }) : [];
  var target = st.ph ? kids.indexOf(st.ph) : -1;
  if (st.ph && st.ph.parentNode) st.ph.parentNode.removeChild(st.ph);
  if (rowsEl) rowsEl.classList.remove('is-dragging');
  document.body.classList.remove('is-qp-dragging');
  if (st.addRow) st.addRow.style.transform = '';
  st.row.classList.remove('qp-src');
  st.row.style.left = '';
  st.row.style.top = '';
  st.row.style.width = '';
  st.row.style.height = '';
  st.row.style.gridTemplateColumns = '';

  if (commit && target >= 0 && target !== st.idx) {
    var moved = state.quickPresets.splice(st.idx, 1)[0];
    state.quickPresets.splice(target, 0, moved);
    saveSettings();
  }
  quickPanelRender();
}

function _qpDragFinishAll(commit) {
  if (_qpDrag) _qpDragFinish(commit);
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
    h += '<div class="qp-row" data-idx="' + i + '">' +
      '<span class="qp-drag" title="' + t('quick_panel.drag', '拖动排序') + '"' +
      ' onpointerdown="qpDragDown(event,' + i + ')">⋮⋮</span>' +
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
  // 非会话页也要跟随语言：这两类的 label 是建页时按当时语言写死的，
  // 而下面的会话页分支对它们不适用（没有 syncedParams，会直接 return）。
  if (tab.type === 'new') { tab.label = t('newtab.title', '新建会话'); return; }
  if (tab.type === 'settings') { tab.label = t('settings.title', '设置'); return; }
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
  state.autoSaveHistory = state.autoSaveHistory !== true;
  saveSettings();
  updateAutoSaveCheck(state.autoSaveHistory);
  try {
    await window.go.main.App.SetHistoryEnabled(state.autoSaveHistory);
  } catch (e) { /* 守护进程离线时只留本地设置，连上后 connect 流程会再推一次 */ }
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
  if (_statusBar) _statusBar.updateDaemon({online: state.daemonOnline, tabsCount: realTabs.length});
  _syncWelcome();
  _updateStatsVisible();
}

// _syncWelcome 决定「尚无串口会话」这张引导页要不要显示。
//
// 它和 offlineOverlay（「守护进程未启用」）共用同一块区域，两个都显示就会叠字
// ——一段黑字压在另一段上，谁都读不清（这个 bug 存在很久了）。
// 规则：只有「守护进程在线 且 没有真实标签」才显示引导页；离线时让位给
// offlineOverlay，由它告诉用户去启动守护进程。
//
// 两处 update 都调它，避免依赖调用顺序：谁后跑都不会留下不一致的状态。
function _syncWelcome() {
  if (!state._welcomePage) return;
  var realTabs = state.tabs.filter(function(t) { return !(t._page && t._page.isWelcome); });
  var show = realTabs.length === 0 && !!state.daemonOnline;
  if (show) state._welcomePage.show();
  else state._welcomePage.hide();
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
    var bottomH = 24;
    var dividerH = 5;
    // 上下限都是**几何位置**，不是比例：
    //   上 = 配置栏下沿（分隔条贴住浮动配置栏下方，显示区被压到刚好藏在栏后面）
    //   下 = 窗口底部状态栏上沿（发送区可以完全贴边收掉）
    // 之前用写死的 102 当上限，分隔条会钻进浮动的配置栏底下，抓不到了。
    var pg = _divDragState.page;
    var cfg = pg ? pg.querySelector('.config-bar') : null;
    var yMin = Math.round(cfg ? cfg.getBoundingClientRect().bottom : 106);
    // 下边界：状态栏上沿。发送区会被完全收起，分隔条自然落在行底，
    // 它的线与状态栏上边框重合 —— 此时把状态栏那条边框隐掉（见 applySendRatio
    // 里的 is-flush-bottom），观感上就是原来那条单线，不会出现"双线"。
    var bottomEdge = appRect.bottom - bottomH;
    var yMax = Math.round(bottomEdge);
    var avail = Math.max(1, yMax - yMin);
    var clampedY = Math.max(yMin, Math.min(yMax, e.clientY - appRect.top));
    // 比例由**几何**反算，让分隔条 1:1 跟着光标走。
    // 之前直接用 (yMax - y)/avail，而 flex 分配里还要减去分隔条自身高度、
    // 外加显示区的 min-height，结果分隔条落后光标 20 多像素（拖动时"黏"）。
    var row = pg ? pg.querySelector('.content-row') : null;
    var rowRect = row ? row.getBoundingClientRect() : null;
    var rowH = rowRect ? rowRect.height : Math.max(1, yMax - yMin);
    var rowTop = rowRect ? rowRect.top : yMin;
    var displayH = clampedY - rowTop;
    state.sendRatio = Math.max(0, Math.min(1, 1 - displayH / Math.max(1, rowH - dividerH)));
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
    displayArea.classList.remove('is-hidden');
    displayArea.classList.remove('is-collapsed');
    sendArea.style.display = 'none';
    sendArea.classList.remove('is-hidden');
    if (divider) divider.style.display = 'none';
    return;
  }
  // 比例允许走到两个端点（贴住配置栏下方 / 完全贴底），所以不再夹 [0.05,0.95]。
  var r = Math.max(0, Math.min(1, state.sendRatio));
  displayArea.style.flex = (1 - r) + ' 1 0px';
  // 显示区最小高度 = 配置栏高度：压到最小时，分隔条正好贴在配置栏下沿，
  // 仍然抓得到（此前是 40px，会让分隔条缩到浮动配置栏底下）。
  var cfgBar = page.querySelector ? page.querySelector('.config-bar') : null;
  var reserve = 44;
  if (cfgBar) {
    var ch = Math.round(cfgBar.getBoundingClientRect().height);
    if (ch > 0) reserve = ch;
  }
  displayArea.style.minHeight = reserve + 'px';
  // 两个极端都不整块隐藏：隐藏会让分隔条跳到内容区顶部（= 配置栏底下）而抓不到。
  displayArea.classList.remove('is-hidden');
  sendArea.style.display = '';
  sendArea.style.flex = r + ' 1 0px';
  sendArea.style.minHeight = '0px';
  // 下边界回到原来的观感：发送区完全收起（不再留 3px 残条），分隔条贴到行底，
  // 自然就在状态栏上沿之上、仍抓得到。
  sendArea.classList.toggle('is-hidden', r <= 0.01);
  // 收起到底时，分隔条的线和状态栏上边框重合：隐掉状态栏那条，避免"双线"
  var sbEl = document.querySelector('.statusbar');
  if (sbEl) sbEl.classList.toggle('is-flush-bottom', r <= 0.01);
  if (divider) divider.style.display = '';
  // 改完高度后，如果本来就在跟最新（未冻结、未锁定），重新贴底。
  // 否则视口一变，滚动位置就"掉队"了，看上去像"拖一下就再也不跟着滚了"。
  var dc = page.displayContent;
  if (dc && !state.scrollLocked && typeof _frozen !== 'undefined' && !_frozen) {
    dc.scrollTop = dc.scrollHeight;
  }
  // 压到只剩配置栏那么高时，把显示区里的按钮/数据流/滚动条整体收起来：
  // 否则它们会从浮动配置栏的模糊层后面透出来（截图里的"数据流/按键没隐藏"）。
  var dispH = Math.round(displayArea.getBoundingClientRect().height);
  displayArea.classList.toggle('is-collapsed', dispH <= reserve + 4);
  if (dc && typeof dc._syncScrollbarInset === 'function') dc._syncScrollbarInset();
}



// ---- .stlog 文件标签页 ----
//
// 拖入 .stlog 开成一个真正的标签页（P1）。壳子照抄设置页那套「非会话标签」：
// 只要 t.type 有值，isSessionTab() 就返回 false，所有会碰串口 / 发送队列 /
// 历史缓存的入口都会自动绕开它（见 isSessionTab 上方的注释）。
//
// 行渲染直接调 history.js 的 makeDataRow：Rx/Tx 配色、Hex/文本开关、等宽字体、
// 屏外不绘制全部继承，不另起一套渲染器。

// 首屏/每批 500 帧。一帧最大 4016 B，转十六进制就是 8 KB 字符串，一次给太多
// 会把过桥的 JSON 和 DOM 一起撑爆；其余靠底部的「加载更多」。
var STLOG_PAGE_SIZE = 500;

function stlogBaseName(p) {
  // 去掉尾部分隔符：拖放给的目录路径可能带 '\'，否则取到的是空串
  return String(p).replace(/[\\/]+$/, '').split(/[\\/]/).pop();
}

// 与 Go 侧 stlogDirName / 导出一致：1/2/3/4 是 rx/tx/P1/P2，其它算系统事件。
function stlogDirName(d) {
  // 会话接口回来的方向已经是字符串（rx/tx/1/2/system），原样用；
  // 本地 ReadStlogFrames 回来的是数字，走下面的映射。
  if (typeof d === 'string') return d;
  switch (d & 0x7f) {
    case 1: return 'rx';
    case 2: return 'tx';
    case 3: return '1';
    case 4: return '2';
    default: return 'system';
  }
}

// 时间戳格式：文件里存的是绝对毫秒时间，年一直在数据里，只是逐行显示时可以选择
// 是否带上（顶栏的时间范围永远带年）。
function stlogPad(n, w) { var s = String(n); while (s.length < w) s = '0' + s; return s; }
function stlogDateText(d) { return d.getFullYear() + '-' + stlogPad(d.getMonth() + 1, 2) + '-' + stlogPad(d.getDate(), 2); }
function stlogTimeText(d) {
  return stlogPad(d.getHours(), 2) + ':' + stlogPad(d.getMinutes(), 2) + ':' +
    stlogPad(d.getSeconds(), 2) + '.' + stlogPad(d.getMilliseconds(), 3);
}
function stlogTsText(ms, withYear) {
  var d = new Date(ms);
  var day = withYear
    ? stlogDateText(d)
    : stlogPad(d.getMonth() + 1, 2) + '-' + stlogPad(d.getDate(), 2);
  return day + ' ' + stlogTimeText(d);
}
function stlogFullTs(ms) { var d = new Date(ms); return stlogDateText(d) + ' ' + stlogTimeText(d); }
// 顶栏时间范围：同一天只写一次日期，跨天两边都写全
function stlogRangeText(first, last) {
  if (typeof first !== 'number' || typeof last !== 'number') return '';
  var a = new Date(first), b = new Date(last);
  return stlogFullTs(first) + ' ~ ' +
    (stlogDateText(a) === stlogDateText(b) ? stlogTimeText(b) : stlogFullTs(last));
}

function stlogSysRow(tab, text) {
  var line = document.createElement('div');
  line.className = 'sys-msg';
  line.textContent = '── ' + text + ' ──';
  var st = tab._stlog;
  st.content.insertBefore(line, st.more);
  return line;
}

// 顶栏一行元信息：时间范围 / 大小 / 页 / 记录 / rx-tx / 设置 / 异常标记。
function stlogBarText(info) {
  var parts = [];
  var range = stlogRangeText(info.firstTs, info.lastTs);
  if (range) parts.push(range);
  parts.push(info.size + ' B');
  parts.push(info.pages + ' 页');
  parts.push(info.records + ' 记录');
  // 转发录制：rx/tx 两栏没有意义（统计块只累计 rx/tx），改显示两个方向的字节数
  if (info.forward) {
    parts.push('A→B ' + (info.bytesAB || 0) + ' / B→A ' + (info.bytesBA || 0) + ' B');
    var setA = (info.settings && info.settings.port) ? info.settings.port : '';
    parts.push('转发 ' + (setA ? setA + ' ↔ ' : '') + (info.portB || ''));
  } else {
    parts.push('rx/tx ' + info.rxTotal + '/' + info.txTotal + ' B');
  }
  if (info.settings && info.settings.label) parts.push('设置 ' + info.settings.label);
  // 来源汇总：只列种类（GUI / CLI / AUTO…），完整串放 title。
  // 「无来源」要明说——样本文件就是这样，空着会让人以为功能没做。
  if (info.origins && info.origins.length) {
    var kinds = [];
    info.origins.forEach(function (o) {
      var label = (typeof _originLabel === 'function') ? _originLabel(o) : String(o);
      if (kinds.indexOf(label) < 0) kinds.push(label);
    });
    parts.push('来源 ' + kinds.join(' · '));
  } else {
    parts.push('无来源');
  }
  if (info.recovered && info.recovered.length) parts.push('影子恢复 ' + info.recovered.length + ' 页');
  if (info.truncated) parts.push('末帧未结束');
  if (info.brokenChain) parts.push('续片链被切断');
  if (info.stoppedAt) parts.push('停在 0x' + Number(info.stoppedAt).toString(16).toUpperCase());
  if (info.stopErr) parts.push(info.stopErr);
  return parts.join(' · ');
}

function stlogUpdateChrome(tab) {
  var st = tab._stlog;
  var info = st.info || {};
  st.meta.textContent = stlogBarText(info);
  // 完整来源串放进 title：顶栏只显示种类，鼠标停上去能看到具体是哪个客户端
  st.meta.title = (info.origins && info.origins.length)
    ? info.origins.join(', ')
    : '这个文件里没有来源信息（Rx 恒无来源；样本文件通常是合成的）';
  var loaded = st.frames.length;
  var total = (typeof info.total === 'number') ? info.total : loaded;
  st.count.textContent = '帧 ' + loaded + ' / ' + total;
  if (st.exhausted) {
    st.more.style.display = 'none';
  } else {
    st.more.style.display = '';
    st.moreBtn.textContent = '加载更多（还有 ' + Math.max(0, total - loaded) + ' 帧）';
  }
}

// stlogLoad 加载下一批帧。
//
// 文件页只有一个数据源——本地文件，分页从第一帧往后翻（_stlogLoadLocal）。
// 工程目录不走这里：它被恢复成真正的会话页，历史由会话那套接口提供。
function stlogLoad(tab) {
  _stlogLoadLocal(tab);
}

// _stlogLoadLocal 直接读本地文件，从第一帧往后翻。
function _stlogLoadLocal(tab) {
  var st = tab._stlog;
  if (st.loading || (st.loaded && st.offset >= st.total)) return;
  st.loading = true;
  st.moreBtn.disabled = true;
  window.go.main.App.ReadStlogFrames(tab.path, st.offset, STLOG_PAGE_SIZE).then(function (info) {
    st.loading = false;
    st.moreBtn.disabled = false;
    st.loaded = true;
    st.info = info || {};
    st.total = info.total || 0;
    var frames = (info.frames || []).map(function (f) {
      return { ts: f.ts, tsMs: f.ts, dir: f.dir, hex: f.hex, origin: f.origin };
    });
    st.offset = (info.offset || 0) + frames.length;
    st.frames = st.frames.concat(frames);
    st.rendered = true;
    st.exhausted = st.offset >= st.total;
    stlogRerender(tab);
    stlogUpdateChrome(tab);
    if (!st.firstLoadDone) {
      st.firstLoadDone = true;
      st.content.scrollTop = 0; // 离线正序：从第一帧开始看
    }
    if (st.total === 0) stlogSysRow(tab, '文件里没有帧');
  }).catch(function (err) {
    st.loading = false;
    st.moreBtn.disabled = false;
    if (window.console) console.error('[stlog] ' + tab.path, err);
    stlogSysRow(tab, '读取失败: ' + String(err));
  });
}

// stlogToggleMode 切换文件页的 Hex / 文本 视图。
//
// 文件页不复用全局 state.displayMode：会话页默认文本（用户在那儿发的是 ASCII），
// 而 .stlog 里是原始字节，一打开就该按 Hex 看。两边各存一份，谁也别改对方的设置。
function stlogToggleMode(tab) {
  var st = tab._stlog;
  st.mode = (st.mode === 'hex') ? 'text' : 'hex';
  stlogRerender(tab);
}

// stlogRerender 用缓存的帧按当前 mode 重画（不重新读文件、不动 offset）。
function stlogRerender(tab) {
  var st = tab._stlog;
  st.content.innerHTML = '';
  st.frames.forEach(function (f) {
    makeDataRow(st.content, stlogDirName(f.dir),
                stlogTsText(f.ts, state.stlogShowYear !== false), f.hex,
                null, { displayMode: st.mode }, f.origin || '');
  });
  st.content.appendChild(st.more);
  st.modeBtn.textContent = (st.mode === 'hex') ? 'Hex' : '文本';
}

function stlogExport(path, btn) {
  var label = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = '导出中…'; }
  window.go.main.App.ExportStlogText(path).then(function (res) {
    if (btn) { btn.disabled = false; btn.textContent = label; }
    if (!res || res.canceled) return; // 用户自己取消，不弹框
    showAlert('.stlog 已导出',
      res.path + '\n\n' + res.frames + ' 帧 · ' + res.bytes + ' B', null, true);
  }).catch(function (err) {
    if (btn) { btn.disabled = false; btn.textContent = label; }
    showAlert('导出失败', String(err), null, true);
  });
}

// openStlogTab 开一个只读标签页（单个 .stlog，或工程目录的本地浏览形态）。
//
// 数据一律来自本地磁盘，离线也能用。kind='session' 的工程页多一个能力：顶栏
// 「推送到守护进程」—— 点了就把它交给守护进程，恢复成能继续用的会话页，然后
// 这个只读页自行关闭（见 stlogPushToDaemon）。
function openStlogTab(path, kind) {
  for (var i = 0; i < state.tabs.length; i++) {
    if (state.tabs[i].type === 'stlog' && state.tabs[i].path === path) {
      switchTab(i);
      return state.tabs[i];
    }
  }

  var page = document.createElement('div');
  page.className = 'tab-page';
  page.style.display = 'none';

  var bar = document.createElement('div');
  bar.className = 'o-stlog-bar t-stlog-bar';

  var infoBox = document.createElement('div');
  infoBox.className = 'o-stlog-bar__info';
  var nameEl = document.createElement('div');
  nameEl.className = 't-stlog-name';
  nameEl.textContent = stlogBaseName(path);
  var metaEl = document.createElement('div');
  metaEl.className = 't-stlog-meta';
  metaEl.textContent = path;
  infoBox.appendChild(nameEl);
  infoBox.appendChild(metaEl);

  var actions = document.createElement('div');
  actions.className = 'o-stlog-bar__actions';
  // 行时间戳是否带年份（顶栏的时间范围永远带年，这个开关只影响逐行显示）。
  // 选择持久化到 settings/gui.ini 的 stlog_show_year。
  var yearBtn = document.createElement('button');
  yearBtn.className = 't-stlog-btn' + (state.stlogShowYear !== false ? ' is-on' : '');
  yearBtn.textContent = '年';
  yearBtn.title = '行时间戳显示年份（顶栏始终带年）';
  yearBtn.addEventListener('click', function () {
    state.stlogShowYear = !(state.stlogShowYear !== false);
    yearBtn.classList.toggle('is-on', state.stlogShowYear);
    stlogRerender(tab);
    if (typeof saveSettings === 'function') saveSettings();
  });
  var countEl = document.createElement('span');
  countEl.className = 't-stlog-meta';
  // Hex / 文本 切换：标签显示的是**当前**视图，点一下换另一种。
  var modeBtn = document.createElement('button');
  modeBtn.className = 't-stlog-btn';
  modeBtn.textContent = 'Hex';
  modeBtn.title = 'Hex / 文本 视图切换';
  modeBtn.addEventListener('click', function () { stlogToggleMode(tab); });
  var exportBtn = document.createElement('button');
  exportBtn.className = 't-stlog-btn';
  exportBtn.textContent = '导出文本';
  exportBtn.addEventListener('click', function () { stlogExport(path, exportBtn); });
  // 「推送到守护进程」：只有工程目录才有，单 .stlog 是包文件，守护进程不收。
  // 打开时不自动推 —— 用户可能只是想翻翻记录。
  var pushBtn = document.createElement('button');
  pushBtn.className = 't-stlog-btn';
  pushBtn.textContent = '推送到守护进程';
  pushBtn.style.display = 'none';
  pushBtn.addEventListener('click', function () { stlogPushToDaemon(tab); });
  actions.appendChild(countEl);
  actions.appendChild(yearBtn);
  actions.appendChild(modeBtn);
  actions.appendChild(exportBtn);
  actions.appendChild(pushBtn);

  bar.appendChild(infoBox);
  bar.appendChild(actions);
  page.appendChild(bar);

  var content = document.createElement('div');
  content.className = 'o-stlog-rows t-stlog-rows';
  page.appendChild(content);

  var more = document.createElement('div');
  more.className = 'o-stlog-more';
  var moreBtn = document.createElement('button');
  moreBtn.className = 't-stlog-btn';
  moreBtn.textContent = '加载更多';
  more.addEventListener('click', function () { stlogLoad(tab); });
  more.appendChild(moreBtn);
  content.appendChild(more);

  var oldPage = getActivePage();
  if (oldPage) oldPage.hide();
  if (state._welcomePage) state._welcomePage.hide();

  var tab = {
    id: state.tabCounter++,
    label: stlogBaseName(path),
    type: 'stlog',
    path: path,
    portOpen: false,
    mode: 'single',
    source: 'gui',
    _stlog: {
      offset: 0, total: 0, loaded: false, loading: false, rendered: false,
      // isDir：工程目录（可以推给守护进程）；单个 .stlog 一律只读
      isDir: kind === 'session',
      mode: 'hex', frames: [], modeBtn: modeBtn, pushBtn: pushBtn,
      content: content, more: more, moreBtn: moreBtn, meta: metaEl, count: countEl
    },
    _page: {
      root: page,
      show: function () {
        page.style.display = '';
        if (!tab._stlog.loaded && !tab._stlog.loading) stlogLoad(tab);
      },
      hide: function () { page.style.display = 'none'; },
      destroy: function () { page.remove(); }
    }
  };
  state.tabs.push(tab);
  var mc = document.getElementById('mainContent');
  if (mc) mc.appendChild(page);

  state.activeTab = state.tabs.length - 1;
  state.currentMode = 'single';
  page.style.display = '';
  renderTabs();
  updateOpenBtn();
  updateEmptyState();
  // 关键：遮罩是按「当前活动标签」判定的，直接改 state.activeTab 不会触发重算。
  // 少了这一句，离线拖入 .stlog 时新打开的文件页会被「守护进程未启用」盖住。
  updateDaemonUI();
  stlogSyncPushBtn(tab);
  stlogLoad(tab);
  return tab;
}

// openProjectTab 打开一个工程目录：先给一页只读浏览（离线也能翻记录）。
//
// 想接着用的时候按顶栏「推送到守护进程」，它会被守护进程恢复成会话页
// （单端口 / 转发），然后这个只读页自行关闭（见 stlogPushToDaemon）。
function openProjectTab(path) {
  openStlogTab(path, 'session');
}

// _waitProjectTab 等 syncDaemonSessions 把工程建成标签页，然后切过去。
//
// 不能只等一次：process-changed 可能比 OpenStlogSession 的响应先到，而
// syncDaemonSessions 有 syncPending 守卫（正在同步时直接返回），所以这里边等边催。
function _waitProjectTab(path) {
  var tries = 0;
  return new Promise(function (resolve, reject) {
    (function tick() {
      for (var i = 0; i < state.tabs.length; i++) {
        if (state.tabs[i]._projectPath === path) { switchTab(i); resolve(state.tabs[i]); return; }
      }
      if (++tries > 50) {
        delete state._pendingProjectPaths[path];
        reject(new Error('守护进程没有把这个工程挂上来（进程可能创建失败）'));
        return;
      }
      syncDaemonSessions();
      setTimeout(tick, 100);
    })();
  });
}

// stlogSyncPushBtn 刷新顶栏「推送到守护进程」按钮的状态。
//
// 单 .stlog 没有这个能力（守护进程不收包文件），直接隐藏 —— 留个永远点不动的
// 灰按钮只会让人以为功能坏了。
function stlogSyncPushBtn(tab) {
  var st = tab._stlog;
  var btn = st.pushBtn;
  if (!btn) return;
  if (!st.isDir) { btn.style.display = 'none'; return; }
  btn.style.display = '';
  btn.textContent = '推送到守护进程';
  if (!state.daemonOnline) {
    btn.disabled = true;
    btn.title = '守护进程未启动（菜单 守护进程 → 启停）';
    return;
  }
  btn.disabled = false;
  btn.title = '把这个工程交给守护进程，恢复成会话页（单端口 / 转发）；历史从共享环读';
}

// stlogPushToDaemon 把当前只读工程页交给守护进程，换成会话页。
//
// 会话页由 syncDaemonSessions 建（_pendingProjectPaths 登记 + _waitProjectTab 等），
// 建成并切过去之后，这个只读页就没有存在意义了，静默关掉。
// 失败时保持原样：数据还在，报个错就行。
function stlogPushToDaemon(tab) {
  var st = tab._stlog;
  if (!st.isDir || st.pushing || !state.daemonOnline) return;
  st.pushing = true;
  if (st.pushBtn) { st.pushBtn.disabled = true; st.pushBtn.textContent = '推送中…'; }
  state._pendingProjectPaths[tab.path] = true;
  window.go.main.App.OpenStlogSession(tab.path).then(function () {
    return _waitProjectTab(tab.path);
  }).then(function () {
    _closeStlogByPath(tab.path);
  }).catch(function (err) {
    st.pushing = false;
    delete state._pendingProjectPaths[tab.path];
    stlogSyncPushBtn(tab);
    showAlert('推送到守护进程失败', String(err) + '\n\n这一页仍然按本地文件读。', null, true);
  });
}

// _closeStlogByPath 静默关掉某个只读页（推送成功后由会话页接手）。
//
// 不走 closeTab：那里会处理 sessionId 清理、焦点转移和模式刷新，而这里只是把
// 一个不持有会话的页摘掉，顺手把 activeTab 索引修正到会话页上。
function _closeStlogByPath(path) {
  for (var i = 0; i < state.tabs.length; i++) {
    var t = state.tabs[i];
    if (t.type !== 'stlog' || t.path !== path) continue;
    if (t._page) { t._page.destroy(); t._page = null; }
    state.tabs.splice(i, 1);
    if (state.activeTab > i) state.activeTab--;
    else if (state.activeTab === i) state.activeTab = Math.max(0, state.tabs.length - 1);
    renderTabs();
    updateEmptyState();
    var cur = getActiveTab();
    if (cur && cur._page) cur._page.show();
    return;
  }
}

// chooseOpenFile 「新建会话 → 从文件打开」的入口：先问开哪种，再弹对应的系统对话框。
//
// 两种都能离线打开、都是只读浏览。区别是工程目录多一个「推送到守护进程」的
// 能力：点了会被守护进程恢复成能继续用的会话页。Windows 一个对话框选不了两种，
// 所以先问一句。
function chooseOpenFile() {
  showChoice('打开会话',
    '要打开的是哪种？\n\n· 工程目录：先按只读页浏览，需要时推送到守护进程，恢复成会话\n· .stlog 文件：单个包文件，只读浏览\n\n两种都能离线打开',
    [
      { label: '工程目录', onPick: pickProjectDir },
      { label: '.stlog 文件', onPick: pickStlogFile }
    ]);
}

// _pickPath 弹对话框 → 预检 → 交给打开函数。
//
// 预检复用拖放那套（ProbeDroppedPaths）：用户完全可能选中一个不相干的目录，或者
// 扩展名对、内容却不是会话的文件；那种路径打开只会得到空页面，不如当场说清楚。
function _pickPath(dlg, kind, open) {
  var App = window.go.main.App;
  dlg.then(function (path) {
    if (!path) return; // 用户取消
    var idx = state.tabs.findIndex(function (t) { return t.type === 'new'; });
    // 先关选择页再开新页：反过来的话 closeTab 可能把焦点从新标签挪走
    function go() {
      if (idx >= 0) closeTab(idx);
      open(path);
    }
    if (typeof App.ProbeDroppedPaths !== 'function') { go(); return; }
    return App.ProbeDroppedPaths([path]).then(function (probes) {
      var pr = probes && probes[0];
      if (pr && pr.kind !== kind) {
        showAlert('无法解析',
          pr.path + '\n  ' + (pr.err || (kind === 'session' ? '这里没有工程' : '这不是 .stlog 文件')), null, true);
        return;
      }
      go();
    });
  }).catch(function (err) {
    showAlert('打开失败', String(err), null, true);
  });
}

// pickProjectDir 选一个工程目录，开成只读页（顶栏可推送到守护进程）。
function pickProjectDir() {
  _pickPath(window.go.main.App.PickStlogDir(), 'session', openProjectTab);
}

// pickStlogFile 选一个 .stlog，开成只读文件页。
function pickStlogFile() {
  _pickPath(window.go.main.App.PickStlogFile(), 'file', openStlogTab);
}

// ---- 拖入注册 ----
//
// 用 window.runtime / window.go 而不是 import：index.html 是经典 <script>，
// wailsjs/ 下都是 ES 模块，混用会抛错。
(function () {
  var INSTALLED_KEY = '__stlogDropInstalled';

  // ---- 拖入遮罩 ----
  // 范围：菜单栏下沿 → 状态栏上沿。不写死像素——每次拖动按实际高度重算，
  // 将来改 header / statusbar 的高度也不用回来改这里。
  var _dropDepth = 0;

  function _hasFiles(e) {
    var dt = e.dataTransfer;
    if (!dt || !dt.types) return false;
    for (var i = 0; i < dt.types.length; i++) {
      if (dt.types[i] === 'Files') return true;
    }
    return false;
  }

  function _dropMaskSync() {
    var el = document.getElementById('dropMask');
    if (!el) return;
    var head = document.querySelector('.header');
    var status = document.getElementById('statusbar');
    el.style.top = (head ? head.offsetHeight : 26) + 'px';
    el.style.bottom = (status ? status.offsetHeight : 24) + 'px';
    var on = _dropDepth > 0;
    el.classList.toggle('is-on', on);
    el.setAttribute('aria-hidden', on ? 'false' : 'true');
  }

  // dragenter/dragleave 在子元素之间来回触发，必须计数，否则遮罩会闪。
  window.addEventListener('dragenter', function (e) {
    if (!_hasFiles(e)) return;
    _dropDepth++;
    _dropMaskSync();
  }, true);
  window.addEventListener('dragleave', function () {
    // 离开窗口时 dataTransfer.types 可能已经空了，这里不能再判 _hasFiles
    if (_dropDepth > 0) { _dropDepth--; _dropMaskSync(); }
  }, true);
  // 拖到窗口外松手 / 切走窗口时计数会残留，统一清零
  window.addEventListener('blur', function () { _dropDepth = 0; _dropMaskSync(); });

  // (1) 无条件阻止默认拖放。必须在任何 return 之前：否则 WebView2 把拖进来的
  //     文件当成"下载"，在 Downloads 里生成 "xxx (1).stlog" 这种副本（实锤过）。
  //     这一步不依赖 window.runtime / window.go。
  window.addEventListener('dragover', function (e) {
    e.preventDefault();
    if (_hasFiles(e) && e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
  }, true);
  window.addEventListener('drop', function (e) {
    e.preventDefault();
    _dropDepth = 0;
    _dropMaskSync();
  }, true);

  function diag() {
    return [
      'runtime = ' + (typeof window.runtime),
      'OnFileDrop = ' + (window.runtime ? typeof window.runtime.OnFileDrop : 'n/a'),
      'go = ' + (typeof window.go),
      'App = ' + (window.go && window.go.main ? typeof window.go.main.App : 'n/a'),
      'OpenStlogFile = ' + (window.go && window.go.main && window.go.main.App
                            ? typeof window.go.main.App.OpenStlogFile : 'n/a'),
      'ReadStlogFrames = ' + (window.go && window.go.main && window.go.main.App
                            ? typeof window.go.main.App.ReadStlogFrames : 'n/a'),
      'ExportStlogText = ' + (window.go && window.go.main && window.go.main.App
                            ? typeof window.go.main.App.ExportStlogText : 'n/a'),
      'enableWailsDragAndDrop = ' + (window.wails && window.wails.flags
                            ? window.wails.flags.enableWailsDragAndDrop : 'n/a')
    ].join('\n');
  }

  // 应用内模态（项目自带）。不能用 window.runtime.MessageDialog：Wails v2 的
  // 对话框只有 Go 侧 API（pkg/runtime/dialog.go），JS 侧 window.runtime 里没有它
  // （那是 v1 的 API），调用会抛 TypeError —— 实测拖入文件后控制台报的就是这个。
  function say(title, msg, mono) {
    if (typeof showAlert === 'function') {
      showAlert(title, msg, null, mono);
      return;
    }
    window.alert(title + '\n\n' + msg);
  }

  // 拖进来的东西统一交给 Go 侧预检：工程目录和 .stlog 文件都能开，
  // 其余类型汇总成一个警告窗。前端拿不到 stat，分不清"目录"和"无扩展名文件"，
  // 所以这个判断只能放 Go（见 App.ProbeDroppedPaths）。
  function onDrop(x, y, paths) {
    var list = (paths || []).filter(Boolean);
    if (!list.length) {
      say('无法解析', '没有拿到拖入的路径。', true);
      return;
    }
    var App = (window.go && window.go.main && window.go.main.App) || {};
    if (typeof App.ProbeDroppedPaths !== 'function') {
      // 老 exe 没有预检：退回「只看扩展名」，至少 .stlog 还能开
      list.filter(function (p) { return /\.stlog$/i.test(p); }).forEach(function (p) {
        openStlogTab(p);
      });
      return;
    }
    App.ProbeDroppedPaths(list).then(function (probes) {
      var bad = [];
      (probes || []).forEach(function (pr) {
        if (pr.kind === 'session' || pr.kind === 'file') {
          try {
            // 工程目录 → 恢复成会话页；单个 .stlog → 只读文件页
            if (pr.kind === 'session') openProjectTab(pr.path);
            else openStlogTab(pr.path);
          } catch (err) {
            if (window.console) console.error('[stlog] ' + pr.path, err);
            bad.push(pr.path + '\n  ' + String(err));
          }
          return;
        }
        bad.push(pr.path + '\n  ' + (pr.err || '无法识别的类型'));
      });
      if (bad.length) say('无法解析', bad.join('\n\n'), true);
    }).catch(function (err) {
      say('无法解析', String(err), true);
    });
  }

  function install() {
    if (window[INSTALLED_KEY]) return true;
    if (!window.runtime || typeof window.runtime.OnFileDrop !== 'function') return false;
    if (!window.go || !window.go.main || !window.go.main.App) return false;
    // 要求的是标签页真正用到的两个绑定：老 exe 只有 OpenStlogFile，
    // 配新前端时会因为缺 ReadStlogFrames 而开不出页签——这时宁可弹诊断框，
    // 也别让用户以为"拖放又没反应"。
    if (typeof window.go.main.App.ReadStlogFrames !== 'function') return false;
    if (typeof window.go.main.App.ExportStlogText !== 'function') return false;
    window.runtime.OnFileDrop(onDrop, false); // false = 整窗口接收，不需要 drop-target 元素
    window[INSTALLED_KEY] = true;
    if (window.console) console.log('[stlog] drag-and-drop registered');
    return true;
  }

  // (2) 注册失败时别装死：用户拖一次就能看到到底缺了哪一环。
  var diagShown = false;
  window.addEventListener('drop', function () {
    if (window[INSTALLED_KEY] || diagShown) return;
    diagShown = true;
    say('拖放未启用（诊断）', diag());
  }, true);

  if (install()) return;
  var tries = 0;
  var timer = setInterval(function () {
    if (install() || ++tries >= 20) clearInterval(timer);
  }, 100);
})();