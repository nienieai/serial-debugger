// Wails v3 bindings 挂载（shim）
//
// v3 的 bindings 是 ES module（`import ... from "/wails/runtime.js"`），而本项目
// 前端全部是普通 <script>（没有打包链）。这个文件是**唯一**的 module：把生成的
// 服务挂回 window.go.main.App，让 app.js 里那 100 多处 window.go.main.App.*
// 一行都不用改。
//
// 为什么 bindings 用 -names 生成（而不是默认的数字 ID）：
// 无构建链时 bindings 是和前端一起提交的，不参与构建；ID 会随 Go 方法增减而变，
// 忘记重新生成就会静默调到错误的方法。名字不会。
import * as AppService from './bindings/github.com/nienieai/serial-debugger/app.js';
import { Events, Clipboard, Application, Stream, JSONStream } from '/wails/runtime.js';

window.go = window.go || {};
window.go.main = window.go.main || {};
window.go.main.App = AppService;

// ---- v2 runtime 兼容层 ----
//
// 阶段 0 的目标是「行为等价迁移」：先把 window.runtime.* 兜住，app.js 里
// 15 处 EventsOn / ClipboardSetText / Quit 一处都不改，降低回归面。
// 阶段 1 拆模块时再逐处换成 window.wails.* 的原生调用。
//
// 关键差异在回调签名：
//   v2  EventsOn(name, (data) => ...)        —— 直接给数据
//   v3  Events.On(name, (event) => event.data)
// 这里把它转回 v2 的形态。
//
// 注意 OnFileDrop 没有兼容实现：v3 的原生拖放只能在 Go 侧收
// （WindowFilesDropped 事件），做不到纯前端注册。那一处必须改（见 app.js）。
//
// 千万不要写 `window.wails.Events = ...`：v3 的 runtime 自己已经把 API 挂到
// window.wails 上，且那些属性是**只读 getter**，赋值会抛
//   TypeError: Cannot set property Events of #<Object> which has only a getter
// 而这个异常发生在 module 顶层，会中断整个文件 —— 直接后果是下面这段兼容层
// 不执行、window.runtime 不存在、app.js 里 15 处 EventsOn 全被 try/catch 吞掉，
// 表现为「界面能渲染、但所有事件都收不到、内容区空白」。
window.runtime = window.runtime || {};
window.runtime.EventsOn = function (name, callback) {
  return Events.On(name, function (event) {
    callback(event ? event.data : undefined);
  });
};
window.runtime.ClipboardSetText = function (text) {
  return Clipboard.SetText(String(text));
};
window.runtime.Quit = function () {
  return Application.Quit();
};

if (window.console) {
  console.log('[wails3] bindings 已挂载，方法数: ' + Object.keys(AppService).length);
}

// v3 的 Stream / JSONStream 用来做 per-window 定向数据帧（事件系统做不到，
// 见 app.go emitSession 的注释）。window.wails 是只读 getter，不能往上加东西，
// 所以单独开一个命名空间给需要定向通道的脚本用。
window.__wailsApi = {
  Stream: Stream,
  JSONStream: JSONStream,
  Events: Events,
  Clipboard: Clipboard,
  Application: Application
};

// 广播一个「bindings 就绪」信号。
//
// 为什么需要它：本文件是 ES module（deferred），而其余前端文件都是普通 script，
// 它们**先**执行。任何在顶层（不在 DOMContentLoaded 里）调 window.go.* 的代码
// 都会拿到 undefined；而这类调用通常被 try/catch 包着，失败是静默的 ——
// 实测症状是 icons.js 拿不到内置图标集合，界面上所有图标退化成"方框+叉"。
// 需要早期用 Go 能力的脚本监听这个事件，而不是在顶层直接调用。
window.dispatchEvent(new Event('wails-ready'));
