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

// 系统消息的 Hex 是本地事件文本的 UTF-8 字节，要整串按 UTF-8 解码。
// 逐字节 fromCharCode 会把每个汉字拆成三个乱码字符（显示成一串点/中点），
// 也会把带 0x 前缀的 Hex 串当数据 —— 前缀必须先剥掉。
function lvHexToText(hex) {
  if (!hex) return '';
  var s = String(hex).replace(/0x/gi, '').replace(/\s+/g, '');
  if (s.length % 2) s = s.slice(0, -1);
  var bytes = new Uint8Array(s.length / 2);
  for (var i = 0; i < bytes.length; i++) {
    var v = parseInt(s.substr(i * 2, 2), 16);
    bytes[i] = isNaN(v) ? 0x2E : v;
  }
  try {
    return new TextDecoder('utf-8').decode(bytes);
  } catch (_) {
    var out = '';
    for (var j = 0; j < bytes.length; j++) {
      out += (bytes[j] >= 32 && bytes[j] < 127) ? String.fromCharCode(bytes[j]) : '\u00B7';
    }
    return out;
  }
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
    live: false, atBottom: true, lastSeen: 0, pollTimer: null,
    // 用户点了「锁定滚动」：新数据进来也不自动贴底（贴底跟随要 atBottom 且没锁）
    scrollLocked: !!opts.scrollLocked,
    trimmedPx: 0, histFile: '', histHasMore: false,
    // 往回翻的分页：oldestTs/oldestSame 是文档最旧帧的游标（Go 侧算，裁头会跟着
    // 前移），loadingOlder 保证一次只问一页，histDone 表示环里再没有更早的了。
    oldestTs: 0, oldestSame: 0, loadingOlder: false, histDone: false,
    // 增量渲染用：rowGen 是"样式代号"，变了就强制重建所有行；
    // cfgChars/needConfigure 决定要不要再发一次 Configure。
    rowGen: 0, cfgChars: 0, needConfigure: true, rulerAt: 0, layoutKey: '',
    reserveTop: opts.reserveTop || 0,
    reserveBottom: opts.reserveBottom || 0,
    trackTop: 0
  };

  host.classList.add('lv-host');
  host.tabIndex = 0;
  host.innerHTML = '';

  // 当前窗口里的行 DOM：帧号 → { el, sig }（增量渲染用）
  var rowEls = {};

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

  // 顶部提示：更早的历史已经被内存缓冲裁掉了（没落盘时就是永久丢失）。
  var trimEl = document.createElement('div');
  trimEl.className = 'lv-trim';
  trimEl.style.display = 'none';
  host.appendChild(trimEl);

  // 「回到底部」按钮：不贴底时出现。未读数去掉了，回底还得有个入口。
  var bottomBtn = document.createElement('button');
  bottomBtn.className = 'lv-tobottom';
  bottomBtn.textContent = '↓ 回到底部';
  bottomBtn.style.display = 'none';
  bottomBtn.addEventListener('click', function () { jumpBottom(); });
  host.appendChild(bottomBtn);

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
    // 会话页的让位量（顶部悬浮配置栏、底部浮动按钮）随窗口尺寸和布局变，而且
    // 挂载那一刻标签页往往还没布局、rect 全是 0。所以交给调用方现量。
    if (typeof opts.reserve === 'function') {
      var rs = opts.reserve() || {};
      st.reserveTop = rs.top || 0;
      st.reserveBottom = rs.bottom || 0;
      st.trackTop = (rs.trackTop == null) ? st.reserveTop : (rs.trackTop || 0);
    } else {
      st.trackTop = st.reserveTop;
    }
    st.vh = Math.max(1, host.clientHeight - st.reserveTop - st.reserveBottom);
    // 滚动条：上边界落在顶部毛玻璃栏的底边，然后一直落到底 —— 底部那 48px
    // 虽然要给浮动按钮让位，但滚动条本身不该被按钮的模糊垫底盖住（垫底已右让 16px）。
    st.trackH = Math.max(1, host.clientHeight - st.trackTop);
    track.style.top = st.trackTop + 'px';
    track.style.height = st.trackH + 'px';
    // 上边界横线只在会话页画（文件页 trackTop=0，不需要一条压住首行的线）
    track.classList.toggle('lv-track-rule', st.trackTop > 0);
    // 尺寸没变就别再量一遍：measure/fitChars 都要建探针节点，每 60~120 ms
    // 跑一次是白烧 CPU（数据密集时是仅次于整窗重建的开销）。
    var key = host.clientWidth + 'x' + host.clientHeight + ':' +
      st.reserveTop + ':' + st.reserveBottom + ':' + st.trackTop;
    if (key === st.layoutKey && st.cfgChars) return st.cfgChars;
    st.layoutKey = key;
    measure();
    var avail = Math.max(1, host.clientWidth - LV_PAD_X - LV_PAD_R - LV_RAIL_W);
    st.cfgChars = fitChars(avail);
    return st.cfgChars;
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

  // 一帧该画 Hex 还是文本，由 **Go 按这一帧自己的方向**决定（转发页的 P1 / P2
  // 可以各是各的模式）：hex 模式给 r.hex，文本模式给 r.segs。所以前端只能看
  // r 上实际带了什么，不能看整页的 st.mode —— 那会把「Rx 切了 Hex、Tx 还是文本」
  // 这种混排画成空白。
  function lineParts(r) {
    // Hex 行在 Go 侧就按 perLine 个字节折好了（用 \n 分隔），这里直接按 \n 拆。
    if (r.hex) return r.hex.split('\n');
    return null;
  }

  function lineCount(r) {
    if (r.dir === 'system' || r.dir === 'sys') return 1; // 系统消息固定一行
    if (r.hex) {
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
    if (r.hex) {
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
      // 系统消息是文本（sys.port_connected|COM4|115200），要先过 i18n 再显示，
      // 否则界面上是一串 key。老渲染把它包成「── 文本 ──」，这里照抄。
      var sysTxt = lvHexToText(r.hex);
      if (typeof formatSysMsg === 'function') sysTxt = formatSysMsg(sysTxt);
      dl.className = 'data-line sys-msg';
      dl.textContent = '── ' + sysTxt + ' ──';
    } else if (r.hex) {
      var sp = document.createElement('span');
      sp.className = 'hex-data ' + lvHexClass(r.dir);
      sp.style.whiteSpace = 'pre';
      sp.textContent = r.hex;
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
  // trackH 只在 layout() 里定（上边界毛玻璃底边、下边界窗口底），这里不能覆盖它。
  function updateThumb() {
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
    // 泳道是"按桶统计来源/落盘"的概览，不需要跟着每一帧重算：帧数涨一点就
    // 重发 Ruler（Go 侧还要 O(n) 重分桶）在数据密集时是纯负担。
    var now = Date.now();
    var key = nb + ':' + st.phase;
    if (!force && key === st.rulerKey && now - st.rulerAt < 500) return;
    st.rulerKey = key;
    st.rulerAt = now;
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

  // 行内容签名：帧只追加、不会原地改，所以 idx/dir/size/ts 加上"样式代号"
  // 就能判断能不能复用 DOM；样式或模式变了 rowGen 会加一，强制重建。
  function rowSig(r) {
    return st.rowGen + '|' + r.dir + '|' + r.size + '|' + r.ts;
  }

  function resetRowEls() {
    for (var k in rowEls) {
      var o = rowEls[k];
      if (o && o.el && o.el.parentNode) o.el.parentNode.removeChild(o.el);
    }
    rowEls = {};
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
    // 增量更新：数据密集时每 120 ms 整窗重建（innerHTML='' + 重新 buildRow）
    // 是 CPU 的大头。按帧号复用 DOM，只新建进入窗口的行、删掉离开窗口的。
    var keep = {};
    for (var i = 0; i < st.rows.length; i++) {
      var r = st.rows[i];
      keep[r.idx] = 1;
      var sig = rowSig(r);
      var rec = rowEls[r.idx];
      if (rec && rec.sig === sig) {
        rec.el.style.transform = 'translate3d(0,' + (r.y - st.base) + 'px,0)';
      } else {
        if (rec && rec.el.parentNode) rec.el.parentNode.removeChild(rec.el);
        var el = buildRow(r);
        layer.appendChild(el);
        rowEls[r.idx] = { el: el, sig: sig };
      }
    }
    for (var k in rowEls) {
      if (!keep[k]) {
        var old = rowEls[k];
        if (old.el.parentNode) layer.removeChild(old.el);
        delete rowEls[k];
      }
    }
    layerTransform();
    paintSelection();
    updateThumb();
    updateBottomBtn();
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

  // 行对齐：offset 落在某一行中间时贴到该行的上边界。「一页一行」靠它落位。
  // 注意 st.rows 是「帧」粒度（一帧有 ts 行 + N 个数据行），所以帧内还要按
  // tsH + k*dataH 再拆一次 —— 只吸到帧首的话，一帧两千多像素，向下滚 22px
  // 会被吸回原处，看起来就是「滚不动」。
  function snapCurrent() {
    var rows = st.rows;
    if (!rows.length) return;
    var last = rows[rows.length - 1];
    // offset 已经滚出当前窗口：交给 fetchWindow 去取新窗口，这里不能把它拉回来。
    if (st.offset < rows[0].y - 0.5 || st.offset > last.y + last.h) return;
    var r = null;
    for (var i = 0; i < rows.length; i++) {
      if (rows[i].y <= st.offset && st.offset < rows[i].y + rows[i].h) { r = rows[i]; break; }
      if (rows[i].y > st.offset) break;
    }
    var y = st.offset;
    if (!r) {
      for (var j = 0; j < rows.length; j++) {
        if (rows[j].y > st.offset) { y = rows[j].y; break; }
      }
    } else {
      var rel = st.offset - r.y;
      if (rel <= st.tsH) {
        y = r.y;
      } else {
        var k = Math.floor((rel - st.tsH) / st.dataH);
        y = r.y + st.tsH + k * st.dataH;
        if (y > r.y + r.h - st.dataH) y = r.y + r.h - st.dataH;
      }
    }
    if (Math.abs(y - st.offset) > 0.5) st.offset = y;
    if (st.offset > st.maxOffset) st.offset = Math.round(st.maxOffset);
  }

  // 裁头补偿：内容整体上移了 dh，offset 跟着减，视口就锁在原地。
  //
  // 这里**不能**走 setOffset —— 那边会 snapCurrent 吸到行首，而裁头是每帧
  // 都在发生的（环形缓冲语义下 50 ms 一次），每吸一次画面就被往上拽半行，
  // 看起来就是持续抖动。
  //
  // base 也要一起减：layer 的位移是 base - offset，两个同时减 dh 才互相抵消。
  // 只减 offset 的话，层会被多推 dh 像素，画面看起来是「往更早的方向猛跳」。
  // 行元素不用动：它们用的是内容坐标，坐标整体平移不影响 base - offset + y。
  function shiftBy(dh) {
    if (!(dh > 0)) return;
    var next = st.offset - dh;
    st.base -= dh;
    if (next < 0) {
      // 视口被推到内容顶部：坐标全变了，得重新取一屏
      st.offset = 0;
      st.atBottom = st.offset >= Math.round(st.maxOffset) - 2;
      updateThumb();
      updateTrimTip();
      updateBottomBtn();
      fetchWindow();
      maybeLoadOlder();
      return;
    }
    st.offset = next;
    st.atBottom = st.offset >= Math.round(st.maxOffset) - 2;
    layerTransform();
    updateThumb();
    updateTrimTip();
    updateBottomBtn();
  }

  function setOffset(v) {
    v = lvClamp(Math.round(v), 0, Math.round(st.maxOffset));
    if (v === st.offset) {
      st.atBottom = v >= Math.round(st.maxOffset) - 2;
      updateTrimTip();
      return;
    }
    st.offset = v;
    snapCurrent();
    // 吸附之后重新判贴底：吸到行首可能就差几十像素，别看错成「还在底部」
    st.atBottom = st.offset >= Math.round(st.maxOffset) - 2;
    layerTransform();
    updateThumb();
    updateTrimTip();
    updateBottomBtn();
    if (needsFetch()) fetchWindow();
    maybeLoadOlder();
  }

  // 一页 = 一行：PageUp/PageDown 一次只走一行（滚动位置已经对齐行首）。
  function pageBy(dir) { setOffset(st.offset + dir * st.dataH); }

  // dirModes 是按方向的模式覆盖（下标 = sessionfile 方向号：0 系统 / 1 读取 /
  // 2 发送 / 3 P1 / 4 P2，空串 = 沿用整页的 mode）。转发页的 P1 / P2 各有
  // 独立的「字 / H」，一个整页 mode 表达不了，所以每次 Configure 都带上。
  function dirModesNow() {
    if (typeof opts.dirModes !== 'function') return null;
    var v = opts.dirModes();
    return (v && v.length) ? v : null;
  }

  function configure(anchorIdx) {
    var chars = layout();
    var want = Math.max(0, anchorIdx | 0);
    return lvCall('Configure', st.id, chars, Math.round(st.tsH * 2),
      Math.round(st.dataH * 2), st.mode, dirModesNow(), want, styleNow()).then(function (cfg) {
      if (st.dead) return cfg;
      st.perLine = cfg.perLine;
      st.contentH = cfg.contentHeight;
      st.maxOffset = cfg.maxOffset;
      st.hexPrefix = !!cfg.hexPrefix;
      // 布局/样式变了：行内容要重建（增量渲染靠 rowGen 判断签名）
      st.rowGen++;
      st.cfgChars = chars;
      st.needConfigure = false;
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
    resetRowEls();
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
    if (e.key === 'End') { e.preventDefault(); jumpBottom(); return; }
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
    // 贴底跟随 = 视口在底部 **且** 用户没点「锁定滚动」。锁定的语义就是
    // 「新数据进来也别自动滚」，所以这两个条件要一起看。
    function follows() { return st.atBottom && !st.scrollLocked; }

    var lastW = 0, lastH = 0;
    ro = new ResizeObserver(function () {
      if (host.clientWidth === lastW && host.clientHeight === lastH) return;
      lastW = host.clientWidth; lastH = host.clientHeight;
      // 会话页贴底时尺寸一变（窗口缩放、按钮条出现）就让位量重算并重新贴底，
      // 否则 maxOffset 还按旧视口算，最后一行会跑到浮动按钮底下。
      if (st.live && follows()) { refreshLive(); return; }
      refresh();
    });
    ro.observe(host);
  }

  // ---- 实时模式（串口监视器 / 转发监视器）----
  //
  // 帧不再由前端缓存，而是 Go 侧文档（LogView.OpenLive）攒着；数据帧事件一到
  // 就 notify，只有「帧数或内容高度变了」才重取窗口 —— 贴底时永远取最后一屏，
  // 不贴底时只更新滚动条（不再有「N 条新数据」气泡）。

  // 顶部提示：三种情况要分开说 ——
  //   ① 正在往回要一页（自动加载，用户什么都不用做）
  //   ② 还有更早的记录没取（首次回放只取一页，这是默认状态）
  //   ③ 内存缓冲已经裁过帧，更早的确实不在了
  // 之前只在 trimmedPx > 0 时提示，于是「首次只回放 500 条」这种情况完全没提示，
  // 用户翻到顶以为到头了。
  function updateTrimTip() {
    trimEl.style.top = (st.reserveTop + 8) + 'px';
    var atTop = st.offset <= 2 && !st.atBottom;
    if (!atTop) {
      trimEl.style.display = 'none';
      return;
    }
    var text = '';
    if (st.loadingOlder) {
      text = '↑ 正在加载更早的记录…';
    } else if (st.histHasMore && !st.histDone) {
      text = '↑ 上面还有更早的记录';
    } else if (st.trimmedPx > 0) {
      text = st.histFile
        ? '↑ 已到内存缓冲起点（更早的在会话文件里）'
        : '↑ 更早的历史数据已被覆盖（本次会话未保存到文件）';
    }
    if (!text) {
      trimEl.style.display = 'none';
      return;
    }
    trimEl.textContent = text;
    trimEl.style.display = '';
  }

  // maybeLoadOlder 顶到最上面时向宿主再要一页更早的记录。
  //
  // 一次只要一页（一页就够一屏）：滚到顶再要下一页，用户不会察觉。正在加载时
  // 不会重复要；宿主回 hasMore=false 之后就不再问，否则滚到顶会变成 RPC 风暴。
  function maybeLoadOlder() {
    if (!st.live || st.dead || !st.id) return;
    if (st.loadingOlder || st.histDone || !st.histHasMore) return;
    // 设置里关掉「自动回补」：只留顶部提示，不主动要（网络差 / 超大历史时由人控）
    if (typeof opts.autoRefill === 'function' && !opts.autoRefill()) {
      updateTrimTip();
      return;
    }
    // 贴底时不加载：内容不满一屏时 offset 也是 0，那时候要「更早的」没有意义
    if (st.offset > 2 || st.atBottom) return;
    if (typeof opts.onNeedOlder !== 'function') return;
    if (!st.oldestTs) return;
    st.loadingOlder = true;
    updateTrimTip();
    var cur = { ts: st.oldestTs, same: st.oldestSame };
    Promise.resolve()
      .then(function () { return opts.onNeedOlder(cur); })
      .then(function (r) {
        st.loadingOlder = false;
        if (st.dead) return;
        if (!r || r.hasMore === false) {
          st.histHasMore = false;
          st.histDone = true;
        }
      })
      .catch(function () { st.loadingOlder = false; })
      .then(function () { if (!st.dead) updateTrimTip(); });
  }

  // 「回到底部」只在离开底部时出现；贴底时藏起来。
  function updateBottomBtn() {
    bottomBtn.style.display = st.atBottom ? 'none' : '';
  }

  function jumpBottom() {
    st.atBottom = true;
    // 贴底不能走 setOffset（那里会吸附到行首，永远差半行），直接落到 maxOffset。
    st.offset = Math.round(st.maxOffset);
    layerTransform();
    updateThumb();
    updateTrimTip();
    updateBottomBtn();
    if (needsFetch()) fetchWindow();
  }

  function refreshLive() {
    if (!st.id || st.dead) return Promise.resolve();
    var chars = layout();
    // 尺寸与样式没变就别再发 Configure：参数没变时 Go 侧也是早退，但跨桥
    // RPC 本身要钱，每 60 ms 一次是纯浪费。
    var cfgStep;
    if (st.needConfigure || chars !== st.cfgChars) {
      cfgStep = lvCall('Configure', st.id, chars, Math.round(st.tsH * 2),
        Math.round(st.dataH * 2), st.mode, dirModesNow(), 0, styleNow()).then(function (cfg) {
        st.cfgChars = chars;
        st.needConfigure = false;
        st.perLine = cfg.perLine;
        st.maxOffset = cfg.maxOffset;
        st.rowGen++;
      });
    } else {
      cfgStep = Promise.resolve();
    }
    return cfgStep.then(function () {
      // 取一屏：contentY 给个超大值，Go 侧夹到 maxOffset，正好是最后一条
      return lvCall('Window', st.id, 1 << 30, Math.round(st.vh), Math.round(st.vh));
    }).then(function (w) {
      applyWindow(w);
      st.maxOffset = w.maxOffset;
      if (follows()) st.offset = Math.round(w.maxOffset);
      layerTransform();
      updateThumb();
      updateTrimTip();
      st.lastSeen = st.total;
      if (typeof opts.onStatus === 'function') {
        opts.onStatus({ live: true, total: st.total, evicted: w.total - st.total });
      }
    }).catch(function (e) {
      showMsg('刷新失败：' + String(e && e.message ? e.message : e));
    });
  }

  // 事件驱动：数据帧一到（history.js 的 appendLine → notify）就合并，
  // 50 ms 内只拉一次 LiveInfo。空闲时没有任何定时器 —— 之前的 60 ms 轮询即使
  // 没数据也在跨桥问，一个核白掉三分之一。
  var tickTimer = null;
  var tickBusy = false;

  function notify() {
    if (st.dead || !st.live || !st.id || tickTimer) return;
    tickTimer = setTimeout(function () {
      tickTimer = null;
      tickLive();
    }, 50);
  }

  function tickLive() {
    if (st.dead || !st.live || !st.id || tickBusy) return;
    tickBusy = true;
    lvCall('LiveInfo', st.id).then(function (info) {
      if (st.dead) return;
      var grew = info.total !== st.total || Math.abs(info.contentH - st.contentH) > 0.5;
      var dh = (info.trimmedPx || 0) - st.trimmedPx;
      st.total = info.total;
      st.trimmedPx = info.trimmedPx || 0;
      // 往回翻的游标跟着文档走（裁头会让它前移）
      if (info.oldestTsMs) {
        st.oldestTs = info.oldestTsMs;
        st.oldestSame = info.oldestSameMs || 1;
      }
      if (typeof opts.onStatus === 'function') {
        opts.onStatus({ live: true, total: info.total, evicted: info.evicted });
      }
      if (!grew) return;
      if (follows()) { refreshLive(); return; }
      // 不贴底（或用户锁定了滚动）：画面不动，但内容还在长 —— 滚动条必须跟着
      // 刷新（滑块比例与位置都按新的总高算），否则看起来像「数据没进来」。
      if (info.contentH > 0) {
        st.contentH = info.contentH;
        st.maxOffset = Math.max(0, info.contentH - st.vh);
      }
      updateThumb();
      drawRuler(false);
      // 裁头会把内容整体往上抬，这里把 offset 同步减掉同样的高度，
      // 视口就锁在原地 —— 滚动条会自己缓慢上推。
      //
      // offset 已经贴到顶（0）时也必须走这里：内容还在被裁，顶部一直在变，
      // 不重取窗口的话画面会冻结在「某一时刻的顶部」，而实际最早那帧早就
      // 往前推了好几屏 —— 看起来就是「数据在进、画面不动」。
      if (dh > 0) {
        shiftBy(dh);
      } else {
        updateTrimTip();
      }
    }).catch(function () {}).then(function () {
      tickBusy = false;
    });
  }

  function openLive(sessionID) {
    st.live = true;
    st.atBottom = true;
    st.trimmedPx = 0;
    st.histFile = '';
    st.histHasMore = false;
    st.oldestTs = 0;
    st.oldestSame = 0;
    st.loadingOlder = false;
    st.histDone = false;
    updateTrimTip();
    updateBottomBtn();
    layout();
    return lvCall('OpenLive', sessionID).then(function (r) {
      st.id = r.id;
      return refreshLive();
    }).then(function () {
      // 打开时取一次概况（此时可能已经有帧），之后全靠数据帧事件驱动。
      tickLive();
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
    // 清空之后内容从零开始：视口也要算「贴底」，否则新数据进来时
    // tickLive 走的是「不贴底」分支，永远不重取窗口，画面会一直空着。
    st.atBottom = true;
    st.lastSeen = 0;
    st.trimmedPx = 0;
    st.oldestTs = 0;
    st.oldestSame = 0;
    st.loadingOlder = false;
    st.histDone = false;
    // 帧号从头开始，旧的行 DOM 不能再复用（否则会撞上同一 idx 的旧签名）
    resetRowEls();
    updateTrimTip();
    return lvCall('ResetLive', st.id).then(function () {
      return refreshLive();
    }).catch(function () {});
  }

  function appendSystem(text) {
    if (!st.id || st.dead) return;
    lvCall('AppendSystem', st.id, text).then(function () { notify(); }).catch(function () {});
  }

  // prepend 把更早的历史插到文档最前面（打开回放 / 往回翻补历史共用）。
  //
  // 前插之后所有内容下移了 delta 像素，把 offset 也加回去，画面才不会跳；
  // 贴底时照旧贴底。取窗口必须用 fetchWindow（按 offset 取），不能用 refreshLive
  // —— 后者永远取最后一屏，不贴底的人会被拉到别处。
  function prepend(frames) {
    if (!frames || !frames.length || !st.id || st.dead) return Promise.resolve();
    var h0 = st.contentH;
    lvLog('prepend in=' + frames.length + ' id=' + st.id + ' h0=' + h0);
    return lvCall('PrependFrames', st.id, frames).then(function (info) {
      if (st.dead || !info) return info;
      lvLog('prepend ok total=' + info.total + ' contentH=' + info.contentH + ' trimmed=' + info.trimmedPx);
      st.trimmedPx = info.trimmedPx || st.trimmedPx;
      if (info.oldestTsMs) {
        st.oldestTs = info.oldestTsMs;
        st.oldestSame = info.oldestSameMs || 1;
      }
      var delta = (info.contentH || 0) - h0;
      if (delta > 0) {
        if (st.atBottom) return refreshLive();
        st.offset += delta;
      }
      return fetchWindow().then(function () { updateTrimTip(); });
    }).catch(function () {});
  }

  // setHistory 记下这个会话有没有更早的历史：顶部提示和「要不要往回要一页」
  // 都按它分流。
  function setHistory(h) {
    st.histFile = (h && h.file) || '';
    st.histHasMore = !!(h && h.hasMore);
    st.histDone = false;
    updateTrimTip();
    maybeLoadOlder();
  }

  return {
    open: open,
    openLive: openLive,
    clear: clearAll,
    appendSystem: appendSystem,
    prepend: prepend,
    setHistory: setHistory,
    isLive: function () { return st.live; },
    // 数据帧到达时由 history.js 调用；事件驱动，空闲时没有定时器。
    notify: notify,
    jumpBottom: jumpBottom,
    // 锁定滚动：只改跟随行为，不动视口（用户原地看着哪就还在哪）。
    setScrollLock: function (on) {
      st.scrollLocked = !!on;
    },
    destroy: function () {
      st.dead = true;
      if (tickTimer) clearTimeout(tickTimer);
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
      // 换模式会改行高/内容，必须重发 Configure（refreshLive 里按这个标志判断）
      st.needConfigure = true;
      st.cfgChars = 0;
      if (st.live) return refreshLive();
      var anchor = st.rows.length ? st.rows[0].idx : 0;
      return configure(anchor).then(function () { return fetchWindow(); });
    },
    // reconfigure 只重发 Configure：按方向的模式（P1 / P2 / 发送）变了，
    // 整页的 mode 没变。折行与行高全在 Go 侧算，所以必须走一次完整重算。
    reconfigure: function () {
      st.needConfigure = true;
      st.cfgChars = 0;
      if (st.live) return refreshLive();
      var anchor = st.rows.length ? st.rows[0].idx : 0;
      return configure(anchor).then(function () { return fetchWindow(); });
    },
    setShowYear: function (on) {
      st.showYear = !!on;
      // 只影响时间戳串，不用重算布局：重画可见行的时间戳即可。
      // 行 DOM 是按帧号存的（增量渲染），不能再按下标猜 layer.children。
      for (var i = 0; i < st.rows.length; i++) {
        var rec = rowEls[st.rows[i].idx];
        var rowEl = rec && rec.el;
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
