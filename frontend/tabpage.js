// ---- TabPage Component ----
// Each tab owns its complete DOM subtree. Switch tab = show/hide.
// Depends on: state (app.js), t()/formatSysMsg() (i18n.js), decoder functions.

// Global baud rates — used here and in settings popup.
var BAUDS = ['300','1200','2400','4800','9600','19200','38400','57600','115200','230400','460800','921600','1000000','1500000','2000000'];

function TabPage(tabIndex, tabData) {
	const self = this;

	// Root DOM
	const page = document.createElement('div');
	page.className = 'tab-page';
	page.style.display = 'none';

	// Inner wrapper — constrains all content within the tab page
	const inner = document.createElement('div');
	inner.className = 'tab-page-inner';

	// ---- Config Bar ----
	const configBar = document.createElement('div');
	configBar.id = 'configBar';
	configBar.className = 'config-bar';

	const btnMode = document.createElement('button');
	btnMode.id = 'btnModeToggle';
	btnMode.className = 'toggle-btn c-mode-btn';
	btnMode.textContent = '⇄ 单端口';
	btnMode.addEventListener('click', toggleMode);
	configBar.appendChild(btnMode);

	const singleRow = document.createElement('div');
	singleRow.id = 'singlePortRow';
	singleRow.className = 'port-row';
	const portSel = makeSelect(singleRow, 'port-sel', [['','-- 刷新中 --']]);
	portSel.id = 'portSelect';
	const baudSel = makeSelect(singleRow, 'baud-sel', BAUDS.map(function(b){return [b,b];}), '115200');
	baudSel.id = 'baudSelect';
	configBar.appendChild(singleRow);

	const fwdRow = document.createElement('div');
	fwdRow.id = 'forwardPortRow';
	fwdRow.className = 'port-row hidden';
	configBar.appendChild(fwdRow);

	const btnSettings = document.createElement('button');
	btnSettings.id = 'btnSettings';
	btnSettings.className = 'c-settings-btn';
	btnSettings.setAttribute('data-icon','more');
	btnSettings.setAttribute('data-icon-w','16');
	btnSettings.setAttribute('data-icon-h','16');
	btnSettings.title = t('btn.settings','详细设置');
	btnSettings.addEventListener('click', toggleSettings);
	configBar.appendChild(btnSettings);

	const btnOpen = document.createElement('button');
	btnOpen.id = 'btnOpen';
	btnOpen.className = 'toggle-btn c-port-btn';
	btnOpen.textContent = '打开串口';
	btnOpen.disabled = true;
	btnOpen.addEventListener('click', togglePort);
	configBar.appendChild(btnOpen);

	// ---- Content Row ----
	const contentRow = document.createElement('div');
	contentRow.id = 'contentRow';
	contentRow.className = 'content-row';

	const leftArea = document.createElement('div');
	leftArea.id = 'leftArea';
	leftArea.className = 'left-area';

	// Display
	const displayArea = document.createElement('div');
	displayArea.id = 'displayArea';
	displayArea.className = 'display-area';
	const btnClear = document.createElement('button');
	btnClear.id = 'btnClearFloat';
	btnClear.className = 'c-clear-btn toggle-btn';
	btnClear.textContent = '清空';
	btnClear.addEventListener('click', clearDisplay);
	displayArea.appendChild(btnClear);
	const displayContent = document.createElement('div');
	displayContent.className = 'display-content';
	displayContent.id = 'displayContent';
	displayContent.innerHTML = '<div class="placeholder">等待数据...</div>';
	displayArea.appendChild(displayContent);

	const fwdFloatBtns = document.createElement('div');
	fwdFloatBtns.id = 'forwardFloatBtns';
	fwdFloatBtns.className = 'c-fwd-float-btns';
	// Display toggle buttons - always visible, mode controls individual visibility
	const btnToggleDisplay = makeBtn(fwdFloatBtns, 'toggle-btn disp-btn-rx is-hidden-fwd', '⇃ 字', toggleDisplayMode);
	btnToggleDisplay.id = 'btnToggleDisplay';
	const btnToggleTx = makeBtn(fwdFloatBtns, 'toggle-btn disp-btn-tx is-hidden-fwd', '↾ 字', toggleTxDisplay);
	btnToggleTx.id = 'btnToggleSend';
	// Separators tagged with mode markers so they hide/show with their adjacent buttons
	addSep(fwdFloatBtns, 'is-hidden-fwd');
	const btnP1 = makeBtn(fwdFloatBtns, 'toggle-btn fwd-btn-p1 is-hidden-single is-hidden', '', toggleP1Display);
	btnP1.id = 'btnP1Display';
	btnP1.textContent = 'P1 文';
	const btnP2 = makeBtn(fwdFloatBtns, 'toggle-btn fwd-btn-p2 is-hidden-single is-hidden', '', toggleP2Display);
	btnP2.id = 'btnP2Display';
	btnP2.textContent = 'P2 文';
	addSep(fwdFloatBtns, 'is-hidden-single is-hidden');
	const btnScrollLock = makeBtn(fwdFloatBtns, 'toggle-btn c-scroll-btn', '自动滚动', toggleScrollLock);
	btnScrollLock.id = 'btnScrollLock';
	displayArea.appendChild(fwdFloatBtns);
	leftArea.appendChild(displayArea);

	// Divider
	const divider = document.createElement('div');
	divider.id = 'divider';
	divider.className = 'o-divider';
	leftArea.appendChild(divider);

	// Send area
	const sendArea = document.createElement('div');
	sendArea.id = 'sendArea';
	sendArea.className = 'send-area';

	const sendControls = document.createElement('div');
	sendControls.id = 'sendControls';
	sendControls.className = 'send-controls';
	const scrollArea = document.createElement('span');
	scrollArea.className = 'send-scroll-area';
	const scrollLeft = document.createElement('span');
	scrollLeft.className = 'send-scroll-arrow send-scroll-arrow--left is-hidden';
	scrollLeft.textContent = '‹';
	scrollArea.appendChild(scrollLeft);
	const scrollRight = document.createElement('span');
	scrollRight.className = 'send-scroll-arrow send-scroll-arrow--right is-hidden';
	scrollRight.textContent = '›';
	scrollArea.appendChild(scrollRight);
	const scrollWrap = document.createElement('span');
	scrollWrap.className = 'send-scroll-wrap';
	scrollArea.appendChild(scrollWrap);
	const ctrlGroup = document.createElement('span');
	ctrlGroup.className = 'send-ctrl-group';
	scrollWrap.appendChild(ctrlGroup);

	const btnAutoSend = document.createElement('button');
	btnAutoSend.id = 'autoSend';
	btnAutoSend.className = 'qp-cycle-btn';
	btnAutoSend.textContent = t('send.auto_send','自动发送');
	btnAutoSend.addEventListener('click', onAutoToggle);
	const btnSendQuick = makeBtn(ctrlGroup, 'toggle-btn', '', toggleSendFormat);
	btnSendQuick.id = 'btnSendQuick';
	btnSendQuick.innerHTML = icon('send', 14, 14) + ' 字';
	addSep(ctrlGroup);
	ctrlGroup.appendChild(btnAutoSend);
	const inpInterval = document.createElement('input');
	inpInterval.id = 'sendInterval';
	inpInterval.type = 'number';
	inpInterval.value = '1000';
	inpInterval.min = '1';
	inpInterval.addEventListener('change', onIntervalChange);
	ctrlGroup.appendChild(inpInterval);
	const spanUnit = document.createElement('span');
	spanUnit.className = 'send-unit';
	spanUnit.textContent = 'ms';
	ctrlGroup.appendChild(spanUnit);
	addSep(ctrlGroup, 'send-ctrl-sep');
	const appendLabel = document.createElement('label');
	appendLabel.setAttribute('data-i18n', 'settings.append');
	appendLabel.textContent = t('settings.append','尾部追加');
	ctrlGroup.appendChild(appendLabel);
	const appendSel = document.createElement('select');
	appendSel.id = 'appendSelect';
	[['',t('settings.append_none','无')],['cr','CR'],['lf','LF'],['crlf','CRLF']].forEach(function(p){
		const o = document.createElement('option');
		o.value = p[0];
		o.textContent = p[1];
		if (p[0] === '') o.selected = true;
		appendSel.appendChild(o);
	});
	ctrlGroup.appendChild(appendSel);

	sendControls.appendChild(scrollArea);
	const spacer = document.createElement('span');
	spacer.style.flex = '1';
	sendControls.appendChild(spacer);
	const sendInfo = document.createElement('span');
	sendInfo.id = 'sendInfo';
	sendControls.appendChild(sendInfo);

	sendArea.appendChild(sendControls);
	const sendInput = document.createElement('textarea');
	sendInput.id = 'sendInput';
	sendInput.spellcheck = false;
	sendInput.addEventListener('input', function() { updateSendInfo(); _syncSendMirror(this); if(_statusBar)_statusBar.updateCursor(); });
	sendInput.addEventListener('scroll', function() {
		sendMirror.scrollTop = sendInput.scrollTop;
		sendMirror.scrollLeft = sendInput.scrollLeft;
	});
	sendInput.addEventListener('keyup', function() { if(_statusBar)_statusBar.updateCursor(); });
	sendInput.addEventListener('click', function() { if(_statusBar)_statusBar.updateCursor(); });
	sendInput.addEventListener('keydown', function(e) {
		if (e.key === 'Enter') {
			var seq = (typeof state !== 'undefined' && state.eolSequence) ? state.eolSequence : 'lf';
			if (seq === 'lf') return; // default
			e.preventDefault();
			var st = this.selectionStart, ed = this.selectionEnd;
			var ins = seq === 'crlf' ? '\r\n' : '\r';
			this.value = this.value.substring(0, st) + ins + this.value.substring(ed);
			this.selectionStart = this.selectionEnd = st + ins.length;
			this.dispatchEvent(new Event('input', { bubbles: true }));
		}
		if (e.key === 'Tab') {
			e.preventDefault();
			var start = this.selectionStart, end = this.selectionEnd;
			this.value = this.value.substring(0, start) + '\t' + this.value.substring(end);
			this.selectionStart = this.selectionEnd = start + 1;
			this.dispatchEvent(new Event('input', { bubbles: true }));
		}
	});
	var sendWrap = document.createElement('div');
	sendWrap.className = 'send-input-wrap';
	var sendMirror = document.createElement('div');
	sendMirror.id = 'sendMirror';
	sendMirror.className = 'send-input-mirror';
	sendMirror.setAttribute('aria-hidden', 'true');
	sendWrap.appendChild(sendMirror);
	sendArea.appendChild(sendWrap);
	sendWrap.appendChild(sendInput);
	setTimeout(function() { _syncSendMirror(sendInput); }, 50);
	const btnSend = document.createElement('button');
	btnSend.id = 'btnSend';
	btnSend.className = 'c-send-btn toggle-btn';
	btnSend.innerHTML = icon('send', 14, 14) + ' ' + t('btn.send','发送');
	btnSend.disabled = true;
	btnSend.addEventListener('click', sendData);
	sendArea.appendChild(btnSend);

	leftArea.appendChild(sendArea);
	contentRow.appendChild(leftArea);
	var sendAreaRef = sendArea;

	const quickDivider = document.createElement('div');
	quickDivider.id = 'quickDivider';
	quickDivider.className = 'o-divider--quick';
	contentRow.appendChild(quickDivider);

	const quickPanel = buildQuickPanelDOM();
	contentRow.appendChild(quickPanel);

	inner.appendChild(configBar);
	inner.appendChild(contentRow);
	page.appendChild(inner);

	// Build forward row within this page
	const fwdParts = buildFwdRow();
	fwdRow.appendChild(fwdParts.row);
	const selFwdA = fwdParts.selA;
	const baudFwdA = fwdParts.baudA;
	const selFwdB = fwdParts.selB;
	const baudFwdB = fwdParts.baudB;

	// Per-tab UI state
	// Per-tab select change listeners
	portSel.addEventListener('change', function() { if (typeof saveSettings === 'function') saveSettings(); });
	baudSel.addEventListener('change', function() { if (typeof saveSettings === 'function') saveSettings(); });
	inpInterval.addEventListener('change', function() { if (typeof saveSettings === 'function') saveSettings(); });
	appendSel.addEventListener('change', function() { if (typeof saveSettings === 'function') saveSettings(); });

	self.ui = {
		displayMode: 'text',
		txDisplayMode: 'text',
		sendFormat: 'text',
		textSendBuf: '',
		hexSendBuf: '',
		autoSendActive: false,
		sendInterval: 1000,
		scrollLocked: false,
		dividerRatio: 50,
		quickPanelRatio: 70,
		appendSuffix: '',
	};

	// Public element references
	self.root = page;
	self.configBar = configBar;
	self.portSelect = portSel;
	self.baudSelect = baudSel;
	self.forwardPortRow = fwdRow;
	self.portSelectA = selFwdA;
	self.baudSelectA = baudFwdA;
	self.portSelectB = selFwdB;
	self.baudSelectB = baudFwdB;
	self.btnModeToggle = btnMode;
	self.btnOpen = btnOpen;
	self.btnSettings = btnSettings;
	self.singlePortRow = singleRow;
	self.displayContent = displayContent;
	self.displayArea = displayArea;
	self.fwdFloatBtns = fwdFloatBtns;
	self.btnP1 = btnP1;
	self.btnP2 = btnP2;
self.divider = divider;
	self.sendControls = sendControls;
	self.sendInput = sendInput;
	self.sendInfo = sendInfo;
	self.sendInterval = inpInterval;
	self.appendSelect = appendSel;
	self.btnSend = btnSend;
	self.autoSend = btnAutoSend;
	self.btnScrollLock = btnScrollLock;
	self.btnToggleDisplay = btnToggleDisplay;
	self.btnToggleSend = btnToggleTx;
	self.btnSendQuick = btnSendQuick;
	self.quickPanel = quickPanel;
	self.quickDivider = quickDivider;
	self.btnClearFloat = btnClear;
	self.contentRow = contentRow;
	self.sendArea = sendAreaRef;
	self.leftArea = leftArea;
	self.ctrlGroup = ctrlGroup;
	self.sendScrollWrap = scrollWrap;
	self.sendScrollLeft = scrollLeft;
	self.sendScrollRight = scrollRight;

	// Methods
	self.show = function() { page.style.display = ''; };
	self.hide = function() { page.style.display = 'none'; };
	self.destroy = function() { page.remove(); };
	self.isVisible = function() { return page.style.display !== 'none'; };
}

// ---- helpers ----

function makeSelect(container, cls, options, defVal) {
	const sel = document.createElement('select');
	sel.className = cls;
	options.forEach(function(p) {
		const o = document.createElement('option');
		o.value = p[0]; o.textContent = p[1];
		if (p[0] === defVal) o.selected = true;
		sel.appendChild(o);
	});
	container.appendChild(sel);
	return sel;
}

function makeBtn(container, cls, text, handler) {
	const b = document.createElement('button');
	b.className = cls;
	b.textContent = text;
	b.addEventListener('click', handler);
	container.appendChild(b);
	return b;
}

function addSep(container, extraCls) {
	const s = document.createElement('span');
	s.className = 'ctrl-sep' + (extraCls ? ' ' + extraCls : '');
	container.appendChild(s);
}

// ---- forward row builder ----

function buildFwdRow() {
	const row = document.createDocumentFragment();
	const r1 = document.createElement('div');
	r1.className = 'forward-row';
	const lbl1 = document.createElement('span');
	lbl1.className = 'fwd-label'; lbl1.textContent = '1';
	r1.appendChild(lbl1);
	const selA = makeSelect(r1, 'port-sel', [['','-- 端口1 --']]);
	selA.id = 'portSelectA';
	const baudA = makeSelect(r1, 'baud-sel', BAUDS.map(function(b){return [b,b];}), '115200');
	baudA.id = 'baudSelectA';
	row.appendChild(r1);
	const swapBtn = document.createElement('button');
	swapBtn.className = 'c-swap-btn';
	swapBtn.title = t('tooltip.swap_ports','交换端口');
	swapBtn.addEventListener('click', swapForwardPorts);
	row.appendChild(swapBtn);
	const r2 = document.createElement('div');
	r2.className = 'forward-row';
	const lbl2 = document.createElement('span');
	lbl2.className = 'fwd-label'; lbl2.textContent = '2';
	r2.appendChild(lbl2);
	const selB = makeSelect(r2, 'port-sel', [['','-- 端口2 --']]);
	selB.id = 'portSelectB';
	const baudB = makeSelect(r2, 'baud-sel', BAUDS.map(function(b){return [b,b];}), '115200');
	baudB.id = 'baudSelectB';
	row.appendChild(r2);
	return {row: row, selA: selA, baudA: baudA, selB: selB, baudB: baudB};
}

// ---- quick panel ----

function buildQuickPanelDOM() {
	const panel = document.createElement('div');
	panel.id = 'quickPanel';
	panel.className = 'quick-panel';

	const title = document.createElement('div');
	title.className = 'quick-panel-title';
	title.innerHTML = '<span class="qp-title-text">' + t('quick_panel.title','多字符串发送') + '</span>' +
		'<span class="qp-title-actions">' +
		'<button class="qp-icon-btn" data-icon="import" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.import','导入')+'" onclick="qpImport()"></button>' +
		'<button class="qp-icon-btn" data-icon="export" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.export','导出')+'" onclick="qpExport()"></button>' +
		'<button class="qp-icon-btn" data-icon="restore_defaults" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.restore_defaults','恢复默认')+'" onclick="qpRestoreDefaults()"></button>' +
		'</span>';
	panel.appendChild(title);

	const toolbar = document.createElement('div');
	toolbar.className = 'qp-toolbar';
	toolbar.innerHTML = '<span class="qp-tb-group">' +
		'<button class="qp-icon-btn" data-icon="enable_all" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.enable_all','全部启用')+'" onclick="qpEnableAll()"></button>' +
		'<button class="qp-icon-btn" data-icon="disable_all" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.disable_all','全部禁用')+'" onclick="qpDisableAll()"></button>' +
		'</span>' +
		'<span class="qp-tb-group">' +
		'<button id="qpBtnSendEnabled" onclick="qpSendEnabled()">'+t('quick_panel.send_enabled','发送启用')+'</button>' +
		'<button class="qp-cycle-btn" id="qpCycleSend" onclick="qpCycleToggle()">'+t('quick_panel.cycle_send','循环发送')+'</button>' +
		'<input type="number" id="qpCycleInterval" value="1000" min="1" title="'+t('quick_panel.cycle_interval','循环间隔(ms)')+'"> ms' +
		'</span>';
	panel.appendChild(toolbar);

	const table = document.createElement('div');
	table.className = 'qp-table';
	table.innerHTML = '<div class="qp-hdr">' +
		'<span class="qp-col qp-col-drag"></span>' +
		'<span class="qp-col qp-col-enable">'+t('quick_panel.enable','启用')+'</span>' +
		'<span class="qp-col qp-col-hex">'+t('quick_panel.hex','Hex')+'</span>' +
		'<span class="qp-col qp-col-content">'+t('quick_panel.content','内容')+'</span>' +
		'<span class="qp-col qp-col-delay">'+t('quick_panel.delay','延时(ms)')+'</span>' +
		'<span class="qp-col qp-col-send"><span class="qp-send-hdr">'+t('quick_panel.send','发送')+'</span></span>' +
		'<span class="qp-col qp-col-del"><button class="qp-clear-all" data-icon="trash" data-icon-w="16" data-icon-h="16" title="'+t('quick_panel.clear_all','清空全部')+'" onclick="quickPanelClearAll()"></button></span>' +
		'</div>' +
		'<div class="qp-rows"></div>';
	panel.appendChild(table);

	const footer = document.createElement('div');
	footer.className = 'qp-footer';
	const btnAdd = document.createElement('button');
	btnAdd.className = 'toggle-btn';
	btnAdd.textContent = t('quick_panel.add','+ 添加');
	btnAdd.addEventListener('click', quickPanelAdd);
	footer.appendChild(btnAdd);
	panel.appendChild(footer);

	return panel;
}

// ---- welcome page (initial empty state) ----

function createWelcomePage() {
	var page = document.createElement('div');
	page.className = 'tab-page o-empty';
	page.style.display = '';

	var msg = document.createElement('div');
	msg.className = 'o-empty__msg';
	msg.setAttribute('data-i18n', 'empty.no_session');
	msg.textContent = t('empty.no_session', '尚无串口会话');

	var hint = document.createElement('div');
	hint.className = 'o-empty__hint';
	hint.setAttribute('data-i18n', 'empty.no_session_hint');
	hint.textContent = t('empty.no_session_hint', '请点击 + 按钮或菜单 文件 → 新建会话 来开始');

	var btn = document.createElement('button');
	btn.className = 'o-empty__btn';
	btn.setAttribute('data-i18n', 'menu.new_session');
	btn.textContent = '+ ' + t('menu.new_session', '新建会话');
	btn.addEventListener('click', function() { if (typeof addTab === 'function') addTab(); });

	page.appendChild(msg);
	page.appendChild(hint);
	page.appendChild(btn);

	return {
		root: page,
		show: function() { page.style.display = ''; },
		hide: function() { page.style.display = 'none'; },
		destroy: function() { page.remove(); },
		isWelcome: true
	};
}

// ---- active page helpers ----

function getActivePage() {
	const tab = getActiveTab();
	if (tab && tab._page) return tab._page;
	return null;
}

// Get any element from the active page by name.
// Falls back to document.getElementById for shared elements (settings, theme, menu popups).
function pageEl(name) {
	const page = getActivePage();
	if (page && page[name]) return page[name];
	return document.getElementById(name);
}
