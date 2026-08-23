// Memory & skills view: what Aux has learned
// about the current project.
(function () {
  "use strict";
  var api = window.AuxCommon.api, esc = window.AuxCommon.esc, card = window.AuxCommon.card, stateClass = window.AuxCommon.stateClass;
  var content = document.getElementById("content");

  function memoryRows(items, emptyText) {
    if (!items || !items.length) return '<div class="empty">' + esc(emptyText) + "</div>";
    return items.map(function (m) {
      return '<div class="list-row"><span class="primary"><span class="badge">' + esc(m.type) + "</span> " + esc(m.stableKey) + "</span>" +
        '<span class="secondary">' + (m.confidence ? (m.confidence * 100).toFixed(0) + "% · " : "") + '<span class="badge ' + stateClass(m.state) + '">' + esc(m.state) + "</span></span></div>";
    }).join("");
  }

  function skillRows(items) {
    if (!items || !items.length) return '<div class="empty">No skills yet.</div>';
    return items.map(function (s) {
      return '<div class="list-row"><span class="primary">' + esc(s.name) + "</span>" +
        '<span class="secondary"><span class="badge ' + stateClass(s.state) + '">' + esc(s.state) + "</span></span></div>";
    }).join("");
  }

  function render(view) {
    var memoryCard = card("Memory",
      '<div class="section-title">Active</div>' + memoryRows(view.active, "No active memory yet.") +
      '<div class="section-title">Candidates</div>' + memoryRows(view.candidates, "No candidates awaiting promotion.") +
      '<div class="section-title">Stale</div>' + memoryRows(view.stale, "None marked stale."));
    var skillCard = card("Skills", skillRows(view.skills));
    content.innerHTML = memoryCard + skillCard;
  }

  if (window.AuxNav) window.AuxNav.mount("Memory & skills");
  api("/api/v1/memory").then(render).catch(function (e) {
    content.innerHTML = '<div class="empty">Failed to load memory: ' + esc(e.message) + "</div>";
  });
})();
