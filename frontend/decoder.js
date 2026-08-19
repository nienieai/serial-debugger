// ---- encoding / decoding utilities ----
// Tolerant multi-byte decoders.  Serial streams often mix valid encoded text
// with raw binary bytes (delimiters, padding).  TextDecoder replaces every
// invalid byte sequence with U+FFFD (�); we instead render invalid bytes as
// /XX hex escapes so nothing is hidden.

const _textDecoders = {};

// ---- segment renderer (new Go-backed path) ----
// Renders a []Segment array from Go into DOM inside container.
// Uses only createTextNode / createElement, zero innerHTML.
// Relies on global state: state.hexEscapeMode, state.crVisible.

function renderSegments(container, segments, dir) {
  if (!segments || segments.length === 0) return;
  var isFwd = (dir === '1' || dir === '2');
  var ec = isFwd ? 'hex-fescape' : 'hex-escape';
  var cc = isFwd ? 'ctrl-fchar' : 'ctrl-char';
  var frag = document.createDocumentFragment();

  function _ws(cls, real) {
    var marker;
    switch (real) {
      case ' ':  marker = '·'; break;  // ·
      case '\t': marker = null; break; // → via ::after on .ctrl-real (overlay, no layout shift)
      case '\r': marker = '←'; break;  // ←
      case '\n': marker = '↵'; break;  // ↵
      default:   marker = '←↵';   // ←↵ (crlf)
    }
    var sp = document.createElement('span');
    sp.className = cls;
    sp.setAttribute('data-ws', real === ' ' ? 'space' : real === '\t' ? 'tab' : real === '\r' ? 'cr' : real === '\n' ? 'lf' : 'crlf');
    var realEl = document.createElement('span');
    realEl.className = 'ctrl-real';
    realEl.textContent = real;
    if (marker) {
      var markEl = document.createElement('span');
      markEl.className = 'ctrl-mark';
      markEl.textContent = marker;
      // marker before real so that ↵ appears before the \n line break
      sp.appendChild(markEl);
    }
    sp.appendChild(realEl);
    return sp;
  }

  for (var i = 0; i < segments.length; i++) {
    var seg = segments[i];
    switch (seg.t) {
      case 'text':
        var parts = seg.v.split(' ');
        for (var pi = 0; pi < parts.length; pi++) {
          if (pi > 0) frag.appendChild(state.crVisible ? _ws(cc, ' ') : document.createTextNode(' '));
          if (parts[pi].length > 0) frag.appendChild(document.createTextNode(parts[pi]));
        }
        break;
      case 'hex':
        if (state.hexEscapeMode === 'hide') break;
        if (state.hexEscapeMode === 'raw') { frag.appendChild(document.createTextNode('�')); break; }
        var hsp = document.createElement('span');
        hsp.className = ec;
        hsp.textContent = _formatHexEsc(parseInt(seg.v, 16));
        frag.appendChild(hsp);
        break;
      case 'cr':
        frag.appendChild(state.crVisible ? _ws(cc, '\r') : document.createTextNode('\r'));
        break;
      case 'lf':
        frag.appendChild(state.crVisible ? _ws(cc, '\n') : document.createTextNode('\n'));
        break;
      case 'crlf':
        frag.appendChild(state.crVisible ? _ws(cc, '\r\n') : document.createTextNode('\r\n'));
        break;
      case 'tab':
        frag.appendChild(state.crVisible ? _ws(cc, '\t') : document.createTextNode('\t'));
        break;
    }
  }
  container.appendChild(frag);
}

function hexToBytes(hex) {
  if (!hex) return [];
  // Normalize: strip 0x/0X prefixes, remove spaces/commas
  var norm = hex.replace(/0x/gi, '').replace(/[\s,]+/g, '');
  if (norm.length === 0) return [];
  // Pad odd-length
  if (norm.length % 2 !== 0) norm = norm.slice(0, -1);
  var bytes = [];
  for (var i = 0; i < norm.length; i += 2) {
    var b = parseInt(norm.substr(i, 2), 16);
    if (!isNaN(b)) bytes.push(b);
  }
  return bytes;
}

function escHtml(s) {
  if (!s) return '';
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function formatHex(bytes) {
  if (!bytes || bytes.length === 0) return '';
  const upper = state.hexCase === 'upper';
  const prefix = state.hexPrefix ? '0x' : '';
  var sep;
  if (state.hexSep === 'comma') sep = ',';
  else if (state.hexSep === 'none') sep = '';
  else sep = ' ';
  return bytes.map(b => {
    const h = b.toString(16).padStart(2, '0');
    return prefix + (upper ? h.toUpperCase() : h.toLowerCase());
  }).join(sep);
}

function sanitizeText(s) {
  if (!s) return '';
  const asciiMode = (state.encoding === 'ascii');
  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c === 0x0A) { out += state.crVisible ? '<span class="ctrl-char" data-ws="lf"><span class="ctrl-mark">↵</span><span class="ctrl-real">\n</span></span>' : '\n'; }
    else if (c === 0x0D) { out += state.crVisible ? '<span class="ctrl-char" data-ws="cr"><span class="ctrl-mark">←</span><span class="ctrl-real">\r</span></span>' : '\r'; }
    else if (c === 0x09) { out += state.crVisible ? '<span class="ctrl-char" data-ws="tab"><span class="ctrl-mark">→</span><span class="ctrl-real">\t</span></span>' : '\t'; }
    else if (c < 0x20 || (asciiMode && c >= 0x7F)) {
      if (state.hexEscapeMode === 'show') {
        out += '<span class="hex-escape">' + _formatHexEsc(c) + '</span>';
      } else if (state.hexEscapeMode === 'raw') {
        out += '�';
      }
    }
    else { out += escHtml(s.charAt(i)); }
  }
  return out;
}

function hexToText(hex) {
  if (!hex) return '';
  const bytes = hexToBytes(hex);
  const enc = state.encoding || 'utf-8';
  if (state.hexEscapeMode !== 'show') {
    const label = enc === 'ascii' ? 'utf-8' : (enc === 'gb2312' ? 'gbk' : enc);
    const key = '_raw_' + label;
    if (!_textDecoders[key]) _textDecoders[key] = new TextDecoder(label, {fatal: false});
    var raw = _textDecoders[key].decode(new Uint8Array(bytes));
    if (state.hexEscapeMode === 'hide') raw = raw.replace(/�/g, '');
    return sanitizeText(raw);
  }
  if (enc === 'ascii') {
    let s = '';
    for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
    return s;
  }
  if (enc === 'utf-8') return _decodeUTF8Tolerant(bytes);
  return _decodeGBKTolerant(bytes);
}

// ---- tolerant UTF-8 decoder ----
function _decodeUTF8Tolerant(bytes) {
  const out = [];
  let i = 0;
  while (i < bytes.length) {
    const b = bytes[i];
    if (b === 0x0D) {
      if (state.crVisible) {
        out.push('<span class="ctrl-char" data-ws="cr"><span class="ctrl-mark">←</span><span class="ctrl-real">\r</span></span>');
        if (i + 1 < bytes.length && bytes[i + 1] === 0x0A) {
          out.push('<span class="ctrl-char" data-ws="lf"><span class="ctrl-mark">↵</span><span class="ctrl-real">\n</span></span>'); i++;
        }
        out.push('\n');
      } else {
        if (i + 1 < bytes.length && bytes[i + 1] === 0x0A) { i++; }
        out.push('\n');
      }
      i++;
    } else if (b === 0x0A) {
      out.push(state.crVisible ? '<span class="ctrl-char" data-ws="lf"><span class="ctrl-mark">↵</span><span class="ctrl-real">\n</span></span>' : '\n'); i++;
    } else if (b === 0x09) { out.push(state.crVisible ? '<span class="ctrl-char" data-ws="tab"><span class="ctrl-mark">→</span><span class="ctrl-real">\t</span></span>' : '\t'); i++; }
    else if (b < 0x20 || b === 0x7F) {
      out.push(_hexEsc(b)); i++;
    } else if (b < 0x80) {
      out.push(escHtml(String.fromCharCode(b)));
      i++;
    } else if (b >= 0xC2 && b <= 0xDF && i + 1 < bytes.length) {
      const b2 = bytes[i + 1];
      if ((b2 & 0xC0) === 0x80) {
        out.push(escHtml(_utf8Decode(bytes.slice(i, i + 2))));
        i += 2;
      } else { out.push(_hexEsc(b)); i++; }
    } else if (b >= 0xE0 && b <= 0xEF && i + 2 < bytes.length) {
      const b2 = bytes[i + 1], b3 = bytes[i + 2];
      if ((b2 & 0xC0) === 0x80 && (b3 & 0xC0) === 0x80 &&
          !(b === 0xE0 && b2 < 0xA0) && !(b === 0xED && b2 > 0x9F)) {
        out.push(escHtml(_utf8Decode(bytes.slice(i, i + 3))));
        i += 3;
      } else { out.push(_hexEsc(b)); i++; }
    } else if (b >= 0xF0 && b <= 0xF4 && i + 3 < bytes.length) {
      const b2 = bytes[i + 1], b3 = bytes[i + 2], b4 = bytes[i + 3];
      if ((b2 & 0xC0) === 0x80 && (b3 & 0xC0) === 0x80 && (b4 & 0xC0) === 0x80 &&
          !(b === 0xF0 && b2 < 0x90) && !(b === 0xF4 && b2 > 0x8F)) {
        out.push(escHtml(_utf8Decode(bytes.slice(i, i + 4))));
        i += 4;
      } else { out.push(_hexEsc(b)); i++; }
    } else {
      out.push(_hexEsc(b));
      i++;
    }
  }
  return out.join('');
}

// ---- tolerant GBK decoder ----
function _decodeGBKTolerant(bytes) {
  const out = [];
  let i = 0;
  while (i < bytes.length) {
    const b = bytes[i];
    if (b === 0x0D) {
      if (state.crVisible) {
        out.push('<span class="ctrl-char" data-ws="cr"><span class="ctrl-mark">←</span><span class="ctrl-real">\r</span></span>');
        if (i + 1 < bytes.length && bytes[i + 1] === 0x0A) {
          out.push('<span class="ctrl-char" data-ws="lf"><span class="ctrl-mark">↵</span><span class="ctrl-real">\n</span></span>'); i++;
        }
        out.push('\n');
      } else {
        if (i + 1 < bytes.length && bytes[i + 1] === 0x0A) { i++; }
        out.push('\n');
      }
      i++;
    } else if (b === 0x0A) {
      out.push(state.crVisible ? '<span class="ctrl-char" data-ws="lf"><span class="ctrl-mark">↵</span><span class="ctrl-real">\n</span></span>' : '\n'); i++;
    } else if (b === 0x09) { out.push(state.crVisible ? '<span class="ctrl-char" data-ws="tab"><span class="ctrl-mark">→</span><span class="ctrl-real">\t</span></span>' : '\t'); i++; }
    else if (b < 0x20 || b === 0x7F) {
      out.push(_hexEsc(b)); i++;
    } else if (b < 0x80) {
      out.push(escHtml(String.fromCharCode(b)));
      i++;
    } else if (b >= 0x81 && b <= 0xFE && i + 1 < bytes.length) {
      const b2 = bytes[i + 1];
      if ((b2 >= 0x40 && b2 <= 0x7E) || (b2 >= 0x80 && b2 <= 0xFE)) {
        out.push(escHtml(_gbkDecode(bytes.slice(i, i + 2))));
        i += 2;
      } else { out.push(_hexEsc(b)); i++; }
    } else {
      out.push(_hexEsc(b));
      i++;
    }
  }
  return out.join('');
}

function _toU8(arr) { return new Uint8Array(arr); }

function _utf8Decode(slice) {
  if (!_textDecoders._u8) _textDecoders._u8 = new TextDecoder('utf-8', {fatal: false});
  return _textDecoders._u8.decode(_toU8(slice));
}

function _gbkDecode(slice) {
  if (!_textDecoders._gbk) _textDecoders._gbk = new TextDecoder('gbk', {fatal: false});
  return _textDecoders._gbk.decode(_toU8(slice));
}

function _hexEsc(b) {
  if (state.hexEscapeMode === 'hide') return '';
  if (state.hexEscapeMode === 'raw') return String.fromCharCode(0xFFFD);
  return '<span class="hex-escape">' + _formatHexEsc(b) + '</span>';
}

function _formatHexEsc(b) {
  var h = b.toString(16).toUpperCase().padStart(2, '0');
  var fmt = state.hexEscapeFormat || 'slash';
  switch (fmt) {
    case 'backslash-x': return '\\x' + h;
    case '0x':          return '0x' + h;
    case 'angle':       return '<' + h + '>';
    case 'bracket':     return '[' + h + ']';
    default:            return '/' + h;
  }
}

// ---- encoding helpers ----
function isAsciiText(text) {
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) > 0x7F) return false;
  }
  return true;
}

function bytesToHexStr(bytes) {
  return Array.from(bytes).map(b => b.toString(16).toUpperCase().padStart(2, '0')).join('');
}

async function encodeTextForSend(text) {
  const enc = state.encoding || 'utf-8';
  if (enc === 'ascii') {
    if (isAsciiText(text)) {
      return bytesToHexStr(new TextEncoder().encode(text));
    }
    return bytesToHexStr(new TextEncoder().encode(text));
  }
  if (enc === 'gb2312') {
    try {
      return await window.go.main.App.EncodeTextForSend(text, 'gb2312');
    } catch {
      return bytesToHexStr(new TextEncoder().encode(text));
    }
  }
  return bytesToHexStr(new TextEncoder().encode(text));
}

function getByteSizeForText(text) {
  const enc = state.encoding || 'utf-8';
  if (enc === 'utf-8' || enc === 'ascii') {
    return (new TextEncoder().encode(text)).length;
  }
  let size = 0;
  for (let i = 0; i < text.length; i++) {
    size += text.charCodeAt(i) > 0x7F ? 2 : 1;
  }
  return size;
}
