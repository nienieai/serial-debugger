// ---- Status Bar Components ----
// Each component owns its DOM subtree and exposes update(data).
// StatusBar is the top-level orchestrator, initialized by initStatusBar().

// ---- StatusDaemon: button-like combined dot + text ----
function createStatusDaemon(container) {
	var btn = document.createElement('button');
	btn.className = 'c-status-btn';
	btn.addEventListener('click', toggleDaemon);

	var dot = document.createElement('span');
	dot.className = 'status-dot';
	btn.appendChild(dot);

	var text = document.createElement('span');
	btn.appendChild(text);

	container.appendChild(btn);

	return {
		update(data) {
			if (data.online) {
				dot.className = 'status-dot is-online';
				text.textContent = data.tabsCount === 0
					? t('status.no_session', '无活动会话')
					: t('status.online', '运行中');
				btn.title = t('btn.stop_daemon', '停止守护进程');
			} else {
				dot.className = 'status-dot is-offline';
				text.textContent = t('status.detecting', '检测中...');
				btn.title = t('btn.start_daemon', '启动守护进程');
			}
		},
		setBusy: function(busy) { btn.disabled = busy; },
		root: btn,
	};
}

// ---- StatusClients: GUI / CLI / MCP badges ----
function createStatusClients(container) {
	function makeBadge(cls) {
		var span = document.createElement('span');
		span.className = 'client-badge ' + cls;
		span.style.display = 'none';
		container.appendChild(span);
		return span;
	}

	var guiBadge = makeBadge('gui-badge');
	var cliBadge = makeBadge('cli-badge');
	var mcpBadge = makeBadge('mcp-badge');

	return {
		update(data) {
			var setBadge = function(el, label, count) {
				if (label === 'GUI' && count <= 1) { el.style.display = 'none'; return; }
				if (count > 0) { el.textContent = label + ':' + count; el.style.display = 'inline'; }
				else { el.style.display = 'none'; }
			};
			setBadge(guiBadge, 'GUI', data.gui || 0);
			setBadge(cliBadge, 'CLI', data.cli || 0);
			setBadge(mcpBadge, 'MCP', data.mcp || 0);
		},
	};
}

// ---- StatusCursor: line/col display, hidden when send input not focused ----
function createStatusCursor(container) {
	var span = document.createElement('span');
	span.className = 'stat-item stat-cursor';
	span.style.display = 'none';
	container.appendChild(span);

	function _update() {
		var ta = typeof pageEl === 'function' ? pageEl('sendInput') : document.getElementById('sendInput');
		if (!ta || document.activeElement !== ta) { span.style.display = 'none'; return; }
		var val = ta.value;
		var pos = ta.selectionStart || 0;
		var line = 1, col = 1;
		for (var i = 0; i < pos; i++) {
			if (val[i] === '\n') { line++; col = 1; }
			else { col++; }
		}
		span.textContent = t('status.cursor_fmt','行{0},列{1}').replace('{0}', line).replace('{1}', col);
		span.style.display = 'inline';
	}

	return {
		update: _update,
		updatePosition: _update,
		hide: function() { span.style.display = 'none'; },
	};
}

// ---- StatusTabSize: tab size display ----
function createStatusTabSize(container) {
	var span = document.createElement('span');
	span.className = 'stat-item';
	span.textContent = 'Tab:4';
	container.appendChild(span);

	return {
		update(data) { span.textContent = 'Tab:' + (data.tabSize || 4); },
	};
}

// ---- StatusEol: line ending display ----
function createStatusEol(container) {
	var span = document.createElement('span');
	span.className = 'stat-item';
	span.textContent = 'LF';
	container.appendChild(span);

	return {
		update: function() {
			var labels = { lf: 'LF', cr: 'CR', crlf: 'CRLF' };
			var v = (typeof state !== 'undefined' && state.eolSequence) ? state.eolSequence : 'lf';
			span.textContent = labels[v] || v.toUpperCase();
		},
	};
}

// ---- StatusEncoding: current encoding label ----
function createStatusEncoding(container) {
	var span = document.createElement('span');
	span.className = 'stat-item';
	span.textContent = 'UTF-8';
	container.appendChild(span);

	return {
		update(data) {
			var labels = { 'utf-8': 'UTF-8', 'ascii': 'ASCII', 'gb2312': 'GB2312' };
			span.textContent = labels[data.encoding] || data.encoding.toUpperCase();
		},
	};
}

// ---- StatusStats: Tx / Rx bytes + speed ----
function createStatusStats(container) {
	function makeItem(defaultCls, defaultLabel) {
		var item = document.createElement('span');
		item.className = 'stat-item';
		var arrow = document.createElement('span');
		arrow.className = 'stat-arrow ' + defaultCls;
		arrow.textContent = defaultLabel;
		var sep = document.createTextNode(':');
		var val = document.createElement('span');
		val.className = 'stat-val';
		val.textContent = '0';
		item.appendChild(arrow);
		item.appendChild(sep);
		item.appendChild(val);
		container.appendChild(item);
		return { arrow: arrow, val: val };
	}

	var txItem = makeItem('tx', 'Tx');
	var rxItem = makeItem('rx', 'Rx');
	var txSpeedItem = makeItem('tx', 'Tx');
	var rxSpeedItem = makeItem('rx', 'Rx');
	txSpeedItem.val.textContent = '0 B/s(0%)';
	rxSpeedItem.val.textContent = '0 B/s(0%)';

	var _isFwd = false;
	var _arrows = [txItem.arrow, rxItem.arrow, txSpeedItem.arrow, rxSpeedItem.arrow];

	function setMode(isFwd) {
		_isFwd = isFwd;
		_arrows.forEach(function(el) {
			if (isFwd) {
				if (el.classList.contains('tx') || el.classList.contains('p1')) {
					el.textContent = t('stat.p1', 'P1');
					el.className = 'stat-arrow p1';
				} else {
					el.textContent = t('stat.p2', 'P2');
					el.className = 'stat-arrow p2';
				}
			} else {
				if (el.classList.contains('rx') || el.classList.contains('p2')) {
					el.textContent = t('stat.rx', 'Rx');
					el.className = 'stat-arrow rx';
				} else {
					el.textContent = t('stat.tx', 'Tx');
					el.className = 'stat-arrow tx';
				}
			}
		});
	}

	function fmtSpeed(val, maxVal) {
		var text = fmtBytes(val) + '/s';
		if (!maxVal || maxVal <= 0) return { text: text + ' (0%)', cls: '' };
		var pct = Math.round(val / maxVal * 100);
		var cls = '';
		if (pct >= 90) cls = 'rate-high';
		else if (pct >= 80) cls = 'rate-warn';
		return { text: text + ' (' + pct + '%)', cls: cls };
	}

	function applySpeed(el, sp) {
		el.textContent = sp.text;
		el.className = 'stat-val' + (sp.cls ? ' ' + sp.cls : '');
	}

	return {
		updateBytes(data) {
			if (_isFwd) {
				txItem.val.textContent = fmtBytes(data.p1 || 0);
				rxItem.val.textContent = fmtBytes(data.p2 || 0);
			} else {
				txItem.val.textContent = fmtBytes(data.tx || 0);
				rxItem.val.textContent = fmtBytes(data.rx || 0);
			}
		},
		updateRates(data) {
			var maxBps = data.maxBps || 0;
			if (_isFwd) {
				applySpeed(txSpeedItem.val, fmtSpeed(data.p1Rate || 0, maxBps));
				applySpeed(rxSpeedItem.val, fmtSpeed(data.p2Rate || 0, maxBps));
			} else {
				applySpeed(txSpeedItem.val, fmtSpeed(data.txRate || 0, maxBps));
				applySpeed(rxSpeedItem.val, fmtSpeed(data.rxRate || 0, maxBps));
			}
		},
		setMode: setMode,
		reset() {
			txItem.val.textContent = '0';
			rxItem.val.textContent = '0';
			txSpeedItem.val.textContent = '0 B/s(0%)';
			txSpeedItem.val.className = 'stat-val';
			rxSpeedItem.val.textContent = '0 B/s(0%)';
			rxSpeedItem.val.className = 'stat-val';
		},
		root: container,
		_isFwdRef: function() { return _isFwd; },
	};
}

// ---- StatusBar: top-level orchestrator ----
var _statusBar = null;

function initStatusBar() {
	var bar = document.getElementById('statusbar');
	if (!bar) return null;
	bar.innerHTML = '';

	var left = document.createElement('div');
	left.className = 'statusbar-left';
	var right = document.createElement('div');
	right.className = 'statusbar-right';

	var daemon = createStatusDaemon(left);
	var clients = createStatusClients(left);

	var cursor = createStatusCursor(right);
	var tabSize = createStatusTabSize(right);
	var encoding = createStatusEncoding(right);
	var eol = createStatusEol(right);
	eol.update();
	var statsWrap = document.createElement('span');
	statsWrap.id = 'statusbar-stats';
	right.appendChild(statsWrap);
	var stats = createStatusStats(statsWrap);

	bar.appendChild(left);
	bar.appendChild(right);

	document.addEventListener('focusin', function(e) {
		if (e.target && e.target.id === 'sendInput') cursor.updatePosition();
	});
	document.addEventListener('focusout', function(e) {
		if (e.target && e.target.id === 'sendInput') cursor.hide();
	});

	_statusBar = {
		daemon: daemon,
		clients: clients,
		cursor: cursor,
		tabSize: tabSize,
		encoding: encoding,
		eol: eol,
		stats: stats,

		updateDaemon: function(data)       { daemon.update(data); },
		updateClients: function(data)      { clients.update(data); },
		updateCursor: function()           { cursor.updatePosition(); },
		updateTabSize: function(data)      { tabSize.update(data); },
		updateEncoding: function(data)     { encoding.update(data); },
		updateEol: function()              { eol.update(); },
		updateStatsBytes: function(data)   { stats.updateBytes(data); },
		updateStatsRates: function(data)   { stats.updateRates(data); },
		resetStats: function()             { stats.reset(); },
		setStatsMode: function(isFwd)      { stats.setMode(isFwd); },
	};
	return _statusBar;
}
