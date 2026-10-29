// ---- history rendering & cache ----
// Depends on: state (app.js), t()/formatSysMsg() (i18n.js), decoder functions (decoder.js)
//
// 设计要点 —— 这个窗口存在的理由是**性能**，所以三条界必须各自成立：
//
//  · 渲染界：DOM 只保留「渲染窗口」内的数据行。窗口大小随视口推导（设置里可覆盖）。
//    实测证据：DOM 无上限时单帧成本随节点数线性增长（2000 节点 140µs → 20000 节点
//    4005µs，整体 O(n²)）；有窗口时恒定在 ~1000µs。**窗口不能拆。**
//
//  · 每帧工作量有界：入站帧先进队列，由 rAF 合并成一批 —— 一批一次 fragment 插入、
//    一次裁剪、一次贴底。逐条插入时每帧要读两次 scrollHeight（改完 DOM 立刻读会强制
//    同步布局），而读的成本是 O(节点数)：150 节点 44µs、20000 节点 4313µs。
//    实测 893 帧/秒 → 批量后 8400+ 帧/秒（同一 DOM 上限）。
//
//  · 贴底用写不用读：`scrollTop = 极大值` 会被浏览器夹到底部，写只要 ~2µs，
//    而 `scrollTop = scrollHeight` 要先读一次 scrollHeight。
//
// 用户往回翻时（或锁定滚动时）：视图冻结，新数据**不进 DOM**，改用末尾一个
// 「占位块」按估算行高撑开高度 —— 视图不动，但滚动条继续变化，DOM 仍是 O(窗口)。

var HISTORY_WINDOW_MIN = 150;
var HISTORY_WINDOW_MAX = 400;
var HISTORY_WINDOW = HISTORY_WINDOW_MIN; // 兼容旧引用；实际用 historyWindowSize()

// ---- 有界缓存 ----
// 缓存存在的理由是「往回翻」而不是「全都在手上」：整环 5MB ≈ 19 万条，
// 全量留在 JS 里既吃内存又要每次重绘时遍历。留多少是有取舍的：
//   MAX  跟随时保留的条数（正常收数据时的稳态上限）
//   HARD 往回看时允许涨到的上限（否则刚回补的一页会立刻被自己裁掉）
//   SLACK 攒够这么多条再裁一次，把 splice 的成本摊薄
var HISTORY_CACHE_MAX = 10000;
var HISTORY_CACHE_HARD = 40000;
var HISTORY_CACHE_SLACK = 512;
// 缓存里 hex 字符总量上限。只按条数裁不够：4 KB 的包一条就是 8 K 字符，
// 1 万条 = 80 MB 字符串，再叠上 Go 解码出来的 segments 会更炸。
var HISTORY_CACHE_MAX_CHARS = 6 * 1024 * 1024;

// 一页回补多少条；首次加载取 INITIAL（回看深度至少等于稳态缓存的量级）
var HISTORY_PAGE_SIZE = 2000;
var HISTORY_INITIAL = 3000;

var _renderStart = -1; // DOM 窗口起点（cache 索引）
var _renderEnd = -1;   // DOM 窗口终点（cache 索引，不含）
var _atBottom = true;  // 用户是否贴着底部
var _frozen = false;   // 冻结：锁定滚动，或用户往回翻离开了底部
// 上一次 scroll 事件看到的 scrollTop。只靠 scrollHeight 判断"是否到底"会被
// content-visibility 骗：行是屏外估值高度（contain-intrinsic-size），滚进视口后
// 真实高度替换估值，scrollHeight 会凭空变大而 scrollTop 没动 —— 于是"明明在底部"
// 被判成"离开底部"，自动跟滚就此停住、未读越攒越多。所以还要看 scrollTop 有没有变小。
var _lastScrollTop = 0;
var _healing = false;  // 未读提示自愈重绘时的递归挡板
var _appendQueue = []; // 待渲染队列
var _flushScheduled = false;
var _flushTimer = null;
var _olderLoading = false;

// 渲染窗口行数：设置里的覆盖值优先，否则按视口推导。
function historyWindowSize() {
  var v = state.historyWindowRows;
  if (typeof v === 'number' && v > 0) {
    return Math.max(20, Math.min(2000, Math.floor(v)));
  }
  return calcRenderCount();
}

// 按视口推导：一条数据约两行，留约 12 屏的回看缓冲，夹在 [MIN, MAX]。
function calcRenderCount() {
  var display = pageEl('displayContent');
  if (!display) return HISTORY_WINDOW_MIN;
  var fs = state.displayFontSize || 15;
  var lineH = Math.max(8, fs * 1.6) * 2;
  var visible = Math.max(1, Math.floor(display.clientHeight / lineH));
  var rows = Math.ceil(visible * 12);
  return Math.max(HISTORY_WINDOW_MIN, Math.min(HISTORY_WINDOW_MAX, rows));
}

// ---- 调试/测试钩子（夹具用；线上无副作用） ----
function _histDebug() {
  var display = pageEl('displayContent');
  var entries = _activeCache();
  return {
    queue: _appendQueue.length,
    flushing: _flushScheduled,
    renderStart: _renderStart,
    renderEnd: _renderEnd,
    total: entries ? entries.length : 0,
    hidden: (entries && _renderEnd >= 0) ? Math.max(0, entries.length - _renderEnd) : 0,
    window: historyWindowSize(),
    frozen: _frozen,
    atBottom: _atBottom,
    scrollLocked: !!state.scrollLocked,
    domRows: display ? display.querySelectorAll('.data-row').length : 0,
    spacer: !!(display && display.querySelector('.hist-unread')),
    lastFlushMs: +_lastFlushMs.toFixed(2),
    lastFlushCount: _lastFlushCount,
    // 有界缓存 / 分页（夹具用）
    cacheLen: entries ? entries.length : 0,
    cacheCap: _frozen ? HISTORY_CACHE_HARD : HISTORY_CACHE_MAX,
    olderLoading: _olderLoading,
    ringExhausted: !!(entries && entries._ringExhausted),
    ringHasMore: !!(entries && entries._ringHasMore),
    cursorTs: (function () { var c = _cursorFrom(entries); return c ? c.tsMs : 0; })(),
    cursorSkip: (function () { var c = _cursorFrom(entries); return c ? c.skip : 0; })()
  };
}

function _histReset() {
  _appendQueue = [];
  _flushScheduled = false;
  if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
  _renderStart = -1;
  _renderEnd = -1;
  _atBottom = true;
  _lastScrollTop = 0;
  _frozen = !!state.scrollLocked;
}

function _activeCache() {
  var tab = getActiveTab();
  return (tab && tab.sessionId) ? state.historyCache[tab.sessionId] : null;
}

// ---- 有界缓存：裁剪 / 游标 ----

// 同一帧的指纹。时间戳字符串只有时分秒（跨天会撞车），所以用毫秒时间戳 +
// 方向 + 内容。守护进程的广播事件与写进环里的是**同一个** tsMs，因此前端
// 手上的那条与环里那条指纹必然相同。
function _fp(e) {
  return e.tsMs + '|' + e.direction + '|' + (e.origin || '') + '|' + e.hex;
}

// 分页游标：缓存里最旧一条的毫秒时间戳，以及缓存里一共有几条属于这一毫秒。
//
// 为什么不是简单的「比这个时间戳更早」：毫秒会撞车，同一毫秒里的帧常常一起
// 到达；只用 `ts <` 当游标会把边界那一毫秒剩下的帧永久丢掉。报出「我手上有
// 几条是这一毫秒的」，守护进程就能正好把缺的那几条补上。
//
// system 条目是前端自己造的（没有 tsMs），跳过它们往后找第一条真实数据。
function _cursorFrom(entries) {
  if (!entries || entries.length === 0) return null;
  var i = 0;
  while (i < entries.length && !(entries[i].tsMs > 0)) i++;
  if (i >= entries.length) return null;
  var ts = entries[i].tsMs;
  var skip = 0;
  for (var j = i; j < entries.length; j++) {
    if (entries[j].tsMs !== ts) break;
    skip++;
  }
  return { tsMs: ts, skip: skip };
}

// 把缓存裁到上限内。永远从**最旧的一端**裁，这样「最新的一屏」永远在手上
// （回到底部不需要任何 RPC）。
//
// 冻结时（用户往回翻或锁定滚动）上限放宽到 HARD：否则刚回补进来的一页会被
// 立刻裁掉，游标原地不动，往上翻就再也翻不动了。
//
// 裁到窗口里时，把对应的 DOM 行也一并去掉 —— 不能留着缓存里已经不存在的行
// 继续显示（那会让重绘与屏幕对不上）。
// 缓存里 hex 字符总量：用作「按字节预算裁剪」的依据。
// 平时靠 push 时累加；批量替换/合并之后把它置 undefined，下一次裁剪时重算。
function _cacheChars(arr) {
  if (!arr) return 0;
  if (typeof arr._hexChars === 'number') return arr._hexChars;
  var n = 0;
  for (var i = 0; i < arr.length; i++) {
    if (arr[i] && arr[i].hex) n += arr[i].hex.length;
  }
  arr._hexChars = n;
  return n;
}

function _trimCache(arr) {
  if (!arr) return 0;
  var active = (_activeCache() === arr);
  var cap = (active && _frozen) ? HISTORY_CACHE_HARD : HISTORY_CACHE_MAX;
  var drop = Math.max(0, arr.length - cap);

  // 再按字符预算裁：大包场景下 1 万条会撑到几十上百 MB
  var chars = _cacheChars(arr);
  var overBudget = chars > HISTORY_CACHE_MAX_CHARS;
  while (chars > HISTORY_CACHE_MAX_CHARS && arr.length - drop > 200) {
    var head = arr[drop];
    chars -= (head && head.hex) ? head.hex.length : 0;
    drop++;
  }

  if (drop <= 0) return 0;
  // 小批裁剪会白付 splice 的代价：攒够一批再裁；但超预算时必须立刻裁
  if (!overBudget && drop < HISTORY_CACHE_SLACK) return 0;

  arr.splice(0, drop);
  arr._hexChars = chars;
  if (typeof arr._clearedAt === 'number') {
    arr._clearedAt -= drop;
    if (arr._clearedAt < 0) delete arr._clearedAt; // 清空标记本身也被裁掉了，它已经没有意义
  }
  if (active) {
    var domDrop = Math.max(0, drop - _renderStart);
    _renderStart = Math.max(0, _renderStart - drop);
    _renderEnd = Math.max(0, _renderEnd - drop);
    if (domDrop > 0) _dropDomRows(domDrop);
  }
  return drop;
}

// 从显示区顶部同步移除 n 行数据行（只在缓存裁到窗口里时才发生）
function _dropDomRows(n) {
  var display = pageEl('displayContent');
  if (!display) return;
  var rows = display.querySelectorAll('.data-row');
  for (var i = 0; i < n && i < rows.length; i++) {
    if (rows[i].parentNode) rows[i].parentNode.removeChild(rows[i]);
  }
}

// ---- 行高估算（配 .data-row 的 content-visibility:auto 用） ----
//
// 屏外行没有真实高度，浏览器只能用 contain-intrinsic-size 的估值占位。若统一按
// 46px 估，4 KB 的大包会被估成一行、实际却是上百行，滚动时整块内容会跳；
// 所以这里按「时间戳行 + 数据折行行数」逐行算一个贴近真实的占位高度。

var _charWCache = { key: '', w: 0 };

// 等宽字符宽度：用显示区实际字体量一次；字体或字号变了就重测。
function _monoCharWidth() {
  var key = (state.displayFontFamily || '') + '|' + (state.displayFontSize || 15);
  if (_charWCache.key === key && _charWCache.w > 0) return _charWCache.w;
  var w = 0;
  try {
    var display = pageEl('displayContent');
    var cs = display ? getComputedStyle(display) : null;
    var font = cs
      ? (cs.fontStyle + ' ' + cs.fontWeight + ' ' + cs.fontSize + ' ' + cs.fontFamily)
      : '15px monospace';
    var ctx = document.createElement('canvas').getContext('2d');
    if (ctx) {
      ctx.font = font;
      w = ctx.measureText('0'.repeat(64)).width / 64;
    }
  } catch (e) { w = 0; }
  if (!(w > 0)) w = (state.displayFontSize || 15) * 0.6;   // 兜底：等宽约 0.6em
  _charWCache.key = key;
  _charWCache.w = w;
  return w;
}

function _estRowHeight(dataEl) {
  var display = pageEl('displayContent');
  var fs = state.displayFontSize || 15;
  var width = display ? (display.clientWidth - 28) : 900;   // 左右 padding
  if (!(width > 40)) width = 900;
  var cols = Math.max(10, Math.floor(width / _monoCharWidth()));
  var chars = dataEl ? (dataEl.textContent || '').length : 0;
  var lines = Math.max(1, Math.ceil(chars / cols));
  return Math.ceil(fs * 1.4 + lines * fs * 1.5 + 2);        // .ts-line 1.4 / .data-line 1.5 + 行间距
}

// ---- 单行构造（不再有滚动副作用，贴底统一由批次处理） ----

// Tx 来源的显示名。来源格式见 daemon 侧 RxTxMessage.Origin：
//   "gui-a1b2c3d4" / "cli-..." / "mcp-..."  —— 客户端会话，取连字符前那段大写
//   "auto" / "multistr"                     —— 守护进程自己的发送循环，走 i18n
// 完整来源放在 title 里：同类型有多个客户端时靠它区分。
function _originLabel(origin) {
  var s = String(origin);
  var dash = s.indexOf('-');
  if (dash < 0) {
    // 语言包已加载时 t() 对缺键返回空串，必须自己兜底成原始 token，
    // 否则未来新增的引擎来源会渲染成一个空气泡。
    var text = (typeof t === 'function') ? t('origin.' + s, '') : '';
    return text || s;
  }
  var src = s.slice(0, dash);
  return src ? src.toUpperCase() : s;
}

// opts（可选）：{ displayMode } —— 只给设置页的颜色预览用，让它能在一屏里同时
// 画「文本」和「Hex」两种渲染，而不必去改全局 state.displayMode。
function makeDataRow(container, dir, timestamp, hex, segments, opts, origin) {
  const row = document.createElement('div');
  row.className = 'data-row';

  const tsLine = document.createElement('div');
  const isFwd = (dir === '1' || dir === '2');
  const arrow = isFwd ? '' : (dir === 'tx' ? ' ↾' : ' ⇃');
  const tagClass = dir === '1' ? 'fwd1-line' : dir === '2' ? 'fwd2-line' : (dir === 'rx' ? 'rx-line' : 'tx-line');
  const label = isFwd ? 'P' + dir : (dir === 'rx' ? 'Rx' : 'Tx');
  var byteLen = hex ? hex.length / 2 : 0;
  var tsDirClass = dir === '1' ? 'ts-1' : dir === '2' ? 'ts-2' : (dir === 'rx' ? 'ts-rx' : 'ts-tx');
  tsLine.className = 'ts-line ' + tsDirClass;
  var statClass = isFwd ? 'ts-fstat' : 'ts-stat';
  // 只有 Tx 有来源；老历史（没写来源的包）origin 为空，不渲染任何标记。
  var originHtml = '';
  if (dir === 'tx' && origin) {
    originHtml = '<span class="t-origin" title="' + escHtml(origin) + '">' +
      escHtml(_originLabel(origin)) + '</span>';
  }
  tsLine.innerHTML = '<span class="ts-text">[' + (timestamp || '') + '] ' + label + arrow + ' </span>' +
    originHtml + '<span class="' + statClass + '">~' + byteLen + 'B</span>';
  row.appendChild(tsLine);

  const dataLine = document.createElement('div');
  dataLine.className = 'data-line';

  let dm;
  if (opts && opts.displayMode) {
    dm = opts.displayMode;
  } else {
    dm = state.displayMode;
    if (activeMode() === 'forward') {
      if (dir === '1') dm = state.p1DisplayMode;
      else if (dir === '2') dm = state.p2DisplayMode;
    } else if (dir === 'tx') {
      dm = state.txDisplayMode;
    }
  }
  if (dm === 'hex') {
    const hexSpan = document.createElement('span');
    var hexDirClass = dir === '1' ? 'hex-1' : dir === '2' ? 'hex-2' : (dir === 'rx' ? 'hex-rx' : 'hex-tx');
    hexSpan.className = 'hex-data ' + hexDirClass;
    // 直接从 hex 串格式化（formatHexString 少一次数组分配）
    hexSpan.textContent = (typeof formatHexString === 'function')
      ? formatHexString(hex)
      : formatHex(hexToBytes(hex));
    dataLine.appendChild(hexSpan);
  } else if (segments && segments.length > 0) {
    // Go-decoded segments — zero-innerHTML rendering
    renderSegments(dataLine, segments, dir);
  } else {
    // Legacy fallback: JS-side hexToText (backward compat for cached entries)
    let text;
    if (state.encoding === 'ascii') {
      text = sanitizeText(hexToText(hex));
    } else {
      text = hexToText(hex);
    }
    dataLine.innerHTML = text;
  }

  row.appendChild(dataLine);
  // 给屏外行一个贴近真实的占位高度（见 _estRowHeight 的说明）
  if (window.CSS && CSS.supports && CSS.supports('contain-intrinsic-size', 'auto 46px')) {
    row.style.setProperty('contain-intrinsic-size', 'auto ' + _estRowHeight(dataLine) + 'px');
  }
  container.appendChild(row);
  return row;
}

// 系统消息行的构造（占位块与提示也用这个壳）
function _makeSysRow(text) {
  var line = document.createElement('div');
  line.className = 'sys-msg';
  line.textContent = '── ' + text + ' ──';
  return line;
}

function appendSystemMsg(msg) {
  var display = pageEl('displayContent');
  if (!display) return;
  if (display.querySelector('.placeholder')) display.innerHTML = '';
  var line = document.createElement('div');
  line.className = 'sys-msg';
  line.setAttribute('data-sys-raw', msg);
  line.textContent = '── ' + formatSysMsg(msg) + ' ──';
  display.appendChild(line);
  if (!_frozen) _stickToBottom(display);

  var tab = getActiveTab();
  if (tab && tab.sessionId) {
    var ts = new Date().toISOString().slice(11, 23).replace(/\./g, ':').slice(0, 12); // HH:MM:SS:mmm
    var entry = { direction: 'system', hex: msg, timestamp: ts };
    if (!state.historyCache[tab.sessionId]) state.historyCache[tab.sessionId] = [];
    var arr = state.historyCache[tab.sessionId];
    var dup = arr.length > 0 && arr[arr.length - 1].direction === 'system' && arr[arr.length - 1].hex === msg;
    if (!dup) arr.push(entry);
    arr._hexChars = undefined;   // 批量入口：下次裁剪时重算字符总量
    if (_renderEnd >= 0) _renderEnd = arr.length;
    _trimCache(arr);
  }
}

// ---- 入站数据：只入队，渲染交给批次 ----

function appendLine(dir, msg) {
  // Find the tab matching this message's processId
  var ownerTab = null;
  if (msg.processId) {
    for (var i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i].sessionId === msg.processId) { ownerTab = state.tabs[i]; break; }
    }
  }
  if (!ownerTab) return;

  // Always cache the entry for the owning tab, even if not active
  const entry = { direction: dir, hex: msg.hex, timestamp: msg.timestamp, tsMs: msg.tsMs, segments: msg.segments, origin: msg.origin };
  if (!state.historyCache[ownerTab.sessionId]) { state.historyCache[ownerTab.sessionId] = []; state.historyCache[ownerTab.sessionId]._id = ownerTab.sessionId; }
  var ownerCache = state.historyCache[ownerTab.sessionId];
  ownerCache.push(entry);
  if (typeof ownerCache._hexChars === 'number') {
    ownerCache._hexChars += entry.hex ? entry.hex.length : 0;
  }
  _trimCache(ownerCache);

  // Only render to DOM if this tab is currently active
  const activeTab = getActiveTab();
  if (!activeTab || activeTab.sessionId !== ownerTab.sessionId) return;

  _appendQueue.push(entry);
  _scheduleFlush();
}

// rAF 合并 + 定时兜底（窗口最小化时 rAF 不触发，队列不能无界滞留）
function _scheduleFlush() {
  if (_flushScheduled) return;
  _flushScheduled = true;
  requestAnimationFrame(_flushNow);
  if (_flushTimer === null) _flushTimer = setTimeout(_flushNow, 200);
}

function _flushNow() {
  _flushScheduled = false;
  if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
  _flushAppends();
}

// 上一批的渲染耗时与条数（诊断与夹具用）
var _lastFlushMs = 0;
var _lastFlushCount = 0;

function _flushAppends() {
  var display = pageEl('displayContent');
  var q = _appendQueue;
  _appendQueue = [];
  if (!display) return;
  if (q.length === 0) { _updateUnreadChrome(display); return; }

  var t0 = performance.now();

  if (display.querySelector('.placeholder')) display.innerHTML = '';

  // 冻结：视图不动，未读交给占位块撑高度（滚动条继续变化，但 DOM 不增长）
  if (_frozen) { _updateUnreadChrome(display); return; }

  var win = historyWindowSize();
  // 一批可能远超窗口：只渲染窗口装得下的最新那部分，多建的本来也会被裁掉
  var from = Math.max(0, q.length - win);
  var frag = document.createDocumentFragment();
  for (var i = from; i < q.length; i++) {
    var e = q[i];
    if (e.direction === 'system') {
      var line = _makeSysRow(formatSysMsg(e.hex || ''));
      line.setAttribute('data-sys-raw', e.hex || '');
      frag.appendChild(line);
    } else {
      makeDataRow(frag, e.direction, e.timestamp, e.hex, e.segments || null, null, e.origin);
    }
  }
  display.appendChild(frag);
  var entries = _activeCache();
  _renderEnd = entries ? entries.length : _renderEnd;
  _trimToWindow(display, win);
  _stickToBottom(display);
  _updateUnreadChrome(display);

  _lastFlushMs = performance.now() - t0;
  _lastFlushCount = q.length;
}

// 贴底：写一个极大值，浏览器会夹到底部 —— 不去读 scrollHeight（读是 O(节点数)）
function _stickToBottom(display) {
  if (!display) return;
  display.scrollTop = 1e9;
  _atBottom = true;
  _lastScrollTop = display.scrollTop;   // 夹到底之后的真实值
}

// 只裁剪数据行，不碰提示行与占位块；始终执行（无论是否锁定/冻结）
//
// fromBottom：往上翻加载更早时，新行插在**顶部**，超出的部分要从**底部**裁。
// 此前一律从顶部裁，于是刚插进去的那批又被自己删掉（_renderStart 反而被加回
// 原值），表现为「滚到顶加载更早，画面纹丝不动」。
function _trimToWindow(display, win, fromBottom) {
  var rows = display.querySelectorAll('.data-row');
  var excess = rows.length - win;
  if (excess <= 0) return;
  if (fromBottom) {
    for (var b = 0; b < excess; b++) {
      var tail = rows[rows.length - 1 - b];
      if (!tail || !tail.parentNode) continue;
      tail.parentNode.removeChild(tail);
    }
    var kept = rows.length - excess;
    _renderEnd = Math.max(_renderStart + kept, _renderStart);
    return;
  }
  for (var i = 0; i < excess; i++) {
    var el = rows[i];
    if (!el || !el.parentNode) continue;
    el.parentNode.removeChild(el);
    if (_renderStart >= 0) _renderStart++;
  }
}

// ---- 未读提示 + 自绘滚动条 ----
//
// 以前冻结时往滚动区里塞一块「占位块」撑高度，想让滚动条跟着未读数变长。代价
// 太大：那块空白可以被滚进去（整屏空白），并且把「是否到底」的判据污染掉。
// 现在两者分离：
//   · 自绘滚动条只反映**真实内容**（拖到底就是真到底）
//   · 未读数放在右下角一个小胶囊里，点它直接回到最新一条

function _unreadCount() {
  var entries = _activeCache();
  if (!entries || _renderEnd < 0) return 0;
  return Math.max(0, entries.length - _renderEnd);
}

function _updateUnreadChrome(display) {
  if (!display) display = pageEl('displayContent');
  if (!display) return;
  var host = display.parentNode;
  if (!host) return;

  // 防御：冻结时视图里必须还有内容（缓存裁剪 / 切页等异常路径会把行清空）。
  // 用 _healing 挡住递归。
  if (_frozen && _unreadCount() > 0 && !_healing &&
      display.querySelectorAll('.data-row').length === 0) {
    _healing = true;
    var prevTop = display.scrollTop;
    try {
      renderHistoryLines(_activeCache(), true);
      display.scrollTop = prevTop;
    } finally {
      _healing = false;
    }
    return;
  }

  var unread = _frozen ? _unreadCount() : 0;
  var chip = host.querySelector('.hist-unread');
  if (unread <= 0) {
    if (chip && chip.parentNode) chip.parentNode.removeChild(chip);
  } else {
    if (!chip) {
      chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'hist-unread';
      chip.title = t('history.unseen_click', '回到最新一条');
      chip.addEventListener('click', function() {
        _frozen = false;
        renderHistoryLines(_activeCache());
      });
      host.appendChild(chip);
    }
    chip.textContent = t('history.unseen', '↓ %s 条未显示').replace('%s', unread);
  }
  _syncCustomScrollbar(display);
}

// 自绘滚动条（显示区与发送框共用同一套外观）。
// 原生条隐藏（CSS），轨道与滑块只按真实内容计算；opts:
//   className  额外类名（发送框用 is-send）
//   top        轨道上沿（像素）；不给就按配置栏高度算，避免钻到浮动栏底下
//   onRelease  松手回调（显示区用它解冻）
function _initOverlayScrollbar(box, opts) {
  if (!box) return;
  opts = opts || {};
  var host = box.parentNode;
  if (!host || host.querySelector('.hist-scrollbar')) return;

  var track = document.createElement('div');
  track.className = 'hist-scrollbar' + (opts.className ? ' ' + opts.className : '');
  var thumb = document.createElement('div');
  thumb.className = 'hist-scrollbar-thumb';
  track.appendChild(thumb);
  host.appendChild(track);

  if (typeof opts.top === 'number') track.style.top = opts.top + 'px';

  var drag = null;

  function metrics() {
    var sh = Math.max(1, box.scrollHeight);
    var ch = box.clientHeight;
    var trackH = Math.max(1, track.clientHeight);
    var th = Math.min(trackH, Math.max(24, Math.round(trackH * (ch / sh))));
    return { sh: sh, ch: ch, trackH: trackH, th: th };
  }

  function sync() {
    var m = metrics();
    if (m.sh <= m.ch + 1) { track.style.display = 'none'; return; }
    track.style.display = '';
    thumb.style.height = m.th + 'px';
    var maxTop = m.trackH - m.th;
    var top = (box.scrollTop / Math.max(1, m.sh - m.ch)) * maxTop;
    thumb.style.transform = 'translateY(' + Math.max(0, Math.min(maxTop, top)) + 'px)';
    if (opts.frozenClass) thumb.classList.toggle('is-frozen', !!_frozen);
  }

  box._syncScrollbar = sync;
  box._scrollbarTrack = track;

  if (typeof opts.top !== 'number') {
    // 轨道的上沿不能进到浮动配置栏底下：配置栏是 absolute 叠在内容上的，
    // 轨道默认 top:6px 会从配置栏底边钻出来。
    box._syncScrollbarInset = function() {
      var page = box.closest('.tab-page');
      var cfg = page ? page.querySelector('.config-bar') : null;
      var h = cfg ? Math.round(cfg.getBoundingClientRect().height) : 44;
      track.style.top = (h + 6) + 'px';
    };
    box._syncScrollbarInset();
  }

  thumb.addEventListener('pointerdown', function(e) {
    e.preventDefault();
    e.stopPropagation();
    try { thumb.setPointerCapture(e.pointerId); } catch (err) {}
    drag = { y: e.clientY, top: box.scrollTop };
  });
  thumb.addEventListener('pointermove', function(e) {
    if (!drag) return;
    var m = metrics();
    var ratio = (m.sh - m.ch) / Math.max(1, m.trackH - m.th);
    box.scrollTop = drag.top + (e.clientY - drag.y) * ratio;
  });
  function endDrag() {
    drag = null;
    if (typeof opts.onRelease === 'function') opts.onRelease();
  }
  thumb.addEventListener('pointerup', endDrag);
  thumb.addEventListener('pointercancel', endDrag);

  // 点轨道空白：翻一屏
  track.addEventListener('pointerdown', function(e) {
    if (e.target === thumb) return;
    var before = thumb.getBoundingClientRect().top + thumb.offsetHeight / 2;
    box.scrollTop += (e.clientY < before) ? -box.clientHeight * 0.9 : box.clientHeight * 0.9;
  });

  box.addEventListener('scroll', sync);
  // 输入改变高度时不一定触发 scroll（比如光标在顶部），补一次
  box.addEventListener('input', sync);
  if (window.ResizeObserver) {
    try { new ResizeObserver(sync).observe(box); } catch (err) {}
  }
  sync();
}

function initDisplayScrollbar(display) {
  if (!display) return;
  _initSelectionBackdropToggle();
  _initOverlayScrollbar(display, {
    frozenClass: true,
    onRelease: function() {
      // 拖到底（或拖回底部附近）时立刻恢复跟随，不等下一次数据到达。
      var atBottom = display.scrollTop + display.clientHeight >= display.scrollHeight - 4;
      if (atBottom && !state.scrollLocked && _frozen) {
        _frozen = false;
        renderHistoryLines(_activeCache());
      }
    },
  });
}

// 发送框滚动条：与显示区同一套外观，上沿从工具栏底边（40px）开始。
function initSendScrollbar(box) {
  _initOverlayScrollbar(box, { className: 'is-send', top: 40 });
}

// 自绘滚动条同步：每次 flush 都调会读 scrollHeight（强制布局），大流量时
// 每帧一次很不划算 —— 滑块位置 12 Hz 的更新在肉眼上已经连续。
var _sbSyncLast = 0;
function _syncCustomScrollbar(display, force) {
  var now = performance.now();
  if (!force && now - _sbSyncLast < 80) return;
  _sbSyncLast = now;
  if (display && typeof display._syncScrollbar === 'function') display._syncScrollbar();
}

// 选中文本时不显示浮动条垫底（模糊/渐变）：那层会压住选择高亮，选到靠下的
// 数据行时看不到自己选中了哪一段。selectionchange 只在结束时收尾，很轻。
function _initSelectionBackdropToggle() {
  if (window.__selBackdropBound) return;
  window.__selBackdropBound = true;
  document.addEventListener('selectionchange', function() {
    var page = (typeof getActivePage === 'function') ? getActivePage() : null;
    if (!page || !page.displayArea) return;
    var sel = window.getSelection();
    var inDisplay = false;
    if (sel && !sel.isCollapsed && sel.rangeCount > 0) {
      try {
        var r = sel.getRangeAt(0);
        inDisplay = page.displayArea.contains(r.startContainer) ||
                    page.displayArea.contains(r.endContainer);
      } catch (e) { inDisplay = false; }
    }
    page.displayArea.classList.toggle('is-selecting', !!inDisplay);
  });
}

// ---- 窗口重绘 ----

// 注意：这里**没有节流**。旧实现在开头 `if (now - _lastRender < 50) return;`，
// 把 50ms 内的重绘请求直接丢掉 —— 实测「渲染后立刻点清空 → 显示区空白」、
// 「连续切换显示模式 → 画面与 state 不一致」。合并语义已由批量队列承担，
// 这里必须每次同步生效（expandHistory 等调用方依赖它同步更新 DOM）。
function renderHistoryLines(entries, keepScroll) {
  if (!entries) return;
  var display = pageEl('displayContent');
  if (!display) return;

  _updateUnreadChrome();
  display.innerHTML = '';
  if (entries.length === 0) {
    display.innerHTML = '<div class="placeholder">' + t('placeholder.no_process', '无串口进程') + '</div>';
    _renderStart = -1;
    _renderEnd = -1;
    return;
  }

  // Fix system message timestamps for chronological order
  var lastTs = 0;
  for (var j = 0; j < entries.length; j++) {
    if (entries[j].direction !== 'system' && entries[j].timestamp) {
      lastTs = entries[j].timestamp;
    } else if (entries[j].direction === 'system' && !entries[j].timestamp) {
      entries[j].timestamp = lastTs;
    }
  }

  var total = entries.length;
  var win = historyWindowSize();
  var start, end;
  if (keepScroll && _renderStart >= 0 && _renderStart < total) {
    start = _renderStart;
    end = Math.min(total, start + win);
  } else {
    // If cleared, start from after the clear marker so old data
    // is hidden until the user explicitly scrolls up.
    var clearPos = (typeof entries._clearedAt === 'number') ? entries._clearedAt + 1 : 0;
    start = Math.max(clearPos, total - win);
    // Never start past the end — at minimum show the last entry
    // (the clear marker itself) so the display isn't blank.
    if (start > total - 1) start = Math.max(0, total - 1);
    end = total;
  }
  _renderStart = start;
  _renderEnd = end;

  var fragment = document.createDocumentFragment();
  if (start > 0 || _needsLoadOlderTip(entries)) fragment.appendChild(_moreAboveTip(entries));

  for (var i = start; i < end; i++) {
    var entry = entries[i];
    if (entry.direction === 'system') {
      var line = document.createElement('div');
      line.className = 'sys-msg';
      line.setAttribute('data-sys-raw', entry.hex || '');
      line.textContent = '── ' + formatSysMsg(entry.hex || '') + ' ──';
      fragment.appendChild(line);
    } else {
      makeDataRow(fragment, entry.direction, entry.timestamp, entry.hex, entry.segments || null, null, entry.origin);
    }
  }
  display.appendChild(fragment);

  // keepScroll 表示这是「在原有窗口上重绘」（切显示模式等）：此时不要把用户
  // 往回翻出来的冻结状态清掉，否则一换模式就被拽回最新一条、未读提示也消失。
  _frozen = !!state.scrollLocked || (!!keepScroll && _frozen);
  _atBottom = !_frozen;
  if (!keepScroll) {
    display.scrollTop = 1e9;
    _atBottom = true;
    _lastScrollTop = display.scrollTop;
  }
  display.onscroll = _onDisplayScroll;
  _updateUnreadChrome(display);
}

function _moreAboveTip(entries) {
  var more = document.createElement('div');
  more.className = 'sys-msg';
  more.setAttribute('data-hist-tip', 'more');
  more.style.cursor = 'pointer';
  more.textContent = '── ' + t('history.more_above', '↑ 向上滚动加载更多') + ' ──';
  more.onclick = function () { expandHistory(entries); };
  return more;
}

// 关掉自动回补时，窗口已经停在缓存头了也仍然要有东西可点。
function _needsLoadOlderTip(entries) {
  if (!entries || state.autoRefillHistory !== false) return false;
  if (!entries._ringExhausted) return true;
  return !!entries._historyFile;
}

function _onDisplayScroll() {
  var display = pageEl('displayContent');
  if (!display) return;
  // 滚动条只反映真实内容（未读改用右下角胶囊提示），所以到底的判断就是原生语义。
  var atBottom = display.scrollTop + display.clientHeight >= display.scrollHeight - 4;
  // 只有"scrollTop 变小"才算用户往回翻。内容长高（含 content-visibility 把估值
  // 换成真实高度）也会触发 scroll 事件，此时 scrollTop 不变或变大，不能冻结——
  // 否则跟滚会在某一批数据后停住，未读胶囊一路涨。
  var movedUp = display.scrollTop < _lastScrollTop;
  _lastScrollTop = display.scrollTop;
  var wasFrozen = _frozen;
  _atBottom = atBottom;
  var shouldFreeze;
  if (state.scrollLocked) {
    shouldFreeze = true;                       // 锁定了就不跟
  } else if (wasFrozen) {
    shouldFreeze = !atBottom;                  // 已冻结：真的回到底部才解冻
  } else {
    shouldFreeze = movedUp && !atBottom;       // 未冻结：只有用户往上滚才冻结
  }

  if (wasFrozen && !shouldFreeze) {
    // 回到底部：解除冻结，把未读补上
    _frozen = false;
    _updateUnreadChrome();
    renderHistoryLines(_activeCache());
    return;
  }
  if (!wasFrozen && shouldFreeze) {
    _frozen = true;
    _updateUnreadChrome(display);
  }
  // 翻到顶：先看缓存里还有没有（expandHistory 自己处理），没有就去守护进程取。
  // 注意条件里**不能**再带 `_renderStart > 0`：缓存头正好是窗口起点时
  // _renderStart==0，那条判断会让「加载更早」整条路不可达。
  if (display.scrollTop < 80) {
    if (state.autoRefillHistory === false) {
      _ensureLoadOlderTip(display, _activeCache());
    } else {
      expandHistory(_activeCache());
    }
  }
}

// 关掉「自动回补更早历史」时，翻到顶只摆一个可点的提示，由用户决定要不要取。
function _ensureLoadOlderTip(display, entries) {
  if (!display || !entries) return;
  if (entries._ringExhausted && !entries._historyFile) {
    _updateDiskTip(t('history.no_more', '没有更多历史记录'));
    return;
  }
  if (display.querySelector('[data-hist-tip="more"]')) return;
  display.insertBefore(_moreAboveTip(entries), display.firstChild);
}

// 滚动锁定开关变化时调用（app.js 的 toggleScrollLock）
function onScrollLockChanged() {
  var display = pageEl('displayContent');
  if (!display) return;
  if (state.scrollLocked) {
    _frozen = true;
    _atBottom = false;
    _updateUnreadChrome(display);
    return;
  }
  _frozen = false;
  _updateUnreadChrome();
  renderHistoryLines(_activeCache());
}

// ---- 向上扩展窗口 ----

function expandHistory(entries) {
  var display = pageEl('displayContent');
  if (!display || !entries) return;
  var win = historyWindowSize();
  var prevHeight = display.scrollHeight;

  // 已经到缓存头：先去守护进程/磁盘取更早的。
  // 旧实现在这里先算 newStart 再 `if (newStart >= _renderStart) return;`，
  // 于是 _renderStart==0 时第一步就返回 —— 取更早的两条路都在 return 之后，
  // 整条「加载更早」不可达（实测 0 次 RPC、无提示）。
  if (_renderStart <= 0) { _fetchOlder(entries, display, prevHeight); return; }

  var step = Math.max(50, Math.floor(win / 4));
  var newStart = Math.max(0, _renderStart - step);
  if (newStart >= _renderStart) return;

  var frag = document.createDocumentFragment();
  for (var i = newStart; i < _renderStart; i++) {
    var entry = entries[i];
    if (entry.direction === 'system') {
      var line = _makeSysRow(formatSysMsg(entry.hex || ''));
      line.setAttribute('data-sys-raw', entry.hex || '');
      frag.appendChild(line);
    } else {
      makeDataRow(frag, entry.direction, entry.timestamp, entry.hex, entry.segments || null, null, entry.origin);
    }
  }
  // 去掉旧的「更多」提示后插到最前
  var oldMore = display.querySelector('[data-hist-tip="more"]');
  if (oldMore && oldMore.parentNode) oldMore.parentNode.removeChild(oldMore);
  display.insertBefore(frag, display.firstChild);
  if (newStart > 0 || _needsLoadOlderTip(entries)) display.insertBefore(_moreAboveTip(entries), display.firstChild);
  _renderStart = newStart;

  // 窗口有上界：往上加就要从下方裁（裁掉的行数由占位块记着，
  // 所以往下滚时它们仍在缓存里，且滚动条高度不撒谎）
  _trimToWindow(display, win, true);
  display.scrollTop = Math.max(0, display.scrollHeight - prevHeight);
  _updateUnreadChrome(display);
}

function _fetchOlder(entries, display, prevHeight) {
  if (_olderLoading || !entries) return;
  // 环已经翻到头了：不再重复问守护进程（每次滚到顶都问一次会变成 RPC 风暴），
  // 直接把磁盘提示摆出来。
  if (!entries._id || !state.daemonOnline || entries._ringExhausted) {
    if (entries._historyFile) _showDiskTip(display, prevHeight);
    return;
  }
  _olderLoading = true;
  _loadOlderFromDaemon(entries._id, entries).then(function (added) {
    _olderLoading = false;
    if (added > 0) {
      // 前面插了 added 条，窗口索引整体后移，屏幕上看到的内容不动
      _renderStart += added;
      _renderEnd += added;
      expandHistory(entries);
    } else if (entries._historyFile) {
      _showDiskTip(display, prevHeight);
    } else {
      _updateDiskTip(t('history.no_more', '没有更多历史记录'));
    }
  }).catch(function () { _olderLoading = false; });
}

function _showDiskTip(display, prevHeight) {
  if (!display) return;
  // 已经在提示了就别反复插拔：滚到顶每个 scroll 事件都会走到这里
  if (display.querySelector('[data-hist-tip="disk"]')) return;
  var old = display.querySelector('.disk-tip');
  if (old) old.remove();
  var tip = document.createElement('div');
  tip.className = 'sys-msg disk-tip';
  tip.setAttribute('data-hist-tip', 'disk');
  tip.style.cursor = 'pointer';
  tip.textContent = '── ' + t('history.load_disk', '↓ 加载磁盘历史记录') + ' ──';
  tip.onclick = function () {
    if (_diskTipClicked(tip)) return;
    var e = _activeCache();
    _searchDiskHistory(e && e._id, e);
  };
  display.insertBefore(tip, display.firstChild);
  if (typeof prevHeight === 'number') display.scrollTop = Math.max(0, display.scrollHeight - prevHeight);
}

// 点一次就置为加载中，避免连点触发多次搜索
function _diskTipClicked(tip) {
  var entries = _activeCache();
  if (!entries || entries._diskLoading) return true;
  tip.style.cursor = 'default';
  tip.style.pointerEvents = 'none';
  tip.textContent = '── ' + t('history.loading', '加载中...') + ' ──';
  return false;
}

// 从守护进程的环里回补一页更早的记录，返回插到缓存前面的条数。
//
// 只取一页：整环 5MB ≈ 19 万条，一次全要回来就是「把 5MB JSON 过一遍桥、再逐条
// decode」，而界面一次只看得见一屏。游标由 _cursorFrom 给出（毫秒时间戳 + 同
// 毫秒条数），两边对得上才不会重复或漏条。
async function _loadOlderFromDaemon(sessionId, entries) {
  if (!sessionId || !state.daemonOnline || !entries || entries.length === 0) return 0;
  var cur = _cursorFrom(entries);
  if (!cur) return 0;

  // 游标没动过就别再问了：说明上一次没拿到任何更早的东西，
  // 否则滚到顶会变成一秒几十次同样的 RPC。
  if (entries._lastCursorTs === cur.tsMs && entries._lastCursorSkip === cur.skip) {
    entries._ringExhausted = true;
    return 0;
  }

  var data;
  try {
    data = await window.go.main.App.GetSessionHistory(sessionId, HISTORY_PAGE_SIZE, cur.tsMs, cur.skip);
  } catch (e) {
    return 0;
  }
  if (!data) return 0;

  entries._lastCursorTs = cur.tsMs;
  entries._lastCursorSkip = cur.skip;
  if (typeof data.oldestTsMs === 'number') entries._ringOldestTsMs = data.oldestTsMs;
  if (data.historyFile) entries._historyFile = data.historyFile;
  entries._ringHasMore = !!data.hasMore;
  if (!data.hasMore) entries._ringExhausted = true;

  var history = (data.history || []).filter(function (e) { return e.direction !== 'system'; });
  if (history.length === 0) return 0;

  Array.prototype.unshift.apply(entries, history);
  entries._hexChars = undefined;
  if (typeof entries._clearedAt === 'number') entries._clearedAt += history.length;
  return history.length;
}

// Search the disk history file for entries older than the ring buffer.
// Uses entries._diskOffset to paginate through the file on repeated calls.
async function _searchDiskHistory(sessionId, entries) {
  if (!entries || !entries._historyFile || !state.daemonOnline) return;
  if (entries._diskLoading) return; // prevent concurrent clicks
  entries._diskLoading = true;

  try {
    var beforeMs = entries._ringOldestTsMs || 0;
    var offset = entries._diskOffset || 0;
    var result = await window.go.main.App.SearchHistory(entries._historyFile, '', 200, offset);
    if (!result || !result.history || result.history.length === 0) {
      _updateDiskTip(t('history.no_more', '没有更多历史记录'));
      return;
    }

    var diskEntries = result.history;

    // Build dedup set from existing cache
    var seen = new Set();
    for (var k = 0; k < entries.length; k++) {
      seen.add(_fp(entries[k]));
    }

    // Only keep entries older than the ring buffer's oldest AND not already in cache
    var prepend = [];
    for (var i = 0; i < diskEntries.length; i++) {
      var de = diskEntries[i];
      if (de.direction === 'system') continue;
      if (beforeMs > 0 && de.tsMs >= beforeMs) continue;
      if (seen.has(_fp(de))) continue;
      prepend.push(de);
      seen.add(_fp(de));
    }

    if (prepend.length > 0) {
      Array.prototype.unshift.apply(entries, prepend);
      entries._hexChars = undefined;
      if (entries._clearedAt !== undefined) {
        entries._clearedAt += prepend.length;
      }
      entries._diskOffset = offset + diskEntries.length;
      // Keep the adjusted render position so the newly loaded entries
      // are visible at the top instead of jumping back to the bottom.
      _renderStart = Math.max(0, prepend.length - 5);
      _renderEnd = _renderStart;
      renderHistoryLines(entries, true);
    } else if (result.hasMore) {
      // Current batch was all duplicates, try next page
      entries._diskOffset = offset + diskEntries.length;
      entries._diskLoading = false;
      _searchDiskHistory(sessionId, entries);
      return;
    } else {
      _updateDiskTip(t('history.no_more', '没有更多历史记录'));
    }
  } catch (e) {} finally {
    entries._diskLoading = false;
  }
}

// Replace the disk-paging tip at the top of the display.
function _updateDiskTip(text) {
  var display = pageEl('displayContent');
  if (!display) return;
  var old = display.querySelector('.disk-tip');
  if (old) old.remove();
  if (!text) return;
  var tip = document.createElement('div');
  tip.className = 'sys-msg disk-tip';
  tip.style.cursor = 'default';
  tip.style.opacity = '0.6';
  tip.textContent = '── ' + text + ' ──';
  display.insertBefore(tip, display.firstChild);
}

// Merge new ring buffer entries into the cache (used after connect / disconnect).
async function mergeRingBuffer(sessionId) {
  if (!sessionId || !state.daemonOnline) return;
  try {
    const data = await window.go.main.App.GetSessionHistory(sessionId, HISTORY_INITIAL, 0, 0);
    const history = (data && data.history) ? data.history : [];
    if (!state.historyCache[sessionId]) { state.historyCache[sessionId] = []; state.historyCache[sessionId]._id = sessionId; }
    const arr = state.historyCache[sessionId];
    if (data && data.historyFile) arr._historyFile = data.historyFile;
    if (data && typeof data.oldestTsMs === 'number') arr._ringOldestTsMs = data.oldestTsMs;
    if (data && typeof data.hasMore === 'boolean') arr._ringHasMore = data.hasMore;
    if (history.length > 0) {
      const seen = new Set();
      const scanStart = Math.max(0, arr.length - 200);
      for (let i = scanStart; i < arr.length; i++) {
        const e = arr[i];
        seen.add(_fp(e));
      }
      for (const entry of history) {
        const fp = _fp(entry);
        const sysFp = entry.direction === 'system' ? 'system|' + entry.hex : null;
        if (!seen.has(fp) && (!sysFp || !seen.has(sysFp))) {
          arr.push(entry);
          seen.add(fp);
          if (sysFp) seen.add(sysFp);
        }
      }
      arr._hexChars = undefined;
    }
    _trimCache(arr);
  } catch {}
}

var _historyLoading = null; // sessionId currently loading, prevents concurrent IPC

// Load history from daemon for the active tab and display it
async function loadTabHistory() {
  const tab = getActiveTab();
  const display = pageEl('displayContent');
  // 没有活动页时 pageEl 返回 null（守护进程刚停、标签页已被摘掉等）。
  // 此前这里直接 display.innerHTML 会抛 TypeError —— 控制台里那条
  // 「Cannot set properties of null (setting 'innerHTML')」就是它。
  if (!display) return;

  if (!tab || !tab.sessionId) {
    display.innerHTML = '<div class="placeholder">' + t('placeholder.no_process', '无串口进程') + '</div>';
    return;
  }

  const cacheKey = tab.sessionId;
  // Use non-empty cache when available ([] is truthy — guard with length check)
  if (state.historyCache[cacheKey] && state.historyCache[cacheKey].length > 0) {
    renderHistoryLines(state.historyCache[cacheKey]);
    return;
  }

  // Prevent concurrent loads for the same session (e.g. back-to-back
  // syncDaemonSessions calls both trying to load history for a new tab).
  if (_historyLoading === cacheKey) return;
  _historyLoading = cacheKey;

  try {
    // 只取最新的一段：整环 19 万条全要回来是这条路径上唯一的实际开销，
    // 更早的部分由「往上翻」按页回补（见 _loadOlderFromDaemon）。
    const data = await window.go.main.App.GetSessionHistory(tab.sessionId, HISTORY_INITIAL, 0, 0);
    const history = (data && data.history) ? data.history : [];
    history._id = tab.sessionId;
    // Save extra fields for paging / disk paging
    if (data && typeof data.oldestTsMs === 'number') history._ringOldestTsMs = data.oldestTsMs;
    if (data && typeof data.hasMore === 'boolean') { history._ringHasMore = data.hasMore; history._ringExhausted = !data.hasMore; }
    if (data && data.historyFile) history._historyFile = data.historyFile;
    state.historyCache[cacheKey] = history;
    renderHistoryLines(history);
  } catch (e) {
    display.innerHTML = '<div class="placeholder">暂无记录</div>';
  } finally {
    if (_historyLoading === cacheKey) _historyLoading = null;
  }
}

// clearDisplay is a local GUI action — it clears the view without touching
// the daemon ring buffer or disk file.  Old data remains in cache and can
// be recalled by scrolling up (expandHistory → _loadOlderFromDaemon).
function clearDisplay() {
  const tab = getActiveTab();
  const display = pageEl('displayContent');
  if (!display) return;
  if (!tab || !tab.sessionId) {
    display.innerHTML = '';
    return;
  }

  const cacheKey = tab.sessionId;
  var cache = state.historyCache[cacheKey];
  if (!cache) { cache = []; state.historyCache[cacheKey] = cache; }

  // Insert a clear marker at current end of cache (rendered inline later).
  var ts = new Date().toISOString().slice(11, 23).replace(/\./g, ':').slice(0, 12);
  cache._clearedAt = cache.length;
  cache.push({ direction: 'system', hex: 'sys.history_cleared', timestamp: ts });
  cache._hexChars = undefined;

  // Reset stats display (local counters only).
  state.statsBase[cacheKey] = { tx: 0, rx: 0 };
  refreshStatsDOM();

  // Re-render — starts from after the clear marker.
  _histReset();
  _frozen = !!state.scrollLocked;
  renderHistoryLines(cache);
}
