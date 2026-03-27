(function() {
  'use strict';

  const params = new URLSearchParams(window.location.search);
  const serverID = params.get('server');
  const sessionName = params.get('session');

  if (!serverID || !sessionName) {
    document.getElementById('session-info').textContent = 'Error: missing server or session parameter';
    return;
  }

  document.title = sessionName + ' @ terminal';
  document.getElementById('session-info').textContent = sessionName;

  const container = document.getElementById('terminal-container');
  const term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace",
    theme: {
      background: '#1a1a2e',
      foreground: '#e0e0e0',
      cursor: '#4ecca3',
      selectionBackground: '#4ecca340',
    }
  });

  const fitAddon = new FitAddon.FitAddon();
  term.loadAddon(fitAddon);
  term.open(container);
  fitAddon.fit();

  function getCookie(name) {
    const match = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
    return match ? decodeURIComponent(match[1]) : '';
  }

  let ws = null;
  let resizeTimeout = null;

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const cols = term.cols;
    const rows = term.rows;
    const csrfToken = getCookie('csrf');
    const url = `${proto}//${location.host}/ws/terminal/${serverID}/${sessionName}?cols=${cols}&rows=${rows}&csrf=${csrfToken}`;

    ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';

    ws.onopen = function() {
      document.getElementById('status').textContent = 'connected';
      document.getElementById('status').className = 'status';
      document.getElementById('reconnect-overlay').classList.remove('show');
    };

    let idleTimeout = false;

    ws.onmessage = function(event) {
      if (event.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(event.data));
      } else if (typeof event.data === 'string') {
        try {
          const msg = JSON.parse(event.data);
          if (msg.type === 'idle_timeout') {
            idleTimeout = true;
            const overlay = document.getElementById('reconnect-overlay');
            const overlayMsg = overlay.querySelector('.overlay-message');
            if (overlayMsg) {
              overlayMsg.textContent = msg.message || 'Session idle, disconnecting';
            } else {
              overlay.setAttribute('data-idle-message', msg.message || 'Session idle, disconnecting');
            }
          }
        } catch (_) {
          // not JSON, ignore
        }
      }
    };

    ws.onclose = function() {
      document.getElementById('status').textContent = idleTimeout ? 'idle timeout' : 'disconnected';
      document.getElementById('status').className = 'status disconnected';
      const overlay = document.getElementById('reconnect-overlay');
      overlay.classList.add('show');
      if (idleTimeout) {
        overlay.setAttribute('data-idle-timeout', 'true');
        // Show idle message in overlay if element exists
        const overlayMsg = overlay.querySelector('.overlay-message');
        const idleMsg = overlay.getAttribute('data-idle-message');
        if (overlayMsg && idleMsg) {
          overlayMsg.textContent = idleMsg;
        }
      } else {
        overlay.removeAttribute('data-idle-timeout');
      }
    };

    ws.onerror = function() {
      document.getElementById('status').textContent = 'error';
      document.getElementById('status').className = 'status disconnected';
    };
  }

  // Terminal input -> WebSocket
  term.onData(function(data) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data));
    }
  });

  // Window resize -> WebSocket resize message
  window.addEventListener('resize', function() {
    clearTimeout(resizeTimeout);
    resizeTimeout = setTimeout(function() {
      fitAddon.fit();
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      }
    }, 150);
  });

  window.reconnect = function() {
    const overlay = document.getElementById('reconnect-overlay');
    if (overlay.getAttribute('data-idle-timeout') === 'true') {
      // After idle timeout, clear state and allow reconnect
      overlay.removeAttribute('data-idle-timeout');
      overlay.removeAttribute('data-idle-message');
    }
    term.clear();
    connect();
  };

  connect();
})();
