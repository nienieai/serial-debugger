// 守护进程日志标签页（菜单「守护进程 → 查看守护进程日志」）。
//
// 数据只来自 <exe>/logs/daemon.log：daemon 照旧自己写文件，这边按偏移量增量读
// （App.ReadDaemonLog）。走文件而不是事件流，是为了能看见 GUI 启动**之前**就已经
// 发生的事 —— 排查「守护进程起不来」时，恰恰是那些行最关键。
//
// 轮询只在标签可见时跑：切走就停，切回来先补读再继续。
//
// 依赖 app.js 的全局：state / switchTab / renderTabs / updateOpenBtn /
// updateEmptyState / getActivePage / t / saveSettings。

var DAEMONLOG_POLL_MS = 800;     // 增量轮询间隔
var DAEMONLOG_MAX_LINES = 5000;  // DOM 里最多留这么多行（2 MB 全渲染会卡死）

// _dl 是单例状态。标签关了要置空，否则下次开页会认着已经销毁的 DOM 写。
var _dl = null;

function _dlT(key, fb) {
  return (typeof t === 'function') ? t(key, fb) : fb;
}

function _dlFmtSize(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1024 / 1024).toFixed(2) + ' MB';
}

// 日志类别 -> 着色类名。类别由守护进程写死（daemon/logOp 的第一个参数），
// 但日志语言可配（daemon.ini），所以中英文都要认。
function _dlCatClass(cat) {
  var c = String(cat || '').trim().toLowerCase();
  if (c.indexOf('错误') >= 0 || c.indexOf('error') >= 0) return 'is-error';
  if (c.indexOf('警告') >= 0 || c.indexOf('warn') >= 0) return 'is-warn';
  return '';
}

// _dlRow 把一行日志画成「时间戳 + 类别 + 正文」三段。
// 对不上格式的行（续行、外部追加的内容）原样显示，不要吞掉。
function _dlRow(text) {
  var row = document.createElement('div');
  row.className = 'c-daemonlog__row';
  var m = /^(\d{2}:\d{2}:\d{2}\.\d{3})\s+\[([^\]]*)\]\s?([\s\S]*)$/.exec(text);
  if (!m) {
    row.textContent = text;
    return row;
  }
  var ts = document.createElement('span');
  ts.className = 'c-daemonlog__ts';
  ts.textContent = m[1];
  var cat = document.createElement('span');
  cat.className = 'c-daemonlog__cat ' + _dlCatClass(m[2]);
  cat.textContent = '[' + m[2].trim() + ']';
  var msg = document.createElement('span');
  msg.className = 'c-daemonlog__msg';
  msg.textContent = m[3];
  row.appendChild(ts);
  row.appendChild(cat);
  row.appendChild(msg);
  return row;
}

function _dlAppend(text) {
  if (!_dl || !text) return;
  var parts = String(text).split('\n');
  if (parts.length && parts[parts.length - 1] === '') parts.pop();
  if (!parts.length) return;

  var frag = document.createDocumentFragment();
  for (var i = 0; i < parts.length; i++) frag.appendChild(_dlRow(parts[i]));
  _dl.box.appendChild(frag);
  _dl.lines += parts.length;

  // 只在跟随时裁旧行：否则用户往上翻到一半，删头会把视图顶跳。
  while (_dl.follow && _dl.lines > DAEMONLOG_MAX_LINES && _dl.box.firstChild) {
    _dl.box.removeChild(_dl.box.firstChild);
    _dl.lines--;
  }
  if (_dl.follow) {
    // 记下「这次滚动是我自己做的」：一次性追加大批行时，scrollTop 赋值会立刻
    // 触发 scroll 事件，而那时布局还没落定，下面的判定会算出「不在底部」，
    // 于是刚打开的页面自己把跟随关掉、停在最顶上（实测）。
    _dl.autoScrollAt = Date.now();
    _dl.box.scrollTop = _dl.box.scrollHeight;
  }
}

function _dlSetStatus(msg) {
  if (!_dl) return;
  _dl.stateEl.textContent = msg || '';
  _dl.stateEl.classList.toggle('is-hidden', !msg);
}

function _dlPoll(first) {
  if (!_dl) return;
  var App = window.go && window.go.main && window.go.main.App;
  if (!App || typeof App.ReadDaemonLog !== 'function') {
    _dlSetStatus(_dlT('daemonlog.unavailable', '这个版本没有读日志的接口（老 exe？）'));
    return;
  }
  var offset = first ? -1 : _dl.offset;
  App.ReadDaemonLog(offset, 0).then(function (res) {
    if (!_dl || !res) return;
    if (res.path) _dl.pathEl.textContent = res.path;
    _dl.sizeEl.textContent = _dlFmtSize(res.size);
    if (res.missing) {
      _dlSetStatus(_dlT('daemonlog.missing', '日志文件还没生成（守护进程没启动过？）'));
      return;
    }
    _dlSetStatus('');
    if (res.rotated) {
      _dlAppend(_dlT('daemonlog.rotated', '—— 日志已轮转（守护进程重启过），以下从新文件开头 ——'));
    }
    _dl.offset = res.next || 0;
    _dlAppend(res.text || '');
  }).catch(function (err) {
    if (!_dl) return;
    _dlSetStatus(String(err));
  });
}

function _dlStartPolling() {
  if (!_dl || _dl.timer) return;
  _dlPoll(_dl.offset === null);   // 第一次打开取末尾，之后按偏移量续读
  _dl.timer = setInterval(function () { _dlPoll(false); }, DAEMONLOG_POLL_MS);
}

function _dlStopPolling() {
  if (_dl && _dl.timer) {
    clearInterval(_dl.timer);
    _dl.timer = null;
  }
}

// openDaemonLogTab 打开（或切回）守护进程日志标签页。单例。
function openDaemonLogTab() {
  if (_dl && _dl.tab) {
    for (var i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i] === _dl.tab) { switchTab(i); return; }
    }
    // 标签被关过：状态作废，重建
    _dlStopPolling();
    _dl = null;
  }

  var page = document.createElement('div');
  page.className = 'tab-page';
  page.style.display = 'none';

  // 顶栏：路径 / 大小 / 状态 + 按钮组。布局与文件页同构（o-stlog-bar 那套）。
  var bar = document.createElement('div');
  bar.className = 'o-daemonlog-bar t-daemonlog-bar';
  var infoBox = document.createElement('div');
  infoBox.className = 'o-daemonlog-bar__info';
  var nameEl = document.createElement('div');
  nameEl.className = 't-daemonlog-name';
  nameEl.textContent = _dlT('daemonlog.title', '守护进程日志');
  var pathEl = document.createElement('div');
  pathEl.className = 't-daemonlog-meta';
  pathEl.textContent = '…';
  infoBox.appendChild(nameEl);
  infoBox.appendChild(pathEl);

  var actions = document.createElement('div');
  actions.className = 'o-daemonlog-bar__actions';

  var sizeEl = document.createElement('span');
  sizeEl.className = 't-daemonlog-meta';
  sizeEl.textContent = '0 B';
  var stateEl = document.createElement('span');
  stateEl.className = 't-daemonlog-meta is-hidden';

  var followBtn = document.createElement('button');
  followBtn.className = 't-daemonlog-btn is-on';
  followBtn.textContent = _dlT('daemonlog.follow', '跟随');
  followBtn.title = _dlT('daemonlog.follow_tip', '自动滚到最新一行；往上翻会自动关掉');

  var clearBtn = document.createElement('button');
  clearBtn.className = 't-daemonlog-btn';
  clearBtn.textContent = _dlT('daemonlog.clear', '清空');
  clearBtn.title = _dlT('daemonlog.clear_tip', '只清空显示，不动日志文件');

  var copyBtn = document.createElement('button');
  copyBtn.className = 't-daemonlog-btn';
  copyBtn.textContent = _dlT('daemonlog.copy', '复制');
  copyBtn.title = _dlT('daemonlog.copy', '复制');

  var dirBtn = document.createElement('button');
  dirBtn.className = 't-daemonlog-btn';
  dirBtn.textContent = _dlT('daemonlog.open_dir', '目录');
  dirBtn.title = _dlT('daemonlog.open_dir', '目录');

  actions.appendChild(stateEl);
  actions.appendChild(sizeEl);
  actions.appendChild(followBtn);
  actions.appendChild(clearBtn);
  actions.appendChild(copyBtn);
  actions.appendChild(dirBtn);
  bar.appendChild(infoBox);
  bar.appendChild(actions);
  page.appendChild(bar);

  var box = document.createElement('div');
  box.className = 'o-daemonlog-rows c-daemonlog t-daemonlog-rows';
  page.appendChild(box);

  var tab = {
    id: state.tabCounter++,
    label: _dlT('daemonlog.title', '守护进程日志'),
    type: 'daemonlog',
    portOpen: false,
    mode: 'single',
    source: 'gui',
    _page: {
      root: page,
      show: function () { page.style.display = ''; _dlStartPolling(); },
      hide: function () { page.style.display = 'none'; _dlStopPolling(); },
      destroy: function () {
        _dlStopPolling();
        if (_dl && _dl.tab === tab) _dl = null;
        page.remove();
      }
    }
  };

  _dl = {
    tab: tab, page: page, box: box, pathEl: pathEl, sizeEl: sizeEl, stateEl: stateEl,
    followBtn: followBtn, offset: null, lines: 0, follow: true, timer: null
  };

  // 往上翻离开底部就关跟随；滚回底部再自动打开。
  box.addEventListener('scroll', function () {
    if (!_dl) return;
    // 自己刚贴过底：这一批 scroll 事件不算用户操作
    if (Date.now() - (_dl.autoScrollAt || 0) < 250) return;
    var nearBottom = (box.scrollHeight - box.scrollTop - box.clientHeight) < 24;
    if (nearBottom === _dl.follow) return;
    _dl.follow = nearBottom;
    followBtn.classList.toggle('is-on', nearBottom);
  });

  followBtn.addEventListener('click', function () {
    if (!_dl) return;
    _dl.follow = !_dl.follow;
    followBtn.classList.toggle('is-on', _dl.follow);
    if (_dl.follow) box.scrollTop = box.scrollHeight;
  });

  clearBtn.addEventListener('click', function () {
    if (!_dl) return;
    box.innerHTML = '';
    _dl.lines = 0;
  });

  copyBtn.addEventListener('click', function () {
    var text = box.innerText || '';
    try {
      if (window.runtime && window.runtime.ClipboardSetText) window.runtime.ClipboardSetText(text);
      else if (navigator.clipboard) navigator.clipboard.writeText(text);
    } catch (e) { /* 复制失败不打断使用 */ }
  });

  dirBtn.addEventListener('click', function () {
    var App = window.go && window.go.main && window.go.main.App;
    if (App && typeof App.ShowFolderInExplorer === 'function') {
      App.ShowFolderInExplorer('logs').then(function (res) {
        if (res && res.ok === false) _dlSetStatus(String(res.error || '打开目录失败'));
      }).catch(function (err) { _dlSetStatus(String(err)); });
    }
  });

  var oldPage = getActivePage();
  if (oldPage) oldPage.hide();
  if (state._welcomePage) state._welcomePage.hide();

  state.tabs.push(tab);
  var mc = document.getElementById('mainContent');
  if (mc) mc.appendChild(page);

  state.activeTab = state.tabs.length - 1;
  state.currentMode = 'single';
  page.style.display = '';
  renderTabs();
  updateOpenBtn();
  updateEmptyState();
  _dlStartPolling();
}
