// ---- 会话页剩下的三件事 ----
//
// 数据行、滚动条、历史缓存、未读块、分页加载全部搬到 Go 侧的 stlogview 引擎
// （Go: stlogview/ + logview.go，前端: logview.js）。这个文件只剩三样：
//
//   1. 三个入口 —— 数据帧 / 系统消息 / 清空，原样转给引擎
//   2. 发送框的自绘滚动条（那是输入框自己的横向滚动，与显示区无关）
//   3. Tx 来源角标的显示名（logview.js 造行时要用）
//
// 别再往这里加渲染逻辑：显示区已经完完全全归引擎了。

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

// ---- 引擎的三个入口 ----
//
// 数据本身不用前端搬：Go 侧的 emitSession 每帧都会喂给实时文档
// （logview.appendSession），前端只需要催一次「重新取窗口」。

function appendLine(dir, msg) {
  var ownerTab = null;
  if (msg && msg.processId) {
    for (var i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i].sessionId === msg.processId) { ownerTab = state.tabs[i]; break; }
    }
  }
  if (!ownerTab || !ownerTab._live) return;
  ownerTab._live.notify();
}

// 系统消息（连上 / 断开 / 统计启动）走 dir=system 的一条记录，不按 Hex 画。
// 引擎还没挂上时直接丢掉也没关系：系统帧本来也会进守护进程的历史环，回放时补出来。
function appendSystemMsg(msg) {
  var tab = (typeof getActiveTab === 'function') ? getActiveTab() : null;
  if (!tab || !tab._live || typeof tab._live.appendSystem !== 'function') return;
  tab._live.appendSystem(typeof formatSysMsg === 'function' ? formatSysMsg(msg) : msg);
}

// clearDisplay 只清界面：守护进程的环和磁盘文件都不动。
function clearDisplay() {
  var tab = getActiveTab();
  if (!tab || !tab.sessionId) {
    var box = pageEl('displayContent');
    if (box) box.innerHTML = '';
    return;
  }
  if (!tab._live || typeof tab._live.clear !== 'function') return;
  tab._live.clear();
  // 统计基线归零：清空之后界面上重新从 0 数（守护进程侧的累计值不受影响）。
  state.statsBase[tab.sessionId] = { tx: 0, rx: 0 };
  refreshStatsDOM();
}

// ---- 发送框的自绘滚动条 ----
//
// 原生条隐藏（CSS），轨道与滑块只按真实内容计算。显示区不用它了 —— 那边是
// 引擎自己的两道轨道 + 泳道。

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
  }

  box._syncScrollbar = sync;
  box._scrollbarTrack = track;

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
  function endDrag() { drag = null; }
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

// 发送框滚动条：上沿从工具栏底边（40px）开始。
function initSendScrollbar(box) {
  _initOverlayScrollbar(box, { className: 'is-send', top: 40 });
}
