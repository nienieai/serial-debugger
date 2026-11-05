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
				btn.setAttribute('data-tip', t('btn.stop_daemon', '停止守护进程'));
			} else {
				dot.className = 'status-dot is-offline';
				// 「未运行」而不是「检测中」：离线态没有任何轮询在跑，写「检测中」
				// 会让人以为程序卡在探测里。
				text.textContent = t('status.offline', '未运行');
				btn.setAttribute('data-tip', t('btn.start_daemon', '启动守护进程'));
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

// ---- 状态栏上的「可点项」----
//
// Tab / 编码 / 换行 / 格式解析 / 保存 五项都能点：点击在**这一项正上方**弹一个浮层
// （menu.js 的 cMenuOpen，和右键菜单同一套皮肤），列表里打勾选当前值 —— 和 VS Code
// 底部状态栏的手感一致。原来这五项是纯文字，想改只能翻「标签页设置」对话框。
function _sbClickable(span, open) {
	span.classList.add('is-click');
	span.setAttribute('role', 'button');
	span.tabIndex = 0;
	span.addEventListener('click', function (e) {
		e.stopPropagation();
		if (cMenuIsOpen()) { cMenuClose(); return; }
		open(span);
	});
	span.addEventListener('keydown', function (e) {
		if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); span.click(); }
	});
}

// _sbPick 是这五项共用的「勾选列表」菜单。
//   key    disp 里的字段名（Tab / 编码 / 换行 / 格式解析）
//   opts   [{ value, label }]
function _sbPick(anchor, key, opts, after) {
	var cur = _dispOf()[key];
	cMenuOpen({
		anchor: anchor,
		placement: 'up',
		items: opts.map(function (o) {
			return {
				label: o.label,
				checked: cur === o.value,
				onClick: function () {
					_setDisp(key, o.value);
					if (typeof after === 'function') after();
					if (typeof _syncDispUi === 'function') _syncDispUi();
				}
			};
		})
	});
}

// ---- StatusTabSize: tab size display ----
function createStatusTabSize(container) {
	var span = document.createElement('span');
	// dsp-item：这几项属于「某一条会话」，没有会话的页面（设置 / 选择页）要藏起来
	span.className = 'stat-item dsp-item';
	span.textContent = 'Tab:4';
	span.setAttribute('data-tip', t('status.tip_tab', '制表符宽度（每行显示几个格）'));
	container.appendChild(span);
	_sbClickable(span, function (anchor) {
		_sbPick(anchor, 'tabSize', [
			{ value: 2, label: t('status.tab_pick', '{n} 个空格').replace('{n}', 2) },
			{ value: 4, label: t('status.tab_pick', '{n} 个空格').replace('{n}', 4) },
			{ value: 8, label: t('status.tab_pick', '{n} 个空格').replace('{n}', 8) }
		], function () {
			if (typeof _applyDisplayStyle === 'function') _applyDisplayStyle();
		});
	});

	return {
		update(data) { span.textContent = 'Tab:' + (data.tabSize || 4); },
	};
}

// ---- StatusEol: line ending display ----
function createStatusEol(container) {
	var span = document.createElement('span');
	span.className = 'stat-item dsp-item';
	span.textContent = 'LF';
	span.setAttribute('data-tip', t('status.tip_eol', '行尾序列'));
	container.appendChild(span);
	_sbClickable(span, function (anchor) {
		_sbPick(anchor, 'eolSequence', [
			{ value: 'lf', label: 'LF' },
			{ value: 'cr', label: 'CR' },
			{ value: 'crlf', label: 'CRLF' }
		]);
	});

	return {
		update: function() {
			var labels = { lf: 'LF', cr: 'CR', crlf: 'CRLF' };
			var v = (typeof _dispOf === 'function' ? _dispOf().eolSequence : 'lf') || 'lf';
			span.textContent = labels[v] || v.toUpperCase();
		},
	};
}

// ---- StatusEncoding: current encoding label ----
function createStatusEncoding(container) {
	var span = document.createElement('span');
	span.className = 'stat-item dsp-item';
	span.textContent = 'UTF-8';
	span.setAttribute('data-tip', t('status.tip_encoding', '文本编码'));
	container.appendChild(span);
	_sbClickable(span, function (anchor) {
		_sbPick(anchor, 'encoding', [
			{ value: 'utf-8', label: 'UTF-8' },
			{ value: 'gb2312', label: 'GB2312' },
			{ value: 'ascii', label: 'ASCII' }
		], function () {
			if (typeof updateEncodingDisplay === 'function') updateEncodingDisplay();
		});
	});

	return {
		update(data) {
			var labels = { 'utf-8': 'UTF-8', 'ascii': 'ASCII', 'gb2312': 'GB2312' };
			var enc = data.encoding || 'utf-8';
			span.textContent = labels[enc] || String(enc).toUpperCase();
		},
	};
}

// ---- StatusParse: 格式解析（无解析 / 按分隔符切帧 / Modbus RTU）----
//
// 0.9.1 只做到「选项 + 状态栏显示」：这一项只影响标签页自己的 disp.parseMode，
// 不改任何历史行（解析接进渲染是下一步）。所以菜单底下挂一条禁用提示，
// 免得选完发现显示没变、以为坏了。
function createStatusParse(container) {
	var span = document.createElement('span');
	span.className = 'stat-item dsp-item';
	span.textContent = t('status.parse_none', '无解析');
	span.setAttribute('data-tip', t('status.tip_parse', '格式解析方式'));
	container.appendChild(span);

	function labelOf(v) {
		if (v === 'delim') return t('status.parse_delim', '按分隔符切帧');
		if (v === 'modbus') return t('status.parse_modbus', 'Modbus RTU');
		return t('status.parse_none', '无解析');
	}

	_sbClickable(span, function (anchor) {
		var cur = (typeof _dispOf === 'function' ? _dispOf().parseMode : 'none') || 'none';
		cMenuOpen({
			anchor: anchor,
			placement: 'up',
			items: [
				{ label: t('status.parse_none', '无解析'), checked: cur === 'none',
					onClick: function () { _setDisp('parseMode', 'none'); span.textContent = labelOf('none'); } },
				{ label: t('status.parse_delim', '按分隔符切帧'), checked: cur === 'delim',
					onClick: function () { _setDisp('parseMode', 'delim'); span.textContent = labelOf('delim'); } },
				{ label: t('status.parse_modbus', 'Modbus RTU'), checked: cur === 'modbus',
					onClick: function () { _setDisp('parseMode', 'modbus'); span.textContent = labelOf('modbus'); } },
				{ sep: true },
				{ label: t('status.parse_soon', '解析显示还没接上（只记选择）'), disabled: true }
			]
		});
	});

	return {
		update: function() {
			var v = (typeof _dispOf === 'function' ? _dispOf().parseMode : 'none') || 'none';
			span.textContent = labelOf(v);
		},
	};
}

// ---- StatusSave: 保存状态（图标 + 绿点）----
//
// 绿点 = 这个标签页正在把历史落盘。点击弹「保存历史」开关 + 打开保存目录。
// 状态来源是 tab.histOn（守护进程的真实开关），未知时先把点藏起来，点菜单时再问。
function createStatusSave(container) {
	var span = document.createElement('span');
	span.className = 'stat-item dsp-item is-save';
	// 图标外面裹一层定位参照：绿点绝对定位在图标**右下角**。
	//
	// 必须能在图标到货后重画：icons.js 的图标是 wails-ready 之后异步拉的，而
	// initStatusBar 跑在那之前 —— 直接 icon('save') 拿到的是「缺图标」的方框叉，
	// 而且之后不会自己重画（表现就是状态栏里一颗 ⊠，看着像图标包缺 key）。
	function paintGlyph() {
		span.innerHTML = '<span class="c-save-glyph">' + icon('save', 13, 13) + '<i class="stat-save-dot"></i></span>';
	}
	paintGlyph();
	if (window._iconsReady && typeof window._iconsReady.then === 'function') {
		window._iconsReady.then(paintGlyph).catch(function () {});
	}
	span.setAttribute('data-tip', t('status.save', '保存历史'));
	container.appendChild(span);

	function activeTab() {
		return (typeof getActiveTab === 'function') ? getActiveTab() : null;
	}
	function paint() {
		var tab = activeTab();
		var on = !!(tab && tab.histOn === true);
		span.classList.toggle('is-on', on);
		span.setAttribute('data-tip', on ? t('status.save_on', '保存已开启') : t('status.save_off', '保存已关闭'));
	}
	function setSave(tab, on) {
		if (!tab || !tab.sessionId) return;
		tab.histOn = !!on;
		if (typeof _liveRecording === 'function') _liveRecording(tab, !!on);
		try {
			window.go.main.App.SetHistoryEnabledFor(tab.sessionId, !!on).catch(function () {});
		} catch (_) {}
		paint();
	}

	_sbClickable(span, function (anchor) {
		var tab = activeTab();
		var canSave = !!(tab && tab.sessionId);
		var menu = cMenuOpen({
			anchor: anchor,
			placement: 'up',
			items: [
				{
					label: t('status.save', '保存历史'),
					checked: !!(tab && tab.histOn === true),
					disabled: !canSave,
					onClick: function () { setSave(tab, !(tab && tab.histOn === true)); }
				},
			]
		});
		// 本地还不知道开关时，问一次守护进程：拿到就点亮绿点并更新菜单的勾选。
		if (canSave && tab.histOn === undefined) {
			try {
				window.go.main.App.GetHistoryStatusFor(tab.sessionId).then(function (on) {
					tab.histOn = !!on;
					if (typeof _liveRecording === 'function') _liveRecording(tab, !!on);
					paint();
					var chk = menu && menu.querySelector('.menu-chk');
					if (chk) chk.classList.toggle('on', !!on);
				}).catch(function () {});
			} catch (_) {}
		}
	});

	paint();
	return { update: paint };
}

// ---- StatusStats: Tx / Rx bytes + speed ----
function createStatusStats(container) {
	// 方向箭头沿用项目既有约定：TX = ↾，RX = ⇃（见 history.js 的时间戳前缀
	// 与设置页的颜色预览）。状态栏只放箭头 + TX/RX 缩写，不放「发送/接收」
	// 这类词——右下角空间有限，且各语言的缩写本来就是通用的。
	var TX_ARROW = '↾';
	var RX_ARROW = '⇃';

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

	var txItem = makeItem('tx', TX_ARROW + ' TX');
	var rxItem = makeItem('rx', RX_ARROW + ' RX');
	var txSpeedItem = makeItem('tx', TX_ARROW + ' TX');
	var rxSpeedItem = makeItem('rx', RX_ARROW + ' RX');
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
					el.textContent = RX_ARROW + ' ' + t('stat.rx', 'RX');
					el.className = 'stat-arrow rx';
				} else {
					el.textContent = TX_ARROW + ' ' + t('stat.tx', 'TX');
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
		if (pct >= 90) cls = 'rate-red';
		else if (pct >= 80) cls = 'rate-orange';
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
			// 走同一个格式化函数：此前这里硬编码 '0 B/s(0%)'，与正常路径
			// 产出的 '0/s (0%)' 不一致（同一块区域两种写法）
			var zero = fmtSpeed(0, 0);
			applySpeed(txSpeedItem.val, zero);
			applySpeed(rxSpeedItem.val, zero);
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
	var parse = createStatusParse(right);
	parse.update();
	var save = createStatusSave(right);
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
		parse: parse,
		save: save,
		stats: stats,

		updateDaemon: function(data)       { daemon.update(data); },
		updateClients: function(data)      { clients.update(data); },
		updateCursor: function()           { cursor.updatePosition(); },
		updateTabSize: function(data)      { tabSize.update(data); },
		updateEncoding: function(data)     { encoding.update(data); },
		updateEol: function()              { eol.update(); },
		updateParse: function()            { parse.update(); },
		updateSave: function()             { save.update(); },
		updateStatsBytes: function(data)   { stats.updateBytes(data); },
		updateStatsRates: function(data)   { stats.updateRates(data); },
		resetStats: function()             { stats.reset(); },
		setStatsMode: function(isFwd)      { stats.setMode(isFwd); },
	};
	return _statusBar;
}
