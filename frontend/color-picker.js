// color-picker.js
// 依赖同目录下的 color-picker.html，加载模板并注册 <color-picker> 自定义元素
//
// 整体包在 IIFE 中：templateContent / initTemplate 等不再泄漏到全局，
// 避免同一页面重复加载本脚本时全局变量互相覆盖。
(function () {
    'use strict';

    let templateContent = null;

    // 默认取色图标（胶头滴管）。
    // 放在 JS 而非模板里，是为了让 color-picker.html 体积足够小 ——
    // 部分开发服务器（如 VS Code Live Server）处理 HTML 响应时有缓冲区上限，
    // 过大的模板会被截断，导致元素缺失。
    const DEFAULT_ICON_SVG =
        '<svg class="eyedropper-icon" viewBox="0 0 24 24" fill="currentColor" ' +
        'fill-rule="nonzero" aria-hidden="true"><path d="' +
        'M19.68 0.99 20.44 1.04 21.08 1.24 21.63 1.55 22.22 2.07 22.56 2.51 22.76 2.88 22.96 3.42 ' +
        '23.06 3.96 23.06 4.71 22.86 5.52 22.62 5.99 22.35 6.37 19.48 9.11 19.61 9.34 19.65 9.65 ' +
        '19.61 9.82 19.38 10.19 18.38 11.12 17.97 11.29 17.70 11.29 17.46 11.22 17.26 11.09 16.72 10.58 ' +
        '10.36 16.94 6.67 20.56 6.27 20.73 5.01 21.07 4.78 21.17 1.33 23.47 0.95 23.50 0.75 23.40 ' +
        '0.53 23.15 0.46 22.74 0.50 22.61 0.73 22.20 2.80 19.12 2.93 18.85 3.20 17.70 3.41 17.13 ' +
        '13.39 7.18 12.78 6.57 12.68 6.40 12.61 6.16 12.61 5.93 12.81 5.49 13.71 4.59 13.95 4.42 ' +
        '14.15 4.35 14.55 4.35 14.93 4.56 16.16 3.25 17.70 1.71 17.97 1.51 18.48 1.24 18.85 1.11 ' +
        '19.29 1.00 19.66 1.00Z M13.88 7.77 4.12 17.50 3.98 17.80 3.71 18.99 3.54 19.39 2.36 21.12 ' +
        '1.61 22.30 1.66 22.32 1.83 22.22 4.51 20.42 5.93 20.02 6.27 19.88 13.47 12.74 16.20 10.02 ' +
        '13.95 7.74 13.89 7.75Z M19.63 1.78 19.36 1.78 18.85 1.92 18.51 2.09 18.17 2.32 15.48 5.08 ' +
        '18.95 8.55 21.39 6.25 21.74 5.89 21.98 5.55 22.22 4.98 22.29 4.64 22.29 3.96 22.15 3.42 ' +
        '22.01 3.12 21.74 2.71 21.32 2.29 21.08 2.12 20.68 1.92 20.34 1.82 19.87 1.75 19.65 1.77Z ' +
        'M14.25 5.17 13.42 5.99 13.42 6.10 17.77 10.48 17.84 10.51 17.94 10.48 18.68 9.77 18.80 9.65 ' +
        '18.83 9.51 14.49 5.17 14.27 5.15Z"/></svg>';

    /**
     * 解析 color-picker.html 的地址。
     * 优先用 document.currentScript；若为 null（例如脚本被动态插入、
     * 或被某些开发服务器处理过），则回退到遍历 document.scripts 找到本脚本，
     * 用它的已解析绝对 URL 作为基准 —— 避免相对路径基于页面 URL 解析到别处。
     */
    function getTemplateUrl() {
        const cs = document.currentScript;
        if (cs && cs.src) {
            return cs.src.substring(0, cs.src.lastIndexOf('/') + 1) + 'color-picker.html';
        }
        const scripts = document.querySelectorAll('script[src]');
        for (let i = scripts.length - 1; i >= 0; i--) {
            const raw = scripts[i].getAttribute('src') || '';
            if (raw.indexOf('color-picker.js') !== -1) {
                const abs = scripts[i].src;      // 浏览器已解析为绝对 URL
                return abs.substring(0, abs.lastIndexOf('/') + 1) + 'color-picker.html';
            }
        }
        return 'color-picker.html';
    }

    async function initTemplate() {
        const url = getTemplateUrl();
        try {
            const response = await fetch(url);
            if (!response.ok) throw new Error('HTTP ' + response.status);
            const text = await response.text();
            const parser = new DOMParser();
            const doc = parser.parseFromString(text, 'text/html');
            const template = doc.getElementById('color-picker-template');
            if (!template) {
                throw new Error('模板中未找到 <template id="color-picker-template">' +
                    '（收到 ' + text.length + ' 字节，可能是 404 页面或被截断的内容）');
            }
            templateContent = template.content.cloneNode(true);
        } catch (e) {
            console.error('[color-picker] 模板加载失败:', e.message +
                '\n  尝试的地址: ' + url +
                '\n  请确认 color-picker.html 与 color-picker.js 在同一目录，' +
                '且内容完整（应包含 <template id="color-picker-template">）。');
            templateContent = null;
        }

        if (templateContent) {
            customElements.define('color-picker', ColorPicker);
        }
    }

    class ColorPicker extends HTMLElement {
        static get observedAttributes() {
            return ['color', 'theme', 'transparency', 'title', 'swatch-bg'];
        }

        constructor() {
            super();
            if (!templateContent) return;

            this._isDragging = false;
            this._dragOffset = { x: 0, y: 0 };
            this._savedColors = [];
            this._selectedSavedIndex = 0;
            this._outsideClickHandler = null;
            this._eyeDropperAbort = null;      // 取色中的 AbortController
            this._transparency = this.getAttribute('transparency') || 'solid';
            this._currentColor = this._normalizeColor(this.getAttribute('color') || 'rgba(52,152,219,1)');

            this._basicColors = [
                { color: '#ff0000', name: '纯红' },
                { color: '#ff8000', name: '纯橙' },
                { color: '#ffff00', name: '纯黄' },
                { color: '#80ff00', name: '黄绿' },
                { color: '#00ff00', name: '纯绿' },
                { color: '#00ff80', name: '青绿' },
                { color: '#00ffff', name: '纯青' },
                { color: '#0080ff', name: '天蓝' },
                { color: '#0000ff', name: '纯蓝' },
                { color: '#8000ff', name: '蓝紫' },
                { color: '#ff00ff', name: '品红' },
                { color: '#ff0080', name: '玫红' },
                { color: '#ffffff', name: '纯白' },
                { color: '#cccccc', name: '浅灰' },
                { color: '#999999', name: '中灰' },
                { color: '#666666', name: '深灰' },
                { color: 'rgba(255,0,0,0.5)', name: '半透明红' },
                { color: 'rgba(255,128,0,0.5)', name: '半透明橙' },
                { color: 'rgba(255,255,0,0.5)', name: '半透明黄' },
                { color: 'rgba(0,255,0,0.5)', name: '半透明绿' },
                { color: 'rgba(0,255,255,0.5)', name: '半透明青' },
                { color: 'rgba(0,0,255,0.5)', name: '半透明蓝' },
                { color: 'rgba(255,0,255,0.5)', name: '半透明品红' },
                { color: 'rgba(255,255,255,0.5)', name: '半透明白' },
                { color: 'rgba(0,0,0,0.5)', name: '半透明黑' },
                { color: 'rgba(255,200,200,0.5)', name: '半透明浅红' },
                { color: 'rgba(200,255,200,0.5)', name: '半透明浅绿' },
                { color: 'rgba(200,200,255,0.5)', name: '半透明浅蓝' },
                { color: 'rgba(255,255,200,0.5)', name: '半透明浅黄' },
                { color: 'rgba(255,200,255,0.5)', name: '半透明浅紫' },
                { color: 'rgba(200,255,255,0.5)', name: '半透明浅青' },
                { color: 'rgba(128,128,128,0.5)', name: '半透明灰' }
            ];

            this.attachShadow({ mode: 'open' });
            this.shadowRoot.appendChild(templateContent.cloneNode(true));

            this._cacheElements();

            // 模板与脚本版本不匹配时，_cacheElements 已打印诊断。
            // 这里直接跳过后续初始化，避免 _bindEvents 等处连锁抛出
            // "Cannot read properties of null"，同时保证 open()/close() 仍可用。
            if (this._templateBroken) return;

            this._loadSavedColors();
            this._renderBasicColors();
            this._renderSavedColors();
            this._bindEvents();
            this._applySwatchBg();
            this._ensureDefaultIcon();

            if (this._currentColor) {
                this._updatePreview();
            }

            // 不支持取色 API 时隐藏按钮
            if (!window.EyeDropper && this._els.eyedropperBtn) {
                this._els.eyedropperBtn.style.display = 'none';
            }

            // 应用初始 title
            const t = this.getAttribute('title');
            if (t && this._els.popupTitle) {
                this._els.popupTitle.textContent = t;
            }
        }

        attributeChangedCallback(name, oldVal, newVal) {
            if (oldVal === newVal) return;
            if (name === 'color' && newVal) {
                this._currentColor = this._normalizeColor(newVal);
                if (this._els && this._els.currentColorDisplay) this._updatePreview();
            } else if (name === 'title' && this._els && this._els.popupTitle) {
                this._els.popupTitle.textContent = newVal || '选择颜色';
            } else if (name === 'swatch-bg') {
                this._applySwatchBg();
            }
            // theme / transparency 由 CSS 属性选择器自动生效，无需 JS 处理
        }

        open(triggerElement) {
            if (triggerElement) {
                const rect = triggerElement.getBoundingClientRect();
                const popupWidth = 320;
                const popupHeight = 580;
                let top = rect.bottom + 5;
                let left = rect.left;
                if (top + popupHeight > window.innerHeight) top = rect.top - popupHeight - 5;
                if (top < 0) top = Math.max(0, window.innerHeight - popupHeight - 5);
                if (left + popupWidth > window.innerWidth) left = window.innerWidth - popupWidth - 10;
                if (left < 10) left = 10;
                this.style.left = left + 'px';
                this.style.top = top + 'px';
            }
            this.classList.add('active');

            const actualH = this.offsetHeight;
            const actualW = this.offsetWidth;
            let t = parseInt(this.style.top) || 0;
            let l = parseInt(this.style.left) || 0;
            if (t + actualH > window.innerHeight) t = Math.max(0, window.innerHeight - actualH - 5);
            if (l + actualW > window.innerWidth) l = Math.max(10, window.innerWidth - actualW - 10);
            this.style.top = t + 'px';
            this.style.left = l + 'px';

            if (this._outsideClickHandler) {
                document.removeEventListener('click', this._outsideClickHandler);
            }
            this._outsideClickHandler = (e) => {
                if (!this.contains(e.target) && e.target !== triggerElement && !triggerElement?.contains(e.target)) {
                    this.close();
                }
            };
            setTimeout(() => document.addEventListener('click', this._outsideClickHandler), 0);
        }

        close() {
            this.classList.remove('active');
            if (this._outsideClickHandler) {
                document.removeEventListener('click', this._outsideClickHandler);
                this._outsideClickHandler = null;
            }
            // 若正在取色，一并中止（否则取色模式下弹窗已关，结果仍会写回）
            if (this._eyeDropperAbort) {
                this._eyeDropperAbort.abort();
                this._eyeDropperAbort = null;
            }
        }

        // ---------- 私有方法 ----------

        _cacheElements() {
            const root = this.shadowRoot;
            this._els = {
                popupHeader: root.getElementById('popupHeader'),
                popupTitle: root.getElementById('popupTitle'),
                currentColorDisplay: root.getElementById('currentColorDisplay'),
                hexInput: root.getElementById('hexInput'),
                redSlider: root.getElementById('redSlider'),
                greenSlider: root.getElementById('greenSlider'),
                blueSlider: root.getElementById('blueSlider'),
                alphaSlider: root.getElementById('alphaSlider'),
                redFill: root.getElementById('redFill'),
                greenFill: root.getElementById('greenFill'),
                blueFill: root.getElementById('blueFill'),
                alphaFill: root.getElementById('alphaFill'),
                redInput: root.getElementById('redInput'),
                greenInput: root.getElementById('greenInput'),
                blueInput: root.getElementById('blueInput'),
                alphaInput: root.getElementById('alphaInput'),
                basicColorsGrid: root.getElementById('basicColorsGrid'),
                savedColorsGrid: root.getElementById('savedColorsGrid'),
                saveColorBtn: root.getElementById('saveColorBtn'),
                applyColorBtn: root.getElementById('applyColorBtn'),
                eyedropperBtn: root.getElementById('eyedropperBtn'),
                closePopup: root.getElementById('closePopup'),
            };

            // 模板与脚本版本不匹配时，给出明确提示（否则报错信息晦涩，
            // 例如 "Cannot set properties of null (setting 'innerHTML')"）
            const missing = Object.keys(this._els).filter(k => !this._els[k]);
            if (missing.length) {
                console.error(
                    '[color-picker] 模板缺少以下元素: ' + missing.join(', ') +
                    '\n  请确认 color-picker.html 与 color-picker.js 同目录且版本一致' +
                    '（0.2.4 的模板应包含 id: basicColorsGrid / savedColorsGrid / eyedropperBtn 等）。' +
                    '\n  当前模板地址: ' + getTemplateUrl() +
                    '\n  若浏览器缓存了旧模板，请强制刷新（Ctrl+F5）。'
                );
                this._templateBroken = true;
            }
        }

        _bindEvents() {
            const els = this._els;

            // pointerdown 同时覆盖鼠标 / 触摸 / 触控笔（此前只用 mousedown，触摸屏无法拖动）
            els.popupHeader.addEventListener('pointerdown', (e) => this._startDrag(e));
            els.closePopup.addEventListener('click', (e) => { e.stopPropagation(); this.close(); });

            els.hexInput.addEventListener('input', () => this._onHexInput());
            els.hexInput.addEventListener('keydown', (e) => this._onInputKeydown(e, 'hexInput'));

            els.eyedropperBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this._openEyeDropper();
            });

            els.redSlider.addEventListener('input', () => this._onSliderChange());
            els.greenSlider.addEventListener('input', () => this._onSliderChange());
            els.blueSlider.addEventListener('input', () => this._onSliderChange());
            els.alphaSlider.addEventListener('input', () => this._onSliderChange());

            els.redInput.addEventListener('input', () => this._onInputChange());
            els.greenInput.addEventListener('input', () => this._onInputChange());
            els.blueInput.addEventListener('input', () => this._onInputChange());
            els.alphaInput.addEventListener('input', () => this._onInputChange());

            els.redInput.addEventListener('keydown', (e) => this._onInputKeydown(e, 'redInput'));
            els.greenInput.addEventListener('keydown', (e) => this._onInputKeydown(e, 'greenInput'));
            els.blueInput.addEventListener('keydown', (e) => this._onInputKeydown(e, 'blueInput'));
            els.alphaInput.addEventListener('keydown', (e) => this._onInputKeydown(e, 'alphaInput'));

            els.saveColorBtn.addEventListener('click', (e) => { e.stopPropagation(); this._saveColor(); });
            els.applyColorBtn.addEventListener('click', (e) => { e.stopPropagation(); this._applyColor(); });
        }

        _startDrag(e) {
            if (e.button !== undefined && e.button !== 0) return;   // 仅左键 / 主触点
            e.preventDefault();
            this._isDragging = true;
            const rect = this.getBoundingClientRect();
            this._dragOffset.x = e.clientX - rect.left;
            this._dragOffset.y = e.clientY - rect.top;
            this._doDragBound = this._doDrag.bind(this);
            this._stopDragBound = this._stopDrag.bind(this);
            document.addEventListener('pointermove', this._doDragBound);
            document.addEventListener('pointerup', this._stopDragBound);
            document.addEventListener('pointercancel', this._stopDragBound);
            this._els.popupHeader.style.cursor = 'grabbing';
        }

        _doDrag(e) {
            if (!this._isDragging) return;
            let nx = e.clientX - this._dragOffset.x;
            let ny = e.clientY - this._dragOffset.y;
            const w = this.offsetWidth, h = this.offsetHeight;
            nx = Math.max(0, Math.min(window.innerWidth - w, nx));
            ny = Math.max(0, Math.min(window.innerHeight - h, ny));
            this.style.left = nx + 'px';
            this.style.top = ny + 'px';
        }

        _stopDrag() {
            this._isDragging = false;
            document.removeEventListener('pointermove', this._doDragBound);
            document.removeEventListener('pointerup', this._stopDragBound);
            document.removeEventListener('pointercancel', this._stopDragBound);
            this._els.popupHeader.style.cursor = 'move';
        }

        _onHexInput() {
            let val = this._els.hexInput.value;
            if (val && !val.startsWith('#')) {
                val = '#' + val;
                this._els.hexInput.value = val;
            }
            if (this._isValidHex(val)) {
                this._currentColor = this._hexToRgba(val);
                this._updatePreview(false);
            }
        }

        _onSliderChange() {
            const r = parseInt(this._els.redSlider.value);
            const g = parseInt(this._els.greenSlider.value);
            const b = parseInt(this._els.blueSlider.value);
            const a = parseInt(this._els.alphaSlider.value) / 100;
            this._els.redInput.value = r;
            this._els.greenInput.value = g;
            this._els.blueInput.value = b;
            this._els.alphaInput.value = a.toFixed(2);
            this._currentColor = `rgba(${r}, ${g}, ${b}, ${a})`;
            this._updatePreview(false);
            this._updateSliderFills();
        }

        _onInputChange() {
            const r = Math.min(255, Math.max(0, parseInt(this._els.redInput.value) || 0));
            const g = Math.min(255, Math.max(0, parseInt(this._els.greenInput.value) || 0));
            const b = Math.min(255, Math.max(0, parseInt(this._els.blueInput.value) || 0));
            const a = Math.min(1, Math.max(0, parseFloat(this._els.alphaInput.value) || 0));
            this._els.redSlider.value = r;
            this._els.greenSlider.value = g;
            this._els.blueSlider.value = b;
            this._els.alphaSlider.value = Math.round(a * 100);
            this._currentColor = `rgba(${r}, ${g}, ${b}, ${a})`;
            this._updatePreview(false);
            this._updateSliderFills();
        }

        _onInputKeydown(e, currentId) {
            const order = ['hexInput', 'redInput', 'greenInput', 'blueInput', 'alphaInput'];
            const idx = order.indexOf(currentId);

            if (e.key === 'Enter') {
                e.preventDefault();
                if (currentId === 'hexInput') {
                    this._onHexInput();
                    this._applyColor();
                } else if (currentId === 'alphaInput') {
                    this._applyColor();
                } else {
                    const next = order[idx + 1];
                    if (next && this._els[next]) this._els[next].focus();
                }
            } else if (e.key === 'ArrowDown') {
                e.preventDefault();
                const next = order[idx + 1];
                if (next && this._els[next]) {
                    this._els[next].focus();
                    this._els[next].select();
                }
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                const prev = order[idx - 1];
                if (prev && this._els[prev]) {
                    this._els[prev].focus();
                    this._els[prev].select();
                }
            }
        }

        _updateSliderFills() {
            const els = this._els;
            const rMax = parseFloat(els.redSlider.max) || 255;
            const gMax = parseFloat(els.greenSlider.max) || 255;
            const bMax = parseFloat(els.blueSlider.max) || 255;
            const aMax = parseFloat(els.alphaSlider.max) || 100;

            els.redFill.style.width = (parseFloat(els.redSlider.value) / rMax * 100) + '%';
            els.greenFill.style.width = (parseFloat(els.greenSlider.value) / gMax * 100) + '%';
            els.blueFill.style.width = (parseFloat(els.blueSlider.value) / bMax * 100) + '%';
            els.alphaFill.style.width = (parseFloat(els.alphaSlider.value) / aMax * 100) + '%';
        }

        /**
         * 刷新预览。
         * @param {boolean} updateInputs true=回填滑块/输入框（初始载入、点色块时用）
         *                               false=只更新 HEX 与滑块进度条（拖滑块/输入时用，避免光标跳动）
         * 两条路径都会派发 color-change。
         * 修复：此前 false 分支提前 return，导致拖动滑块、输入 RGBA 时
         * 外层收不到 color-change（与 README 描述的触发时机不符）。
         */
        _updatePreview(updateInputs = true) {
            this._els.currentColorDisplay.style.backgroundColor = this._currentColor;

            if (updateInputs) {
                const rgba = this._parseRgba(this._currentColor);
                this._els.redSlider.value = rgba.r;
                this._els.greenSlider.value = rgba.g;
                this._els.blueSlider.value = rgba.b;
                this._els.alphaSlider.value = Math.round(rgba.a * 100);
                this._els.redInput.value = rgba.r;
                this._els.greenInput.value = rgba.g;
                this._els.blueInput.value = rgba.b;
                this._els.alphaInput.value = rgba.a.toFixed(2);
            }

            this._els.hexInput.value = this._rgbaToHex(this._currentColor);
            this._updateSliderFills();

            this.dispatchEvent(new CustomEvent('color-change', {
                detail: { color: this._currentColor },
                bubbles: true,
                composed: true
            }));
        }

        _renderBasicColors() {
            const grid = this._els.basicColorsGrid;
            if (!grid) return;      // 模板异常时不让整个构造函数崩溃（_cacheElements 已给出诊断）
            grid.innerHTML = '';
            this._basicColors.forEach(entry => {
                const c = entry.color;
                const wrapper = document.createElement('div');
                wrapper.className = 'swatch-bg';
                const inner = document.createElement('span');
                inner.className = 'swatch-bg-inner';
                const div = document.createElement('div');
                div.className = 'basic-color';
                div.style.backgroundColor = c;
                div.title = entry.name || '';
                div.dataset.color = c;
                div.addEventListener('click', (e) => {
                    e.stopPropagation();
                    this._currentColor = c.startsWith('#') ? this._hexToRgba(c) : c;
                    this._updatePreview();
                });
                wrapper.appendChild(inner);
                wrapper.appendChild(div);
                grid.appendChild(wrapper);
            });
        }

        _renderSavedColors() {
            const grid = this._els.savedColorsGrid;
            if (!grid) return;      // 同上
            grid.innerHTML = '';
            for (let i = 0; i < 8; i++) {
                const wrapper = document.createElement('div');
                wrapper.className = 'swatch-bg';
                const inner = document.createElement('span');
                inner.className = 'swatch-bg-inner';
                const div = document.createElement('div');
                div.className = 'saved-color';
                if (i < this._savedColors.length && this._savedColors[i]) {
                    div.style.backgroundColor = this._savedColors[i];
                    div.title = this._rgbaToHex(this._savedColors[i]);
                    div.classList.remove('empty');
                    wrapper.classList.remove('empty');
                } else {
                    div.classList.add('empty');
                    wrapper.classList.add('empty');
                }
                if (i === this._selectedSavedIndex) div.classList.add('selected');
                div.dataset.index = i;
                div.addEventListener('click', (e) => {
                    e.stopPropagation();
                    this._selectedSavedIndex = i;
                    if (this._savedColors[i]) {
                        this._currentColor = this._savedColors[i];
                        this._updatePreview();
                    }
                    this._renderSavedColors();
                });
                wrapper.appendChild(inner);
                wrapper.appendChild(div);
                grid.appendChild(wrapper);
            }
        }

        /**
         * 保存当前颜色到自定义槽位。
         * 修复：此前先自增 _selectedSavedIndex 再派发事件，save 事件的 index
         * 报的是"下一个待写槽位"而非本次写入的槽位，调用方按 index 回读会取错。
         * 同时消除了重复的 _renderSavedColors() 调用。
         */
        _saveColor() {
            const existing = this._savedColors.indexOf(this._currentColor);
            if (existing !== -1) {
                // 该颜色已在槽位中：仅选中它，不重复写入
                this._selectedSavedIndex = existing;
                this._renderSavedColors();
                this._persistSavedColors();
                this._emitSave(existing);
                return;
            }

            const savedIndex = this._selectedSavedIndex;
            this._savedColors[savedIndex] = this._currentColor;
            this._selectedSavedIndex = (savedIndex + 1) % 8;
            this._renderSavedColors();
            this._persistSavedColors();
            this._emitSave(savedIndex);
        }

        _emitSave(index) {
            this.dispatchEvent(new CustomEvent('save', {
                detail: { color: this._currentColor, index },
                bubbles: true,
                composed: true
            }));
        }

        /**
         * 启动屏幕取色。
         * 取消方式由 EyeDropper API 决定：用户按 Esc 取消（Promise reject AbortError），
         * 页面无法自定义取色模式下的鼠标交互（该模式由浏览器接管，右键等不会到页面）。
         * 这里额外支持组件侧主动中止（close() 时通过 AbortController）。
         */
        async _openEyeDropper() {
            if (!window.EyeDropper) {
                this._els.eyedropperBtn.title = '当前浏览器不支持取色';
                return;
            }
            this._eyeDropperAbort = new AbortController();
            const dropper = new EyeDropper();
            try {
                let result;
                try {
                    // 支持 open(options) 的浏览器可被 AbortController 中止
                    result = await dropper.open({ signal: this._eyeDropperAbort.signal });
                } catch (e) {
                    // 旧实现不接受 options 参数时会抛 TypeError —— 回退到无参调用
                    if (e instanceof TypeError) {
                        result = await dropper.open();
                    } else {
                        throw e;
                    }
                }
                this._currentColor = this._hexToRgba(result.sRGBHex);
                this._updatePreview();
            } catch (e) {
                // AbortError = 用户按 Esc 取消，或组件主动中止 —— 属正常流程
                if (!e || e.name !== 'AbortError') {
                    console.warn('[color-picker] 取色未完成:', (e && e.name) || e);
                }
            } finally {
                this._eyeDropperAbort = null;
            }
        }

        _applyColor() {
            this.dispatchEvent(new CustomEvent('apply', {
                detail: { color: this._currentColor },
                bubbles: true,
                composed: true
            }));
            this.close();
        }

        _loadSavedColors() {
            try {
                const raw = localStorage.getItem('color-picker-saved');
                const parsed = raw ? JSON.parse(raw) : [];
                // localStorage 可能被外部写成非数组，这里做类型校验并限制槽位数
                this._savedColors = Array.isArray(parsed) ? parsed.slice(0, 8) : [];
            } catch (e) {
                this._savedColors = [];
            }
        }

        _persistSavedColors() {
            try { localStorage.setItem('color-picker-saved', JSON.stringify(this._savedColors)); } catch (e) { }
        }

        _normalizeColor(color) {
            if (color && color.startsWith('#')) {
                return this._hexToRgba(color);
            }
            return color;
        }

        _applySwatchBg() {
            const color = this.getAttribute('swatch-bg');
            if (color) {
                this.style.setProperty('--swatch-bg-color', color);
            } else {
                this.style.removeProperty('--swatch-bg-color');
            }
        }

        /**
         * 未注入 slot 内容时，把默认图标放进 slot 作为 fallback。
         * 图标定义在 JS 里（DEFAULT_ICON_SVG），以减小模板体积。
         */
        _ensureDefaultIcon() {
            const slot = this.shadowRoot.querySelector('slot[name="eyedropper-icon"]');
            if (!slot || slot.querySelector('.eyedropper-icon')) return;
            slot.insertAdjacentHTML('afterbegin', DEFAULT_ICON_SVG);
        }

        // ---------- 静态工具方法 ----------

        _isValidHex(color) {
            return /^#([A-Fa-f0-9]{3,4}|[A-Fa-f0-9]{6}|[A-Fa-f0-9]{8})$/.test(color);
        }

        _hexToRgba(hex) {
            hex = hex.replace('#', '');
            let r, g, b, a = 1;
            if (hex.length === 3 || hex.length === 4) {
                r = parseInt(hex[0] + hex[0], 16);
                g = parseInt(hex[1] + hex[1], 16);
                b = parseInt(hex[2] + hex[2], 16);
                if (hex.length === 4) a = parseInt(hex[3] + hex[3], 16) / 255;
            } else if (hex.length === 6 || hex.length === 8) {
                r = parseInt(hex.substring(0, 2), 16);
                g = parseInt(hex.substring(2, 4), 16);
                b = parseInt(hex.substring(4, 6), 16);
                if (hex.length === 8) a = parseInt(hex.substring(6, 8), 16) / 255;
            }
            return `rgba(${r}, ${g}, ${b}, ${a})`;
        }

        _rgbaToHex(rgba) {
            const match = rgba.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/i);
            if (!match) return '#000000';
            const r = parseInt(match[1]), g = parseInt(match[2]), b = parseInt(match[3]);
            let a = match[4] ? parseFloat(match[4]) : 1;
            const toHex = (n) => { const h = n.toString(16); return h.length === 1 ? '0' + h : h; };
            let hex = '#' + toHex(r) + toHex(g) + toHex(b);
            if (a < 1) hex += Math.round(a * 255).toString(16).padStart(2, '0');
            return hex;
        }

        _parseRgba(rgba) {
            const match = rgba.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/i);
            if (!match) return { r: 0, g: 0, b: 0, a: 1 };
            return { r: parseInt(match[1]), g: parseInt(match[2]), b: parseInt(match[3]), a: match[4] ? parseFloat(match[4]) : 1 };
        }
    }

    // 启动模板加载，完成后自动注册元素
    initTemplate();
})();
