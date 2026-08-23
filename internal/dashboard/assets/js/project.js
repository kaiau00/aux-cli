// Project Brain view: project identity,
// compiled effective profile, and related-project graph.
(function () {
  "use strict";
  var api = window.AuxCommon.api, esc = window.AuxCommon.esc, card = window.AuxCommon.card, kv = window.AuxCommon.kv;
  var content = document.getElementById("content");

  function relatedList(rows, emptyText) {
    if (!rows || !rows.length) return '<div class="empty">' + esc(emptyText) + "</div>";
    return rows.map(function (r) {
      return '<div class="list-row"><span class="primary">' + esc(r.projectId) + '</span>' +
        '<span class="secondary"><span class="badge">' + esc(r.relationType) + "</span> via " + esc(r.source) + "</span></div>";
    }).join("");
  }

  function render(view) {
    var identity = card("Project",
      '<div class="objective">' + esc(view.name || view.projectId) + "</div>" +
      '<div class="kv">' +
        kv("Root", esc(view.root)) +
        kv("VCS", esc(view.vcsType) + (view.branch ? " @" + esc(view.branch) : "")) +
        (view.revision ? kv("Revision", esc(view.revision.slice(0, 12)) + (view.dirty ? " [dirty]" : "")) : "") +
      "</div>");

    var profile = view.profile || {};
    var conflicts = (profile.conflicts || []).length
      ? '<div class="secondary">Conflicts: ' + profile.conflicts.map(esc).join(", ") + "</div>"
      : "";
    var profileCard = card("Effective profile  " + (profile.entries || 0) + " entries · ~" + (profile.tokenEstimate || 0) + " tokens",
      conflicts + '<div class="manifest">' + esc(profile.manifest || "(empty)") + "</div>");

    var relatedCard = card("Related projects",
      '<div class="section-title">Depends on</div>' + relatedList(view.dependsOn, "No known dependencies.") +
      '<div class="section-title">Depended on by</div>' + relatedList(view.dependedOnBy, "No known dependents."));

    content.innerHTML = identity + profileCard + relatedCard;
  }

  if (window.AuxNav) window.AuxNav.mount("Project Brain");
  api("/api/v1/project").then(render).catch(function (e) {
    content.innerHTML = '<div class="empty">Failed to load project: ' + esc(e.message) + "</div>";
  });
})();
