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

// 一页回补多少条；首次加载取 MAX（回看深度至少等于稳态缓存）
var HISTORY_PAGE_SIZE = 5000;
var HISTORY_INITIAL = HISTORY_CACHE_MAX;

var _renderStart = -1; // DOM 窗口起点（cache 索引）
var _renderEnd = -1;   // DOM 窗口终点（cache 索引，不含）
var _atBottom = true;  // 用户是否贴着底部
var _frozen = false;   // 冻结：锁定滚动，或用户往回翻离开了底部
var _spacer = null;    // 未读区占位块
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
    spacer: !!(display && display.querySelector('.hist-spacer')),
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
  _spacer = null;
  _renderStart = -1;
  _renderEnd = -1;
  _atBottom = true;
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
  return e.tsMs + '|' + e.direction + '|' + e.hex;
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
function _trimCache(arr) {
  if (!arr) return 0;
  var active = (_activeCache() === arr);
  var cap = (active && _frozen) ? HISTORY_CACHE_HARD : HISTORY_CACHE_MAX;
  var over = arr.length - cap;
  if (over < HISTORY_CACHE_SLACK) return 0;

  var drop = over;
  arr.splice(0, drop);
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

// ---- 单行构造（不再有滚动副作用，贴底统一由批次处理） ----

function makeDataRow(container, dir, timestamp, hex, segments) {
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
  tsLine.innerHTML = '<span class="ts-text">[' + (timestamp || '') + '] ' + label + arrow + ' </span><span class="' + statClass + '">~' + byteLen + 'B</span>';
  row.appendChild(tsLine);

  const dataLine = document.createElement('div');
  dataLine.className = 'data-line';

  let dm = state.displayMode;
  if (activeMode() === 'forward') {
    if (dir === '1') dm = state.p1DisplayMode;
    else if (dir === '2') dm = state.p2DisplayMode;
  } else if (dir === 'tx') {
    dm = state.txDisplayMode;
  }
  if (dm === 'hex') {
    const hexSpan = document.createElement('span');
    var hexDirClass = dir === '1' ? 'hex-1' : dir === '2' ? 'hex-2' : (dir === 'rx' ? 'hex-rx' : 'hex-tx');
    hexSpan.className = 'hex-data ' + hexDirClass;
    hexSpan.textContent = formatHex(hexToBytes(hex));
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
  const entry = { direction: dir, hex: msg.hex, timestamp: msg.timestamp, tsMs: msg.tsMs, segments: msg.segments };
  if (!state.historyCache[ownerTab.sessionId]) { state.historyCache[ownerTab.sessionId] = []; state.historyCache[ownerTab.sessionId]._id = ownerTab.sessionId; }
  var ownerCache = state.historyCache[ownerTab.sessionId];
  ownerCache.push(entry);
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
  if (q.length === 0) { _syncSpacer(display); return; }

  var t0 = performance.now();

  if (display.querySelector('.placeholder')) display.innerHTML = '';

  // 冻结：视图不动，未读交给占位块撑高度（滚动条继续变化，但 DOM 不增长）
  if (_frozen) { _syncSpacer(display); return; }

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
      makeDataRow(frag, e.direction, e.timestamp, e.hex, e.segments || null);
    }
  }
  display.appendChild(frag);
  var entries = _activeCache();
  _renderEnd = entries ? entries.length : _renderEnd;
  _trimToWindow(display, win);
  _stickToBottom(display);
  _syncSpacer(display);

  _lastFlushMs = performance.now() - t0;
  _lastFlushCount = q.length;
}

// 贴底：写一个极大值，浏览器会夹到底部 —— 不去读 scrollHeight（读是 O(节点数)）
function _stickToBottom(display) {
  if (!display) return;
  display.scrollTop = 1e9;
  _atBottom = true;
}

// 只裁剪数据行，不碰提示行与占位块；始终执行（无论是否锁定/冻结）
function _trimToWindow(display, win) {
  var rows = display.querySelectorAll('.data-row');
  var excess = rows.length - win;
  for (var i = 0; i < excess; i++) {
    var el = rows[i];
    if (!el || !el.parentNode) continue;
    el.parentNode.removeChild(el);
    if (_renderStart >= 0) _renderStart++;
  }
}

// ---- 未读区占位块 ----

function _avgRowHeight(display) {
  var rows = display.querySelectorAll('.data-row');
  if (rows.length === 0) {
    var fs = state.displayFontSize || 15;
    return Math.max(8, fs * 1.6) * 2;
  }
  var n = Math.min(5, rows.length), sum = 0, cnt = 0;
  for (var i = rows.length - n; i < rows.length; i++) {
    var h = rows[i].offsetHeight;
    if (h > 0) { sum += h; cnt++; }
  }
  if (cnt === 0) return Math.max(8, (state.displayFontSize || 15) * 1.6) * 2;
  return sum / cnt;
}

function _syncSpacer(display) {
  var entries = _activeCache();
  var hidden = (entries && _renderEnd >= 0) ? Math.max(0, entries.length - _renderEnd) : 0;
  if (!_frozen || hidden <= 0) { _removeSpacer(); return; }

  if (!_spacer || !_spacer.parentNode) {
    _spacer = document.createElement('div');
    _spacer.className = 'sys-msg hist-spacer';
    display.appendChild(_spacer);
  }
  _spacer.setAttribute('data-sys-raw', '');
  _spacer.textContent = '── ' + t('history.unseen', '↓ %s 条未显示').replace('%s', hidden) + ' ──';
  // 高度按实测平均行高估算：视图不动，但滚动条的变化幅度与方向都对
  var h = Math.round(hidden * _avgRowHeight(display));
  _spacer.style.height = Math.min(h, 8000000) + 'px';
}

function _removeSpacer() {
  if (_spacer && _spacer.parentNode) _spacer.parentNode.removeChild(_spacer);
  _spacer = null;
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

  _removeSpacer();
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
      makeDataRow(fragment, entry.direction, entry.timestamp, entry.hex, entry.segments || null);
    }
  }
  display.appendChild(fragment);

  _frozen = !!state.scrollLocked;
  _atBottom = !_frozen;
  if (!keepScroll) {
    display.scrollTop = 1e9;
    _atBottom = true;
  }
  display.onscroll = _onDisplayScroll;
  _syncSpacer(display);
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
  var atBottom = display.scrollTop + display.clientHeight >= display.scrollHeight - 4;
  var wasFrozen = _frozen;
  _atBottom = atBottom;
  var shouldFreeze = !!state.scrollLocked || !atBottom;

  if (wasFrozen && !shouldFreeze) {
    // 回到底部：解除冻结，把未读补上
    _frozen = false;
    _removeSpacer();
    renderHistoryLines(_activeCache());
    return;
  }
  if (!wasFrozen && shouldFreeze) {
    _frozen = true;
    _syncSpacer(display);
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
    _syncSpacer(display);
    return;
  }
  _frozen = false;
  _removeSpacer();
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
      makeDataRow(frag, entry.direction, entry.timestamp, entry.hex, entry.segments || null);
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
  _trimToWindow(display, win);
  display.scrollTop = Math.max(0, display.scrollHeight - prevHeight);
  _syncSpacer(display);
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

  // Reset stats display (local counters only).
  state.statsBase[cacheKey] = { tx: 0, rx: 0 };
  refreshStatsDOM();

  // Re-render — starts from after the clear marker.
  _histReset();
  _frozen = !!state.scrollLocked;
  renderHistoryLines(cache);
}
