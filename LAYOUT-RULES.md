# 布局约束规则

记录各区域元素的宽度/拉伸/裁剪策略，避免反复拉扯。

## 自定义下拉框 (.cs-wrap)

`initCustomSelect()` 把原生 `<select>` 包裹为 `.cs-wrap > .cs-trigger + .cs-dropdown`。原生 select 被设为 `position: absolute; opacity: 0`，不参与布局。

- `.cs-wrap` — `display: inline-flex`，默认宽度跟随内容
- `.cs-trigger` — `overflow: hidden; text-overflow: ellipsis` 截断过长文字
- `.cs-dropdown` — 打开时 portal 到 `document.body`，`position: fixed`，不受父容器 `overflow:hidden` 裁剪

**规则：原生 `<select>` 的 width 对可见布局无效，必须约束 `.cs-trigger` 或 `.cs-wrap`。**

## 配置栏 (config-bar) 端口/波特率

| 元素 | CSS | 策略 |
|------|-----|------|
| `.port-wrap` | `flex: 1; min-width: 80px; overflow: hidden` | 弹性填充，文字截断，不顶开布局 |
| `.port-wrap > .cs-wrap` | `width: 100%` | 填满父容器，否则 inline-flex 不撑满 |
| `.baud-wrap` | `width: 80px; flex-shrink: 0` | 定宽，不伸缩 |
| `.baud-wrap .cs-text` | `overflow: visible` | 编辑模式允许文字溢出可见 |

## 发送工具栏 (.send-controls)

| 元素 | CSS | 策略 |
|------|-----|------|
| `.send-scroll-area` | `position: relative; flex-shrink: 1` | 横滚容器 |
| `.send-scroll-wrap` | `overflow-x: auto; scrollbar-width: none` | 隐藏滚动条，鼠标拖拽 |
| `#appendSelect` 的 trigger | `width: 74px`（通过 `:has()` 定位） | 定宽，不被选项文字撑开 |

## 串口详细设置窗 (#settingsOverlay)

| 元素 | CSS | 策略 |
|------|-----|------|
| `.c-form-row__label` | `font-size: 13px; white-space: nowrap` | **grid auto 列对齐**，同菜单栏：`#settingsSingle` / `.c-form-col` 为 grid 容器，`display: contents` 消去行包装，label 列 `auto` 匹配最宽项 |
| `.c-form-row .cs-trigger` | `width: 100px` | **定宽**，不被选中项文字撑开 |
| `.c-form-col .cs-trigger` | `width: 130px` | 转发模式列更宽 |
| `.c-form-row` 原生 select | `width: 100px` / `width: 130px` | 对可见布局无效，仅保留作为语义标记 |

## 设置页 (.settings-layout)

**响应式断点：800px。** 宽屏左右分栏，窄屏导航变为悬浮覆盖层。

| 元素 | CSS | 策略 |
|------|-----|------|
| `.settings-page-inner` | `overflow: hidden; background: var(--input-bg)` | 隐藏滚动，背景与历史框一致 |
| `.settings-wrapper` | `display: flex; flex-direction: column; height: 100%; position: relative; overflow: hidden` | 纵向弹性，定位上下文 |
| `.settings-header` | `position: absolute; top:0; left:0; right:0; z-index:5; height:52px` | 悬浮标题栏，透明+渐变模糊 |
| `.settings-header::before` | `backdrop-filter: blur(5px); mask-image: linear-gradient(to bottom, black 75%, transparent 100%)` | 模糊渐变，3/4 全模糊 + 1/4 过渡 |
| `.settings-header-title` | `display: flex; align-items: center; padding: 0 8px; @media≥800px: width:200px; padding-left:16px` | 标题+汉堡键，宽屏固定宽度对齐左栏 |
| `.settings-nav-toggle` | 无边框按钮 36px，窄屏 `display: flex`，宽屏 `display: none` | 汉堡菜单切换键，窄屏可见 |
| `.settings-header-search` | `flex: 1; justify-content: flex-end（窄）/ center（宽）` | 搜索区域，窄屏贴右宽屏居中 |
| `.settings-search-wrap` | `position: relative; max-width: 360px（窄）/ 720px（宽）` | 搜索框包裹，内嵌图标 |
| `.settings-search-icon` | `position: absolute; left: 10px; color: var(--placeholder)` | 放大镜图标 |
| `.settings-search-clear` | `position: absolute; right: 6px;` 无文字时 `is-hidden` | 叉图标清空搜索 |
| `.settings-search` | `height: 30px; padding: 0 32px; border-radius: 6px` | 搜索输入框，两侧避开图标 |
| `.settings-layout` | `display: flex; flex-direction: row; flex: 1; min-height: 0; position: relative` | 左右分栏，填满标题栏下方空间 |
| `.settings-body` | `flex: 1; overflow-y: auto; padding: 60px 16px 12px 16px（窄）/ 60px 32px 12px 32px（宽）` | 右侧内容区，唯一滚动容器 |
| `.settings-panel` | 窄屏 `max-width: 100%`，宽屏 `max-width: 720px; margin: 0 auto` | 面板居中约束 |
| `.settings-nav` | 窄屏：`position: absolute; width: 240px; transform: translateX(-100%);` 悬浮覆盖+遮罩；宽屏：`position: relative; width: 200px;` 固定侧栏 | 响应式侧边栏 |
| `.settings-nav.is-open` | `transform: translateX(0)` | 窄屏下展开浮层 |
| `.settings-nav-backdrop` | `position: absolute; inset:0; background: rgba(0,0,0,0.3)` | 窄屏遮罩，点击关闭 |
| `.settings-nav-item` | `padding: 8px 20px; border-left: 3px solid transparent` | 选中时左侧蓝色竖线 |
| `.settings-search` 过滤 | JS `input` 事件遍历 `.settings-bubble` 和 `.settings-nav-item`，不匹配则 `is-hidden` | 实时过滤设置项和导航 |

## 气泡卡片 (.settings-bubble)

| 元素 | CSS | 策略 |
|------|-----|------|
| `.settings-bubble` | `border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; margin-bottom: 16px` | 圆角卡片包裹相关设置 |
| `.settings-bubble-head` | `display: flex; justify-content: space-between` | 标题在左，操作按键在右 |
| `.settings-bubble-title` | `font-size: 15px; font-weight: 600` | 比旧版更大的标题 |
| `.settings-bubble-footer` | `border-top: 1px; flex row` | 提示文字 + 创建按键 |
| `.settings-mode-btn` | `flex: 1; border-radius: 6px` | 整体外观三选一，active 蓝色 |
| `.settings-theme-swatch` | `width: 20px; height: 20px; border-radius: 50%` | 深浅色圆点标注主题能力 |
| `.settings-refresh-btn` | `height: 28px` | 匹配串口页 `.send-controls button` 样式 |

## Tab 页容器 overflow 链

以下元素全部 `overflow: hidden`，形成裁剪链：

```
.main-content  →  .tab-page  →  .tab-page-inner > .content-row  →  .display-area
```

自定义下拉框通过 portal 到 `document.body` 突破此链。其他弹窗（离线遮罩、通知、主题）在 body 级别创建，不受影响。

## 状态栏 (.statusbar)

- 位于 `#app` 底部，flex-shrink: 0，不参与滚动
- 下拉框 portal 时以此栏顶部计算 max-height

## 标签栏 (.tabs-bar)

| 元素 | CSS | 策略 |
|------|-----|------|
| `.tabs-bar` | `overflow: visible; height: 36px` | 激活标签 `margin-bottom: -1px` 延伸至页面 |
| `.tabs-scroll` | `overflow-x: auto; scrollbar-width: none` | 内容宽度自适应，隐藏滚动条，鼠标滚轮水平滚动 |
| `.tab-btn.tab-new` | `flex-shrink: 0; width: 28px; height: 28px` | 固定在 scroll 外右侧，方形 |

## 快速面板 (.quick-panel)

- `min-width: 160px`，通过 `.o-divider--quick` 拖动调整宽度
- 内部表格 CSS Subgrid，表头与行共享列宽

## 数据高亮颜色设置 (.c-color-row)

v0.6.2 重构：每行 [设置项] [加粗] [背景颜色:深/浅] [字体颜色:深/浅]。所有项均有 bg+fg 两组色块（非控制字符 bg 默认 transparent）。`bubble`+`special` 合并为 `ctrlBg`+`ctrlFg`（控制字符背景/字体色）。

| 元素 | CSS | 策略 |
|------|-----|------|
| `.c-color-head` | `display: flex; align-items: flex-end; gap: 8px` | 双层列标题：行1="背景颜色/字体颜色"，行2="深色/浅色" |
| `.c-color-head__name` | `flex: 1; min-width: 100px` | 对齐下方 `.c-color-label` |
| `.c-color-head__chk` | `width: 32px; text-align: center` | 加粗列标题 |
| `.c-color-head__grp` | `flex-direction: column; width: 68px` | 双层表头容器 |
| `.c-color-head__row1` | `font-weight: 600; font-size: 10px` | 第一层："背景颜色"/"字体颜色" |
| `.c-color-head__row2` | `display: flex; gap: 8px` | 第二层：`<span>`深色 `</span><span>`浅色 |
| `.c-color-row` | `display: flex; gap: 8px; padding: 5px 6px; border-left: 3px solid transparent` | 悬停左蓝竖线+bg3 背景 |
| `.c-color-label` | `flex: 1; font-size: 13px; min-width: 100px` | 标签弹性填充 |
| `.c-color-chk` | `width: 32px; flex-shrink: 0` | 加粗复选框 |
| `.c-color-swatches` | `width: 68px; gap: 8px; justify-content: center` | 两组色块（bg + fg），各有深/浅两个 swatch |
| `.c-color-swatches.is-empty` | `visibility: hidden` | fg-only 行隐藏 bg 色块列 |
| `.c-color-swatch` | `width: 28px; height: 28px; border-radius: 6px` | 两层：外层(sw-dark=#1e1e1e / sw-light=#f3f3f3) + 内层 fill |
| `.c-color-swatch-fill` | `position: absolute; inset: 4px` | 显示实际颜色 |

## 控制字符渲染

v0.6.2 改为单节点 + `::before` 方案：

| 字符 | DOM | `::before` 标记 | CSS |
|------|-----|-----------------|-----|
| 空格 | `<span data-ws="space"> </span>` | `·` | `color: transparent` 隐藏真字符 |
| Tab | `<span data-ws="tab">\t</span>` | `→` | 箭头贴左（内联 `::before`）|
| CR | `<span data-ws="cr">\r</span>` | `←` | |
| LF | `<span data-ws="lf">\n</span>` | `↵` | `\n` 在 span 内仍产生换行 |
| CRLF | `<span data-ws="crlf">\r\n</span>` | `←↵` | |
| 转义字节 | `<span class="hex-escape">/FF</span>` | — | 直接显示 |

- `user-select` 无限制，标记可被选中高亮
- 复制走 `cloneContents()` → 显式 `\n` 插入块间 → `textContent`，`::before` 伪元素不含在克隆中

## 预览气泡 (.c-color-preview)

| 元素 | CSS | 策略 |
|------|-----|------|
| `.c-color-preview-wrap` | `display: flex; gap: 6px; margin-bottom: 4px` | 深浅两个预览并排 |
| `.c-color-preview` | `flex: 1; min-width: 160px; border-radius: 6px; font-family: monospace` | 预览示例行，控制字符模拟 `::before` 叠层渲染 |
| `.c-preview-dark` | `background: #1e1e1e; color: #d4d4d4` | 深色预览 |
| `.c-preview-light` | `background: #f3f3f3; color: #1a1a1a` | 浅色预览 |

## 模糊渐变条

所有浮动条（config-bar、send-controls、settings-header、c-fwd-float-btns）统一采用透明 + 模糊渐变：

| 元素 | 位置 | mask 方向 | 模糊核 |
|------|------|-----------|--------|
| `.config-bar::before` | 历史框上方 | `to bottom, black 75%, transparent 100%` | 5px |
| `.send-controls::before` | 发送工具栏 | `to bottom, black 75%, transparent 100%` | 5px |
| `.settings-header::before` | 设置标题栏 | `to bottom, black 75%, transparent 100%` | 5px |
| `.c-fwd-float-btns::before` | 转发按键栏 | `to top, black 75%, transparent 100%` | 5px |

- 模糊通过 `::before` 伪元素承载，`z-index: -1`
- `mask-image`：3/4 区域完全模糊，1/4 区域渐变消失
- 转发按键栏通过 CSS 父级选择器 `.c-fwd-display .c-fwd-float-btns` 控制显隐
