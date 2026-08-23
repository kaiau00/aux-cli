// Optimization view: evaluated experiments and
// governed-cost policies. This is where "governed vs baseline" and "skill vs
// baseline" results computed by `aux eval ab` become visible once a user has
// run them.
(function () {
  "use strict";
  var api = window.AuxCommon.api, esc = window.AuxCommon.esc, card = window.AuxCommon.card, stateClass = window.AuxCommon.stateClass;
  var content = document.getElementById("content");

  function comparisonRow(c) {
    if (!c) return "";
    var verdict = c.improved ? "improved" : "no improvement";
    return '<div class="list-row secondary">&nbsp;&nbsp;' +
      '<span class="badge ' + stateClass(c.improved ? "validated" : "unverified") + '">' + esc(verdict) + "</span> " +
      "baseline " + c.baseline.changesPerDollar.toFixed(3) + " changes/$ &rarr; variant " +
      c.variant.changesPerDollar.toFixed(3) + " changes/$ (&Delta;" + c.delta.toFixed(3) + ")</div>";
  }

  function experimentRows(items) {
    if (!items || !items.length) return '<div class="empty">No experiments recorded yet. Run `aux eval experiment` or `aux eval ab` to populate this view.</div>';
    return items.map(function (e) {
      return '<div class="list-row"><span class="primary">' + esc(e.name) + '</span>' +
        '<span class="secondary"><span class="badge ' + stateClass(e.status) + '">' + esc(e.status) + "</span> " + (e.runs || 0) + " runs</span></div>" +
        comparisonRow(e.comparison);
    }).join("");
  }

  function policyRows(items, emptyText) {
    if (!items || !items.length) return '<div class="empty">' + esc(emptyText) + "</div>";
    return items.map(function (p) {
      return '<div class="list-row"><span class="primary">' + esc(p.taskClass || p.id) + '</span>' +
        '<span class="secondary"><span class="badge ' + stateClass(p.state) + '">' + esc(p.state) + "</span></span></div>";
    }).join("");
  }

  function render(view) {
    var experimentsCard = card("Experiments", experimentRows(view.experiments));
    var policiesCard = card("Governed-cost policies",
      '<div class="section-title">Active</div>' + policyRows(view.activePolicies, "No active policies.") +
      '<div class="section-title">Candidates</div>' + policyRows(view.candidatePolicies, "No candidates awaiting evaluation."));
    content.innerHTML = experimentsCard + policiesCard;
  }

  if (window.AuxNav) window.AuxNav.mount("Optimization");
  api("/api/v1/optimization").then(render).catch(function (e) {
    content.innerHTML = '<div class="empty">Failed to load optimization history: ' + esc(e.message) + "</div>";
  });
})();
