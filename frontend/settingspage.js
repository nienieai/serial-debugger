// ---- Settings Page ----
// Full-page settings view shown when the settings tab is active.


// ---- Settings Overlay (串口详细设置 popup) ----
// Built in JS, referenced by toggleSettings() in app.js

function createSettingsOverlay() {
  var ov = document.createElement('div');
  ov.className = 'o-overlay--modal is-hidden';
  ov.id = 'settingsOverlay';
  ov.addEventListener('click', function(e) { if (e.target === ov && typeof toggleSettings === 'function') toggleSettings(); });

  var popup = document.createElement('div');
  popup.className = 'settings-popup';
  popup.id = 'settingsPopup';

  var head = document.createElement('div');
  head.className = 'settings-head';
  var htitle = document.createElement('span');
  htitle.setAttribute('data-i18n', 'settings.detail_title');
  htitle.textContent = t('settings.detail_title', '串口详细设置');
  head.appendChild(htitle);
  var closeBtn = document.createElement('button');
  closeBtn.className = 'c-close-btn';
  closeBtn.textContent = '✕';
  closeBtn.addEventListener('click', function() { if (typeof toggleSettings === 'function') toggleSettings(); });
  head.appendChild(closeBtn);
  popup.appendChild(head);

  // ---- Single port settings ----
  var single = document.createElement('div');
  single.id = 'settingsSingle';
  single.className = 'settings-content';

  function makeRow(labelText, selId, opts, i18nKey) {
    var row = document.createElement('div');
    row.className = 'c-form-row';
    var lbl = document.createElement('label');
    lbl.className = 'c-form-row__label';
    if (i18nKey) lbl.setAttribute('data-i18n', i18nKey);
    lbl.textContent = labelText;
    row.appendChild(lbl);
    var sel = document.createElement('select');
    sel.id = selId;
    opts.forEach(function(o) {
      var opt = document.createElement('option');
      opt.value = o[0]; opt.textContent = o[1] || o[0];
      if (o[2]) opt.selected = true;
      if (o[3]) opt.setAttribute('data-i18n', o[3]);
      sel.appendChild(opt);
    });
    row.appendChild(sel);
    return row;
  }

  single.appendChild(makeRow('数据位', 'dataBitsSelect', [['5','5'],['6','6'],['7','7'],['8','8',true]], 'label.data_bits'));
  single.appendChild(makeRow('停止位', 'stopBitsSelect', [['1','1',true],['1.5','1.5'],['2','2']], 'label.stop_bits'));
  single.appendChild(makeRow('校验位', 'paritySelect', [['none','None',true],['odd','Odd'],['even','Even'],['mark','Mark'],['space','Space']], 'label.parity'));
  single.appendChild(makeRow('流控', 'flowControlSelect', [
    ['none','None',true,'flow_control.none'],['rts_cts','RTS/CTS',false,'flow_control.rts_cts'],['xon_xoff','XON/XOFF',false,'flow_control.xon_xoff']
  ], 'settings.flow_control'));
  popup.appendChild(single);

  // ---- Forward mode settings ----
  var fwd = document.createElement('div');
  fwd.id = 'settingsForward';
  fwd.className = 'settings-content is-hidden';
  var split = document.createElement('div');
  split.className = 'settings-split';

  function makeFwdCol(label, pfx, titleKey) {
    var col = document.createElement('div');
    col.className = 'c-form-col';
    var ct = document.createElement('div');
    ct.className = 'c-form-col__title';
    if (titleKey) ct.setAttribute('data-i18n', titleKey);
    ct.textContent = label;
    col.appendChild(ct);
    col.appendChild(makeRow('端口', 'portSelect' + pfx + 'Settings', [['','-- ' + label + ' --']], 'label.port'));
    col.appendChild(makeRow('波特率', 'baudSelect' + pfx + 'Settings', [
      ['300'],['1200'],['2400'],['4800'],['9600'],['19200'],['38400'],['57600'],['115200',null,true],['230400'],['460800'],['921600'],['1000000'],['1500000'],['2000000']
    ].map(function(b){return [b[0],b[0],b[1]];}), 'label.baud'));
    col.appendChild(makeFwdRow('数据位', 'dataBitsSelect' + pfx, [['5'],['6'],['7'],['8',null,true]], 'label.data_bits'));
    col.appendChild(makeFwdRow('停止位', 'stopBitsSelect' + pfx, [['1',null,true],['1.5'],['2']], 'label.stop_bits'));
    col.appendChild(makeFwdRow('校验位', 'paritySelect' + pfx, [['none',null,true],['odd'],['even'],['mark'],['space']], 'label.parity'));
    col.appendChild(makeFwdRow('流控', 'flowControlSelect' + pfx, [
      ['none','None',true,'flow_control.none'],['rts_cts','RTS/CTS',false,'flow_control.rts_cts'],['xon_xoff','XON/XOFF',false,'flow_control.xon_xoff']
    ], 'settings.flow_control'));
    return col;
  }

  function makeFwdRow(labelText, selId, opts, i18nKey) {
    var row = document.createElement('div');
    row.className = 'c-form-row fwd-settings-row';
    var lbl = document.createElement('label');
    lbl.className = 'c-form-row__label';
    if (i18nKey) lbl.setAttribute('data-i18n', i18nKey);
    lbl.textContent = labelText;
    row.appendChild(lbl);
    var sel = document.createElement('select');
    sel.id = selId;
    opts.forEach(function(o) {
      var opt = document.createElement('option');
      opt.value = o[0]; opt.textContent = o[1] || o[0];
      if (o[2]) opt.selected = true;
      if (o[3]) opt.setAttribute('data-i18n', o[3]);
      sel.appendChild(opt);
    });
    row.appendChild(sel);
    return row;
  }

  var col1 = makeFwdCol('端口 1', 'A', 'stat.p1');
  split.appendChild(col1);

  var swapBtn = document.createElement('button');
  swapBtn.className = 'c-swap-btn';
  swapBtn.title = t('tooltip.swap_ports', '交换端口');
  swapBtn.addEventListener('click', function() { if (typeof swapForwardPorts === 'function') swapForwardPorts(); });
  split.appendChild(swapBtn);

  var col2 = makeFwdCol('端口 2', 'B', 'stat.p2');
  split.appendChild(col2);

  fwd.appendChild(split);
  popup.appendChild(fwd);

  ov.appendChild(popup);
  document.getElementById('app').appendChild(ov);
  return ov;
}

function buildSettingsPage() {

	var wrapper = document.createElement('div');
	wrapper.className = 'settings-wrapper';

	// ── Header bar (single row, floats above layout) ──
	var header = document.createElement('div');
	header.className = 'settings-header';

	// Left: title, same width as nav column
	var headerTitle = document.createElement('div');
	headerTitle.className = 'settings-header-title';
	var title = document.createElement('h1');
	title.className = 'settings-page-title';
	title.setAttribute('data-i18n', 'settings.title');
	title.textContent = t('settings.title', '设置');
	// Nav toggle button (hamburger) — left of title
	var navToggle = document.createElement('button');
	navToggle.className = 'settings-nav-toggle';
	navToggle.innerHTML = icon('menu', 20, 20);
	navToggle.title = t('settings.toggle_nav','收起/展开侧栏');
	headerTitle.appendChild(navToggle);
	headerTitle.appendChild(title);
	header.appendChild(headerTitle);

	// Right: search box container
	var headerSearch = document.createElement('div');
	headerSearch.className = 'settings-header-search';
	var searchWrap = document.createElement('div');
	searchWrap.className = 'settings-search-wrap';
	// Magnifying glass
	var searchIcon = document.createElement('span');
	searchIcon.className = 'settings-search-icon';
	searchIcon.innerHTML = icon('search', 16, 16);
	searchWrap.appendChild(searchIcon);
	var search = document.createElement('input');
	search.type = 'text';
	search.className = 'settings-search';
	search.placeholder = t('settings.search_placeholder', '搜索设置...');
	searchWrap.appendChild(search);
	// Clear button
	var searchClear = document.createElement('button');
	searchClear.className = 'settings-search-clear is-hidden';
	searchClear.innerHTML = icon('close', 14, 14);
	searchClear.title = t('settings.search_clear','清除搜索');
	searchClear.addEventListener('click', function() {
		search.value = '';
		search.dispatchEvent(new Event('input'));
		searchClear.classList.add('is-hidden');
		search.focus();
	});
	searchWrap.appendChild(searchClear);
	// Toggle clear button visibility
	search.addEventListener('input', function() {
		searchClear.classList.toggle('is-hidden', search.value.trim() === '');
	});
	headerSearch.appendChild(searchWrap);
	header.appendChild(headerSearch);

	wrapper.appendChild(header);

	var layout = document.createElement('div');
	layout.className = 'settings-layout';

	var nav = document.createElement('div');
	nav.className = 'settings-nav';

	var body = document.createElement('div');
	body.className = 'settings-body';


	// ── Panel: 编码与显示 ──
	var panelDisplay = document.createElement('div');
	panelDisplay.className = 'settings-panel';
	panelDisplay.setAttribute('data-panel', 'display');

	var bInput = _bubble(t('settings.bubble_encoding','编码格式'));
	bInput.appendChild(_row(t('settings.bubble_encoding','编码格式'), _buildEncodingSelect()));
	bInput.appendChild(_row(t('settings.tab_size','制表符长度'),
		_buildSelect([1,2,3,4,5,6,7,8], ['1','2','3','4','5','6','7','8'], state.tabSize, function(v) { state.tabSize = parseInt(v); _applyDisplayStyle(); saveSettings(); })
	));
	bInput.appendChild(_row(t('settings.eol_sequence','行尾序列'),
		_buildSelect(['lf','cr','crlf'], ['LF','CR','CRLF'], state.eolSequence, function(v) { state.eolSequence = v; saveSettings(); if(_statusBar)_statusBar.updateEol(); })
	));
	panelDisplay.appendChild(bInput);

	var bFont = _bubble(t('settings.bubble_font','显示字体'));
	bFont.appendChild(_row(t('settings.font_family','字体'),
		_buildSelect(
			['Consolas','Cascadia Code','Fira Code','JetBrains Mono','Source Code Pro','Courier New','Microsoft YaHei','SimSun','KaiTi','Segoe UI'],
			['Consolas','Cascadia Code','Fira Code','JetBrains Mono','Source Code Pro','Courier New','微软雅黑','宋体','楷体','Segoe UI'],
			state.displayFontFamily,
			function(v) { state.displayFontFamily = v; _applyDisplayStyle(); saveSettings(); }
		)
	));
	var fontSizeInput = document.createElement('input');
	fontSizeInput.type = 'number';
	fontSizeInput.className = 'settings-select';
	fontSizeInput.value = state.displayFontSize;
	fontSizeInput.min = 4;
	fontSizeInput.max = 64;
	fontSizeInput.addEventListener('change', function() {
		var n = parseInt(fontSizeInput.value);
		if (isNaN(n) || n < 4) n = 4;
		if (n > 64) n = 64;
		state.displayFontSize = n;
		fontSizeInput.value = String(n);
		_applyDisplayStyle();
		saveSettings();
	});
	bFont.appendChild(_row(t('settings.font_size','字号'), fontSizeInput));
	panelDisplay.appendChild(bFont);

	var bHex = _bubble(t('settings.bubble_hex','Hex显示设置'));
	bHex.appendChild(_row(t('settings.hex_prefix','Hex 前缀 (0x)'), _buildToggle(function() { toggleHexPrefix(); }, state.hexPrefix)));
	bHex.appendChild(_row(t('settings.hex_case','Hex 大写'), _buildToggle(function() { toggleHexCase(); }, state.hexCase === 'upper')));
	bHex.appendChild(_row(t('settings.hex_sep','Hex 分隔符'),
		_buildSelect(['none','space','comma'], [t('settings.sep_none','无分割'),t('settings.sep_space','空格'),t('settings.sep_comma','逗号')], state.hexSep, function(v) { setHexSep(v); })
	));
	panelDisplay.appendChild(bHex);

	var bText = _bubble(t('settings.bubble_text','文本显示设置'));
	bText.appendChild(_row(t('settings.hex_escape','转义字节'),
		_buildSelect(['show','hide','raw'], [t('settings.esc_show','显示 /FF'),t('settings.esc_hide','隐藏转义'),t('settings.esc_raw','原始输出')], state.hexEscapeMode, function(v) { setHexEscapeMode(v); _updateHexEscFmtRow(); })
	));
	var hexEscFmtRow = _row(t('settings.hex_escape_fmt','转义格式'),
		_buildSelect(['slash','backslash-x','0x','angle','bracket'],
			[t('settings.esc_fmt_slash','/FF'), t('settings.esc_fmt_bslash_x','\\xFF'), t('settings.esc_fmt_0x','0xFF'), t('settings.esc_fmt_angle','<FF>'), t('settings.esc_fmt_bracket','[FF]')],
			state.hexEscapeFormat || 'slash',
			function(v) { setHexEscapeFormat(v); }
		)
	);
	hexEscFmtRow.id = 'hexEscFmtRow';
	bText.appendChild(hexEscFmtRow);
	bText.appendChild(_row(t('settings.cr_visible','可见控制字符'), _buildToggle(function() { toggleCrVisible(); }, state.crVisible)));
	bText.appendChild(_row(t('settings.copy_hex_escapes','允许复制转义字节'), _buildToggle(function() { state.copyHexEscapes = !state.copyHexEscapes; saveSettings(); }, state.copyHexEscapes)));
	bText.appendChild(_row(t('settings.lock_scroll','默认锁定滚动'), _buildToggle(function() { state.scrollLocked = !state.scrollLocked; saveSettings(); }, state.scrollLocked)));
	// Initial visibility for format row
	hexEscFmtRow.classList.toggle('is-hidden', state.hexEscapeMode !== 'show');
	panelDisplay.appendChild(bText);

	// Display color overrides — new layout: [name] [bg-color:dark|light] [fg-color:dark|light]
	var bColor = _bubble(t('settings.bubble_colors_display','数据高亮'), _buildColorResetBtn());
	var colorGroups = [
		{ title: t('settings.color_group_single','单端口'), items: [
			{ key: 'tsTx',   i18n: 'settings.color_ts_tx',   fb: '发送时间戳' },
			{ key: 'tsRx',   i18n: 'settings.color_ts_rx',   fb: '接收时间戳' },
			{ key: 'stat',   i18n: 'settings.color_stat',    fb: '字符统计' },
			{ key: 'textTx', i18n: 'settings.color_text_tx', fb: '发送文本' },
			{ key: 'textRx', i18n: 'settings.color_text_rx', fb: '接收文本' },
			{ key: 'hexTx',  i18n: 'settings.color_hex_tx',  fb: '发送Hex' },
			{ key: 'hexRx',  i18n: 'settings.color_hex_rx',  fb: '接收Hex' },
			{ key: 'hexInv', i18n: 'settings.color_hex_inv', fb: '非法Hex' },
			{ key: 'hexEsc', i18n: 'settings.color_hex_esc', fb: '转义字节' },
			{ key: 'ctrl',   i18n: 'settings.color_ctrl',    fb: '控制字符', bgKey: 'ctrlBg', fgKey: 'ctrlFg' }
		]},
		{ title: t('settings.color_group_forward','端口转发'), items: [
			{ key: 'ts1',    i18n: 'settings.color_ts_1',    fb: 'P1时间戳' },
			{ key: 'ts2',    i18n: 'settings.color_ts_2',    fb: 'P2时间戳' },
			{ key: 'fstat',  i18n: 'settings.color_stat',    fb: '字符统计' },
			{ key: 'text1',  i18n: 'settings.color_text_1',  fb: 'P1文本' },
			{ key: 'text2',  i18n: 'settings.color_text_2',  fb: 'P2文本' },
			{ key: 'hex1',   i18n: 'settings.color_hex_1',   fb: 'P1 Hex' },
			{ key: 'hex2',   i18n: 'settings.color_hex_2',   fb: 'P2 Hex' },
			{ key: 'fhexEsc', i18n: 'settings.color_hex_esc', fb: '转义字节' },
			{ key: 'fctrl',  i18n: 'settings.color_ctrl',    fb: '控制字符', bgKey: 'fctrlBg', fgKey: 'fctrlFg' }
		]}
	];
	colorGroups.forEach(function(grp) {
		var sep = document.createElement('div');
		sep.className = 'c-color-sep';
		var sepLbl = document.createElement('span');
		sepLbl.textContent = grp.title;
		sep.appendChild(sepLbl);
		bColor.appendChild(sep);
		// Preview section
		var preview = document.createElement('div');
		preview.className = 'c-color-preview-wrap';
		preview.id = 'colorPreview_' + grp.items[0].key;
		preview._colorItems = grp.items;
		bColor.appendChild(preview);
		// Two-layer column header: 设置项 | 加粗 | 背景颜色 | 字体颜色
		var hdr = document.createElement('div');
		hdr.className = 'c-color-head';
		var hdrName = document.createElement('span');
		hdrName.className = 'c-color-head__name';
		hdrName.textContent = t('settings.color_head_name','设置项');
		hdr.appendChild(hdrName);
		var hdrBold = document.createElement('span');
		hdrBold.className = 'c-color-head__chk';
		hdrBold.textContent = t('settings.color_head_bold','加粗');
		hdr.appendChild(hdrBold);
		var hdrItalic = document.createElement('span');
		hdrItalic.className = 'c-color-head__chk';
		hdrItalic.textContent = t('settings.color_head_italic','斜体');
		hdr.appendChild(hdrItalic);
		var hdrBg = document.createElement('span');
		hdrBg.className = 'c-color-head__grp';
		hdrBg.innerHTML = '<div class="c-color-head__row1">' + t('settings.color_head_bg','背景颜色') + '</div><div class="c-color-head__row2"><span>' + t('theme.dark_label','深色') + '</span><span>' + t('theme.light_label','浅色') + '</span></div>';
		hdr.appendChild(hdrBg);
		var hdrFg = document.createElement('span');
		hdrFg.className = 'c-color-head__grp';
		hdrFg.innerHTML = '<div class="c-color-head__row1">' + t('settings.color_head_fg','字体颜色') + '</div><div class="c-color-head__row2"><span>' + t('theme.dark_label','深色') + '</span><span>' + t('theme.light_label','浅色') + '</span></div>';
		hdr.appendChild(hdrFg);
		bColor.appendChild(hdr);
		grp.items.forEach(function(cfg) { bColor.appendChild(_buildColorRow(cfg)); });
		// Render preview after DOM attached
		setTimeout(function() { _renderColorPreview(grp.items, preview); }, 10);
	});
	panelDisplay.appendChild(bColor);

	// ── Panel: 外观 ──
	var panelAppear = document.createElement('div');
	panelAppear.className = 'settings-panel is-hidden';
	panelAppear.setAttribute('data-panel', 'appearance');

	var bLook = _bubble(t('settings.bubble_look','整体外观'));
	var modeBar = document.createElement('div');
	modeBar.className = 'settings-mode-bar';
	[{ v: 'auto', k: 'theme.auto_label', fb: '跟随系统', ico: 'theme_auto' },
	 { v: 'dark', k: 'theme.dark_label', fb: '深色',   ico: 'theme_dark' },
	 { v: 'light', k: 'theme.light_label', fb: '浅色', ico: 'theme_light' }].forEach(function(m) {
		var btn = document.createElement('button');
		btn.className = 'settings-mode-btn';
		btn.innerHTML = icon(m.ico, 16, 16) + ' <span>' + t(m.k, m.fb) + '</span>';
		if (state.colorThemeMode === m.v) btn.classList.add('is-active');
		btn.addEventListener('click', function() {
			state.colorThemeMode = m.v;
			modeBar.querySelectorAll('.settings-mode-btn').forEach(function(b) { b.classList.remove('is-active'); });
			btn.classList.add('is-active');
			applyColorTheme();
			saveSettings();
		});
		modeBar.appendChild(btn);
	});
	bLook.appendChild(modeBar);
	panelAppear.appendChild(bLook);

	var themeRefresh = document.createElement('button');
	themeRefresh.className = 'c-icon-btn';
	themeRefresh.innerHTML = icon('reset', 16, 16);
	themeRefresh.title = t('settings.refresh','刷新');
	themeRefresh.addEventListener('click', function() {
		initColorThemes().then(function() { renderSettingsThemeGrid(); });
	});
	var bTheme = _bubble(t('settings.bubble_colors','颜色主题'), themeRefresh);
	var themeGrid = document.createElement('div');
	themeGrid.className = 'settings-theme-grid';
	themeGrid.id = 'settingsThemeGrid';
	bTheme.appendChild(themeGrid);
	bTheme.appendChild(_bubbleFooter('colors', t('settings.hint_colors','想使用其他主题？在软件目录中新建 \\themes\\colors\\ 导入主题文件，或单击此按键创建示例文件夹')));
	panelAppear.appendChild(bTheme);

	var iconRefresh = document.createElement('button');
	iconRefresh.className = 'c-icon-btn';
	iconRefresh.innerHTML = icon('reset', 16, 16);
	iconRefresh.title = t('settings.refresh','刷新');
	iconRefresh.addEventListener('click', function() {
		initIconThemes().then(function() { renderSettingsIconGrid(); });
	});
	var bIcons = _bubble(t('settings.bubble_icons','图标主题'), iconRefresh);
	var iconGrid = document.createElement('div');
	iconGrid.className = 'settings-theme-grid';
	iconGrid.id = 'settingsIconGrid';
	bIcons.appendChild(iconGrid);
	bIcons.appendChild(_bubbleFooter('icons', t('settings.hint_icons','想使用其他图标主题？在软件目录中新建 \\themes\\icons\\ 导入图标文件，或单击此按键创建示例文件夹')));
	panelAppear.appendChild(bIcons);

	// ── Panel: 存储与行为 ──
	var panelStorage = document.createElement('div');
	panelStorage.className = 'settings-panel is-hidden';
	panelStorage.setAttribute('data-panel', 'storage');
	var bStorage = _bubble(t('settings.bubble_storage','存储与行为'));
	bStorage.appendChild(_row(t('settings.auto_save','自动保存历史记录'), _buildToggle(function() { toggleAutoSaveHistory(); }, state.autoSaveHistory !== false)));
	bStorage.appendChild(_row(t('settings.auto_create','启动时自动创建会话'), _buildToggle(function() { toggleAutoCreateSession(); }, state.autoCreateSession !== false)));
	panelStorage.appendChild(bStorage);

	// ── Panel: 语言 ──
	var panelLang = document.createElement('div');
	panelLang.className = 'settings-panel is-hidden';
	panelLang.setAttribute('data-panel', 'language');

	var langRefresh = document.createElement('button');
	langRefresh.className = 'c-icon-btn';
	langRefresh.innerHTML = icon('reset', 16, 16);
	langRefresh.title = t('settings.refresh','刷新');
	langRefresh.addEventListener('click', function() { refreshSettingsLangList(); });
	var bLang = _bubble(t('settings.bubble_lang','界面语言'), langRefresh);
	var langList = document.createElement('div');
	langList.className = 'settings-lang-list';
	langList.id = 'settingsLangList';
	bLang.appendChild(langList);
	bLang.appendChild(_bubbleFooter('i18n', t('settings.hint_i18n','想使用其他语言？在软件目录中新建 \\i18n\\ 导入语言文件，或单击此按键创建示例文件夹')));
	panelLang.appendChild(bLang);

	// ── Panel: 关于 ──
	var panelAbout = document.createElement('div');
	panelAbout.className = 'settings-panel is-hidden';
	panelAbout.setAttribute('data-panel', 'about');
	var bAbout = _bubble(t('settings.about_version','版本'));
	var aboutInfo = document.createElement('div');
	aboutInfo.style.cssText = 'font-size:13px;line-height:2;color:var(--fg);';
	var aboutHTML = '<div style="font-size:18px;font-weight:700;margin-bottom:6px">' + escHtml(t('app.title','串口调试工具')) + ' v' + (window._appVersion || '0.6.1') + '</div>';
	aboutHTML += '<div style="color:var(--placeholder);margin-bottom:14px">' + escHtml(t('settings.about_desc','Go + Wails 跨平台串口调试工具')) + '</div>';
	aboutHTML += '<p style="margin-bottom:6px;line-height:1.8">' + escHtml(t('settings.about_p1','')) + '</p>';
	aboutHTML += '<p style="margin-bottom:6px;line-height:1.8">' + escHtml(t('settings.about_p2','')) + '</p>';
	aboutHTML += '<p style="line-height:1.8">' + escHtml(t('settings.about_p3','')) + '</p>';
	aboutInfo.innerHTML = aboutHTML;
	bAbout.appendChild(aboutInfo);
	panelAbout.appendChild(bAbout);

	// Nav items + panels
	var sections = [
		{ key: 'display',    i18n: 'settings.nav_display',    fb: '编码与显示', panel: panelDisplay },
		{ key: 'appearance', i18n: 'settings.nav_appearance', fb: '外观',       panel: panelAppear },
		{ key: 'storage',    i18n: 'settings.nav_storage',    fb: '存储与行为', panel: panelStorage },
		{ key: 'language',   i18n: 'settings.nav_language',   fb: '语言',       panel: panelLang },
		{ key: 'about',      i18n: 'settings.nav_about',      fb: '关于',       panel: panelAbout }
	];

	sections.forEach(function(sec, idx) {
		var item = document.createElement('div');
		item.className = 'settings-nav-item' + (idx === 0 ? ' is-active' : '');
		item.setAttribute('data-i18n', sec.i18n);
		item.textContent = t(sec.i18n, sec.fb);
		item.setAttribute('data-nav', sec.key);
		item.addEventListener('click', function() {
			nav.querySelectorAll('.settings-nav-item').forEach(function(el) { el.classList.remove('is-active'); });
			item.classList.add('is-active');
			body.querySelectorAll('.settings-panel').forEach(function(p) { p.classList.add('is-hidden'); });
			sec.panel.classList.remove('is-hidden');
			if (sec.key === 'appearance') {
				setTimeout(function() { renderSettingsThemeGrid(); renderSettingsIconGrid(); }, 50);
			}
			if (sec.key === 'language') {
				setTimeout(function() { refreshSettingsLangList(); }, 50);
			}
		});
		nav.appendChild(item);
		body.appendChild(sec.panel);
	});

	layout.appendChild(nav);
	layout.appendChild(body);
	wrapper.appendChild(layout);
	// Nav backdrop & toggle (narrow: overlay; wide: always visible via CSS)
	var navBackdrop = document.createElement('div');
	navBackdrop.className = 'settings-nav-backdrop';
	wrapper.appendChild(navBackdrop);
	function _closeNav() { nav.classList.remove('is-open'); navBackdrop.classList.remove('is-shown'); }
	function _openNav()  { nav.classList.add('is-open');    navBackdrop.classList.add('is-shown'); }
	navToggle.addEventListener('click', function() {
		if (nav.classList.contains('is-open')) _closeNav(); else _openNav();
	});
	navBackdrop.addEventListener('click', _closeNav);
	// Close floating nav when a nav item is clicked
	nav.addEventListener('click', function(e) {
		if (e.target.classList.contains('settings-nav-item')) _closeNav();
	});
	// ---- Search filter ----
	search.addEventListener('input', function() {
		var q = search.value.trim().toLowerCase();
		var bubbles = body.querySelectorAll('.settings-bubble');
		bubbles.forEach(function(b) {
			var text = (b.textContent || '').toLowerCase();
			b.classList.toggle('is-hidden', q !== '' && text.indexOf(q) < 0);
		});
		var navItems = nav.querySelectorAll('.settings-nav-item');
		navItems.forEach(function(item) {
			var text = (item.textContent || '').toLowerCase();
			item.classList.toggle('is-hidden', q !== '' && text.indexOf(q) < 0);
		});
	});

	return wrapper;
}

// ---- helpers ----

function _bubbleFooter(kind, hintText) {
	var footer = document.createElement('div');
	footer.className = 'settings-bubble-footer';
	var hint = document.createElement('span');
	hint.className = 'settings-bubble-hint';
	hint.textContent = hintText;
	footer.appendChild(hint);
	var btnExample = document.createElement('button');
	btnExample.className = 'settings-refresh-btn';
	btnExample.textContent = t('settings.create_example','创建示例');
	btnExample.addEventListener('click', function() {
		btnExample.disabled = true;
		btnExample.textContent = '...';
		window.go.main.App.CreateExampleFiles(kind).then(function(r) {
			if (r && r.ok) {
				btnExample.textContent = 'OK';
				if (kind === 'colors') { initColorThemes().then(function() { renderSettingsThemeGrid(); }); }
				if (kind === 'icons') { initIconThemes().then(function() { renderSettingsIconGrid(); }); }
				if (kind === 'i18n') { refreshSettingsLangList(); }
			} else {
				btnExample.textContent = 'ERR';
			}
			setTimeout(function() { btnExample.disabled = false; btnExample.textContent = t('settings.create_example','创建示例'); }, 3000);
		}).catch(function() {
			btnExample.textContent = 'ERR';
			setTimeout(function() { btnExample.disabled = false; btnExample.textContent = t('settings.create_example','创建示例'); }, 3000);
		});
	});
	footer.appendChild(btnExample);
	var btnOpen = document.createElement('button');
	btnOpen.className = 'settings-refresh-btn';
	btnOpen.textContent = t('settings.open_folder','打开文件夹');
	btnOpen.addEventListener('click', function() {
		window.go.main.App.ShowFolderInExplorer(kind);
	});
	footer.appendChild(btnOpen);
	return footer;
}

function _bubbleInline(title, control) {
	// Bubble with title and control on the same row (no separate body)
	var card = document.createElement('div');
	card.className = 'settings-bubble';
	var hdr = document.createElement('div');
	hdr.className = 'settings-bubble-head';
	var hdrTitle = document.createElement('span');
	hdrTitle.className = 'settings-bubble-title';
	hdrTitle.textContent = title;
	hdr.appendChild(hdrTitle);
	hdr.appendChild(control);
	card.appendChild(hdr);
	return card;
}

function _bubble(title, actionBtn) {
	var card = document.createElement('div');
	card.className = 'settings-bubble';
	var hdr = document.createElement('div');
	hdr.className = 'settings-bubble-head';
	var hdrTitle = document.createElement('span');
	hdrTitle.className = 'settings-bubble-title';
	hdrTitle.textContent = title;
	hdr.appendChild(hdrTitle);
	if (actionBtn) hdr.appendChild(actionBtn);
	card.appendChild(hdr);
	return card;
}

function _sectTitle(text) {
	var h2 = document.createElement('h2');
	h2.className = 'settings-section-title';
	h2.textContent = text;
	return h2;
}

function _row(labelText, control, i18nKey) {
	var row = document.createElement('div');
	row.className = 'settings-row';
	var lbl = document.createElement('span');
	lbl.className = 'settings-row-label';
	if (i18nKey) lbl.setAttribute('data-i18n', i18nKey);
	lbl.textContent = labelText;
	row.appendChild(lbl);
	row.appendChild(control);
	return row;
}

function _buildToggle(onChange, checked) {
	var label = document.createElement('label');
	label.className = 'settings-toggle';
	var input = document.createElement('input');
	input.type = 'checkbox';
	input.checked = !!checked;
	input.addEventListener('change', function() { onChange(); });
	label.appendChild(input);
	var slider = document.createElement('span');
	slider.className = 'settings-toggle-slider';
	label.appendChild(slider);
	return label;
}

function _buildSelect(values, labels, current, onChange) {
	var sel = document.createElement('select');
	sel.className = 'settings-select';
	values.forEach(function(v, i) {
		var o = document.createElement('option');
		o.value = v;
		o.textContent = labels[i] || v;
		if (v === current) o.selected = true;
		sel.appendChild(o);
	});
	sel.addEventListener('change', function() { onChange(sel.value); });
	return sel;
}

function _buildEncodingSelect() {
	return _buildSelect(['utf-8','gb2312','ascii'], ['UTF-8','GB2312','ASCII'], state.encoding, function(v) { setEncoding(v); });
}

function _buildLangSelect() {
	var langs = [
		['zh', '简体中文'], ['zh-Hant', '繁體中文'], ['en', 'English'],
		['fr', 'Français'], ['es', 'Español'], ['ru', 'Русский'],
		['ar', 'العربية'], ['ja', '日本語'], ['ko', '한국어']
	];
	if (window._externalLangs) {
		window._externalLangs.forEach(function(el) {
			if (!langs.some(function(l) { return l[0] === el.code; })) langs.push([el.code, el.name]);
		});
	}
	return _buildSelect(langs.map(function(p){return p[0];}), langs.map(function(p){return p[1];}), state.language, function(v) { setLanguage(v); });
}

// ---- theme color cache ----
var _themeColorCache = {};

function _themeColorsForMode(data, mode) {
	var bg = '', fg = '';
	var block = data || {};
	if (block.common) { for (var k in block.common) { if (k === '--bg') bg = block.common[k]; if (k === '--fg') fg = block.common[k]; } }
	if (block[mode]) { for (var k2 in block[mode]) { if (k2 === '--bg') bg = block[mode][k2]; if (k2 === '--fg') fg = block[mode][k2]; } }
	return { bg: bg || (mode === 'dark' ? '#1e1e1e' : '#f5f5f5'), fg: fg || (mode === 'dark' ? '#e0e0e0' : '#333') };
}

function _makeSwatch(mode, bg, fg, label) {
	var sw = document.createElement('span');
	sw.className = 'settings-theme-swatch';
	sw.title = label;
	sw.style.background = bg;
	sw.style.color = fg;
	var dot = document.createElement('span');
	dot.className = 'settings-theme-swatch-dot';
	dot.style.background = fg;
	sw.appendChild(dot);
	var lbl = document.createTextNode(label);
	sw.appendChild(lbl);
	return sw;
}

async function _fetchThemeColors(th) {
	if (_themeColorCache[th.id]) return _themeColorCache[th.id];
	try {
		var data = null;
		if (th.builtin) {
			data = await window.go.main.App.GetTheme(th.id);
		} else {
			data = await window.go.main.App.GetExternalColorTheme(th.id);
		}
		if (data && Object.keys(data).length > 0) {
			_themeColorCache[th.id] = data;
			return data;
		}
	} catch(e) {}
	// Fallback: empty data means use defaults
	_themeColorCache[th.id] = {};
	return {};
}

// ---- theme grid rendering ----
async function renderSettingsThemeGrid() {
	var grid = document.getElementById('settingsThemeGrid');
	if (!grid) return;
	var themes = state.colorThemes || [];
	// Pre-fetch colors for all themes
	for (var ti = 0; ti < themes.length; ti++) { await _fetchThemeColors(themes[ti]); }
	// Load built-in system theme data
	var sysData = window._systemThemeData;
	if (!sysData && window.go && window.go.main) {
		try { sysData = await window.go.main.App.GetTheme('system'); window._systemThemeData = sysData || {}; } catch(e) { window._systemThemeData = {}; }
	}
	grid.innerHTML = '';
	themes.forEach(function(th) {
		var data = _themeColorCache[th.id] || {};
		// system theme: use actual computed CSS --bg/--fg as fallback since its data is empty
		if (th.id === 'system') data = _systemThemeComputedFallback();
		var card = document.createElement('div');
		card.className = 'settings-theme-card' + (state.colorThemeId === th.id ? ' is-selected' : '');
		var name = document.createElement('span');
		name.className = 'settings-theme-card-name';
		name.textContent = th.name || th.id;
		card.appendChild(name);
		var modes = th.modes || ['dark'];
		var swatches = document.createElement('span');
		swatches.className = 'settings-theme-swatches';
		if (modes.indexOf('dark') >= 0) {
			var dc = _themeColorsForMode(data, 'dark');
			swatches.appendChild(_makeSwatch('dark', dc.bg, dc.fg, t('theme.dark_label','深色')));
		}
		if (modes.indexOf('light') >= 0) {
			var lc = _themeColorsForMode(data, 'light');
			swatches.appendChild(_makeSwatch('light', lc.bg, lc.fg, t('theme.light_label','浅色')));
		}
		card.appendChild(swatches);
		card.addEventListener('click', function() {
			if (th.external) {
				window.go.main.App.GetExternalColorTheme(th.id).then(function(data2) {
					if (data2 && Object.keys(data2).length > 0) {
						var style = document.getElementById('ext-theme-style');
						if (!style) { style = document.createElement('style'); style.id = 'ext-theme-style'; document.head.appendChild(style); }
						var css = ':root{';
						for (var k in data2) { if (k.indexOf('_') !== 0) css += k + ':' + data2[k] + ';'; }
						css += '}';
						style.textContent = css;
					}
					state.colorThemeId = th.id;
					applyColorTheme();
					saveSettings();
					renderSettingsThemeGrid();
				}).catch(function(){});
			} else {
				state.colorThemeId = th.id;
				applyColorTheme();
				saveSettings();
				renderSettingsThemeGrid();
			}
		});
		grid.appendChild(card);
	});
}

function _systemThemeComputedFallback() {
	var cs = getComputedStyle(document.documentElement);
	// The system theme has no explicit data — it follows the browser's
	// prefers-color-scheme.  Sample the current computed colours for dark
	// mode and use the built-in light-mode defaults for light mode (both
	// must match the values in style.css :root / media-query blocks).
	return {
		common: {},
		dark:  {
			'--bg': cs.getPropertyValue('--bg').trim() || '#1e1e1e',
			'--fg': cs.getPropertyValue('--fg').trim() || '#d4d4d4'
		},
		light: {
			'--bg': '#f3f3f3',
			'--fg': '#1a1a1a'
		}
	};
}

function renderSettingsIconGrid() {
	var grid = document.getElementById('settingsIconGrid');
	if (!grid) return;
	grid.innerHTML = '';
	var themes = state.iconThemes || [];
	themes.forEach(function(th) {
		var card = document.createElement('div');
		card.className = 'settings-theme-card' + (state.iconThemeId === th.id ? ' is-selected' : '');
		if (th._i18nKey) { card.textContent = t(th._i18nKey, '内置默认'); }
		else if (th.name && typeof th.name === 'object') { card.textContent = _themeName(th.name); }
		else { card.textContent = th.name || th.id; }
		card.addEventListener('click', function() {
			state.iconThemeId = th.id;
			saveSettings();
			renderSettingsIconGrid();
		});
		grid.appendChild(card);
	});
}

// ---- language list in settings ----
async function refreshSettingsLangList() {
	var list = document.getElementById('settingsLangList');
	if (!list) return;
	// Refresh external languages directly via Go API
	try {
		window._externalLangs = await window.go.main.App.ListExternalI18n() || [];
	} catch(e) { window._externalLangs = []; }
	var lang = state.language || 'zh';
	var builtinLangs = ['zh','zh-Hant','en','fr','es','ru','ar','ja','ko'];
	var all = [];
	for (var i = 0; i < builtinLangs.length; i++) {
		var lc = builtinLangs[i];
		var name = lc;
		if (lc === 'zh') name = '简体中文';
		else if (lc === 'zh-Hant') name = '繁體中文';
		else if (lc === 'en') name = 'English';
		else if (lc === 'fr') name = 'Français';
		else if (lc === 'es') name = 'Español';
		else if (lc === 'ru') name = 'Русский';
		else if (lc === 'ar') name = 'العربية';
		else if (lc === 'ja') name = '日本語';
		else if (lc === 'ko') name = '한국어';
		all.push({ code: lc, name: name, builtin: true });
	}
	if (window._externalLangs) {
		for (var j = 0; j < window._externalLangs.length; j++) {
			if (!builtinLangs.includes(window._externalLangs[j].lang)) {
				all.push({ code: window._externalLangs[j].lang, name: window._externalLangs[j].name, builtin: false });
			}
		}
	}
	list.innerHTML = '';
	var isFirstExt = true;
	for (var k = 0; k < all.length; k++) {
		var item = all[k];
		if (!item.builtin && isFirstExt) {
			var sep = document.createElement('div');
			sep.className = 'settings-lang-sep';
			list.appendChild(sep);
			isFirstExt = false;
		}
		var row = document.createElement('div');
		row.className = 'settings-lang-item' + (lang === item.code ? ' is-selected' : '');
		row.textContent = item.name;
		(function(code, el) {
			el.addEventListener('click', function() {
				el.parentNode.querySelectorAll('.settings-lang-item').forEach(function(s) { s.classList.remove('is-selected'); });
				el.classList.add('is-selected');
				if (typeof setLanguage === 'function') {
					setLanguage(code).then(function() {
						refreshSettingsLangList();
					}).catch(function() {
						refreshSettingsLangList();
					});
				}
			});
		})(item.code, row);
		list.appendChild(row);
	}
}

// ---- color picker integration ----
function _openColorPicker(trigger, defaultColor, onApply) {
	var picker = document.createElement('color-picker');
	picker.setAttribute('theme', document.documentElement.getAttribute('data-theme') || 'dark');
	picker.setAttribute('transparency', 'solid');
	var cur = trigger.style.background;
	// Pass current color to picker if it differs from default; fall back to default.
	if (cur && !/^\s*$/.test(cur) && cur !== defaultColor) picker.setAttribute('color', cur);
	else picker.setAttribute('color', defaultColor);
	document.body.appendChild(picker);
	picker.addEventListener('apply', function(e) {
		var c = e.detail.color;
		// Normalise to hex when possible; keep rgba() for semi-transparent colors.
		var m = c.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/i);
		if (m) {
			var a = m[4] ? parseFloat(m[4]) : 1;
			if (a >= 1) {
				c = '#' + [m[1], m[2], m[3]].map(function(x) { var h = parseInt(x).toString(16); return h.length === 1 ? '0' + h : h; }).join('');
			}
			// a < 1: keep as rgba() — valid in CSS color and preserves user intent
		}
		onApply(c);
		picker.remove();
	});
	picker.open(trigger);
}

// Apply display color overrides as CSS custom properties on :root
var _displayColorKeys = ['tsRx','tsTx','ts1','ts2','textRx','textTx','text1','text2','hexRx','hexTx','hex1','hex2','hexEsc','hexInv','ctrlBg','ctrlFg','stat','fhexEsc','fctrlBg','fctrlFg','fstat'];

function _applyDisplayColors() {
	var colors = state.displayColors || _defaultDisplayColors();
	var el = document.getElementById('display-colors-style');
	if (!el) {
		el = document.createElement('style');
		el.id = 'display-colors-style';
		document.head.appendChild(el);
	}
	var darkCSS = '', lightCSS = '';
	for (var i = 0; i < _displayColorKeys.length; i++) {
		var k = _displayColorKeys[i];
		var v = colors[k] || _defaultDisplayColors()[k] || {};
		var d = v.dark || '';
		var l = v.light || '';
		var bd = v.bgDark || '';
		var bl = v.bgLight || '';
		if (d) darkCSS += '--dc-' + k + ':' + d + ';';
		if (l) lightCSS += '--dc-' + k + ':' + l + ';';
		if (bd) darkCSS += '--dc-' + k + '-bg:' + bd + ';';
		if (bl) lightCSS += '--dc-' + k + '-bg:' + bl + ';';
		if (v.bold) { darkCSS += '--dc-' + k + '-bold:bold;'; lightCSS += '--dc-' + k + '-bold:bold;'; }
		if (v.italic) { darkCSS += '--dc-' + k + '-italic:italic;'; lightCSS += '--dc-' + k + '-italic:italic;'; }
	}
	el.textContent = ':root[data-theme="dark"] {' + darkCSS + '} :root[data-theme="light"] {' + lightCSS + '}';
}

function _buildColorResetBtn() {
	var btn = document.createElement('button');
	btn.className = 'c-icon-btn';
	btn.innerHTML = icon('restore_defaults', 16, 16);
	btn.title = t('settings.reset_colors','重置');
	btn.addEventListener('click', function() {
		state.displayColors = _defaultDisplayColors();
		_applyDisplayColors();
		saveSettings();
		refreshSettingsPage();
	});
	return btn;
}

function _buildColorRow(cfg) {
	var colors = state.displayColors || _defaultDisplayColors();

	// Resolve background color key and data
	var bgKey = cfg.bgKey;
	var bgV, bgDflt, bgDark, bgLight, bgBold, bgItalic;
	if (bgKey) {
		bgV = colors[bgKey] || {};
		bgDflt = (_defaultDisplayColors()[bgKey]) || {};
		bgDark = bgV.dark !== undefined ? bgV.dark : bgDflt.dark;
		bgLight = bgV.light !== undefined ? bgV.light : bgDflt.light;
		bgBold = bgV.bold !== undefined ? bgV.bold : (bgDflt.bold || false);
		bgItalic = bgV.italic !== undefined ? bgV.italic : (bgDflt.italic || false);
	} else {
		// Standard item: bg from same key's bgDark/bgLight
		bgKey = cfg.key;
		bgV = colors[bgKey] || {};
		bgDflt = (_defaultDisplayColors()[bgKey]) || {};
		bgDark = bgV.bgDark !== undefined ? bgV.bgDark : bgDflt.bgDark;
		bgLight = bgV.bgLight !== undefined ? bgV.bgLight : bgDflt.bgLight;
		bgBold = bgV.bold !== undefined ? bgV.bold : (bgDflt.bold || false);
		bgItalic = bgV.italic !== undefined ? bgV.italic : (bgDflt.italic || false);
	}

	// Resolve font color key and data
	var fgKey = cfg.fgKey || cfg.key;
	var fgV = colors[fgKey] || {};
	var fgDflt = (_defaultDisplayColors()[fgKey]) || {};
	var fgDark = fgV.dark !== undefined ? fgV.dark : fgDflt.dark;
	var fgLight = fgV.light !== undefined ? fgV.light : fgDflt.light;

	var row = document.createElement('div');
	row.className = 'c-color-row';
	// Label
	var lbl = document.createElement('span');
	lbl.className = 'c-color-label';
	lbl.textContent = t(cfg.i18n, cfg.fb);
	row.appendChild(lbl);

	// Bold checkbox
	var boldWrap = document.createElement('span');
	boldWrap.className = 'c-color-chk';
	var chkBold = document.createElement('input');
	chkBold.type = 'checkbox';
	chkBold.checked = bgBold;
	chkBold.title = t('settings.color_bold_tip','加粗显示');
	boldWrap.appendChild(chkBold);
	row.appendChild(boldWrap);

	// Italic checkbox
	var italicWrap = document.createElement('span');
	italicWrap.className = 'c-color-chk';
	var chkItalic = document.createElement('input');
	chkItalic.type = 'checkbox';
	chkItalic.checked = bgItalic;
	chkItalic.title = t('settings.color_italic_tip','斜体显示');
	italicWrap.appendChild(chkItalic);
	row.appendChild(italicWrap);

	// Helper: make a swatch pair (dark + light) inside a container
	function _makeSwatchPair(swrap, curDark, curLight, defDark, defLight, onDark, onLight) {
		function _one(cls, fill, pickerDef, onClick) {
			var outer = document.createElement('span');
			outer.className = 'c-color-swatch ' + cls;
			var inner = document.createElement('span');
			inner.className = 'c-color-swatch-fill';
			inner.style.background = fill;
			outer.appendChild(inner);
			outer.title = cls === 'sw-dark' ? t('settings.color_sw_dark','深色主题') : t('settings.color_sw_light','浅色主题');
			outer.addEventListener('click', function() {
				_openColorPicker(outer, pickerDef, function(color) { onClick(color); });
			});
			swrap.appendChild(outer);
			return { outer: outer, fill: inner };
		}
		_one('sw-dark', curDark, defDark, function(c) { onDark(c); });
		_one('sw-light', curLight, defLight, function(c) { onLight(c); });
	}

	// Background color swatches
	var bgWrap = document.createElement('span');
	bgWrap.className = 'c-color-swatches';
	_makeSwatchPair(bgWrap, bgDark, bgLight, bgDflt.dark || bgDflt.bgDark, bgDflt.light || bgDflt.bgLight,
		function(c) {
			if (cfg.bgKey) { bgV.dark = c; } else { bgV.bgDark = c; }
			bgDark = c; _updateSwatchFills(bgWrap, bgDark, bgLight); _persistColor(bgKey, bgV);
		},
		function(c) {
			if (cfg.bgKey) { bgV.light = c; } else { bgV.bgLight = c; }
			bgLight = c; _updateSwatchFills(bgWrap, bgDark, bgLight); _persistColor(bgKey, bgV);
		}
	);
	row.appendChild(bgWrap);

	// Font color swatches
	var fgWrap = document.createElement('span');
	fgWrap.className = 'c-color-swatches';
	_makeSwatchPair(fgWrap, fgDark, fgLight, fgDflt.dark, fgDflt.light,
		function(c) { fgV.dark = c; fgDark = c; _updateSwatchFills(fgWrap, fgDark, fgLight); _persistColor(fgKey, fgV); },
		function(c) { fgV.light = c; fgLight = c; _updateSwatchFills(fgWrap, fgDark, fgLight); _persistColor(fgKey, fgV); }
	);
	row.appendChild(fgWrap);

	// Bold + Italic change handlers
	chkBold.addEventListener('change', function() {
		bgBold = chkBold.checked;
		if (cfg.bgKey) {
			bgV.bold = bgBold; fgV.bold = bgBold;
			_persistColor(cfg.bgKey, bgV);
			_persistColor(cfg.fgKey, fgV);
		} else {
			bgV.bold = bgBold;
			_persistColor(bgKey, bgV);
		}
	});
	chkItalic.addEventListener('change', function() {
		bgItalic = chkItalic.checked;
		if (cfg.bgKey) {
			bgV.italic = bgItalic; fgV.italic = bgItalic;
			_persistColor(cfg.bgKey, bgV);
			_persistColor(cfg.fgKey, fgV);
		} else {
			bgV.italic = bgItalic;
			_persistColor(bgKey, bgV);
		}
	});

	return row;
}

// Update swatch fill backgrounds after color picker change
function _updateSwatchFills(container, dark, light) {
	var fills = container.querySelectorAll('.c-color-swatch-fill');
	if (fills.length >= 2) {
		fills[0].style.background = dark;
		fills[1].style.background = light;
	}
}

function _persistColor(key, v) {
	if (!state.displayColors) state.displayColors = _defaultDisplayColors();
	if (!state.displayColors[key]) {
		var d = (_defaultDisplayColors()[key]) || {};
		state.displayColors[key] = { dark: d.dark || '', light: d.light || '', bgDark: d.bgDark || '', bgLight: d.bgLight || '', bold: d.bold || false };
	}
	state.displayColors[key].dark = v.dark || '';
	state.displayColors[key].light = v.light || '';
	state.displayColors[key].bgDark = v.bgDark !== undefined ? v.bgDark : '';
	state.displayColors[key].bgLight = v.bgLight !== undefined ? v.bgLight : '';
	state.displayColors[key].bold = !!v.bold;
	_applyDisplayColors();
	_refreshColorPreview(key);
	saveSettings();
}

function _refreshColorPreview(key) {
		var previews = document.querySelectorAll(".c-color-preview-wrap");
		for (var pi = 0; pi < previews.length; pi++) {
			var pv = previews[pi];
			var items = pv._colorItems;
			if (!items) continue;
			for (var ii = 0; ii < items.length; ii++) {
				var it = items[ii];
				if (it.key === key || it.bgKey === key || it.fgKey === key) { _renderColorPreview(items, pv); return; }
			}
		}
	}

	function _renderColorPreview(items, container) {
	if (!container) return;
	container.innerHTML = '';
	var colors = state.displayColors || _defaultDisplayColors();
	var darkLight = ['dark','light'];
	var groupKey = items[0].key;
	var isSingle = groupKey === 'tsTx';
	var isFwd = groupKey === 'ts1';
	for (var m = 0; m < 2; m++) {
		var ml = darkLight[m];
		var box = document.createElement('div');
		box.className = 'c-color-preview ' + (m === 0 ? 'c-preview-dark' : 'c-preview-light');
		// Get color + bg + bold for a key
		function _clr(k) {
			var v = colors[k] || _defaultDisplayColors()[k] || {};
			if (v[ml]) return v[ml];
			var d = (_defaultDisplayColors()[k]) || {};
			return d[ml] || '';
		}
		function _cbg(k) {
			var v = colors[k] || _defaultDisplayColors()[k] || {};
			if (v.bgDark !== undefined && v.bgLight !== undefined) { return ml === 'dark' ? (v.bgDark || 'transparent') : (v.bgLight || 'transparent'); }
			var d = (_defaultDisplayColors()[k]) || {};
			var bv = ml === 'dark' ? d.bgDark : d.bgLight;
			return bv || 'transparent';
		}
		function _cbold(k) {
			var v = colors[k] || _defaultDisplayColors()[k] || {};
			if (v.bold !== undefined) return v.bold;
			var d = (_defaultDisplayColors()[k]) || {};
			return !!d.bold;
		}
		function _span(k, t) {
			var fw = _cbold(k) ? 'font-weight:bold;' : '';
			var bg = _cbg(k);
			return '<span style="color:' + _clr(k) + ';background:' + bg + ';' + fw + '">' + escHtml(t) + '</span>';
		}
		// Preview ctrl char — child-span structure matching decoder.js
		// .ctrl-mark (visible, colored) + .ctrl-real (font-size:0, hidden)
		var _ctrl = function(clbl, ws) {
			var cfgKey = isSingle ? 'ctrlFg' : 'fctrlFg';
			var cbgKey = isSingle ? 'ctrlBg' : 'fctrlBg';
			var fw = _cbold(cfgKey) ? 'font-weight:bold;' : '';
			return '<span style="background:' + _clr(cbgKey) + ';">'
				+ '<span class="ctrl-mark" style="color:' + _clr(cfgKey) + ';' + fw + '">' + escHtml(clbl) + '</span>'
				+ '<span class="ctrl-real">' + escHtml(ws) + '</span>'
				+ '</span>';
		};
		var html = '';
		if (isSingle) {
			// Rx text line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('tsRx', '[13:00:01.100] Rx ⇃ ') + _span('stat', '~5B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('textRx', 'Hi') + _span('hexEsc', '/01') + _ctrl('←', '\r') + _ctrl('↵', '\n') + '</div>';
			// Rx hex line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('tsRx', '[13:00:01.100] Rx ⇃ ') + _span('stat', '~5B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('hexRx', '48 69 01 0D 0A') + '</div>';
			// Tx text line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('tsTx', '[13:00:01.200] Tx ↾ ') + _span('stat', '~5B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('textTx', 'OK') + _span('hexEsc', '/FF') + _ctrl('←', '\r') + _ctrl('↵', '\n') + '</div>';
			// Tx hex line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('tsTx', '[13:00:01.200] Tx ↾ ') + _span('stat', '~5B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('hexTx', '4F 4B FF 0D 0A') + '</div>';
		} else if (isFwd) {
			// P1 text line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('ts1', '[13:00:02.100] P1 ') + _span('fstat', '~10B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('text1', '请求') + _span('fhexEsc', '/02') + _ctrl('←', '\r') + _ctrl('↵', '\n') + '</div>';
			// P1 hex line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('ts1', '[13:00:02.100] P1 ') + _span('fstat', '~10B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('hex1', 'E8 AF B7 E6 B1 82 02 0D 0A') + '</div>';
			// P2 text line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('ts2', '[13:00:02.200] P2 ') + _span('fstat', '~10B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('text2', '确认') + _span('fhexEsc', '/03') + _ctrl('←', '\r') + _ctrl('↵', '\n') + '</div>';
			// P2 hex line
			html += '<div style="font-size:10px;line-height:1.4">' +
				_span('ts2', '[13:00:02.200] P2 ') + _span('fstat', '~10B') + '</div>' +
				'<div style="font-size:11px;line-height:1.5">' +
				_span('hex2', 'E7 A1 AE E8 AE A4 03 0D 0A') + '</div>';
		}
		box.innerHTML = html;
		container.appendChild(box);
	}
}

// ---- refresh settings page ----
function refreshSettingsPage() {
	var page = document.querySelector('.settings-layout');
	if (!page) return;
	// Remember which panel is active before rebuilding
	var activeNav = page.parentNode.querySelector('.settings-nav-item.is-active');
	var activeKey = activeNav ? activeNav.getAttribute('data-nav') : 'display';
	var parent = page.parentNode;
	if (parent) {
		var newPage = buildSettingsPage();
		parent.replaceChild(newPage, page);
		// Restore the active panel
		var newNav = parent.querySelector('.settings-nav-item[data-nav="' + activeKey + '"]');
		var newPanel = parent.querySelector('.settings-panel[data-panel="' + activeKey + '"]');
		if (newNav) {
			parent.querySelectorAll('.settings-nav-item').forEach(function(el) { el.classList.remove('is-active'); });
			newNav.classList.add('is-active');
		}
		if (newPanel) {
			parent.querySelectorAll('.settings-panel').forEach(function(p) { p.classList.add('is-hidden'); });
			newPanel.classList.remove('is-hidden');
		}
		renderSettingsThemeGrid();
		renderSettingsIconGrid();
		refreshSettingsLangList();
	}
}

// ---- check if settings tab is active ----
function isSettingsTab() {
	var tab = getActiveTab();
	return tab && tab.type === 'settings';
}
