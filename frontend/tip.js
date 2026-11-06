// ---- 自绘悬浮提示 ----
//
// 原生 title 的提示是系统画的（英文环境字体、方角、延迟一两秒、不能换行），和菜单
// 那套毛玻璃完全两个观感。这里用 [data-tip] 统一接管：鼠标停上去 400ms 弹一张
// 和菜单同款的小卡，离开 / 按下 / 失焦 / 滚动立刻收回。
//
// 只接管**带 data-tip 的元素**，没写的元素保留 title 的原生行为（还没迁完的地方
// 不至于失去提示）。
var _tipEl = null;
var _tipTimer = null;
var _tipTarget = null;

function _tipHide() {
  if (_tipTimer) { clearTimeout(_tipTimer); _tipTimer = null; }
  _tipTarget = null;
  if (_tipEl && _tipEl.parentNode) _tipEl.parentNode.removeChild(_tipEl);
  _tipEl = null;
}

// 定位：默认贴在目标**上方**居中，上方放不下翻到下方，左右夹在视口里。
function _tipPlace(target) {
  if (!_tipEl || !target) return;
  var r = target.getBoundingClientRect();
  var tr = _tipEl.getBoundingClientRect();
  var gap = 6;
  var x = r.left + r.width / 2 - tr.width / 2;
  var y = r.top - tr.height - gap;
  if (y < 4) y = r.bottom + gap;
  if (x + tr.width + 4 > window.innerWidth) x = window.innerWidth - tr.width - 4;
  if (x < 4) x = 4;
  _tipEl.style.left = Math.round(x) + 'px';
  _tipEl.style.top = Math.round(y) + 'px';
}

function _tipShow(target) {
  var text = target.getAttribute('data-tip');
  if (!text) return;
  _tipHide();
  _tipTarget = target;
  var el = document.createElement('div');
  el.className = 'c-tip';
  el.textContent = text;
  document.body.appendChild(el);
  _tipEl = el;
  _tipPlace(target);
}

function _tipBind() {
  document.addEventListener('pointerover', function (e) {
    var el = e.target && e.target.closest ? e.target.closest('[data-tip]') : null;
    if (!el) { if (_tipEl) _tipHide(); return; }
    if (_tipTarget === el) return;
    if (_tipTimer) { clearTimeout(_tipTimer); _tipTimer = null; }
    if (_tipEl) _tipHide();
    var anchor = el;
    _tipTimer = setTimeout(function () { _tipTimer = null; _tipShow(anchor); }, 400);
  }, true);
  document.addEventListener('pointerdown', _tipHide, true);
  document.addEventListener('keydown', _tipHide, true);
  window.addEventListener('blur', _tipHide);
  // 滚动 / 尺寸变化时直接收：跟手重算容易抖，收掉再停一下重开更稳
  document.addEventListener('scroll', _tipHide, true);
  window.addEventListener('resize', _tipHide);
  window.addEventListener('tips-refresh', function () {
    // 换了语言 / 图标主题后 data-tip 文本已经更新，正在显示的那张卡收掉重来
    _tipHide();
  });
}
