// 对话框窗口的控制器。
//
// 职责很小：取内容 → 按主题变量上色 → 画按钮 → 把用户的选择回给 Go。
// 结果通过 ResolveDialog 交回，Go 再经 frames 流通知主窗口，主窗口 resolve
// 自己那个 Promise（见 app.js 的 _openDialog / _onDialogFrame）。

(function () {
  'use strict';

  var dlgId = '';
  var App = null;
  var primaryKey = '';
  var cancelKey = '';
  var sent = false;

  function byId(id) { return document.getElementById(id); }

  // bindings-global.js 是另一个 module，加载顺序理论上先于本文件，
  // 但为了稳妥还是轮询等一下（超时 3 秒）。
  function waitForBindings() {
    return new Promise(function (resolve, reject) {
      var tries = 0;
      (function tick() {
        var app = window.go && window.go.main && window.go.main.App;
        if (app) return resolve(app);
        if (++tries > 100) return reject(new Error('bindings 未就绪'));
        setTimeout(tick, 30);
      })();
    });
  }

  function applyVars(vars) {
    if (!vars) return;
    var root = document.documentElement;
    Object.keys(vars).forEach(function (k) {
      if (k.indexOf('--') === 0) root.style.setProperty(k, vars[k]);
    });
  }

  function send(actionKey) {
    if (sent || !actionKey) return;
    sent = true;

    var dontAskBox = byId('dlgDontAsk');
    var dontAskInput = byId('dlgDontAskInput');
    var dontAsk = !!(dontAskBox && !dontAskBox.classList.contains('is-hidden') &&
                     dontAskInput && dontAskInput.checked);

    // 先禁按钮，避免连点造成两次结算（Go 侧也幂等，这里是省一次往返）
    var btns = document.querySelectorAll('.c-dialog__btn');
    for (var i = 0; i < btns.length; i++) btns[i].disabled = true;

    App.ResolveDialog(dlgId, actionKey, dontAsk).catch(function (e) {
      if (window.console) console.error('[dialog] ResolveDialog 失败', e);
      sent = false;
      for (var j = 0; j < btns.length; j++) btns[j].disabled = false;
    });
  }

  function render(spec) {
    applyVars(spec.vars);
    document.title = spec.title || '串口调试工具';

    var msg = byId('dlgMsg');
    msg.textContent = spec.message || '';
    if (spec.mono) msg.classList.add('t-mono');

    if (spec.dontAsk) {
      byId('dlgDontAsk').classList.remove('is-hidden');
      byId('dlgDontAskText').textContent = spec.dontAskLabel || '不再提醒';
    }

    var box = byId('dlgActions');
    var primaryBtn = null;
    (spec.buttons || []).forEach(function (b) {
      var btn = document.createElement('button');
      btn.className = 'c-dialog__btn' + (b.kind === 'primary' ? ' is-primary' : '');
      btn.textContent = b.label;
      btn.setAttribute('data-key', b.key);
      btn.addEventListener('click', function () { send(b.key); });
      if (b.kind === 'primary' && !primaryBtn) primaryBtn = btn;
      if (b.key === 'cancel') cancelKey = b.key;
      box.appendChild(btn);
    });
    if (primaryBtn) primaryKey = primaryBtn.getAttribute('data-key');
    if (!primaryKey && spec.buttons && spec.buttons.length) primaryKey = spec.buttons[0].key;
    if (!cancelKey && spec.buttons && spec.buttons.length) cancelKey = spec.buttons[spec.buttons.length - 1].key;

    // 键盘：Enter = 主按钮，Esc = 取消（没有取消键就当作主按钮）
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); send(primaryKey); }
      else if (e.key === 'Escape') { e.preventDefault(); send(cancelKey); }
    });

    // 渲染完按实际内容高度调一次窗口大小（Go 侧只做了粗估）
    requestAnimationFrame(function () {
      var col = document.querySelector('.o-dialog-col');
      if (!col || !App) return;
      var need = col.scrollHeight + 2;
      App.ResizeDialog(dlgId, need).catch(function () { /* 忽略 */ });
    });

    if (primaryBtn) primaryBtn.focus();
  }

  function fail(err) {
    var msg = byId('dlgMsg');
    if (msg) msg.textContent = '对话框加载失败：' + err;
    if (window.console) console.error('[dialog]', err);
  }

  dlgId = new URLSearchParams(location.search).get('id') || '';
  if (!dlgId) { fail('缺少 id 参数'); return; }

  waitForBindings()
    .then(function (app) {
      App = app;
      return App.GetDialog(dlgId);
    })
    .then(render)
    .catch(fail);
})();
