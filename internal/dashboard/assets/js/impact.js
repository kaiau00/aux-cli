// Impact graph view: the project's
// deterministic AST-derived change-impact graph, never an LLM guess.
(function () {
  "use strict";
  var api = window.AuxCommon.api, esc = window.AuxCommon.esc, card = window.AuxCommon.card, kv = window.AuxCommon.kv;
  var content = document.getElementById("content");

  // A static circular layout (no physics simulation) so the diagram is
  // deterministic and cheap to render for a few dozen nodes.
  function svgGraph(nodes, edges) {
    if (!nodes.length) return "";
    var size = 420, cx = size / 2, cy = size / 2, r = size / 2 - 40;
    var pos = {};
    nodes.forEach(function (n, i) {
      var angle = (2 * Math.PI * i) / nodes.length;
      pos[n.id] = { x: cx + r * Math.cos(angle), y: cy + r * Math.sin(angle) };
    });
    var lines = edges.map(function (e) {
      var a = pos[e.from], b = pos[e.to];
      if (!a || !b) return "";
      return '<line class="edge" x1="' + a.x + '" y1="' + a.y + '" x2="' + b.x + '" y2="' + b.y + '" />';
    }).join("");
    var circles = nodes.map(function (n) {
      var p = pos[n.id];
      return '<circle class="node" cx="' + p.x + '" cy="' + p.y + '" r="5"><title>' + esc(n.stableKey) + "</title></circle>" +
        '<text class="node-label" x="' + (p.x + 7) + '" y="' + (p.y + 3) + '">' + esc(shortKey(n.stableKey)) + "</text>";
    }).join("");
    return '<svg class="graph-svg" viewBox="0 0 ' + size + " " + size + '">' + lines + circles + "</svg>";
  }

  function shortKey(key) {
    var parts = String(key || "").split("/");
    return parts[parts.length - 1];
  }

  function nodeRows(nodes) {
    if (!nodes.length) return '<div class="empty">No nodes.</div>';
    return nodes.slice(0, 100).map(function (n) {
      return '<div class="list-row"><span class="primary"><span class="badge">' + esc(n.type) + "</span> " + esc(n.stableKey) + "</span></div>";
    }).join("");
  }

  function edgeRows(edges, nodesByID) {
    if (!edges.length) return '<div class="empty">No edges.</div>';
    return edges.slice(0, 100).map(function (e) {
      var from = nodesByID[e.from] ? nodesByID[e.from].stableKey : e.from;
      var to = nodesByID[e.to] ? nodesByID[e.to].stableKey : e.to;
      return '<div class="list-row"><span class="primary">' + esc(from) + " → " + esc(to) + '</span><span class="secondary"><span class="badge">' + esc(e.type) + "</span></span></div>";
    }).join("");
  }

  function render(view) {
    if (!view.built) {
      content.innerHTML = card("Impact graph", '<div class="empty">Not built yet — the graph indexes automatically as Aux works in this project.</div>');
      return;
    }
    var nodesByID = {};
    (view.nodes || []).forEach(function (n) { nodesByID[n.id] = n; });

    var summary = card("Impact graph",
      '<div class="kv">' +
        kv("Nodes", String(view.nodeCount || 0)) +
        (view.status ? kv("Status", esc(view.status)) : "") +
        (view.sourceRevision ? kv("Revision", esc(view.sourceRevision.slice(0, 12))) : "") +
      "</div>" + svgGraph(view.nodes || [], view.edges || []));
    var nodesCard = card("Nodes", nodeRows(view.nodes || []));
    var edgesCard = card("Edges", edgeRows(view.edges || [], nodesByID));

    content.innerHTML = summary + nodesCard + edgesCard;
  }

  if (window.AuxNav) window.AuxNav.mount("Impact graph");
  api("/api/v1/impact").then(render).catch(function (e) {
    content.innerHTML = '<div class="empty">Failed to load impact graph: ' + esc(e.message) + "</div>";
  });
})();
