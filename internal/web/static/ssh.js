// SSH console (/ssh/{id}): xterm.js in the page, a WebSocket to the server,
// which holds the SSH connection. Terminal data travels in binary frames,
// control messages as JSON text frames (see internal/web/ssh.go).
(function () {
  "use strict";
  var root = document.getElementById("ssh");
  if (!root || !window.Terminal || !window.FitAddon) return;

  // xterm.js creates <style> elements; this page's CSP allows them only with
  // the response's style nonce.
  var nonce = root.dataset.nonce;
  var realCreate = Document.prototype.createElement;
  Document.prototype.createElement = function (name) {
    var el = realCreate.apply(this, arguments);
    if (String(name).toLowerCase() === "style") el.setAttribute("nonce", nonce);
    return el;
  };

  function $(id) { return document.getElementById(id); }
  var csrf = document.body.dataset.csrf || "";
  var statusEl = $("ssh-status"), infoEl = $("ssh-info"), reconnect = $("ssh-reconnect");
  var loginForm = $("ssh-login"), keyBox = $("ssh-hostkey"), termEl = $("ssh-term");
  var msg = root.dataset;

  var term = new window.Terminal({ cursorBlink: true, fontSize: 14, scrollback: 5000,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'DejaVu Sans Mono', monospace" });
  var fit = new window.FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(termEl);
  try { fit.fit(); } catch (e) { /* not visible yet */ }

  var ws = null, failed = false, nonceKey = "";
  // A login typed on this page is kept in memory (never in storage) so that
  // confirming the host key does not ask for it again.
  var typed = null, typedUsed = false;
  var encoder = new TextEncoder();

  function setStatus(text, err) {
    statusEl.textContent = text;
    statusEl.classList.toggle("err", !!err);
  }
  function send(obj) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
  }
  function errorText(key, detail) {
    var name = "msgErr" + key.replace("ssh.err.", "").replace(/^./, function (c) { return c.toUpperCase(); });
    return (msg[name] || key) + (detail ? " – " + detail : "");
  }
  function template(id) { return $(id).content.textContent; }

  function showLogin(reason) {
    $("ssh-login-msg").textContent = reason === "rejected" ? msg.msgRejected : msg.msgLogin;
    loginForm.hidden = false;
    loginForm.elements.user.focus();
  }
  function showHostKey(m, changed) {
    keyBox.hidden = false;
    termEl.hidden = true;
    $("ssh-hostkey-title").textContent = template(changed ? "ssh-t-changed" : "ssh-t-new");
    setStatus(template(changed ? "ssh-t-changed" : "ssh-t-new"), changed);
    $("ssh-hostkey-text").textContent = template(changed ? "ssh-t-changed-text" : "ssh-t-new-text");
    $("ssh-hostkey-type").textContent = m.type || "";
    $("ssh-hostkey-fp").textContent = m.fingerprint || "";
    $("ssh-hostkey-want-row").hidden = !changed;
    $("ssh-hostkey-want").textContent = m.want || "";
    $("ssh-hostkey-trust").hidden = changed;
    $("ssh-hostkey-edit").hidden = !changed;
    nonceKey = m.nonce || "";
    failed = true;
  }

  function connect() {
    failed = false;
    typedUsed = false;
    loginForm.hidden = true;
    keyBox.hidden = true;
    termEl.hidden = false;
    reconnect.hidden = true;
    infoEl.textContent = "";
    try { fit.fit(); } catch (e) { /* ignore */ }
    setStatus(msg.msgConnecting);
    var proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(proto + "//" + window.location.host + msg.ws + "?csrf=" + encodeURIComponent(csrf));
    ws.binaryType = "arraybuffer";
    ws.onopen = function () { send({ t: "start", cols: term.cols, rows: term.rows }); };
    ws.onmessage = function (ev) {
      if (typeof ev.data !== "string") {
        term.write(new Uint8Array(ev.data));
        return;
      }
      var m;
      try { m = JSON.parse(ev.data); } catch (e) { return; }
      switch (m.t) {
        case "status":
          setStatus(msg.msgConnecting);
          break;
        case "login":
          if (m.reason === "rejected") typed = null;
          if (typed && !typedUsed) {
            typedUsed = true;
            send({ t: "auth", user: typed.user, pass: typed.pass });
          } else {
            showLogin(m.reason);
          }
          break;
        case "connected":
          setStatus(msg.msgConnected);
          infoEl.textContent = [m.profile, m.kex, m.hostkey, m.cipher, m.mac].filter(Boolean).join(" · ");
          term.focus();
          break;
        case "hostkey":
          showHostKey(m, false);
          break;
        case "hostkey_changed":
          showHostKey(m, true);
          break;
        case "error":
          failed = true;
          setStatus(errorText(m.key, m.detail), true);
          break;
        case "closed":
          setStatus(msg.msgClosed + " (" + m.reason + ")");
          break;
      }
    };
    ws.onclose = function () {
      reconnect.hidden = false;
      if (!failed && statusEl.textContent === msg.msgConnecting) setStatus(msg.msgErrBusy, true);
      else if (!failed && statusEl.textContent.indexOf(msg.msgClosed) !== 0) setStatus(msg.msgClosed);
    };
  }

  term.onData(function (d) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(d));
  });
  term.onBinary(function (d) {
    var b = new Uint8Array(d.length);
    for (var i = 0; i < d.length; i++) b[i] = d.charCodeAt(i) & 255;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(b);
  });
  term.onResize(function (size) { send({ t: "resize", cols: size.cols, rows: size.rows }); });
  window.addEventListener("resize", function () { try { fit.fit(); } catch (e) { /* ignore */ } });

  loginForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var f = loginForm.elements;
    typed = { user: f.user.value, pass: f.pass.value };
    typedUsed = true;
    send({ t: "auth", user: typed.user, pass: typed.pass });
    f.pass.value = "";
    loginForm.hidden = true;
    setStatus(msg.msgConnecting);
  });
  $("ssh-hostkey-trust").addEventListener("click", function () {
    var body = new URLSearchParams();
    body.set("nonce", nonceKey);
    fetch(msg.trust, { method: "POST", credentials: "same-origin", body: body,
      headers: { "X-CSRF-Token": csrf } })
      .then(function (r) {
        if (!r.ok) throw new Error(String(r.status));
        setStatus(msg.msgTrusted);
        connect();
      })
      .catch(function (err) { setStatus(errorText("ssh.err.connect", err.message), true); });
  });
  reconnect.addEventListener("click", connect);
  connect();
})();
