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

  let ws = null;
  let resizeTimeout = null;

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const cols = term.cols;
    const rows = term.rows;
    const url = `${proto}//${location.host}/ws/terminal/${serverID}/${sessionName}?cols=${cols}&rows=${rows}`;

    ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';

    ws.onopen = function() {
      document.getElementById('status').textContent = 'connected';
      document.getElementById('status').className = 'status';
      document.getElementById('reconnect-overlay').classList.remove('show');
    };

    ws.onmessage = function(event) {
      if (event.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(event.data));
      }
    };

    ws.onclose = function() {
      document.getElementById('status').textContent = 'disconnected';
      document.getElementById('status').className = 'status disconnected';
      document.getElementById('reconnect-overlay').classList.add('show');
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
    term.clear();
    connect();
  };

  connect();
})();
