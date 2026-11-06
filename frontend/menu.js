// ---- 统一浮层菜单 ----
//
// 全应用只有**一种**弹出的菜单：毛玻璃底 + 四栏网格（勾选 22px / 文本 / 快捷键 / 箭头）。
// 顶部菜单栏那个是「悬停展开」的，不走这里；这里管的是所有浮层：
//   标签右键 / 数据区右键 / 输入框右键 / 日志页与 .stlog 文件页右键 / 状态栏各项的选项
//
// 为什么非要自己画：WebView2 的原生菜单是英文 + 系统样式，和自绘的玻璃皮是两套观感；
// 同一份「复制 / 粘贴 / 复制全部」在发送框里是原生菜单、在数据区里是自绘菜单，
// 用户看到的就是「风格不统一」。所以这里把开合、定位、贴边、自动关闭收敛成一份，
// 各处只负责给条目。
//
// 一条硬规则：**不监听 scroll**。菜单是 position:fixed，页面怎么滚都不会错位；
// 而数据区刷屏时每帧都在滚，监听了 scroll 就会「菜单刚出来就被自己关掉」，
// 表现为刷屏时右键打不开（标签菜单的老注释里记着这个坑）。
var _cMenuEl = null;

function cMenuIsOpen() { return !!_cMenuEl; }

// 菜单项沿用顶部菜单的四栏网格。opts:
//   checked   勾选态
//   disabled  禁用（灰色、不响应）
//   shortcut  右侧快捷键文案
//   onClick   点击后执行（默认先收菜单）
function cMenuItem(label, opts) {
  opts = opts || {};
  var btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'tab-menu-item';
  btn.setAttribute('role', 'menuitem');
  var chk = document.createElement('span');
  chk.className = 'menu-chk chk' + (opts.checked ? ' on' : '');
  btn.appendChild(chk);
  var txt = document.createElement('span');
  txt.className = 'menu-text';
  txt.textContent = label;
  btn.appendChild(txt);
  var shortcut = document.createElement('span');
  shortcut.className = 'menu-shortcut';
  shortcut.textContent = opts.shortcut || '';
  btn.appendChild(shortcut);
  var arrow = document.createElement('span');
  arrow.className = 'menu-arrow';
  btn.appendChild(arrow);
  btn.addEventListener('click', function (e) {
    e.stopPropagation();
    if (opts.disabled) return;
    cMenuClose();
    if (opts.onClick) opts.onClick();
  });
  if (opts.disabled) btn.disabled = true;
  return btn;
}

function cMenuSep() {
  var sep = document.createElement('div');
  sep.className = 'menu-sep';
  return sep;
}

function _cMenuOnDown(e) {
  if (_cMenuEl && !_cMenuEl.contains(e.target)) cMenuClose();
}

function _cMenuOnKey(e) {
  if (e.key === 'Escape') cMenuClose();
}

function cMenuClose() {
  if (!_cMenuEl) return;
  var el = _cMenuEl;
  _cMenuEl = null;
  if (el.parentNode) el.parentNode.removeChild(el);
  document.removeEventListener('pointerdown', _cMenuOnDown, true);
  document.removeEventListener('keydown', _cMenuOnKey, true);
  window.removeEventListener('blur', cMenuClose);
  window.removeEventListener('resize', cMenuClose);
}

// cMenuOpen 打开一个浮层菜单。opts:
//   el         已经拼好的 DOM（沿用老代码的构建方式时用）
//   items      条目数组：[{ label, checked, disabled, shortcut, onClick } | { sep: true }]
//   x, y       鼠标坐标（右键菜单）
//   anchor     锚点元素（状态栏那几项）：贴在它上方，左边缘对齐
//   placement  'down'（默认）| 'up'
//   className  额外的容器类
function cMenuOpen(opts) {
  opts = opts || {};
  cMenuClose();
  var el = opts.el;
  if (!el) {
    el = document.createElement('div');
    el.className = 'menu-dropdown tab-menu' + (opts.className ? ' ' + opts.className : '');
    el.setAttribute('role', 'menu');
    (opts.items || []).forEach(function (it) {
      if (!it) return;
      if (it.sep) { el.appendChild(cMenuSep()); return; }
      el.appendChild(cMenuItem(it.label, it));
    });
  }
  document.body.appendChild(el);

  var mw = el.offsetWidth || 176;
  var mh = el.offsetHeight || 40;
  var x = 0;
  var y = 0;
  if (opts.anchor && typeof opts.anchor.getBoundingClientRect === 'function') {
    // 状态栏那种：贴着这一项弹。收不住就翻到另一侧，左右也留 4px。
    var r = opts.anchor.getBoundingClientRect();
    x = r.left;
    y = (opts.placement === 'up') ? (r.top - mh - 6) : (r.bottom + 6);
    if (y + mh + 4 > window.innerHeight || y < 4) y = Math.max(4, r.top - mh - 6);
    if (x + mw + 4 > window.innerWidth) x = Math.max(4, window.innerWidth - mw - 4);
  } else {
    // 右键那种：以光标为左上角，贴边翻转。
    x = opts.x || 0;
    y = opts.y || 0;
    if (x + mw + 4 > window.innerWidth) x = Math.max(4, x - mw);
    if (y + mh + 4 > window.innerHeight) y = Math.max(4, y - mh);
  }
  el.style.left = Math.round(x) + 'px';
  el.style.top = Math.round(y) + 'px';

  _cMenuEl = el;
  document.addEventListener('pointerdown', _cMenuOnDown, true);
  document.addEventListener('keydown', _cMenuOnKey, true);
  window.addEventListener('blur', cMenuClose);
  window.addEventListener('resize', cMenuClose);
  return el;
}

// ---- 输入框右键：拿回被原生菜单占着的那几项 ----
//
// 发送框、重命名输入框、设置页的数字框都走这一套：复制 / 粘贴 / 全选。
// 粘贴优先用 navigator.clipboard（WebView2 里 document.execCommand('paste')
// 早就不保证了），拿不到就退化成什么都不做 —— 总比弹出英文原生菜单好。
function _cMenuInputPaste(input) {
  if (!navigator.clipboard || !navigator.clipboard.readText) return;
  navigator.clipboard.readText().then(function (text) {
    if (!text) return;
    var start = input.selectionStart;
    var end = input.selectionEnd;
    var value = input.value || '';
    input.value = value.slice(0, start) + text + value.slice(end);
    var caret = start + text.length;
    try { input.setSelectionRange(caret, caret); } catch (_) {}
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }).catch(function () {});
}

function _cMenuInputCopy(input) {
  var start = input.selectionStart;
  var end = input.selectionEnd;
  if (start === end) return;
  var text = (input.value || '').slice(start, end);
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text);
  } catch (_) {}
}

function cMenuBindInputs() {
  document.addEventListener('contextmenu', function (e) {
    var el = e.target;
    if (!el || !el.tagName) return;
    var tag = el.tagName.toLowerCase();
    if (tag !== 'textarea' && tag !== 'input') return;
    if (el.type === 'color' || el.type === 'range' || el.type === 'checkbox' || el.type === 'radio') return;
    e.preventDefault();
    var hasSel = (el.selectionEnd || 0) > (el.selectionStart || 0);
    var canRead = !!(navigator.clipboard && navigator.clipboard.readText);
    cMenuOpen({
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: t('ctxmenu.copy', '复制'), disabled: !hasSel, onClick: function () { _cMenuInputCopy(el); } },
        { label: t('ctxmenu.paste', '粘贴'), disabled: !canRead, onClick: function () { _cMenuInputPaste(el); } },
        {
          label: t('ctxmenu.select_all', '全选'),
          onClick: function () { try { el.select(); } catch (_) {} }
        }
      ]
    });
  }, true);
}
