// ---- history rendering & cache ----
// Depends on: state (app.js), t()/formatSysMsg() (i18n.js), decoder functions (decoder.js)

var HISTORY_WINDOW = 100;
var _renderStart = -1;
var _lastRender = 0;

function makeDataRow(display, dir, timestamp, hex, segments) {
  const row = document.createElement('div');
  row.className = 'data-row';

  const tsLine = document.createElement('div');
  const isFwd = (dir === '1' || dir === '2');
  const arrow = isFwd ? '' : (dir === 'tx' ? ' ↾' : ' ⇃');
  const tagClass = dir === '1' ? 'fwd1-line' : dir === '2' ? 'fwd2-line' : (dir === 'rx' ? 'rx-line' : 'tx-line');
  const label = isFwd ? 'P' + dir : (dir === 'rx' ? 'Rx' : 'Tx');
  var byteLen = hex ? hex.length / 2 : 0;
  var tsDirClass = dir === '1' ? 'ts-1' : dir === '2' ? 'ts-2' : (dir === 'rx' ? 'ts-rx' : 'ts-tx');
  tsLine.className = 'ts-line ' + tsDirClass;
  var statClass = isFwd ? 'ts-fstat' : 'ts-stat';
  tsLine.innerHTML = '<span class="ts-text">[' + (timestamp || '') + '] ' + label + arrow + ' </span><span class="' + statClass + '">~' + byteLen + 'B</span>';
  row.appendChild(tsLine);

  const dataLine = document.createElement('div');
  dataLine.className = 'data-line';

  let dm = state.displayMode;
  if (activeMode() === 'forward') {
    if (dir === '1') dm = state.p1DisplayMode;
    else if (dir === '2') dm = state.p2DisplayMode;
  } else if (dir === 'tx') {
    dm = state.txDisplayMode;
  }
  if (dm === 'hex') {
    const hexSpan = document.createElement('span');
    var hexDirClass = dir === '1' ? 'hex-1' : dir === '2' ? 'hex-2' : (dir === 'rx' ? 'hex-rx' : 'hex-tx');
    hexSpan.className = 'hex-data ' + hexDirClass;
    hexSpan.textContent = formatHex(hexToBytes(hex));
    dataLine.appendChild(hexSpan);
  } else if (segments && segments.length > 0) {
    // Go-decoded segments — zero-innerHTML rendering
    renderSegments(dataLine, segments, dir);
  } else {
    // Legacy fallback: JS-side hexToText (backward compat for cached entries)
    let text;
    if (state.encoding === 'ascii') {
      text = sanitizeText(hexToText(hex));
    } else {
      text = hexToText(hex);
    }
    dataLine.innerHTML = text;
  }

  row.appendChild(dataLine);
  display.appendChild(row);
  if (!state.scrollLocked) display.scrollTop = display.scrollHeight;
}

function appendSystemMsg(msg) {
  var display = pageEl('displayContent');
  if (display.querySelector('.placeholder')) display.innerHTML = '';
  var line = document.createElement('div');
  line.className = 'sys-msg';
  line.setAttribute('data-sys-raw', msg);
  line.textContent = '── ' + formatSysMsg(msg) + ' ──';
  display.appendChild(line);
  if (!state.scrollLocked) {
    display.scrollTop = display.scrollHeight;
    trimHistoryDOM(display);
  }

  var tab = getActiveTab();
  if (tab && tab.sessionId) {
    var ts = new Date().toISOString().slice(11, 23).replace(/\./g, ':').slice(0, 12); // HH:MM:SS:mmm
    var entry = { direction: 'system', hex: msg, timestamp: ts };
    if (!state.historyCache[tab.sessionId]) state.historyCache[tab.sessionId] = [];
    var arr = state.historyCache[tab.sessionId];
    var dup = arr.length > 0 && arr[arr.length - 1].direction === 'system' && arr[arr.length - 1].hex === msg;
    if (!dup) arr.push(entry);
  }
}

function appendLine(dir, msg) {
  // Find the tab matching this message's processId
  var ownerTab = null;
  if (msg.processId) {
    for (var i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i].sessionId === msg.processId) { ownerTab = state.tabs[i]; break; }
    }
  }
  if (!ownerTab) return;

  // Always cache the entry for the owning tab, even if not active
  const entry = { direction: dir, hex: msg.hex, timestamp: msg.timestamp, segments: msg.segments };
  if (!state.historyCache[ownerTab.sessionId]) { state.historyCache[ownerTab.sessionId] = []; state.historyCache[ownerTab.sessionId]._id = ownerTab.sessionId; }
  state.historyCache[ownerTab.sessionId].push(entry);

  // Only render to DOM if this tab is currently active
  const activeTab = getActiveTab();
  if (!activeTab || activeTab.sessionId !== ownerTab.sessionId) return;

  const display = pageEl('displayContent');
  if (display.querySelector('.placeholder')) display.innerHTML = '';

  makeDataRow(display, dir, msg.timestamp, msg.hex, msg.segments || null);

  if (!state.scrollLocked) {
    display.scrollTop = display.scrollHeight;
    trimHistoryDOM(display);
  }
}

function trimHistoryDOM(display) {
  var maxNodes = HISTORY_WINDOW + 50;
  while (display.children.length > maxNodes) {
    display.removeChild(display.firstChild);
    if (_renderStart >= 0) _renderStart++;
  }
}

// Render history entries from cache or IPC data (windowed: only visible + buffer)
function renderHistoryLines(entries, keepScroll) {
  if (!entries) return;
  var now = Date.now();
  if (now - _lastRender < 50) return;
  _lastRender = now;
  var display = pageEl('displayContent');
  display.innerHTML = '';
  if (entries.length === 0) {
    display.innerHTML = '<div class="placeholder">' + t('placeholder.no_process', '无串口进程') + '</div>';
    _renderStart = -1;
    return;
  }

  // Fix system message timestamps for chronological order
  var lastTs = 0;
  for (var j = 0; j < entries.length; j++) {
    if (entries[j].direction !== 'system' && entries[j].timestamp) {
      lastTs = entries[j].timestamp;
    } else if (entries[j].direction === 'system' && !entries[j].timestamp) {
      entries[j].timestamp = lastTs;
    }
  }

  var total = entries.length;
  var start, end;
  if (keepScroll && _renderStart >= 0 && _renderStart < total) {
    start = _renderStart;
    end = Math.min(total, start + HISTORY_WINDOW);
  } else {
    // If cleared, start from after the clear marker so old data
    // is hidden until the user explicitly scrolls up.
    var clearPos = (typeof entries._clearedAt === 'number') ? entries._clearedAt + 1 : 0;
    start = Math.max(clearPos, total - HISTORY_WINDOW);
    // Never start past the end — at minimum show the last entry
    // (the clear marker itself) so the display isn't blank.
    if (start > total - 1) start = Math.max(0, total - 1);
    end = total;
  }
  _renderStart = start;

  // "More above" indicator
  if (start > 0) {
    var more = document.createElement('div');
    more.className = 'sys-msg';
    more.style.cursor = 'pointer';
    more.textContent = '── ' + t('history.more_above', '↑ 向上滚动加载更多') + ' ──';
    more.onclick = function() { expandHistory(entries); };
    display.appendChild(more);
  }

  var fragment = document.createDocumentFragment();
  for (var i = start; i < end; i++) {
    var entry = entries[i];
    if (entry.direction === 'system') {
      var line = document.createElement('div');
      line.className = 'sys-msg';
      line.setAttribute('data-sys-raw', entry.hex || '');
      line.textContent = '── ' + formatSysMsg(entry.hex || '') + ' ──';
      fragment.appendChild(line);
    } else {
      makeDataRow(fragment, entry.direction, entry.timestamp, entry.hex, entry.segments || null);
    }
  }
  display.appendChild(fragment);
  if (!keepScroll) display.scrollTop = display.scrollHeight;

  // Scroll-top detection for expanding
  display.onscroll = function() {
    if (display.scrollTop < 80 && _renderStart > 0) {
      expandHistory(entries);
    }
  };
}

function expandHistory(entries) {
  var display = pageEl('displayContent');
  var prevHeight = display.scrollHeight;
  var newStart = Math.max(0, _renderStart - 50);
  if (newStart >= _renderStart) return;

  // If we're at the beginning of the cache but the ring buffer may have
  // data older than what's cached, fetch from daemon first.
  if (newStart <= 0 && entries._id && state.daemonOnline && !_historyLoading) {
    _loadOlderFromDaemon(entries._id, entries).then(function(added) {
      if (added > 0) {
        // Cache was extended — restart expansion from the same scroll position
        _renderStart = _renderStart + added;
        expandHistory(entries);
      } else if (entries._historyFile) {
        // Ring buffer exhausted — offer disk paging (one-shot clickable tip)
        var tip = document.createElement('div');
        tip.className = 'sys-msg disk-tip';
        tip.style.cursor = 'pointer';
        tip.textContent = '── ' + t('history.load_disk', '↓ 加载磁盘历史记录') + ' ──';
        tip.onclick = function() {
          if (entries._diskLoading) return;
          tip.style.cursor = 'default';
          tip.style.pointerEvents = 'none';
          tip.textContent = '── ' + t('history.loading', '加载中...') + ' ──';
          _searchDiskHistory(entries._id, entries);
        };
        var top = display.querySelector('.sys-msg');
        if (top) top.remove();
        display.insertBefore(tip, display.firstChild);
        display.scrollTop = display.scrollHeight - prevHeight;
      }
    }).catch(function() {});
    return;
  }

  var end = Math.min(entries.length, _renderStart);
  var fragment = document.createDocumentFragment();
  for (var i = newStart; i < end; i++) {
    var entry = entries[i];
    if (entry.direction === 'system') {
      var line = document.createElement('div');
      line.className = 'sys-msg';
      line.setAttribute('data-sys-raw', entry.hex || '');
      line.textContent = '── ' + formatSysMsg(entry.hex || '') + ' ──';
      fragment.appendChild(line);
    } else {
      makeDataRow(fragment, entry.direction, entry.timestamp, entry.hex, entry.segments || null);
    }
  }
  var oldMore = display.querySelector('.sys-msg');
  if (oldMore && oldMore.textContent.indexOf('↑') >= 0) oldMore.remove();
  display.insertBefore(fragment, display.firstChild);
  if (newStart > 0) {
    var more = document.createElement('div');
    more.className = 'sys-msg';
    more.style.cursor = 'pointer';
    more.textContent = '── ' + t('history.more_above', '↑ 向上滚动加载更多') + ' ──';
    more.onclick = function() { expandHistory(entries); };
    display.insertBefore(more, display.firstChild);
  } else if (!entries._historyFile && !_historyLoading) {
    // At the very top — check if daemon has more
    var more2 = document.createElement('div');
    more2.className = 'sys-msg';
    more2.style.cursor = 'pointer';
    more2.textContent = '── ' + t('history.more_above', '↑ 向上滚动加载更多') + ' ──';
    more2.onclick = function() { expandHistory(entries); };
    display.insertBefore(more2, display.firstChild);
  }
  _renderStart = newStart;
  display.scrollTop = display.scrollHeight - prevHeight;
}

// Fetch older entries from daemon ring buffer and merge into cache.
// Returns the number of new entries prepended to the cache.
async function _loadOlderFromDaemon(sessionId, entries) {
  try {
    const data = await window.go.main.App.GetSessionHistory(sessionId);
    const history = (data && data.history) ? data.history : [];
    if (!data) return 0;

    // Save file info for potential disk paging
    if (data.oldestTs) entries._oldestTs = data.oldestTs;
    if (data.historyFile) entries._historyFile = data.historyFile;

    if (history.length === 0) return 0;

    // Build fingerprint set from existing cache
    var seen = new Set();
    for (var k = 0; k < entries.length; k++) {
      var e = entries[k];
      if (e.direction !== 'system') {
        seen.add(e.timestamp + '|' + e.direction + '|' + e.hex);
      }
    }

    // Collect new entries not yet in cache
    var prepend = [];
    for (var j = history.length - 1; j >= 0; j--) {
      var he = history[j];
      if (he.direction === 'system') continue;
      var fp = he.timestamp + '|' + he.direction + '|' + he.hex;
      if (!seen.has(fp)) {
        prepend.push(he);
        seen.add(fp);
      }
    }

    if (prepend.length > 0) {
      prepend.reverse();
      // Prepend to the beginning of the cache array
      Array.prototype.unshift.apply(entries, prepend);
      // Adjust clear marker offset
      if (entries._clearedAt !== undefined) {
        entries._clearedAt += prepend.length;
      }
    }
    return prepend.length;
  } catch (e) {
    return 0;
  }
}

// Search the disk history file for entries older than the ring buffer.
// Uses entries._diskOffset to paginate through the file on repeated calls.
async function _searchDiskHistory(sessionId, entries) {
  if (!entries._historyFile || !state.daemonOnline) return;
  if (entries._diskLoading) return; // prevent concurrent clicks
  entries._diskLoading = true;

  try {
    var beforeTs = entries._oldestTs || '';
    var offset = entries._diskOffset || 0;
    var result = await window.go.main.App.SearchHistory(entries._historyFile, '', 200, offset);
    if (!result || !result.history || result.history.length === 0) {
      // No more results — update tip to indicate exhausted
      _updateDiskTip(t('history.no_more', '没有更多历史记录'));
      return;
    }

    var diskEntries = result.history;

    // Build dedup set from existing cache
    var seen = new Set();
    for (var k = 0; k < entries.length; k++) {
      var e = entries[k];
      if (e.direction !== 'system') {
        seen.add(e.timestamp + '|' + e.direction + '|' + e.hex);
      }
    }

    // Only keep entries older than the ring buffer's oldest AND not already in cache
    var prepend = [];
    for (var i = 0; i < diskEntries.length; i++) {
      var de = diskEntries[i];
      if (de.direction === 'system') continue;
      if (beforeTs && de.timestamp >= beforeTs) continue;
      var fp = de.timestamp + '|' + de.direction + '|' + de.hex;
      if (seen.has(fp)) continue;
      prepend.push(de);
      seen.add(fp);
    }

    if (prepend.length > 0) {
      Array.prototype.unshift.apply(entries, prepend);
      if (entries._clearedAt !== undefined) {
        entries._clearedAt += prepend.length;
      }
      entries._diskOffset = offset + diskEntries.length;
      // Keep the adjusted render position so the newly loaded entries
      // are visible at the top instead of jumping back to the bottom.
      _renderStart = Math.max(0, prepend.length - 5);
      renderHistoryLines(entries, true);
    } else if (result.hasMore) {
      // Current batch was all duplicates, try next page
      entries._diskOffset = offset + diskEntries.length;
      entries._diskLoading = false;
      _searchDiskHistory(sessionId, entries);
      return;
    } else {
      _updateDiskTip(t('history.no_more', '没有更多历史记录'));
    }
  } catch (e) {} finally {
    entries._diskLoading = false;
  }
}

// Replace the disk-paging tip at the top of the display.
function _updateDiskTip(text) {
  var display = pageEl('displayContent');
  if (!display) return;
  var old = display.querySelector('.disk-tip');
  if (old) old.remove();
  if (!text) return;
  var tip = document.createElement('div');
  tip.className = 'sys-msg disk-tip';
  tip.style.cursor = 'default';
  tip.style.opacity = '0.6';
  tip.textContent = '── ' + text + ' ──';
  display.insertBefore(tip, display.firstChild);
}

function setupHistoryScroll(entries) {}

// Merge new ring buffer entries into the cache (used after connect / disconnect).
async function mergeRingBuffer(sessionId) {
  if (!sessionId || !state.daemonOnline) return;
  try {
    const data = await window.go.main.App.GetSessionHistory(sessionId);
    const history = (data && data.history) ? data.history : [];
    if (history.length > 0) {
      if (!state.historyCache[sessionId]) { state.historyCache[sessionId] = []; state.historyCache[sessionId]._id = sessionId; }
      const arr = state.historyCache[sessionId];
      const seen = new Set();
      const scanStart = Math.max(0, arr.length - 200);
      for (let i = scanStart; i < arr.length; i++) {
        const e = arr[i];
        seen.add(e.timestamp + '|' + e.direction + '|' + e.hex);
      }
      for (const entry of history) {
        const fp = entry.timestamp + '|' + entry.direction + '|' + entry.hex;
        const sysFp = entry.direction === 'system' ? 'system|' + entry.hex : null;
        if (!seen.has(fp) && (!sysFp || !seen.has(sysFp))) {
          arr.push(entry);
          seen.add(fp);
          if (sysFp) seen.add(sysFp);
        }
      }
    }
  } catch {}
}

var _historyLoading = null; // sessionId currently loading, prevents concurrent IPC

// Load history from daemon for the active tab and display it
async function loadTabHistory() {
  const tab = getActiveTab();
  const display = pageEl('displayContent');

  if (!tab || !tab.sessionId) {
    display.innerHTML = '<div class="placeholder">' + t('placeholder.no_process', '无串口进程') + '</div>';
    return;
  }

  const cacheKey = tab.sessionId;
  // Use non-empty cache when available ([] is truthy — guard with length check)
  if (state.historyCache[cacheKey] && state.historyCache[cacheKey].length > 0) {
    renderHistoryLines(state.historyCache[cacheKey]);
    return;
  }

  // Prevent concurrent loads for the same session (e.g. back-to-back
  // syncDaemonSessions calls both trying to load history for a new tab).
  if (_historyLoading === cacheKey) return;
  _historyLoading = cacheKey;

  try {
    const data = await window.go.main.App.GetSessionHistory(tab.sessionId);
    const history = (data && data.history) ? data.history : [];
    history._id = tab.sessionId;
    // Save extra fields for disk paging
    if (data && data.oldestTs) history._oldestTs = data.oldestTs;
    if (data && data.historyFile) history._historyFile = data.historyFile;
    state.historyCache[cacheKey] = history;
    renderHistoryLines(history);
  } catch (e) {
    display.innerHTML = '<div class="placeholder">暂无记录</div>';
  } finally {
    if (_historyLoading === cacheKey) _historyLoading = null;
  }
}

// clearDisplay is a local GUI action — it clears the view without touching
// the daemon ring buffer or disk file.  Old data remains in cache and can
// be recalled by scrolling up (expandHistory → _loadOlderFromDaemon).
function clearDisplay() {
  const tab = getActiveTab();
  const display = pageEl('displayContent');
  if (!tab || !tab.sessionId) {
    display.innerHTML = '';
    return;
  }

  const cacheKey = tab.sessionId;
  var cache = state.historyCache[cacheKey];
  if (!cache) { cache = []; state.historyCache[cacheKey] = cache; }

  // Insert a clear marker at current end of cache (rendered inline later).
  var ts = new Date().toISOString().slice(11, 23).replace(/\./g, ':').slice(0, 12);
  cache._clearedAt = cache.length;
  cache.push({ direction: 'system', hex: 'sys.history_cleared', timestamp: ts });

  // Reset stats display (local counters only).
  state.statsBase[cacheKey] = { tx: 0, rx: 0 };
  refreshStatsDOM();

  // Re-render — starts from after the clear marker.
  display.innerHTML = '';
  _renderStart = -1;
  renderHistoryLines(cache);
}

function calcRenderCount() {
  const display = pageEl('displayContent');
  if (!display) return 30;
  const lineH = 15 * 1.6 * 2; // font-size x line-height x ~2 rows per entry
  return Math.ceil(display.clientHeight / lineH * 1.5);
}
