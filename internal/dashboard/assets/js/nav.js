// Shared top navigation, injected into every dashboard page. No page owns
// its own copy of this markup, so
// adding a view means adding one entry here.
//
// This builds the header with DOM APIs rather than innerHTML on purpose. The
// nav has to carry the dashboard token from the current URL into every link,
// and location.search is attacker-controlled: concatenating it into an
// href="..." string let a crafted URL break out of the attribute and inject
// markup on every page that mounts the nav. Using createElement/textContent
// and URLSearchParams means query values are never parsed as HTML.
(function () {
  "use strict";

  var LINKS = [
    { href: "tasks", label: "Tasks" },
    { href: "project", label: "Project Brain" },
    { href: "memory", label: "Memory" },
    { href: "impact", label: "Impact" },
    { href: "optimization", label: "Optimization" },
    { href: "sessions", label: "Sessions" }
  ];

  function currentPage() {
    var path = location.pathname.replace(/^\//, "").replace(/\/$/, "");
    return path === "" ? "tasks" : path;
  }

  // Carry only the dashboard token forward, rather than echoing back whatever
  // else happens to be in the query string.
  function linkTarget(href) {
    var token = new URLSearchParams(location.search).get("token");
    if (!token) return href;
    return href + "?" + new URLSearchParams({ token: token }).toString();
  }

  window.AuxNav = {
    mount: function (subtitle) {
      var header = document.createElement("header");
      header.className = "topbar";

      var brand = document.createElement("span");
      brand.className = "brand";
      brand.textContent = "Aux";
      header.appendChild(brand);

      if (subtitle) {
        var sub = document.createElement("span");
        sub.className = "subtitle";
        sub.textContent = subtitle;
        header.appendChild(sub);
      }

      var active = currentPage();
      var nav = document.createElement("nav");
      nav.className = "navlinks";
      LINKS.forEach(function (l) {
        var a = document.createElement("a");
        a.className = "navlink" + (l.href === active ? " active" : "");
        a.setAttribute("href", linkTarget(l.href));
        a.textContent = l.label;
        nav.appendChild(a);
      });
      header.appendChild(nav);

      document.body.insertBefore(header, document.body.firstChild);
    }
  };
})();
