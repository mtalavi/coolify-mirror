// Coolify Mirror site: tabs, screenshot galleries, copy buttons, menu, reveal.
(function () {
  document.documentElement.classList.remove("no-js");
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Tutorial tabs (keyboard: arrows, Home, End).
  var tabs = Array.prototype.slice.call(document.querySelectorAll('[role="tab"]'));
  function select(tab, focus) {
    tabs.forEach(function (t) {
      var on = t === tab;
      t.setAttribute("aria-selected", on ? "true" : "false");
      t.tabIndex = on ? 0 : -1;
      var p = document.getElementById(t.getAttribute("aria-controls"));
      if (!p) return;
      p.hidden = !on;
      if (on && !reduce) { p.classList.remove("enter"); void p.offsetWidth; p.classList.add("enter"); }
    });
    if (focus) tab.focus();
    if (history.replaceState) history.replaceState(null, "", "#" + tab.id);
  }
  tabs.forEach(function (t, i) {
    t.addEventListener("click", function () { select(t, false); });
    t.addEventListener("keydown", function (e) {
      var n = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: 1, ArrowUp: -1 }[e.key];
      if (document.dir === "rtl" && (e.key === "ArrowRight" || e.key === "ArrowLeft")) n = -n;
      if (n) { e.preventDefault(); select(tabs[(i + n + tabs.length) % tabs.length], true); }
      if (e.key === "Home") { e.preventDefault(); select(tabs[0], true); }
      if (e.key === "End") { e.preventDefault(); select(tabs[tabs.length - 1], true); }
    });
  });
  var fromHash = location.hash && document.getElementById(location.hash.slice(1));
  if (fromHash && fromHash.getAttribute("role") === "tab") select(fromHash, false);

  // Galleries: each button shows its screenshot in the main image.
  document.querySelectorAll(".gallery").forEach(function (g) {
    var img = g.querySelector(".main");
    var cap = g.querySelector("figcaption");
    var btns = g.querySelectorAll(".thumb");
    btns.forEach(function (b) {
      b.addEventListener("click", function () {
        btns.forEach(function (x) { x.setAttribute("aria-pressed", x === b ? "true" : "false"); });
        var show = function () {
          img.src = b.dataset.src;
          img.width = +b.dataset.w; img.height = +b.dataset.h;
          img.alt = b.dataset.alt;
          cap.textContent = b.dataset.cap;
          img.classList.remove("swap");
        };
        if (reduce) return show();
        img.classList.add("swap");
        var pre = new Image();
        pre.onload = pre.onerror = function () { setTimeout(show, 120); };
        pre.src = b.dataset.src;
      });
    });
  });

  // Copy buttons.
  document.querySelectorAll(".copy").forEach(function (b) {
    b.addEventListener("click", function () {
      var text = b.dataset.copy;
      var done = function () {
        b.classList.add("done");
        var i = b.querySelector(".ph");
        i.className = "ph ph-check";
        b.setAttribute("aria-label", b.dataset.copied || "Copied");
        setTimeout(function () { b.classList.remove("done"); i.className = "ph ph-copy"; b.setAttribute("aria-label", b.dataset.label || "Copy"); }, 1800);
      };
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(done, function () { fallback(text); done(); });
      } else { fallback(text); done(); }
    });
  });
  function fallback(text) {
    var t = document.createElement("textarea");
    t.value = text; t.setAttribute("readonly", ""); t.style.position = "fixed"; t.style.opacity = "0";
    document.body.appendChild(t); t.select();
    try { document.execCommand("copy"); } catch (e) {}
    document.body.removeChild(t);
  }

  // Mobile menu.
  var nav = document.querySelector(".nav");
  var menu = document.querySelector(".menu-btn");
  if (menu) {
    menu.addEventListener("click", function () {
      var open = nav.classList.toggle("open");
      menu.setAttribute("aria-expanded", open ? "true" : "false");
    });
    nav.querySelectorAll(".nav-links a").forEach(function (a) {
      a.addEventListener("click", function () { nav.classList.remove("open"); menu.setAttribute("aria-expanded", "false"); });
    });
  }

  // Reveal on scroll: content arrives as it comes into view.
  var items = document.querySelectorAll(".reveal");
  if (reduce || !("IntersectionObserver" in window)) {
    items.forEach(function (el) { el.classList.add("in"); });
    return;
  }
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
    });
  }, { rootMargin: "0px 0px -8% 0px", threshold: 0.08 });
  items.forEach(function (el) { io.observe(el); });
})();
