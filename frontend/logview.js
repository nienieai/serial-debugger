// ---- 虚窗日志视图（logview）----
//
// 数据在 Go（服务 main.LogView，文档类型 stlogview.Store），前端只做三件事：
//   1. 按像素取窗口，把可见的那几十行定位出来（translate3d，不量 DOM 折行）；
//   2. 拖选（帧号 + 视觉行 + 格）交给 Go 换算成字节并写剪贴板；
//   3. 画滚动条的两道：来源/发送方（真实 origin）与落盘完整性。
//
// 为什么不由前端自己排版：一帧折几行取决于它占几格（宽字 2 格、Tab 跳到 8 的
// 倍数、非法字节 /XX 三格、控制字符开关），这套规则在 Go 的 width.go 里是唯一
// 口径；前端要是自己量 DOM，改一处渲染就和滚动位置对不上了。
//
// 行的 DOM 复用主工程自己的类（.ts-line / .data-line / .hex-data / .seg-*），
// 所以配色主题、字体、可见控制字符这些都跟着会话页走，不另起一套样式。

var LV_PAD_X = 18;      // 行内左边距（4 + 5 + 5 + 4，两条竖条占中间 10px）
var LV_PAD_R = 8;       // 行内右边距（与 .lv-row 的 padding-right 一致）
var LV_TS_H = 21;       // 时间戳行高（首帧量出来之前的占位）
var LV_DATA_H = 22.5;   // 数据行行高
var LV_LANE_W = 5;      // 一道泳道 5px
var LV_LANES = 3;       // 读取 / 校验 / 落盘
var LV_RAIL_W = 1 + LV_LANES * LV_LANE_W; // 左边 1px 分界线 + 三条泳道
var LV_THUMB_MIN = 15;

// 来源/发送方的配色（按来源序号取，1 起）。第七个之后回环。
var LV_ORIGIN_COLORS = [
  '#f59e0b', '#a855f7', '#22c55e', '#38bdf8', '#f472b6', '#eab308', '#94a3b8'
];

// 未捕获的异常写进主程序日志：GUI 没有控制台，出问题只能靠这一行查。
function lvLog(msg) {
  try {
    if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
      window.go.main.App.Log('[logview] ' + msg);
    }
  } catch (_) {}
}
window.addEventListener('error', function (e) {
  lvLog('JSERR ' + (e.message || '') + ' @' + String(e.filename || '').split('/').pop() + ':' + e.lineno);
});
window.addEventListener('unhandledrejection', function (e) {
  var r = e.reason || {};
  lvLog('REJ ' + (r.message || r));
});

function lvCall(method) {
  var api = window.__wailsApi;
  if (!api || !api.Call) return Promise.reject(new Error('bindings 未就绪'));
  var args = Array.prototype.slice.call(arguments, 1);
  args.unshift('main.LogView.' + method);
  return api.Call.ByName.apply(api.Call, args);
}

function lvHexToText(hex) {
  if (!hex) return '';
  var s = String(hex).replace(/\s+/g, ''), out = '';
  for (var i = 0; i + 1 < s.length; i += 2) {
    var c = parseInt(s.substr(i, 2), 16);
    if (c === 10) out += '\n';
    else if (c === 13) out += '';
    else if (c >= 32 && c < 127) out += String.fromCharCode(c);
    else out += '\u00B7';
  }
  return out;
}

function lvClamp(v, lo, hi) { return v < lo ? lo : (v > hi ? hi : v); }

// tsClassOf / hexClassOf 与 history.js 的 makeDataRow 同一套类名，颜色才一致。
function lvTsClass(dir) {
  if (dir === '1') return 'ts-1';
  if (dir === '2') return 'ts-2';
  if (dir === 'rx') return 'ts-rx';
  if (dir === 'system' || dir === 'sys') return 'ts-sys';
  return 'ts-tx';
}
function lvHexClass(dir) {
  if (dir === '1') return 'hex-1';
  if (dir === '2') return 'hex-2';
  if (dir === 'rx') return 'hex-rx';
  if (dir === 'system' || dir === 'sys') return 'hex-sys';
  return 'hex-tx';
}

// styleOf 把标签页的显示设置翻成 Go 的 Style（只有会改变宽度的字段）。
function lvStyleOf(d) {
  d = d || {};
  var enc = 0;
  if (d.encoding === 'ascii') enc = 1;
  else if (d.encoding === 'gb2312' || d.encoding === 'gbk') enc = 2;
  var esc = 0;
  if (d.hexEscapeMode === 'hide') esc = 1;
  else if (d.hexEscapeMode === 'raw') esc = 2;
  var sep = 0;
  if (d.hexSep === 'comma') sep = 1;
  else if (d.hexSep === 'none') sep = 2;
  return {
    enc: enc,
    crVisible: d.crVisible !== false,
    escMode: esc,
    hexPrefix: !!d.hexPrefix,
    hexSep: sep,
    hexUpper: d.hexCase !== 'lower'
  };
}

// createLogView 在 host（页面里的内容区）上挂一个日志视图。
//
// opts:
//   disp()       → 当前标签页的显示设置（每次取，允许变）
//   tsText(ms)   → 时间戳显示串（带不带年份由调用方决定）
//   onMeta(meta) → 打开完成 / 元信息就绪
//   onStatus(st) → 窗口刷新（给顶栏更新「帧 x/y」）
//   onFlash(msg) → 一行提示（复制结果之类）
function createLogView(host, opts) {
  opts = opts || {};
  var st = {
    id: '', phase: 'idle', err: '',
    total: 0, contentH: 0, maxOffset: 0,
    offset: 0, vh: 0, base: 0,
    rows: [], mode: (opts.mode === 'text' ? 'text' : 'hex'), perLine: 1, hexPrefix: false,
    charW: 8, tsH: LV_TS_H, dataH: LV_DATA_H,
    showYear: true,
    sel: null, dragging: false, focus: null,
    thumbH: LV_THUMB_MIN, trackH: 1,
    fetching: false, needFetch: false,
    ruler: null, rulerKey: '',
    dead: false,
    // 实时模式（串口监视器 / 转发监视器）
    live: false, atBottom: true, unread: 0, lastSeen: 0, pollTimer: null,
    reserveTop: opts.reserveTop || 0,
    reserveBottom: opts.reserveBottom || 0
  };

  host.classList.add('lv-host');
  host.tabIndex = 0;
  host.innerHTML = '';

  var view = document.createElement('div');
  view.className = 'lv-view';
  var layer = document.createElement('div');
  layer.className = 'lv-layer';
  view.appendChild(layer);
  var msg = document.createElement('div');
  msg.className = 'lv-msg';
  msg.style.display = 'none';
  view.appendChild(msg);

  var rail = document.createElement('div');
  rail.className = 'lv-rail';
  rail.title = '① 读取 ② 校验 ③ 落盘';
  var track = document.createElement('div');
  track.className = 'lv-track';
  var lanes = document.createElement('div');
  lanes.className = 'lv-lanes';
  var thumb = document.createElement('div');
  thumb.className = 'lv-thumb';
  track.appendChild(lanes);
  track.appendChild(thumb);
  rail.appendChild(track);

  host.appendChild(view);
  host.appendChild(rail);

  // 「有新数据」胶囊：不贴底时显示，点一下回到底部（替代老的 .hist-unread）
  var unreadEl = document.createElement('button');
  unreadEl.className = 'lv-unread';
  unreadEl.style.display = 'none';
  unreadEl.addEventListener('click', function () { st.unread = 0; jumpBottom(); });
  host.appendChild(unreadEl);

  // 选区盒子单独一层，跟着 layer 一起平移（用 row.y - base 定位，不受 LayoutUnit 上限影响）
  var selLayer = document.createElement('div');
  selLayer.className = 'lv-sel-layer';
  layer.appendChild(selLayer);

  // ---- 量尺寸：字体宽度与两种行高 ----
  function measure() {
    var probe = document.createElement('div');
    probe.className = 'lv-probe';
    var span = document.createElement('span');
    span.textContent = new Array(101).join('M');
    probe.appendChild(span);
    host.appendChild(probe);
    var w = span.getBoundingClientRect().width / 100;
    if (!(w > 0)) w = 8;
    var tl = document.createElement('div');
    tl.className = 'ts-line';
    tl.textContent = 'M';
    var dl = document.createElement('div');
    dl.className = 'data-line';
    dl.textContent = 'M';
    probe.appendChild(tl);
    probe.appendChild(dl);
    var th = tl.getBoundingClientRect().height || LV_TS_H;
    var dh = dl.getBoundingClientRect().height || LV_DATA_H;
    host.removeChild(probe);
    st.charW = w;
    st.tsH = th;
    st.dataH = dh;
  }

  // fitChars 算一行放几个格。先按测出来的字宽除一次，再用真串量一遍校准：
  // 逐字符推进是分数宽度，除法得到的 n 往往多出半个字 —— 多一个格就换行错位。
  function fitChars(avail) {
    var n = lvClamp(Math.floor(avail / st.charW), 16, 400);
    var probe = document.createElement('span');
    probe.className = 'lv-probe';
    probe.style.whiteSpace = 'pre';
    host.appendChild(probe);
    for (var guard = 0; guard < 12 && n > 16; guard++) {
      probe.textContent = new Array(n + 1).join('M');
      if (probe.getBoundingClientRect().width <= avail) break;
      n -= 1;
    }
    host.removeChild(probe);
    return n;
  }

  function layout() {
    st.vh = Math.max(1, host.clientHeight - st.reserveTop - st.reserveBottom);
    st.trackH = Math.max(1, st.vh);
    // 滚动条避让：轨道从 reserveTop 开始、到 reserveBottom 结束（分界线仍满高）
    track.style.top = st.reserveTop + 'px';
    track.style.height = st.trackH + 'px';
    measure();
    var avail = Math.max(1, host.clientWidth - LV_PAD_X - LV_PAD_R - LV_RAIL_W);
    return fitChars(avail);
  }

  function styleNow() { return lvStyleOf(typeof opts.disp === 'function' ? opts.disp() : opts.disp); }

  // ---- 行 ----
  function tsHTML(r) {
    var dir = r.dir;
    var isFwd = (dir === '1' || dir === '2');
    var isSys = (dir === 'system' || dir === 'sys');
    var arrow = (isFwd || isSys) ? '' : (dir === 'tx' ? ' ↾' : ' ⇃');
    var label = isFwd ? 'P' + dir : (dir === 'rx' ? 'Rx' : (isSys ? 'Sys' : 'Tx'));
    var statClass = isFwd ? 'ts-fstat' : 'ts-stat';
    var origin = '';
    // 只有 Tx 有来源；老文件（没写来源）origin 为空，不渲染标记。
    if (dir === 'tx' && r.origin) {
      var lbl = (typeof _originLabel === 'function') ? _originLabel(r.origin) : r.origin;
      origin = '<span class="t-origin" title="' + escHtml(r.origin) + '">' +
        escHtml(lbl) + '</span>';
    }
    var ts = (typeof opts.tsText === 'function') ? opts.tsText(r.ts) : String(r.ts);
    return '<span class="ts-text">[' + escHtml(ts) + '] ' + label + arrow + ' </span>' +
      origin + '<span class="' + statClass + '">~' + r.size + 'B</span>';
  }

  function lineParts(r) {
    // Hex 行在 Go 侧就按 perLine 个字节折好了（用 \n 分隔），这里直接按 \n 拆。
    if (st.mode === 'hex') return r.hex ? r.hex.split('\n') : [''];
    return null;
  }

  function lineCount(r) {
    if (r.dir === 'system' || r.dir === 'sys') return 1; // 系统消息固定一行
    if (st.mode === 'hex') {
      return Math.max(1, Math.ceil(r.size / Math.max(1, st.perLine)));
    }
    var n = 0, per = Math.max(1, st.perLine);
    for (var i = 0; i < r.cells.length; i++) {
      n += Math.max(1, Math.ceil(r.cells[i] / per));
    }
    return n || 1;
  }

  // lineCells 是这一视觉行有几个显示格（Hex 模式按字符数，文本按格数）。
  function lineCells(r, vline) {
    if (r.dir === 'system' || r.dir === 'sys') return 0;
    if (st.mode === 'hex') {
      var parts = lineParts(r);
      return parts[vline] ? parts[vline].length : 0;
    }
    var per = Math.max(1, st.perLine);
    var line = 0;
    for (var i = 0; i < r.cells.length; i++) {
      var wraps = Math.max(1, Math.ceil(r.cells[i] / per));
      if (vline < line + wraps) {
        var rem = r.cells[i] - (vline - line) * per;
        return Math.min(rem, per);
      }
      line += wraps;
    }
    return 0;
  }

  function buildRow(r) {
    var el = document.createElement('div');
    el.className = 'lv-row';
    el.style.transform = 'translate3d(0,' + (r.y - st.base) + 'px,0)';
    // 行首两条竖条（读取指示）：占位，有标记才填色
    var g1 = document.createElement('i');
    g1.className = 'lv-gut lv-gut-cli' + ((r.read & 1) ? ' is-cli' : '');
    var g2 = document.createElement('i');
    g2.className = 'lv-gut lv-gut-mcp' + ((r.read & 2) ? ' is-mcp' : '');
    el.appendChild(g1);
    el.appendChild(g2);
    var ts = document.createElement('div');
    ts.className = 'ts-line ' + lvTsClass(r.dir);
    ts.innerHTML = tsHTML(r);
    var dl = document.createElement('div');
    dl.className = 'data-line';
    if (r.dir === 'system' || r.dir === 'sys') {
      // 系统消息是文本（sys.port_connected|COM4|115200），不按 Hex 画
      dl.textContent = lvHexToText(r.hex);
    } else if (st.mode === 'hex') {
      var sp = document.createElement('span');
      sp.className = 'hex-data ' + lvHexClass(r.dir);
      sp.style.whiteSpace = 'pre';
      sp.textContent = r.hex || '';
      dl.appendChild(sp);
    } else if (r.segs && r.segs.length) {
      if (typeof setDisplayContext === 'function') {
        setDisplayContext(typeof opts.disp === 'function' ? opts.disp() : opts.disp);
      }
      renderSegments(dl, r.segs, r.dir);
    }
    el.appendChild(ts);
    el.appendChild(dl);
    return el;
  }

  // ---- 滚动条 ----
  function updateThumb() {
    st.trackH = Math.max(1, st.vh);
    var thumbH = st.thumbH;
    if (st.contentH > 0) {
      thumbH = st.trackH * Math.min(1, st.vh / st.contentH);
    }
    thumbH = lvClamp(thumbH, LV_THUMB_MIN, st.trackH);
    st.thumbH = thumbH;
    thumb.style.height = thumbH + 'px';
    var span = st.trackH - thumbH;
    var top = st.maxOffset > 0 ? (st.offset / st.maxOffset) * span : 0;
    thumb.style.transform = 'translate3d(0,' + top + 'px,0)';
  }

  function drawRuler(force) {
    var nb = lvClamp(Math.round(st.trackH), 32, 4000);
    var key = nb + ':' + st.total + ':' + st.phase;
    if (!force && key === st.rulerKey) return;
    st.rulerKey = key;
    lvCall('Ruler', st.id, nb).then(function (r) {
      if (st.dead) return;
      st.ruler = r;
      paintRuler(r, nb);
    }).catch(function () {});
  }

  function paintRuler(r, nb) {
    lanes.innerHTML = '';
    var H = st.trackH;
    var step = H / nb;
    // 三道泳道先铺占位底（没有数据源的也保留几何，不伪装成有数据）
    for (var l = 0; l < LV_LANES; l++) {
      var laneEl = document.createElement('i');
      laneEl.className = 'lv-lane lv-lane-' + ['read', 'check', 'disk'][l];
      lanes.appendChild(laneEl);
    }
    // 第一道：读取（非交互 CLI / MCP 读到哪）。读取记录还没实现 —— 占位。
    // 第二道：校验（Modbus 严重度）。校验引擎这一版不做 —— 占位。
    // 第三道：落盘。静态文件 = 完整性；实时会话 = 是否在录（等守护进程事件）。
    var diskLane = document.createElement('i');
    diskLane.className = 'lv-lane-mark';
    diskLane.style.left = (2 * LV_LANE_W) + 'px';
    diskLane.style.top = '0px';
    diskLane.style.height = H + 'px';
    diskLane.style.background = r.diskOk
      ? 'color-mix(in srgb, var(--fg) 22%, transparent)'
      : 'color-mix(in srgb, var(--red) 22%, transparent)';
    lanes.appendChild(diskLane);
    for (var i = 0; i < (r.diskLine || []).length; i++) {
      var ln = document.createElement('i');
      ln.className = 'lv-lane-mark';
      ln.style.left = (2 * LV_LANE_W) + 'px';
      ln.style.top = Math.round(r.diskLine[i] * H) + 'px';
      ln.style.height = '1px';
      ln.style.background = 'var(--red)';
      lanes.appendChild(ln);
    }
  }

  // ---- 选区 ----
  function posAt(clientX, clientY) {
    var box = host.getBoundingClientRect();
    var y = clientY - box.top - st.reserveTop; // 视口内坐标（扣掉顶部让位）
    var x = clientX - box.left;
    var cy = st.offset + y;             // 内容坐标
    var rows = st.rows;
    if (!rows.length) return null;
    var i = -1, lo = 0, hi = rows.length - 1;
    while (lo <= hi) {
      var mid = (lo + hi) >> 1, rr = rows[mid];
      if (cy < rr.y) hi = mid - 1;
      else if (cy >= rr.y + rr.h) lo = mid + 1;
      else { i = mid; break; }
    }
    if (i < 0) {
      // 落在窗口之外：贴到最近的一行的两端
      var first = rows[0], last = rows[rows.length - 1];
      if (cy < first.y) return { idx: first.idx, vline: 0, cell: 0 };
      return { idx: last.idx, vline: lineCount(last) - 1, cell: lineCells(last, lineCount(last) - 1) };
    }
    var r = rows[i];
    var lines = lineCount(r);
    var vline = Math.floor((cy - r.y - st.tsH) / st.dataH);
    vline = lvClamp(vline, 0, lines - 1);
    var cell = Math.round((x - LV_PAD_X) / st.charW);
    cell = lvClamp(cell, 0, lineCells(r, vline));
    return { idx: r.idx, vline: vline, cell: cell };
  }

  function selRange() {
    if (!st.sel) return null;
    var a = st.sel.a, b = st.sel.b;
    if (a.idx > b.idx || (a.idx === b.idx && (a.vline > b.vline ||
      (a.vline === b.vline && a.cell > b.cell)))) {
      return { from: b, to: a };
    }
    return { from: a, to: b };
  }

  function paintSelection() {
    selLayer.innerHTML = '';
    var rng = selRange();
    if (!rng) return;
    var dataH = st.dataH, tsH = st.tsH, cw = st.charW;
    for (var i = 0; i < st.rows.length; i++) {
      var r = st.rows[i];
      if (r.idx < rng.from.idx || r.idx > rng.to.idx) continue;
      var lines = lineCount(r);
      var lf = (r.idx === rng.from.idx) ? rng.from.vline : 0;
      var cf = (r.idx === rng.from.idx) ? rng.from.cell : 0;
      var lt = (r.idx === rng.to.idx) ? rng.to.vline : lines - 1;
      var ct = (r.idx === rng.to.idx) ? rng.to.cell : lineCells(r, lines - 1);
      if (lt < lf) continue;
      for (var L = lf; L <= lt; L++) {
        var w = lineCells(r, L);
        var x0 = LV_PAD_X + ((L === lf) ? cf * cw : 0);
        var x1 = LV_PAD_X + ((L === lt) ? Math.min(ct, w) * cw : w * cw);
        if (x1 <= x0) continue;
        var box = document.createElement('i');
        box.className = 'lv-selbox';
        box.style.top = (r.y - st.base + tsH + L * dataH) + 'px';
        box.style.left = x0 + 'px';
        box.style.width = (x1 - x0) + 'px';
        box.style.height = dataH + 'px';
        selLayer.appendChild(box);
      }
    }
  }

  // ---- 取窗口 ----
  function needsFetch() {
    if (!st.total) return false;
    if (!st.rows.length) return true;
    var lo = st.rows[0].y, hi = st.rows[st.rows.length - 1].y + st.rows[st.rows.length - 1].h;
    return st.offset < lo || st.offset + st.vh > hi;
  }

  function fetchWindow() {
    if (!st.id || st.dead) return;
    if (st.fetching) { st.needFetch = true; return; }
    st.fetching = true;
    var contentY = Math.max(0, Math.round(st.offset));
    lvCall('Window', st.id, contentY, Math.round(st.vh), Math.round(st.vh))
      .then(function (w) {
        st.fetching = false;
        if (st.dead) return;
        applyWindow(w);
        if (st.needFetch) { st.needFetch = false; if (needsFetch()) fetchWindow(); }
      })
      .catch(function (e) {
        st.fetching = false;
        showMsg('读取窗口失败：' + String(e && e.message ? e.message : e));
      });
  }

  function applyWindow(w) {
    st.total = w.total || 0;
    st.contentH = w.contentHeight || 0;
    st.maxOffset = w.maxOffset || 0;
    st.mode = w.mode || st.mode;
    st.perLine = w.perLine || st.perLine;
    st.hexPrefix = !!w.hexPrefix;
    st.base = w.base || 0;
    st.rows = w.rows || [];
    layer.innerHTML = '';
    layer.appendChild(selLayer);
    for (var i = 0; i < st.rows.length; i++) layer.appendChild(buildRow(st.rows[i]));
    layerTransform();
    paintSelection();
    updateThumb();
    drawRuler(false);
    if (typeof opts.onStatus === 'function') {
      opts.onStatus({ total: st.total, contentH: st.contentH, offset: st.offset, mode: st.mode });
    }
  }

  function showMsg(text) {
    msg.textContent = text || '';
    msg.style.display = text ? '' : 'none';
  }

  // ---- 滚动 ----
  // 层位移：内容坐标 → 视口坐标，顶部再让出 reserveTop（会话页的悬浮工具条）。
  function layerTransform() {
    layer.style.transform = 'translate3d(0,' + (st.base - st.offset + st.reserveTop) + 'px,0)';
  }

  function setOffset(v) {
    v = lvClamp(Math.round(v), 0, Math.round(st.maxOffset));
    if (v === st.offset) {
      st.atBottom = v >= Math.round(st.maxOffset) - 2;
      return;
    }
    st.offset = v;
    st.atBottom = v >= Math.round(st.maxOffset) - 2;
    if (st.atBottom) { st.unread = 0; updateUnread(); }
    layerTransform();
    updateThumb();
    if (needsFetch()) fetchWindow();
  }

  function pageBy(dir) { setOffset(st.offset + dir * (st.vh - st.dataH * 2)); }

  function configure(anchorIdx) {
    var chars = layout();
    var want = Math.max(0, anchorIdx | 0);
    return lvCall('Configure', st.id, chars, Math.round(st.tsH * 2),
      Math.round(st.dataH * 2), st.mode, want, styleNow()).then(function (cfg) {
      if (st.dead) return cfg;
      st.perLine = cfg.perLine;
      st.contentH = cfg.contentHeight;
      st.maxOffset = cfg.maxOffset;
      st.hexPrefix = !!cfg.hexPrefix;
      return cfg;
    });
  }

  function refresh() {
    if (!st.id || st.dead) return Promise.resolve();
    var top = st.offset;
    return configure(0).then(function () {
      return lvCall('Window', st.id, Math.round(st.offset), Math.round(st.vh), Math.round(st.vh));
    }).then(function (w) {
      applyWindow(w);
      st.offset = Math.min(top, st.maxOffset);
      layerTransform();
      updateThumb();
    }).catch(function (e) {
      showMsg('刷新失败：' + String(e && e.message ? e.message : e));
      throw e;
    });
  }

  // ---- 打开 / 轮询扫描 ----
  function poll() {
    if (st.dead || !st.id) return;
    lvCall('Status', st.id).then(function (s) {
      if (st.dead) return;
      st.phase = s.phase;
      if (s.phase === 'ready') {
        st.total = s.frames;
        lvCall('Meta', st.id).then(function (m) {
          if (st.dead) return;
          if (typeof opts.onMeta === 'function') opts.onMeta(m);
          refresh();
        }).catch(function (e) {
          showMsg('元信息失败：' + String(e && e.message ? e.message : e));
          refresh();
        });
        showMsg('');
        return;
      }
      if (s.phase === 'error') {
        st.err = s.err || '扫描失败';
        showMsg('打开失败：' + st.err);
        return;
      }
      showMsg('扫描中… ' + (s.size ? Math.round((s.done / s.size) * 100) + '%' : ''));
      setTimeout(poll, 120);
    }).catch(function (e) {
      showMsg('读取状态失败：' + String(e && e.message ? e.message : e));
    });
  }

  function open(path) {
    st.dead = false;
    st.phase = 'scan';
    st.rows = [];
    st.sel = null;
    layer.innerHTML = '';
    layer.appendChild(selLayer);
    showMsg('打开中…');
    try {
      layout();
    } catch (err) {
      lvLog('layout 抛错 ' + (err && err.message ? err.message : err) + ' ' +
        (err && err.stack ? String(err.stack).slice(0, 300) : ''));
      throw err;
    }
    return lvCall('Open', path).then(function (r) {
      st.id = r.id;
      st.phase = (r.status && r.status.phase) || 'scan';
      if (st.phase === 'ready') {
        lvCall('Meta', st.id).then(function (m) {
          if (typeof opts.onMeta === 'function') opts.onMeta(m);
          refresh();
        }).catch(function (e) {
          showMsg('元信息失败：' + String(e && e.message ? e.message : e));
          refresh();
        });
        showMsg('');
      } else {
        poll();
      }
      return r;
    }).catch(function (e) {
      showMsg('打开失败：' + String(e && e.message ? e.message : e));
      throw e;
    });
  }

  // ---- 复制 ----
  function copySelection(format) {
    var rng = selRange();
    if (!rng) return Promise.resolve(null);
    var req = {
      fromIdx: rng.from.idx, fromVline: rng.from.vline, fromCell: rng.from.cell,
      toIdx: rng.to.idx, toVline: rng.to.vline, toCell: rng.to.cell,
      format: format || (st.mode === 'hex' ? 'hex' : 'escaped'),
      withTs: false, withDir: false, withLen: false, hexPrefix: st.hexPrefix
    };
    return lvCall('CopySelection', st.id, req).then(function (r) {
      if (r.err) { if (typeof opts.onFlash === 'function') opts.onFlash(r.err); return r; }
      if (typeof opts.onFlash === 'function') {
        opts.onFlash('已复制 ' + r.frames + ' 帧 · ' + r.bytes + ' B');
      }
      return r;
    }).catch(function (e) {
      if (typeof opts.onFlash === 'function') opts.onFlash('复制失败：' + String(e));
      return null;
    });
  }

  // ---- 事件 ----
  function onWheel(e) {
    if (e.ctrlKey) return; // 缩放交给浏览器
    e.preventDefault();
    var d = e.deltaY;
    if (e.deltaMode === 1) d *= st.dataH;
    else if (e.deltaMode === 2) d *= st.vh;
    setOffset(st.offset + d);
  }

  function onPointerDown(e) {
    if (e.button !== 0) return;
    if (e.target === thumb) return;
    // 滚动条轨道和「新数据」胶囊也挂在 host 里。这里若把指针 capture 到 host，
    // 它们的 pointerup 会被 host 抢走，按钮的 click 就合成不出来（胶囊点不动、
    // 点轨道还会顺手清掉选区）。只接管内容区（.lv-view）里的按下。
    if (e.target !== view && !(view.contains && view.contains(e.target))) return;
    var p = posAt(e.clientX, e.clientY);
    if (!p) return;
    host.focus(); // 键盘兜底（Ctrl+C / PageUp / Home）靠焦点落在视口上
    st.dragging = true;
    st.sel = { a: p, b: p };
    host.setPointerCapture(e.pointerId);
    paintSelection();
    e.preventDefault();
  }

  function onPointerMove(e) {
    if (!st.dragging) return;
    var p = posAt(e.clientX, e.clientY);
    if (!p) return;
    st.sel.b = p;
    paintSelection();
  }

  function onPointerUp(e) {
    if (!st.dragging) return;
    st.dragging = false;
    try { host.releasePointerCapture(e.pointerId); } catch (_) {}
    // 单击（没拖出范围）当成取消选区
    if (st.sel && st.sel.a.idx === st.sel.b.idx &&
        st.sel.a.vline === st.sel.b.vline && st.sel.a.cell === st.sel.b.cell) {
      st.sel = null;
      paintSelection();
    }
  }

  function onContextMenu(e) {
    if (!selRange()) return;
    e.preventDefault();
    copySelection();
  }

  function onKeyDown(e) {
    if (e.ctrlKey && (e.key === 'c' || e.key === 'C')) {
      if (selRange()) { e.preventDefault(); copySelection(); }
      return;
    }
    if (e.key === 'Escape') { st.sel = null; paintSelection(); return; }
    if (e.key === 'PageDown') { e.preventDefault(); pageBy(1); return; }
    if (e.key === 'PageUp') { e.preventDefault(); pageBy(-1); return; }
    if (e.key === 'Home') { e.preventDefault(); setOffset(0); return; }
    if (e.key === 'End') { e.preventDefault(); setOffset(st.maxOffset); return; }
    if (e.key === 'ArrowDown') { e.preventDefault(); setOffset(st.offset + st.dataH * 3); return; }
    if (e.key === 'ArrowUp') { e.preventDefault(); setOffset(st.offset - st.dataH * 3); return; }
    if (e.key === ' ') { e.preventDefault(); pageBy(e.shiftKey ? -1 : 1); return; }
  }

  // 滑块拖动
  var drag = null;
  function thumbDown(e) {
    e.preventDefault();
    e.stopPropagation();
    drag = { y: e.clientY, off: st.offset };
    thumb.setPointerCapture(e.pointerId);
  }
  function thumbMove(e) {
    if (!drag) return;
    var span = st.trackH - st.thumbH;
    var dy = e.clientY - drag.y;
    setOffset(span > 0 ? drag.off + (dy / span) * st.maxOffset : 0);
  }
  function thumbUp(e) {
    if (!drag) return;
    drag = null;
    try { thumb.releasePointerCapture(e.pointerId); } catch (_) {}
  }

  // 点轨道：跳到以点击处为中心的一屏
  function railDown(e) {
    if (e.target === thumb) return;
    var box = track.getBoundingClientRect();
    var ratio = lvClamp((e.clientY - box.top) / Math.max(1, box.height), 0, 1);
    setOffset(ratio * st.contentH - st.vh / 2);
    e.preventDefault();
  }

  host.addEventListener('wheel', onWheel, { passive: false });
  host.addEventListener('pointerdown', onPointerDown);
  host.addEventListener('pointermove', onPointerMove);
  host.addEventListener('pointerup', onPointerUp);
  host.addEventListener('pointercancel', onPointerUp);
  host.addEventListener('contextmenu', onContextMenu);
  host.addEventListener('keydown', onKeyDown);
  rail.addEventListener('pointerdown', railDown);
  thumb.addEventListener('pointerdown', thumbDown);
  window.addEventListener('pointermove', thumbMove);
  window.addEventListener('pointerup', thumbUp);

  var ro = null;
  if (typeof ResizeObserver === 'function') {
    var lastW = 0, lastH = 0;
    ro = new ResizeObserver(function () {
      if (host.clientWidth === lastW && host.clientHeight === lastH) return;
      lastW = host.clientWidth; lastH = host.clientHeight;
      refresh();
    });
    ro.observe(host);
  }

  // ---- 实时模式（串口监视器 / 转发监视器）----
  //
  // 帧不再由前端缓存，而是 Go 侧文档（LogView.OpenLive）攒着；这里每 120 ms 轮一次
  // 概况，只有「帧数或内容高度变了」才重取窗口 —— 贴底时永远取最后一屏，
  // 不贴底时只涨未读数，不打断用户往回翻。

  function updateUnread() {
    if (st.unread > 0 && !st.atBottom) {
      unreadEl.textContent = '↓ ' + st.unread + ' 条新数据';
      unreadEl.style.display = '';
    } else {
      unreadEl.style.display = 'none';
    }
  }

  function jumpBottom() {
    st.atBottom = true;
    setOffset(st.maxOffset);
    updateUnread();
  }

  function refreshLive() {
    if (!st.id || st.dead) return Promise.resolve();
    var chars = layout();
    return lvCall('Configure', st.id, chars, Math.round(st.tsH * 2),
      Math.round(st.dataH * 2), st.mode, 0, styleNow()).then(function (cfg) {
      st.perLine = cfg.perLine;
      st.maxOffset = cfg.maxOffset;
      // 取一屏：contentY 给个超大值，Go 侧夹到 maxOffset，正好是最后一条
      return lvCall('Window', st.id, 1 << 30, Math.round(st.vh), Math.round(st.vh));
    }).then(function (w) {
      applyWindow(w);
      st.maxOffset = w.maxOffset;
      if (st.atBottom) st.offset = Math.round(w.maxOffset);
      layerTransform();
      updateThumb();
      st.lastSeen = st.total;
      if (typeof opts.onStatus === 'function') {
        opts.onStatus({ live: true, total: st.total, evicted: w.total - st.total });
      }
    }).catch(function (e) {
      showMsg('刷新失败：' + String(e && e.message ? e.message : e));
    });
  }

  function pollLive() {
    if (st.dead || !st.live || !st.id) return;
    lvCall('LiveInfo', st.id).then(function (info) {
      if (st.dead) return;
      var grew = info.total !== st.total || Math.abs(info.contentH - st.contentH) > 0.5;
      var delta = info.total - st.total;
      st.total = info.total;
      if (typeof opts.onStatus === 'function') {
        opts.onStatus({ live: true, total: info.total, evicted: info.evicted });
      }
      if (!grew) return;
      if (st.atBottom) refreshLive();
      else if (delta > 0) { st.unread += delta; updateUnread(); }
    }).catch(function () {}).then(function () {
      if (!st.dead) st.pollTimer = setTimeout(pollLive, 120);
    });
  }

  function openLive(sessionID) {
    st.live = true;
    st.atBottom = true;
    st.unread = 0;
    layout();
    return lvCall('OpenLive', sessionID).then(function (r) {
      st.id = r.id;
      return refreshLive();
    }).then(function () {
      pollLive();
      return st.id;
    }).catch(function (e) {
      showMsg('打开失败：' + String(e && e.message ? e.message : e));
      throw e;
    });
  }

  function clearAll() {
    if (!st.id) return Promise.resolve();
    st.sel = null;
    st.offset = 0;
    st.unread = 0;
    st.lastSeen = 0;
    updateUnread();
    return lvCall('ResetLive', st.id).then(function () {
      return refreshLive();
    }).catch(function () {});
  }

  function appendSystem(text) {
    if (!st.id || st.dead) return;
    lvCall('AppendSystem', st.id, text).catch(function () {});
  }

  return {
    open: open,
    openLive: openLive,
    clear: clearAll,
    appendSystem: appendSystem,
    isLive: function () { return st.live; },
    jumpBottom: jumpBottom,
    destroy: function () {
      st.dead = true;
      if (st.pollTimer) clearTimeout(st.pollTimer);
      if (ro) ro.disconnect();
      host.removeEventListener('wheel', onWheel);
      host.removeEventListener('pointerdown', onPointerDown);
      host.removeEventListener('pointermove', onPointerMove);
      host.removeEventListener('pointerup', onPointerUp);
      host.removeEventListener('contextmenu', onContextMenu);
      host.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('pointermove', thumbMove);
      window.removeEventListener('pointerup', thumbUp);
      if (st.id) lvCall('Close', st.id).catch(function () {});
      host.innerHTML = '';
    },
    refresh: refresh,
    setMode: function (mode) {
      st.mode = (mode === 'text') ? 'text' : 'hex';
      if (st.live) return refreshLive();
      var anchor = st.rows.length ? st.rows[0].idx : 0;
      return configure(anchor).then(function () { return fetchWindow(); });
    },
    setShowYear: function (on) {
      st.showYear = !!on;
      // 只影响时间戳串，不用重算布局：重画可见行即可
      for (var i = 0; i < st.rows.length; i++) {
        var rowEl = layer.children[i + 1]; // 0 是 selLayer
        if (rowEl) {
          var tsEl = rowEl.firstChild;
          if (tsEl) tsEl.innerHTML = tsHTML(st.rows[i]);
        }
      }
      return Promise.resolve();
    },
    mode: function () { return st.mode; },
    total: function () { return st.total; },
    offset: function () { return st.offset; },
    hasSelection: function () { return !!selRange(); },
    copy: copySelection,
    clearSelection: function () { st.sel = null; paintSelection(); },
    // 调试/验收用：行高对账与当前几何
    debug: function () {
      return {
        total: st.total, contentH: st.contentH, maxOffset: st.maxOffset,
        offset: st.offset, vh: st.vh, perLine: st.perLine, mode: st.mode,
        charW: st.charW, tsH: st.tsH, dataH: st.dataH,
        rows: st.rows.length, base: st.base
      };
    }
  };
}
