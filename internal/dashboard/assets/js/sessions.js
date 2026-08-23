// Session/log inspector (secondary, debugging-oriented view — the default
// route is the task-first workspace in tasks.js). Vanilla JS, no build step,
// matching the rest of the dashboard.
(function () {
  "use strict";

  var token = new URLSearchParams(location.search).get("token") || "";
  var state = { sessions: new Map(), selected: "", logs: [], connection: "connecting", lastEvent: null };
  var fmt = new Intl.NumberFormat();
  var money = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 });
  var api = function (path) { return path + (path.indexOf("?") >= 0 ? "&" : "?") + "token=" + encodeURIComponent(token); };

  function setText(id, value) { document.getElementById(id).textContent = value; }
  function escapeHTML(value) {
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function updateStats() {
    var sessions = Array.from(state.sessions.values());
    var stats = sessions.reduce(function (acc, s) {
      acc.prompt += s.promptTokens || 0; acc.completion += s.completionTokens || 0; acc.cost += s.cost || 0;
      return acc;
    }, { prompt: 0, completion: 0, cost: 0 });
    setText("sessionCount", sessions.length);
    setText("promptTokens", fmt.format(stats.prompt));
    setText("completionTokens", fmt.format(stats.completion));
    setText("cost", money.format(stats.cost));
  }

  // Live Activity: real connection/event state (the
  // old "Live Core" panel toggled a decorative label between two fixed
  // strings with no other real data bound to it; this replaces it with
  // actual signals).
  function updateActivity() {
    var sessions = Array.from(state.sessions.values());
    var active = sessions.filter(function (s) { return (s.messageCount || 0) > 0; }).length;
    var dotClass = state.connection === "connected" ? "connected" : state.connection === "reconnecting" ? "reconnecting" : "";
    var lastEventText = state.lastEvent ? state.lastEvent.type + " · " + new Date(state.lastEvent.time || Date.now()).toLocaleTimeString() : "none yet";
    document.getElementById("activity").innerHTML =
      '<div class="activity-row"><span class="k"><span class="dot ' + dotClass + '"></span>Connection</span><span class="v">' + escapeHTML(state.connection) + "</span></div>" +
      '<div class="activity-row"><span class="k">Sessions with activity</span><span class="v">' + active + " / " + sessions.length + "</span></div>" +
      '<div class="activity-row"><span class="k">Last event</span><span class="v">' + escapeHTML(lastEventText) + "</span></div>";
  }

  function setConnection(status) {
    state.connection = status;
    updateActivity();
  }

  function renderTree() {
    var root = document.getElementById("tree");
    var sessions = Array.from(state.sessions.values()).sort(function (a, b) { return (b.updatedAt || 0) - (a.updatedAt || 0); });
    root.innerHTML = sessions.map(function (s) {
      return '<div class="node ' + (s.id === state.selected ? "active" : "") + " " + (s.parentSessionId ? "child" : "") + '" data-session="' + escapeHTML(s.id) + '">' +
        '<div class="title">' + escapeHTML(s.title || "Untitled session") + "</div>" +
        '<div class="meta">' + escapeHTML(s.id.slice(0, 8)) + " · " + fmt.format((s.promptTokens || 0) + (s.completionTokens || 0)) + " tokens · " + money.format(s.cost || 0) + "</div></div>";
    }).join("") || '<div class="meta">No sessions yet.</div>';
    Array.prototype.forEach.call(root.querySelectorAll("[data-session]"), function (el) {
      el.onclick = function () { selectSession(el.dataset.session); };
    });
  }

  function selectSession(id) {
    state.selected = id;
    renderTree();
    var inspector = document.getElementById("inspector");
    inspector.innerHTML = '<div class="meta">Loading session...</div>';
    fetch(api("/api/sessions/" + encodeURIComponent(id) + "/messages")).then(function (r) { return r.json(); }).then(function (messages) {
      inspector.innerHTML = messages.map(renderMessage).join("") || '<div class="meta">No messages.</div>';
    });
  }

  function renderMessage(m) {
    var tools = (m.tools || []).map(function (t) {
      return '<div class="meta">tool: ' + escapeHTML(t.name) + " " + (t.finished ? "done" : "running") + '</div><pre>' + escapeHTML(t.input || "") + "</pre>";
    }).join("");
    var results = (m.results || []).map(function (r) {
      return '<div class="meta">result: ' + escapeHTML(r.name || r.toolCallId) + " " + (r.isError ? "error" : "ok") + '</div><pre>' + escapeHTML(r.content || "") + "</pre>";
    }).join("");
    return '<div class="detail"><div class="event-type">' + escapeHTML(m.role) + " " + escapeHTML(m.finish || "") + '</div><div class="event-text">' + escapeHTML(m.text || "") + "</div>" + tools + results + "</div>";
  }

  function pushFeed(event) {
    var feed = document.getElementById("feed");
    var text = (event.data && (event.data.text || event.data.progress || event.data.title || event.data.path || event.data.message)) || "";
    feed.insertAdjacentHTML("afterbegin", '<div class="event"><div class="event-type">' + escapeHTML(event.type) + '</div><div class="event-text">' + escapeHTML(text) + "</div></div>");
    while (feed.children.length > 80) feed.lastElementChild.remove();
  }

  function renderLogs() {
    var logs = document.getElementById("logs");
    logs.innerHTML = state.logs.slice(-80).reverse().map(function (l) {
      return '<div class="event"><div class="event-type">' + escapeHTML(l.level || "log") + '</div><div class="event-text">' + escapeHTML(l.message || "") + "</div></div>";
    }).join("");
  }

  function applyEvent(event) {
    state.lastEvent = event;
    if (event.type.indexOf("session.") === 0) state.sessions.set(event.data.id, event.data);
    if (event.type === "session.deleted") state.sessions.delete(event.data.id);
    if (event.type.indexOf("log.") === 0) { state.logs.push(event.data); renderLogs(); }
    updateStats(); updateActivity(); renderTree(); pushFeed(event);
    if (state.selected && event.type.indexOf("message.") === 0 && event.data.sessionId === state.selected) selectSession(state.selected);
  }

  function boot() {
    if (window.AuxNav) window.AuxNav.mount("Session and log inspector");
    if (!token) { setConnection("missing token"); return; }
    fetch(api("/api/snapshot")).then(function (r) {
      if (!r.ok) throw new Error("unauthorized");
      return r.json();
    }).then(function (snapshot) {
      snapshot.sessions.forEach(function (s) { state.sessions.set(s.id, s); });
      state.logs = snapshot.logs || [];
      updateStats(); updateActivity(); renderTree(); renderLogs();
      var sorted = Array.from(state.sessions.values()).sort(function (a, b) { return (b.updatedAt || 0) - (a.updatedAt || 0); });
      if (sorted[0]) selectSession(sorted[0].id);

      var events = new EventSource(api("/events"));
      events.onopen = function () { setConnection("connected"); };
      events.onerror = function () { setConnection("reconnecting"); };
      events.onmessage = function (msg) { applyEvent(JSON.parse(msg.data)); };
      ["session.created", "session.updated", "session.deleted", "message.created", "message.updated", "message.deleted",
        "agent.created", "agent.updated", "log.created", "history.created", "history.updated", "history.deleted", "dashboard.resync"
      ].forEach(function (type) {
        events.addEventListener(type, function (msg) { applyEvent(JSON.parse(msg.data)); });
      });
    }).catch(function (err) { setConnection(err.message); });
  }

  boot();
})();
