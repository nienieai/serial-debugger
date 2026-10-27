// ---- Icon library ----
// 图标集合完全由资源文件决定：Go 侧读 builtin/icons/<pack>/*.svg（磁盘优先）或嵌入的
// config/icons/<pack>/，key = 文件名去掉 .svg。前端不再维护硬编码图标表，
// 缺 key 时统一回退到 _ICON_MISSING（方框包一个叉），缺哪个键一眼能看出来。
// Render with icon(key, w, h). Colors inherit via currentColor.

// 唯一的回退图标：方框 + 叉
var _ICON_MISSING = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9.2 9.2l5.6 5.6M14.8 9.2l-5.6 5.6"/></svg>';

var activeIcons = {};    // 当前生效的图标（内置集合 + 可能叠加的外置包）
var _builtinIcons = {};  // 内置集合快照，切回「内置默认」时用它还原

// 读取内置图标（key 由后端按文件名生成）。页面一加载就开始，结果同时给
// initIconThemes() 用——那里要等它完成，否则内置集合还没到手就被外置包覆盖了。
var _iconsReady = (async function _initIcons() {
	try {
		_builtinIcons = (await window.go.main.App.GetBuiltinIcons()) || {};
	} catch(e) {
		_builtinIcons = {};
	}
	activeIcons = Object.assign({}, _builtinIcons);
	return _builtinIcons;
})();

// 把图标包叠到内置集合上；ext 为空表示回到内置默认。
function applyIconTheme(ext) {
	var base = Object.assign({}, _builtinIcons);
	if (!ext || !ext.icons) { activeIcons = base; return false; }
	activeIcons = Object.assign(base, ext.icons);
	return true;
}

function icon(key, w, h, attrs) {
	var svg = activeIcons[key];
	var missing = !svg;
	if (missing) svg = _ICON_MISSING;
	w = w || 16;
	h = h || 16;
	var extra = 'width="' + w + '" height="' + h + '"'
		+ (attrs ? ' ' + attrs : '')
		+ (missing ? ' data-icon-missing="' + key + '"' : '');
	return svg.replace('<svg', '<svg ' + extra);
}

// Auto-populate elements with [data-icon] attribute on DOM ready.
function _populateIcons() {
	document.querySelectorAll('[data-icon]').forEach(function(el) {
		var key = el.getAttribute('data-icon');
		var w = el.getAttribute('data-icon-w') || 16;
		var h = el.getAttribute('data-icon-h') || 16;
		var attrs = el.getAttribute('data-icon-attrs') || '';
		el.innerHTML = icon(key, parseInt(w), parseInt(h), attrs);
	});
}
document.addEventListener('DOMContentLoaded', function() {
	_iconsReady.then(function() { _populateIcons(); });
});
