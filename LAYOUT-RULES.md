# 布局约束规则

> 本文件记录各区域元素的宽度 / 拉伸 / 裁剪策略，避免反复拉扯。
> 适用范围：`serial-tool`（v0.6.4）前端，核对基准为 `frontend/style.css` 与 JS 构建的 DOM（`tabpage.js` / `settingspage.js` / `app.js`）。
> 规则格式：每条规则 = CSS 选择器 + 关键 CSS + 策略。改样式前先查本文件，改后请同步更新。

## 速查索引

| 类名 / ID | 章节 |
| --- | --- |
| `.cs-wrap` `.cs-trigger` `.cs-dropdown` | [2. 自定义下拉框](#2-自定义下拉框-cs-) |
| `.config-bar` `.port-wrap` `.baud-wrap` | [3. 配置栏](#3-配置栏-config-bar) |
| `.tabs-bar` `.tabs-scroll` `.tab-new` | [4. 标签栏](#4-标签栏-tabs-bar) |
| `.send-controls` `.send-scroll-area` `.send-scroll-wrap` | [5. 发送工具栏](#5-发送工具栏-send-controls) |
| `.display-area` `.display-content` `.c-fwd-display` | [6. 显示区](#6-显示区-display-area) |
| `.quick-panel` `.o-divider--quick` `.qp-table` | [7. 快速面板](#7-快速面板-quick-panel) |
| `#settingsOverlay` `.c-form-row` `.c-form-col` | [8. 串口详细设置窗](#8-串口详细设置窗-settingsoverlay) |
| `.settings-layout` `.settings-nav` `.settings-body` | [9. 设置页](#9-设置页-settings-layout) |
| `.settings-bubble` | [10. 气泡卡片](#10-气泡卡片-settings-bubble) |
| `.c-color-row` `.c-color-head` `.c-color-swatch` | [11. 数据高亮颜色设置](#11-数据高亮颜色设置-c-color-row) |
| `.ctrl-char` `.ctrl-mark` `.ctrl-real` | [12. 控制字符渲染](#12-控制字符渲染) |
| `.c-color-preview` | [13. 预览气泡](#13-预览气泡-c-color-preview) |
| `.statusbar` | [14. 状态栏](#14-状态栏-statusbar) |
| `.config-bar::before` `.send-controls::before` 等 | [15. 模糊渐变条](#15-模糊渐变条) |

---

## 1. 全局骨架与裁剪链

`body` / `#app` 纵向弹性：`body { height: 100vh; display: flex; flex-direction: column; overflow: hidden; }`，`#app` 同样纵向撑满。菜单栏、标签栏、主内容区、状态栏自上而下排列，主内容区是唯一可伸缩区域。

主内容区各容器逐层裁剪，形成 **overflow 链**：

| 元素 | overflow | 说明 |
| --- | --- | --- |
| `.main-content` | `hidden` | `flex: 1; min-height: 0` |
| `.tab-page` | `hidden` | 每个标签页的根容器 |
| `.tab-page-inner` | 未设置（默认 visible） | 悬浮条 `.config-bar` 的定位上下文；**不参与裁剪** |
| `.content-row` | `hidden` | `flex: 1; min-height: 0`，左右分栏 |
| `.display-area` | `hidden` | `flex: 1; min-height: 0; position: relative` |

**规则：链上每个 flex 子项都必须带 `min-height: 0`，否则内容会顶开布局；新增内容区子元素一律 `overflow: hidden`。**

弹窗与下拉框如何突破此链：

- 自定义下拉框 `.cs-dropdown` 打开时 **portal 到 `document.body`**（见 [2](#2-自定义下拉框-cs-)），彻底脱离裁剪链；颜色选择器（`color-picker.js`）同样 portal 到 `body`。
- 模态弹窗（串口详细设置 `#settingsOverlay`、通知 `#alertOverlay`、主题 `#themeOverlay`）为 `o-overlay--modal`（`position: fixed; z-index: 100`），挂载于 `#app` 下，不受裁剪链影响。
- 离线遮罩 `#offlineOverlay` 挂载于 `#mainContent` 内（`.o-overlay`，`position: absolute` 只覆盖页面区），不遮挡菜单栏与状态栏。

## 2. 自定义下拉框 (.cs-*)

`initCustomSelect()` 把原生 `<select>` 包裹为 `.cs-wrap > .cs-trigger + .cs-dropdown`。原生 select 被设为 `position: absolute; inset: 0; opacity: 0; pointer-events: none`，不参与布局。

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.cs-wrap` | `display: inline-flex; position: relative; user-select: none` | 宽度默认跟随内容 |
| `.cs-wrap select` | `position: absolute; inset: 0; opacity: 0; pointer-events: none` | 原生 select 仅作语义容器 |
| `.cs-trigger` | `width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: 0 26px 0 8px` | 截断过长文字；默认撑满 `.cs-wrap` |
| `.cs-text` | `overflow: hidden; text-overflow: ellipsis; min-width: 0` | 选中项文字截断 |
| `.cs-arrow` | `position: absolute; right: 5px; top: 50%; transform: translateY(-50%)` | 箭头不占布局 |
| `.cs-dropdown` | 打开时 `is-portalled`：portal 到 `body`，JS 内联 `position: fixed` + `left/top/minWidth/maxWidth/maxHeight` | 不受父容器 `overflow: hidden` 裁剪 |

**规则：原生 `<select>` 的 width 对可见布局无效，必须约束 `.cs-trigger` 或 `.cs-wrap`。**

portal 细节（`app.js` `_csOpenDropdown()`）：

- 下拉框以 `trigger.getBoundingClientRect()` 定位：`top = trigger.bottom + 2`，`minWidth = trigger.width`。
- `max-height` 从 trigger 底部算到 `.statusbar` 顶部（减 6px），下限 80px；超出则内部滚动。
- 滚动（捕获阶段监听 `document` scroll）与窗口 resize 时自动关闭 / 跟随重定位。
- 关闭时移回原 `.cs-wrap` 并清空内联样式。

## 3. 配置栏 (config-bar)

`.tab-page-inner > .config-bar` 是**悬浮条**：`position: absolute; top: 0; left: 0; right: 0; z-index: 5; height: 44px`，透明背景 + 模糊渐变（见 [15](#15-模糊渐变条)）。内容区顶部为其预留空间（`.display-content` 顶部 `padding: 74px`，`.quick-panel` 顶部 `padding-top: 44px`）。

端口 / 波特率下拉框：

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.port-wrap` | `flex: 1; min-width: 0; overflow: hidden` | 弹性填充，文字截断，不顶开布局 |
| `.port-wrap > .cs-wrap` | `width: 100%` | 填满父容器，否则 inline-flex 不撑满 |
| `.baud-wrap` | `width: 80px; flex-shrink: 0` | 定宽，不伸缩 |
| `.baud-wrap .cs-text` | `overflow: visible; text-overflow: unset` | 编辑模式允许文字溢出可见 |

## 4. 标签栏 (.tabs-bar)

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.tabs-bar` | `display: flex; align-items: flex-end; height: 36px; overflow: visible; position: relative` | 激活标签 `margin-bottom: -1px` 延伸至页面；`:after` 绘制底部 1px 分隔线 |
| `.tabs-scroll` | `overflow-x: auto; overflow-y: visible; scrollbar-width: none` | 内容宽度自适应，隐藏滚动条 |
| `.tab-btn.is-active` | `height: 33px; padding-bottom: 8px; margin-bottom: -1px; z-index: 2` | 激活态变高，向下覆盖分隔线 |
| `.tab-new` | `flex-shrink: 0; width: 28px; height: 28px; align-self: center` | 固定在 `.tabs-scroll` 外右侧（`index.html` 中为 `class="tab-btn tab-new"`） |

## 5. 发送工具栏 (.send-controls)

`.send-controls` 自身是**悬浮条**：`position: absolute; top: 0; left: 0; right: 0; z-index: 2; padding: 6px 8px`，透明背景 + 模糊渐变（见 [15](#15-模糊渐变条)）。其内部按钮统一 `height: 28px`（`.send-controls button`）。

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.send-scroll-area` | `position: relative; flex-shrink: 1; min-width: 0; display: flex` | 横滚容器，为浮动箭头提供定位上下文 |
| `.send-scroll-wrap` | `overflow-x: auto; flex: 1; min-width: 0; scrollbar-width: none; cursor: grab` | 隐藏滚动条，鼠标拖拽滚动 |
| `.send-ctrl-group` | `display: flex; gap: 8px; flex-shrink: 0; > * { flex-shrink: 0 }` | 按钮组不参与压缩 |
| `.send-controls .cs-wrap:has(#appendSelect) .cs-trigger` | `width: 74px` | 定宽，不被选项文字撑开 |

浮动箭头（`.send-scroll-arrow--left/right`）：`position: absolute; width: 34px; pointer-events: none`，渐变背景提示可滚动方向，不占布局空间。

## 6. 显示区 (.display-area)

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.display-area` | `flex: 1; min-height: 0; overflow: hidden; position: relative` | 唯一可见滚动的是内部 `.display-content` |
| `.display-content` | `flex: 1; overflow-y: auto; padding: 74px 12px 8px 12px`；底部 `padding-bottom: 40px` | 顶部 74px 为悬浮 config-bar 预留；底部 40px 为转发浮动按钮栏预留 |
| `.display-area.c-fwd-display` | `flex: 1 1 0px !important; min-height: 0 !important; overflow: auto !important` | 转发模式：隐藏发送区后接管全部空间 |

转发模式布局切换（JS `updateModeUI()`）：

- 单端口 → 转发：`#sendArea`、`.o-divider`、`#quickDivider`、`.quick-panel` 全部 `is-hidden`，`.display-area` 加 `c-fwd-display`。
- 浮层按钮显隐由 **JS 按模式切换 `is-hidden-single` / `is-hidden-fwd` 标记**（不是 CSS 父级选择器）。`.c-fwd-display .c-fwd-float-btns::before` 仅承载模糊渐变（见 [15](#15-模糊渐变条)）。

## 7. 快速面板 (.quick-panel)

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.quick-panel` | `min-width: 0; overflow: hidden; display: flex; flex-direction: column; padding-top: 44px` | 宽度由 flex 比例控制，`padding-top` 为 config-bar 预留 |
| `.o-divider--quick` | `width: 5px; cursor: ew-resize; position: relative; z-index: 3` | 拖动调整面板宽度（`::before` 扩展热区） |
| `.qp-table` | `display: grid; grid-template-columns: 14px minmax(40px, max-content) minmax(40px, max-content) minmax(80px, 1fr) minmax(40px, max-content) minmax(40px, max-content) minmax(28px, max-content)` | 表头与行共享列宽 |
| `.qp-hdr` / `.qp-row` | `display: grid; grid-template-columns: subgrid; grid-column: 1 / -1` | CSS Subgrid，表头 sticky 顶部 |

宽度分配（JS `applyQuickPanelRatio()`）：`state.quickPanelRatio`（默认 `0.25`）控制比例，`leftArea` 取 `1 - r`、`.quick-panel` 取 `r`（`flex: r 1 0px`）；`r <= 0.01` 时隐藏面板。转发模式下面板强制隐藏。

## 8. 串口详细设置窗 (#settingsOverlay)

模态弹窗（`o-overlay--modal`），内含 `#settingsSingle`（单端口）与 `.settings-split`（转发双列）。label 与控件用 **grid auto 列对齐**，同菜单栏：

- `#settingsSingle, .c-form-col`：`display: grid; grid-template-columns: auto 1fr`，label 列 `auto` 自动匹配同列最宽项。
- `#settingsSingle > .c-form-row, .c-form-col > .c-form-row`：`display: contents`，消去行包装，让 grid 直接作用于 label 与 select。
- `.c-form-row__label`：`font-size: 13px; white-space: nowrap`，不换行。

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.c-form-row` | `display: flex; align-items: center; gap: 8px; margin-bottom: 8px` | 行内排列（非 grid 容器时） |
| `.c-form-row .cs-trigger` / `select` | `width: 100px; height: 26px` | 定宽，不被选中项文字撑开 |
| `.c-form-col .cs-trigger` / `select` | `width: 130px` | 转发模式列更宽 |
| `.c-form-row` / `.c-form-col` 原生 select | 上述 width | 对可见布局无效，仅保留作为语义标记 |
| `.c-form-col__title` | `grid-column: 1 / -1` | 跨整行的小列标题 |

## 9. 设置页 (.settings-layout)

**响应式断点：800px（`@media (min-width: 800px)`）。** 宽屏左右分栏，窄屏导航变为悬浮覆盖层。设置页是独立标签页（v0.6.0 起），非模态。

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.settings-page-inner` | `overflow: hidden; background: var(--input-bg)` | 隐藏滚动，背景与历史框一致 |
| `.settings-wrapper` | `display: flex; flex-direction: column; height: 100%; position: relative; overflow: hidden` | 纵向弹性，定位上下文 |
| `.settings-header` | `position: absolute; top: 0; left: 0; right: 0; z-index: 5; height: 52px` | 悬浮标题栏，透明 + 模糊渐变（见 [15](#15-模糊渐变条)） |
| `.settings-header-title` | `display: flex; align-items: center; gap: 2px; padding: 0 8px; flex-shrink: 0`；宽屏 `width: 200px; padding: 0 8px 0 16px` | 标题 + 汉堡键，宽屏固定宽度对齐左栏 |
| `.settings-nav-toggle` | 无边框按钮 `width: 36px; height: 36px`；窄屏 `display: flex`，宽屏 `display: none` | 汉堡菜单切换键，窄屏可见 |
| `.settings-header-search` | `flex: 1; justify-content: flex-end; padding: 0 16px`（窄）；宽屏 `justify-content: center; padding: 0 32px` | 搜索区域，窄屏贴右宽屏居中 |
| `.settings-search-wrap` | `position: relative; width: 100%; max-width: 360px`（窄）/ `max-width: 720px`（宽） | 搜索框包裹，内嵌图标 |
| `.settings-search-icon` | `position: absolute; left: 10px; top: 50%; transform: translateY(-50%); pointer-events: none` | 放大镜图标 |
| `.settings-search-clear` | `position: absolute; right: 6px; top: 50%; transform: translateY(-50%)`；无文字时 `is-hidden` | 叉图标清空搜索 |
| `.settings-search` | `display: block; width: 100%; height: 30px; padding: 0 32px; border-radius: 6px` | 搜索输入框，两侧避开图标 |
| `.settings-layout` | `display: flex; flex-direction: row; flex: 1; min-height: 0; position: relative` | 左右分栏，填满标题栏下方空间 |
| `.settings-body` | `flex: 1; min-height: 0; overflow-y: auto; padding: 60px 16px 12px 16px`（窄）/ `60px 32px 12px 32px`（宽） | 右侧内容区，唯一滚动容器 |
| `.settings-panel` | 窄屏 `max-width: 100%`；宽屏 `max-width: 720px; margin: 0 auto` | 面板居中约束 |
| `.settings-nav` | 窄屏：`position: absolute; top: 52px; bottom: 0; left: 0; z-index: 90; width: 240px; transform: translateX(-100%); transition: transform 0.25s ease`；宽屏：`position: relative; width: 200px; padding: 60px 16px 12px 16px; z-index: auto; transform: none` | 响应式侧边栏；宽屏以 `::after` 绘制右侧 1px 分隔线 |
| `.settings-nav.is-open` | `transform: translateX(0)` | 窄屏下展开浮层 |
| `.settings-nav-backdrop` | 默认 `display: none`；`.is-shown` 时 `display: block; position: absolute; inset: 0; z-index: 89; background: rgba(0,0,0,0.3)` | 窄屏遮罩，点击关闭 |
| `.settings-nav-item` | `padding: 8px 20px; border-left: 3px solid transparent` | 选中时（`.is-active`）左侧蓝色竖线 |

JS 行为：`.settings-search` 的 `input` 事件遍历 `.settings-bubble` 与 `.settings-nav-item`，`textContent` 不含关键字则加 `is-hidden`；清空后恢复。

## 10. 气泡卡片 (.settings-bubble)

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.settings-bubble` | `border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; margin-bottom: 16px` | 圆角卡片包裹相关设置 |
| `.settings-bubble-head` | `display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px` | 标题在左，操作按键在右 |
| `.settings-bubble-title` | `font-size: 16px; font-weight: 600` | 卡片标题 |
| `.settings-bubble-footer` | `margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--border); display: flex; gap: 12px` | 提示文字 + 创建按键 |
| `.settings-mode-btn` | `flex: 1; padding: 8px 0; border-radius: 6px` | 整体外观三选一，`.is-active` 蓝色 |
| `.settings-theme-swatch` | `display: flex; align-items: center; gap: 4px; padding: 2px 8px; height: 26px; border-radius: 6px; border: 1px solid var(--border)` | 模式标签：圆点 + 文字标注主题能力；圆点为子元素 `.settings-theme-swatch-dot`（`10px; border-radius: 50%`） |
| `.settings-refresh-btn` | `height: 28px; line-height: 28px` | 匹配串口页 `.send-controls button` 高度 |

## 11. 数据高亮颜色设置 (.c-color-row)

v0.6.2 重构：每行 [设置项] [加粗] [背景颜色:深/浅] [字体颜色:深/浅]。所有项均有 bg+fg 两组色块（非控制字符 bg 默认 transparent）。`bubble` + `special` 合并为 `ctrlBg` + `ctrlFg`（控制字符背景/字体色）。

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.c-color-head` | `display: flex; align-items: flex-end; gap: 8px; padding: 6px 0 2px; border-bottom: 1px solid var(--border)` | 双层列标题：行1 = "背景颜色/字体颜色"，行2 = "深色/浅色" |
| `.c-color-head__name` | `flex: 1; min-width: 100px` | 对齐下方 `.c-color-label` |
| `.c-color-head__chk` | `flex-shrink: 0; width: 32px; text-align: center` | 加粗列标题 |
| `.c-color-head__grp` | `display: flex; flex-direction: column; align-items: center; flex-shrink: 0; width: 68px` | 双层表头容器 |
| `.c-color-head__row1` | `font-weight: 600; font-size: 11px` | 第一层："背景颜色" / "字体颜色" |
| `.c-color-head__row2` | `display: flex; gap: 8px; justify-content: center`；`span` 定宽 28px | 第二层："深色" / "浅色" |
| `.c-color-row` | `display: flex; align-items: center; gap: 8px; padding: 5px 6px; border-left: 3px solid transparent` | 悬停左蓝竖线 + hover 背景 |
| `.c-color-label` | `flex: 1; font-size: 13px; min-width: 100px` | 标签弹性填充 |
| `.c-color-chk` | `flex-shrink: 0; width: 32px` | 加粗复选框 |
| `.c-color-swatches` | `display: flex; gap: 8px; flex-shrink: 0; width: 68px; justify-content: center` | 两组色块（bg + fg），各有深/浅两个 swatch |
| `.c-color-swatches.is-empty` | `visibility: hidden` | fg-only 行隐藏 bg 色块列（不占位） |
| `.c-color-swatch` | `display: block; width: 28px; height: 28px; border-radius: 6px; position: relative; box-shadow: 0 0 0 2px var(--border)` | 两层：底层模式指示（`.sw-dark` = `#1e1e1e` / `.sw-light` = `#f3f3f3`）+ 内层 fill |
| `.c-color-swatch-fill` | `position: absolute; inset: 4px; border-radius: 4px` | 显示实际颜色 |

## 12. 控制字符渲染

**双节点方案（当前实现，非 `::before` 单节点）**：每个控制字符生成 `.ctrl-char[data-ws]` 容器，内含两个子节点：

- `.ctrl-mark`：可见标记字形（如 `·` `←` `↵`），可被选中高亮，参与复制但会被剥离；
- `.ctrl-real`：真实字符，默认 `font-size: 0` 不可见，仅用于复制与保留换行/制表位。

| 字符 | DOM 结构 | 标记实现 | CSS 要点 |
| --- | --- | --- | --- |
| 空格 | `.ctrl-char[data-ws="space"]` > `.ctrl-mark` + `.ctrl-real` | `·`（`.ctrl-mark` 文本） | `.ctrl-real` 通用 `font-size: 0` 隐藏真字符 |
| Tab | `.ctrl-char[data-ws="tab"]` > `.ctrl-real`（无 `.ctrl-mark`） | `→` 由 `.ctrl-real::after` 叠加（不占布局） | `.ctrl-real` 保持 `font-size: inherit; color: transparent`，保留制表位宽度 |
| CR | `.ctrl-char[data-ws="cr"]` > `.ctrl-mark` + `.ctrl-real` | `←` | `.ctrl-real` 隐藏 |
| LF | `.ctrl-char[data-ws="lf"]` > `.ctrl-mark` + `.ctrl-real` | `↵` | `.ctrl-real` 保持 `font-size: inherit; color: transparent`，保留换行 |
| CRLF | 合并节点 `.ctrl-char[data-ws="crlf"]`（标记 `←↵`），或拆为 cr + lf 两个节点（按解码路径） | 同 CR/LF | 保留换行 |
| 转义字节 | `<span class="hex-escape">/FF</span>` | 直接显示 | 颜色走 `var(--dc-hexEsc)` |

复制行为（`app.js` copy 事件）：`cloneContents()` → 移除所有 `.ctrl-mark` → 若 `state.copyHexEscapes` 为 false 再移除 `.hex-escape` / `.hex-fescape` → 在 `.data-line` / `.sys-msg` 前插入 `\n` 保留行结构 → 取 `textContent` 写入剪贴板。`user-select` 无限制，标记可被选中高亮。

## 13. 预览气泡 (.c-color-preview)

| 元素 | CSS | 策略 |
| --- | --- | --- |
| `.c-color-preview-wrap` | `display: flex; gap: 6px; margin-bottom: 4px; flex-wrap: wrap` | 深浅两个预览并排 |
| `.c-color-preview` | `flex: 1; min-width: 160px; max-width: 50%; border-radius: 6px; font-family: monospace` | 预览示例行，控制字符模拟标记叠层渲染 |
| `.c-preview-dark` | `background: #1e1e1e; color: #d4d4d4; border: 1px solid var(--border)` | 深色预览 |
| `.c-preview-light` | `background: #f3f3f3; color: #1a1a1a; border: 1px solid var(--border)` | 浅色预览 |

## 14. 状态栏 (.statusbar)

- 位于 `#app` 底部，`flex-shrink: 0; height: 24px`，不参与滚动。
- 下拉框 portal 时以此栏顶部计算 `max-height`（trigger 底部 → statusbar 顶部，减 6px，下限 80px）。

## 15. 模糊渐变条

所有浮动条（config-bar、send-controls、settings-header、转发按键栏）统一采用透明 + 模糊渐变：

| 元素 | 位置 | mask 方向 | 模糊核 |
| --- | --- | --- | --- |
| `.tab-page-inner > .config-bar::before` | 配置栏（历史框上方） | `to bottom, black 75%, transparent 100%` | 5px |
| `.send-controls::before` | 发送工具栏 | `to bottom, black 75%, transparent 100%` | 5px |
| `.settings-header::before` | 设置标题栏 | `to bottom, black 75%, transparent 100%` | 5px |
| `.c-fwd-display .c-fwd-float-btns::before` | 转发按键栏 | `to top, black 75%, transparent 100%` | 5px |

- 模糊通过 `::before` 伪元素承载，`z-index: -1`（位于条内容之下、页面内容之上）。
- `mask-image`：3/4 区域完全模糊，1/4 区域渐变消失。
- 转发按键栏的**显隐**由 JS 按模式切换按钮 `is-hidden` 标记控制（见 [6](#6-显示区-display-area)）；`.c-fwd-display .c-fwd-float-btns::before` 仅承载模糊渐变。
