// Active-task workspace. Dependency-light vanilla
// JS that renders truthful task state from the read-only /api/v1 projections.
// It never implies validation success without evidence — it displays the state
// the server computed.
(function () {
  "use strict";

  var token = new URLSearchParams(location.search).get("token") || "";
  var listEl = document.getElementById("task-list");
  var detailEl = document.getElementById("task-detail");
  var selected = null;

  function api(path) {
    var sep = path.indexOf("?") >= 0 ? "&" : "?";
    return fetch(path + sep + "token=" + encodeURIComponent(token)).then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.json();
    });
  }

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function stateClass(s) { return "state-" + esc(s || "unverified"); }

  function fmtTokens(n) {
    n = n || 0;
    if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
    if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
    return String(n);
  }

  function loadList() {
    api("/api/v1/tasks").then(function (tasks) {
      if (!tasks || !tasks.length) {
        listEl.innerHTML = '<div class="empty">No tasks yet.</div>';
        return;
      }
      listEl.innerHTML = "";
      tasks.forEach(function (t) {
        var row = document.createElement("div");
        row.className = "task-row" + (t.taskId === selected ? " selected" : "");
        row.innerHTML =
          '<span class="objective">' + esc(t.objective || t.taskId) + "</span>" +
          '<span class="meta"><span class="badge ' + stateClass(t.state) + '">' + esc(t.state) + "</span> " +
          esc(t.mode || "") + "</span>";
        row.addEventListener("click", function () {
          selected = t.taskId;
          loadList();
          loadDetail(t.taskId);
        });
        listEl.appendChild(row);
      });
    }).catch(function (e) {
      listEl.innerHTML = '<div class="empty">Failed to load tasks: ' + esc(e.message) + "</div>";
    });
  }

  function headerCard(h) {
    var pct = h.contextLimit > 0 ? Math.round((h.contextUsed / h.contextLimit) * 100) : 0;
    var cost = h.costUnknown ? "$?" : "$" + (h.cost || 0).toFixed(2);
    return card("Task",
      '<div class="objective">' + esc(h.objective || h.taskId) + "</div>" +
      '<div class="kv">' +
        kv("Project", esc(h.project) + (h.branch ? " @" + esc(h.branch) : "")) +
        kv("Stage", '<span class="badge ' + stateClass(h.state) + '">' + esc(h.stage) + " · " + esc(h.state) + "</span>") +
        (h.model ? kv("Model", esc(h.model)) : "") +
        kv("Context", fmtTokens(h.contextUsed) + " / " + fmtTokens(h.contextLimit) + " (" + pct + "%)") +
        kv("Cost", cost) +
      "</div>");
  }

  function changesCard(c) {
    if (!c || !c.files || !c.files.length) {
      return card("Changes", '<div class="empty">No changes yet</div>');
    }
    var counts = "+" + (c.added || 0) + " ~" + (c.modified || 0) + " -" + (c.deleted || 0);
    var rows = c.files.map(function (f) {
      return '<div class="row op-' + esc(f.operation) + '">' + esc(opGlyph(f.operation)) + " " + esc(f.path) + "</div>";
    }).join("");
    return card("Changes  " + counts, rows);
  }

  function opGlyph(op) { return op === "add" ? "+" : op === "delete" ? "-" : op === "rename" ? "»" : "~"; }

  function validationCard(v) {
    var overall = v && v.overall ? v.overall : "unverified";
    var head = 'Validation · <span class="badge ' + stateClass(overall) + '">' + esc(overall) + "</span>";
    if (!v || !v.criteria || !v.criteria.length) {
      return card(head, '<div class="empty">No validation has run</div>');
    }
    var rows = v.criteria.map(function (c) {
      return '<div class="row"><span class="badge ' + stateClass(c.state) + '">' + esc(c.state) + "</span> " + esc(c.description) + "</div>";
    }).join("");
    return card(head, rows);
  }

  function contextCard(ctx) {
    if (!ctx || !ctx.categories || !ctx.categories.length) return "";
    var total = ctx.totalTokens || 0;
    var rows = ctx.categories.slice().sort(function (a, b) { return b.tokens - a.tokens; }).map(function (cat) {
      var w = total > 0 ? Math.round((cat.tokens / total) * 100) : 0;
      return '<div class="row"><div style="flex:1">' + esc(cat.label) + "</div>" +
        '<div style="width:34%"><div class="bar"><span style="width:' + w + '%"></span></div></div>' +
        '<div style="width:4.5rem;text-align:right">' + fmtTokens(cat.tokens) + "</div></div>";
    }).join("");
    var saved = ctx.savedTokens ? " · saved " + fmtTokens(ctx.savedTokens) : "";
    return card("Context  " + fmtTokens(total) + " / " + fmtTokens(ctx.limitTokens),
      rows + '<div class="kv" style="margin-top:.4rem">' + (ctx.residentPages || 0) + " resident · " + (ctx.pinnedPages || 0) + " pinned" + saved + "</div>");
  }

  function activityCard(groups) {
    if (!groups || !groups.length) return "";
    var rows = groups.map(function (g) {
      var err = g.errors ? ' <span class="badge state-failed">⚠ ' + g.errors + " failed</span>" : "";
      var cnt = g.count > 1 ? " ×" + g.count : "";
      return '<div class="row"><span class="badge ' + stateClass(g.state) + '">' + esc(g.kind) + "</span>" + cnt + err + "</div>";
    }).join("");
    return card("Activity", rows);
  }

  function loadDetail(id) {
    detailEl.innerHTML = '<div class="empty">Loading…</div>';
    api("/api/v1/tasks/" + encodeURIComponent(id)).then(function (view) {
      detailEl.innerHTML =
        headerCard(view.header || {}) +
        changesCard(view.changes) +
        validationCard(view.validation) +
        contextCard(view.context) +
        activityCard(view.activity);
    }).catch(function (e) {
      detailEl.innerHTML = '<div class="empty">Failed to load task: ' + esc(e.message) + "</div>";
    });
  }

  function card(title, body) { return '<div class="card"><h2>' + title + "</h2>" + body + "</div>"; }
  function kv(k, v) { return "<span><b>" + esc(k) + ":</b> " + v + "</span>"; }

  if (window.AuxNav) window.AuxNav.mount("Active task workspace");
  loadList();
})();
