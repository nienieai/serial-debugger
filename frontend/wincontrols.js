// 无边框窗口的窗口按钮（最小化 / 最大化 / 关闭）。
//
// 系统标题栏被拿掉了（Go 侧 Frameless），窗口控制改由菜单栏右侧这三个按钮承担。
// 走的是 App.WindowControl 服务方法，而不是运行时的 window.wails.Window：
// 后者对无边框窗口的 ToggleMaximise 只改状态位不改尺寸（实测窗口纹丝不动），
// 而且 Close 走服务方法才会经过「这个窗口里的会话要不要一起销毁」的确认。
//
// 拖动不在这里做：整条 .header 挂了 --wails-draggable: drag（见 style.css），
// 菜单项和按钮各自 no-drag。双击菜单栏空白处 = 最大化/还原，跟原生标题栏一致。
(function () {
  function api() {
    return (window.go && window.go.main && window.go.main.App) || null;
  }

  // bindings 是 ES module（deferred），普通 script 先跑：调用前一律判空。
  function send(action) {
    var app = api();
    if (!app || typeof app.WindowControl !== 'function') return Promise.resolve();
    return app.WindowControl(action).catch(function () {});
  }

  // 最大化状态没有事件可听，点完按钮主动问一次 + 窗口 resize 兜底。
  function syncMaxIcon() {
    var app = api();
    var btn = document.getElementById('winMax');
    if (!app || !btn || typeof app.IsMaximised !== 'function') return;
    app.IsMaximised().then(function (max) {
      btn.classList.toggle('is-max', !!max);
      btn.title = max ? '还原' : '最大化';
      btn.setAttribute('aria-label', btn.title);
    }).catch(function () {});
  }

  function init() {
    var min = document.getElementById('winMin');
    var max = document.getElementById('winMax');
    var close = document.getElementById('winClose');
    if (!min || !max || !close) return;
    min.addEventListener('click', function () { send('minimise'); });
    max.addEventListener('click', function () {
      send('toggle-maximise').then(function () { syncMaxIcon(); });
    });
    close.addEventListener('click', function () { send('close'); });

    var header = document.querySelector('.header');
    if (header) {
      header.addEventListener('dblclick', function (e) {
        // 双击菜单项是「展开菜单」，双击按钮是按钮自己的事，都不该切窗口状态
        if (e.target.closest('.menu-item, .c-theme-btn, .win-ctrl')) return;
        send('toggle-maximise').then(function () { syncMaxIcon(); });
      });
    }
    window.addEventListener('resize', syncMaxIcon);
    syncMaxIcon();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
  // bindings 挂好之后再来一次：init 时 window.go 可能还不存在
  window.addEventListener('wails-ready', syncMaxIcon);
})();
