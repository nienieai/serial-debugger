// color-picker.js
// 依赖同目录下的 color-picker.html，加载模板并注册 <color-picker> 自定义元素

let templateContent = null;

function getTemplateUrl() {
    const scriptUrl = document.currentScript ? document.currentScript.src : '';
    if (scriptUrl) {
        const base = scriptUrl.substring(0, scriptUrl.lastIndexOf('/') + 1);
        return base + 'color-picker.html';
    }
    return 'color-picker.html';
}

async function initTemplate() {
    try {
        const response = await fetch(getTemplateUrl());
        if (!response.ok) throw new Error('Failed to load template: ' + response.status);
        const text = await response.text();
        const parser = new DOMParser();
        const doc = parser.parseFromString(text, 'text/html');
        const template = doc.getElementById('color-picker-template');
        if (!template) throw new Error('Template element not found in color-picker.html');
        templateContent = template.content.cloneNode(true);
    } catch (e) {
        console.error('[color-picker] Template load error:', e);
        templateContent = null;
    }

    if (templateContent) {
        customElements.define('color-picker', ColorPicker);
    }
}

class ColorPicker extends HTMLElement {
    static get observedAttributes() {
        return ['color', 'theme', 'transparency', 'title', 'fa-icon', 'swatch-bg'];
    }

    constructor() {
        super();
        if (!templateContent) return;

        this._isDragging = false;
        this._dragOffset = { x: 0, y: 0 };
        this._savedColors = [];
        this._selectedSavedIndex = 0;
        this._outsideClickHandler = null;
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
        this._loadSavedColors();
        this._renderBasicColors();
        this._renderSavedColors();
        this._bindEvents();
        this._updateEyedropperIcon();
        this._applySwatchBg();

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

    connectedCallback() {
        this._applyTransparencyStyle();
    }

    attributeChangedCallback(name, oldVal, newVal) {
        if (oldVal === newVal) return;
        if (name === 'color' && newVal) {
            this._currentColor = this._normalizeColor(newVal);
            if (this._els && this._els.currentColorDisplay) this._updatePreview();
        } else if (name === 'theme' || name === 'transparency') {
            if (this._els) this._applyTransparencyStyle();
        } else if (name === 'title' && this._els && this._els.popupTitle) {
            this._els.popupTitle.textContent = newVal || '选择颜色';
        } else if (name === 'fa-icon') {
            this._updateEyedropperIcon();
        } else if (name === 'swatch-bg') {
            this._applySwatchBg();
        }
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
    }

    _bindEvents() {
        const els = this._els;

        els.popupHeader.addEventListener('mousedown', (e) => this._startDrag(e));
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
        e.preventDefault();
        this._isDragging = true;
        const rect = this.getBoundingClientRect();
        this._dragOffset.x = e.clientX - rect.left;
        this._dragOffset.y = e.clientY - rect.top;
        this._doDragBound = this._doDrag.bind(this);
        this._stopDragBound = this._stopDrag.bind(this);
        document.addEventListener('mousemove', this._doDragBound);
        document.addEventListener('mouseup', this._stopDragBound);
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
        document.removeEventListener('mousemove', this._doDragBound);
        document.removeEventListener('mouseup', this._stopDragBound);
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

    _updatePreview(updateInputs = true) {
        this._els.currentColorDisplay.style.backgroundColor = this._currentColor;
        if (!updateInputs) {
            const hex = this._rgbaToHex(this._currentColor);
            this._els.hexInput.value = hex;
            this._updateSliderFills();
            return;
        }
        const rgba = this._parseRgba(this._currentColor);
        this._els.redSlider.value = rgba.r;
        this._els.greenSlider.value = rgba.g;
        this._els.blueSlider.value = rgba.b;
        this._els.alphaSlider.value = Math.round(rgba.a * 100);
        this._els.redInput.value = rgba.r;
        this._els.greenInput.value = rgba.g;
        this._els.blueInput.value = rgba.b;
        this._els.alphaInput.value = rgba.a.toFixed(2);
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

    _saveColor() {
        const idx = this._savedColors.indexOf(this._currentColor);
        if (idx !== -1) {
            this._selectedSavedIndex = idx;
            this._renderSavedColors();
            this._persistSavedColors();
            return;
        }
        this._savedColors[this._selectedSavedIndex] = this._currentColor;
        this._renderSavedColors();
        this._selectedSavedIndex = (this._selectedSavedIndex + 1) % 8;
        this._renderSavedColors();
        this._persistSavedColors();

        this.dispatchEvent(new CustomEvent('save', {
            detail: { color: this._currentColor, index: this._selectedSavedIndex },
            bubbles: true,
            composed: true
        }));
    }

    async _openEyeDropper() {
        if (!window.EyeDropper) {
            this._els.eyedropperBtn.title = '当前浏览器不支持取色';
            return;
        }
        try {
            const dropper = new EyeDropper();
            const result = await dropper.open();
            this._currentColor = this._hexToRgba(result.sRGBHex);
            this._updatePreview();
        } catch (e) {
            // 用户取消取色，忽略
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
            if (raw) this._savedColors = JSON.parse(raw);
        } catch (e) { this._savedColors = []; }
    }

    _persistSavedColors() {
        try { localStorage.setItem('color-picker-saved', JSON.stringify(this._savedColors)); } catch (e) { }
    }

    _applyTransparencyStyle() {
        // 透明度通过属性选择器在 CSS 中自动处理
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

    _findFAStylesheet() {
        const attr = this.getAttribute('fa-icon');
        if (attr === 'true') {
            // 显式开启：查找父文档已加载的 FA，找不到则用 CDN 兜底
            const found = this._findFALinkInDocument();
            return found || 'https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css';
        }
        if (attr === 'false') return null;
        return this._findFALinkInDocument();
    }

    _findFALinkInDocument() {
        const links = document.querySelectorAll('link[rel="stylesheet"], link[rel="preload"]');
        for (const link of links) {
            const href = (link.getAttribute('href') || '').toLowerCase();
            if (href.includes('font-awesome') || href.includes('fontawesome')) {
                return link.getAttribute('href');
            }
        }
        if (typeof window.FontAwesome !== 'undefined') {
            return 'https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css';
        }
        return null;
    }

    _updateEyedropperIcon() {
        if (!this._els || !this._els.eyedropperBtn) return;

        const faHref = this._findFAStylesheet();
        const btn = this._els.eyedropperBtn;

        if (faHref) {
            if (!this.shadowRoot.querySelector('link[href*="font-awesome"], link[href*="fontawesome"]')) {
                const link = document.createElement('link');
                link.rel = 'stylesheet';
                link.href = faHref;
                this.shadowRoot.appendChild(link);
            }
            btn.innerHTML = '<i class="fas fa-eye-dropper"></i>';
            btn.title = '从屏幕取色';
        } else {
            btn.innerHTML = '<span class="eyedropper-icon">💧</span>';
            btn.title = '从屏幕取色';
        }
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
