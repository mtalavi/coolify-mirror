// Hero demo: a real move replayed in two terminals. Every screen line comes
// from real coolify-mirror captures (assets/demo-frames.js); typing, spinners
// and progress are animated the way the program draws them.
(function () {
  var D = window.CM_DEMO;
  var root = document.querySelector("[data-demo]");
  if (!D || !root) return;

  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var ROWS = 26;
  var ACC = "#a78bfa", MUT = "#8b939e", OK = "#49de80", TXT = "#e6edf3", OPT = "#d0d0d0", EMPTY = "#606060";
  var SPIN = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
  var STOP = {};

  // ---------- text helpers ----------
  function esc(t) {
    return t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/[⠀-⣿★]/g, '<i class="g">$&</i>'); // not in the font: keep one cell
  }
  function sp(text, color, bold) {
    if (!text) return "";
    var s = (color ? "color:" + color + ";" : "") + (bold ? "font-weight:700;" : "");
    return s ? '<span style="' + s + '">' + esc(text) + "</span>" : esc(text);
  }
  function runsHTML(runs) {
    return runs.map(function (r) {
      var s = D.styles[r[0]];
      return s ? '<span style="' + s + '">' + esc(r[1]) + "</span>" : esc(r[1]);
    }).join("");
  }
  function runs(f, i) { return D.frames[f][i]; }
  function line(f, i) { return runsHTML(runs(f, i)); }
  function lines(f, a, b) { var o = []; for (var i = a; i <= b; i++) o.push(line(f, i)); return o; }
  function plain(f, i) { return runs(f, i).map(function (r) { return r[1]; }).join(""); }
  function style(r) { return D.styles[r[0]]; }
  function isColor(r, c) { return style(r).indexOf("color:" + c) === 0; }
  function pad(s, n) { while (s.length < n) s += " "; return s; }
  function lpad(s, n) { s = String(s); while (s.length < n) s = " " + s; return s; }
  function mb(x) { return x.toFixed(1) + " MB"; }

  // A menu or option line with the cursor on it or not (huh's look).
  function optLine(f, i, cur) {
    var r = runs(f, i), label = "", rest = [];
    for (var k = 1; k < r.length; k++) {
      if (!label && (isColor(r[k], OK) || isColor(r[k], OPT))) { label = r[k][1]; rest = r.slice(k + 1); break; }
    }
    return sp("┃", ACC) + " " + (cur ? sp("❯ ", ACC, true) + sp(label, OK) : "  " + sp(label, OPT)) + runsHTML(rest);
  }
  // A project line of the multi-select list.
  function pickLine(i, cur, tick) {
    var r = runs("02-select", i), k = 1;
    while (k < r.length && r[k][1].indexOf("[ ]") !== 0) k++;
    var name = r[k + 1][1];
    return sp("┃", ACC) + " " + (cur ? sp("❯ ", ACC, true) : "  ") +
      (tick ? sp("[✓] ", OK) + sp(name, OK) : sp("[ ] ", MUT) + sp(name, OPT)) + runsHTML(r.slice(k + 2));
  }

  // Progress steps: the captured finished line, plus pending/active states.
  function stepInfo(f, i) {
    var r = runs(f, i), p = plain(f, i), last = r[r.length - 1];
    var col = isColor(last, MUT) ? p.length - last[1].length : p.length;
    return { done: line(f, i), name: p.slice(4, col).replace(/\s+$/, ""), col: col };
  }
  function pending(s) { return "  " + sp("○", MUT) + " " + sp(s.name, MUT); }
  function active(s, value, note) {
    return function (fr) {
      return "  " + sp(SPIN[fr], ACC) + " " + sp(pad(s.name, s.col - 4), TXT, true) + (value ? esc(value) : "") + (note ? "  " + sp(note, MUT) : "");
    };
  }
  // The gradient bar exactly as captured at 100%, cut to the fraction.
  function barCells(f, i) {
    var out = [];
    runs(f, i).forEach(function (r) {
      for (var j = 0; j < r[1].length; j++) if (r[1][j] === "█") out.push(style(r));
    });
    return out;
  }
  function bar(cells, frac, width) {
    width = width || cells.length;
    var n = Math.round(frac * width), h = "";
    for (var j = 0; j < n; j++) h += '<span style="' + cells[Math.round(j * (cells.length - 1) / Math.max(1, width - 1))] + '">█</span>';
    return h + sp(new Array(width - n + 1).join("░"), EMPTY);
  }
  function barLine(cells, frac) { return "  " + bar(cells, frac) + " " + lpad(Math.round(frac * 100), 3) + "%"; }

  // ---------- terminals ----------
  function Term(el) {
    this.el = el;
    this.screen = el.querySelector(".term-screen");
    this.body = el.querySelector(".term-body");
    this.key = el.querySelector(".term-key");
    this.lines = [];
    this.cursor = false;
  }
  var terms = [], spinFrame = 0, dirty = false;
  Term.prototype.reset = function () { this.lines = []; this.cursor = false; draw(); };
  Term.prototype.add = function (l) { this.lines.push.apply(this.lines, [].concat(l)); draw(); return this.lines.length - 1; };
  Term.prototype.set = function (i, l) { this.lines[i] = l; draw(); };
  Term.prototype.cut = function (n) { this.lines.length = n; draw(); };
  Term.prototype.html = function () {
    var from = Math.max(0, this.lines.length - ROWS - 6), h = "";
    for (var i = from; i < this.lines.length; i++) {
      var l = this.lines[i];
      if (typeof l === "function") l = l(spinFrame);
      if (this.cursor && i === this.lines.length - 1) l += '<span class="term-cursor"> </span>';
      h += '<div class="ln">' + (l || " ") + "</div>";
    }
    return h;
  };
  function draw() {
    if (dirty) return;
    dirty = true;
    requestAnimationFrame(function () {
      dirty = false;
      terms.forEach(function (t) {
        var h = t.html();
        if (h === t.last) return;
        t.screen.innerHTML = h; t.last = h;
        t.body.scrollTop = t.body.scrollHeight; // a terminal fills from the top, then scrolls
      });
    });
  }
  setInterval(function () {
    if (paused() || reduce) return;
    spinFrame = (spinFrame + 1) % SPIN.length;
    if (terms.some(function (t) { return t.lines.some(function (l) { return typeof l === "function"; }); })) draw();
  }, 83);

  // ---------- clock: pause, jump to a step, stop ----------
  var gen = 0, skipTo = -1, stopAfter = null, step = 0;
  var userPaused = false, offscreen = true, pausedAt = 0, pausedTotal = 0;
  function paused() { return userPaused || offscreen || document.hidden; }
  function vnow() { return performance.now() - pausedTotal - (pausedAt ? performance.now() - pausedAt : 0); }
  function syncPause() {
    if (paused() && !pausedAt) pausedAt = performance.now();
    if (!paused() && pausedAt) { pausedTotal += performance.now() - pausedAt; pausedAt = 0; }
    root.classList.toggle("is-paused", userPaused);
  }
  function fast() { return step < skipTo; }
  function sleep(ms) {
    var g = gen;
    if (fast()) return g === gen ? Promise.resolve() : Promise.reject(STOP);
    return new Promise(function (res, rej) {
      var end = vnow() + ms;
      (function tick() {
        if (g !== gen) return rej(STOP);
        var left = end - vnow();
        if (left <= 0 && !paused()) return res();
        setTimeout(tick, paused() ? 120 : Math.min(left, 120));
      })();
    });
  }
  function chapter(n) {
    if (stopAfter !== null && n > stopAfter) return Promise.reject(STOP);
    step = n;
    if (n >= skipTo) skipTo = -1;
    steps.forEach(function (b, i) { b.setAttribute("aria-current", i === n ? "step" : "false"); b.classList.toggle("seen", i < n); });
    cap.textContent = steps[n] ? steps[n].dataset.caption : "";
    return sleep(0);
  }
  function focus(t) {
    terms.forEach(function (x) { x.el.classList.toggle("is-active", x === t); });
  }
  function press(t, key) {
    if (fast()) return sleep(0);
    t.key.textContent = key;
    t.key.classList.remove("show"); void t.key.offsetWidth; t.key.classList.add("show");
    return sleep(420);
  }
  async function type(t, prompt, text) {
    var i = t.add(prompt);
    t.cursor = true;
    await sleep(500);
    for (var c = 1; c <= text.length; c++) {
      t.set(i, prompt + esc(text.slice(0, c)));
      if (!fast()) await sleep(text[c - 1] === " " ? 55 : 14 + Math.random() * 26);
    }
    await sleep(380);
    await press(t, "enter");
    t.cursor = false;
    draw();
  }
  async function spin(t, label, ms, doneLine) {
    var i = t.add(function (fr) { return "  " + sp(SPIN[fr], ACC) + " " + esc(label); });
    await sleep(ms);
    t.set(i, doneLine);
  }
  async function progress(t, f, first, last, durations, onStep) {
    var s = [], at = [];
    for (var i = first; i <= last; i++) { s.push(stepInfo(f, i)); at.push(t.add(pending(stepInfo(f, i)))); }
    for (var k = 0; k < s.length; k++) {
      await onStep(k, s[k], function (value, note) { t.set(at[k], active(s[k], value, note)); }, durations[k] || 200);
      t.set(at[k], s[k].done);
    }
  }
  async function animate(ms, fn) {
    if (fast()) { fn(1); return; }
    var t0 = vnow();
    for (;;) {
      var p = Math.min(1, (vnow() - t0) / ms);
      fn(p);
      if (p >= 1) return;
      await sleep(70);
    }
  }

  // ---------- the move ----------
  var T1, T2;
  var P1 = sp("root@old-server", OK, true) + ":" + sp("~", "#60a5fa", true) + "# ";
  var P2 = sp("root@new-server", OK, true) + ":" + sp("~", "#60a5fa", true) + "# ";
  var INSTALL = "curl -fsSL https://raw.githubusercontent.com/mtalavi/coolify-mirror/main/install.sh | sudo sh";
  var CODE = "203.0.113.10/uv77-guon-dq4h-4dcr-cbue-7qqp-bm";

  async function scenario() {
    // 0 · install on the old server
    await chapter(0);
    focus(T1);
    await type(T1, P1, INSTALL);
    await sleep(500); T1.add(line("01-menu", 1));
    await sleep(900); T1.add(line("01-menu", 2));
    await sleep(250); T1.add("");
    for (var i = 4; i <= 10; i++) { T1.add(line("01-menu", i)); await sleep(70); }
    await spin(T1, "Checking this server", 1100, line("01-menu", 11));
    for (i = 12; i <= 14; i++) { T1.add(line("01-menu", i)); await sleep(90); }
    var menu = T1.lines.length;
    T1.add(lines("01-menu", 15, 25));
    await sleep(1700);

    // 1 · pick a project
    await chapter(1);
    await press(T1, "enter");
    T1.cut(menu);
    await spin(T1, "Reading projects and domains", 800, line("02-select", 0));
    var list = T1.lines.length;
    T1.add(lines("02-select", 1, 3));
    var cur = 0, tick = false;
    var drawList = function () {
      for (var r = 0; r < 3; r++) T1.set(list + 3 + r, pickLine(4 + r, cur === r, tick && r === 2));
    };
    T1.add(["", "", ""]); drawList();
    T1.add(lines("02-select", 7, 10));
    await sleep(1100);
    await press(T1, "↓"); cur = 1; drawList(); await sleep(300);
    await press(T1, "↓"); cur = 2; drawList(); await sleep(450);
    await press(T1, "space"); tick = true; drawList(); await sleep(800);
    await press(T1, "enter");
    T1.cut(list);
    T1.add(line("04-options", 1));
    await spin(T1, "Looking for databases and services they depend on", 900, line("04-options", 2));
    var ready = T1.lines.length;
    T1.add(lines("04-options", 3, 19));
    await sleep(2300);

    // 2 · back up
    await chapter(2);
    await press(T1, "enter");
    T1.cut(ready);
    T1.add(line("06-done", 0));
    var cells = barCells("06-done", 16);
    var total = 208.2, done = 0, file = 0;
    var barAt = T1.lines.length + 15;
    await progress(T1, "06-done", 1, 14, [450, 350, 300, 130, 130, 130, 130, 110, 800, 160, 700, 350, 2800, 900],
      async function (k, s, show, ms) {
        if (k === 0) { T1.add(["", barLine(cells, 0), sp("  0.0 MB of 208.2 MB · elapsed 0s", MUT), sp("  ctrl+c to cancel", MUT)]); }
        show();
        var from = done, add = k === 10 ? 21.0 : k === 12 ? 187.2 - 0 : 0;
        if (k === 12) add = total - done;
        await animate(ms, function (p) {
          done = from + add * p;
          file = Math.min(47.5, done * 0.228);
          if (k === 12) show(mb(187.2 * p) + " / 187.2 MB", "docker save");
          T1.set(barAt, barLine(cells, done / total));
          T1.set(barAt + 1, sp("  " + mb(done) + " of 208.2 MB · " + (k > 9 ? "14.6 MB/s · " : "") + "elapsed " + Math.round(1 + (done / total) * 16) + "s · file " + mb(file), MUT));
        });
      });
    T1.cut(barAt);
    T1.add(lines("06-done", 16, 30));
    await sleep(2100);

    // 3 · share
    await chapter(3);
    await press(T1, "enter");
    T1.cut(T1.lines.length - 6);
    await spin(T1, "Starting to share", 700, line("07-share", 0));
    T1.add(lines("07-share", 1, 16));
    var wait = T1.add(function (fr) { return "  " + sp(SPIN[fr], ACC) + " Waiting for the other server to download…"; });
    T1.add(["", line("07-share", 19)]);
    await sleep(3600);

    // 4 · restore on the new server
    await chapter(4);
    focus(T2);
    await type(T2, P2, INSTALL + " -s restore " + CODE);
    await sleep(500); T2.add(line("08-paste", 2));
    await sleep(800); T2.add(line("08-paste", 3));
    await sleep(250); T2.add("");
    for (i = 5; i <= 11; i++) { T2.add(line("08-paste", i)); await sleep(70); }
    await spin(T2, "Checking this server", 1000, line("08-paste", 12));
    for (i = 13; i <= 16; i++) { T2.add(line("08-paste", i)); await sleep(90); }
    var dlCells = barCells("08-paste", 20), shareCells = barCells("06-done", 16);
    var d1 = stepInfo("08-paste", 17), d2 = stepInfo("08-paste", 18);
    var a1 = T2.add(pending(d1)), a2 = T2.add(pending(d2));
    var b2 = T2.add(["", barLine(dlCells, 0), sp("  0.0 MB of 95.0 MB · elapsed 0s", MUT)]) - 1;
    T2.set(a1, active(d1, "0.0 MB / 47.5 MB"));
    var peer = function (frac) {
      return function (fr) {
        return "  " + sp(SPIN[fr], ACC) + " " + pad("198.51.100.20", 15) + " " + bar(shareCells, frac, 50) + " " +
          lpad(Math.round(frac * 100), 3) + "%  " + mb(47.5 * frac) + " / 47.5 MB  " + sp("31.4 MB/s", MUT);
      };
    };
    focus(T1); await sleep(fast() ? 0 : 250);
    await animate(1700, function (p) {
      T1.set(wait, peer(p));
      T2.set(a1, active(d1, mb(47.5 * p) + " / 47.5 MB"));
      T2.set(b2, barLine(dlCells, p / 2));
      T2.set(b2 + 1, sp("  " + mb(47.5 * p) + " of 95.0 MB · elapsed 0s", MUT));
    });
    T1.set(wait, "  " + sp("✓", OK) + " 198.51.100.20 downloaded the complete backup (47.5 MB)");
    T1.cut(wait + 1);
    T1.add(["", "  " + sp("The other server has the backup.", OK) + " It verifies and restores it on its own - you can stop sharing now.",
      sp("  Once the restore there is done, free the space here: menu → Saved files & disk space", MUT), "", line("07-share", 19)]);
    T2.set(a1, d1.done);
    await sleep(fast() ? 0 : 500);
    focus(T2);
    T2.set(a2, active(d2));
    await animate(700, function (p) { T2.set(b2, barLine(dlCells, 0.5 + p / 2)); T2.set(b2 + 1, sp("  " + mb(47.5 + 47.5 * p) + " of 95.0 MB · elapsed 0s", MUT)); });
    T2.set(a2, d2.done);
    T2.add(lines("08-paste", 22, 31));
    await sleep(1200);
    await spin(T2, "Checking this Coolify (encryption test, existing resources)", 800, line("10-confirm", 0));
    await spin(T2, "Preparing the import (trial run in a rolled-back transaction)", 1000, line("10-confirm", 1));
    var ask = T2.lines.length;
    T2.add(lines("10-confirm", 2, 6).concat(lines("10-confirm", 11, 14)));
    await sleep(1800);
    await press(T2, "enter");
    T2.cut(ask);
    T2.add(line("12-domains", 0));
    var rCells = barCells("12-domains", 15), rDone = 0;
    var rBar = T2.lines.length + 14;
    await progress(T2, "12-domains", 1, 13, [140, 110, 110, 120, 100, 550, 150, 400, 1000, 300, 450, 220, 300],
      async function (k, s, show, ms) {
        if (k === 0) T2.add(["", barLine(rCells, 0), sp("  0.0 MB of 208.2 MB · elapsed 0s", MUT)]);
        show();
        var from = rDone, add = k === 7 ? 21.0 : k === 8 ? 187.2 : 0;
        await animate(ms, function (p) {
          rDone = from + add * p;
          T2.set(rBar, barLine(rCells, rDone / 208.2));
          T2.set(rBar + 1, sp("  " + mb(rDone) + " of 208.2 MB · elapsed " + Math.round(1 + (rDone / 208.2) * 8) + "s", MUT));
        });
      });
    T2.set(rBar, line("12-domains", 15)); T2.set(rBar + 1, line("12-domains", 16));
    await spin(T2, "Reading the restored domains", 700, line("12-domains", 17));
    var dom = T2.lines.length;
    T2.add(lines("12-domains", 18, 30));
    await sleep(1500);
    await press(T2, "enter");
    var swap = function (i, bar) { var r = runs("12-domains", i).slice(); r[0] = [r[0][0], bar]; return runsHTML(r); };
    T2.set(dom + 6, swap(24, " ")); T2.set(dom + 7, swap(25, " "));
    T2.set(dom + 9, swap(27, "┃")); T2.set(dom + 10, swap(28, "┃"));
    await sleep(900);
    await press(T2, "enter");
    T2.cut(dom);
    T2.add(line("13-complete", 0));
    var sCells = barCells("13-complete", 8), sBar = T2.lines.length + 7;
    await progress(T2, "13-complete", 1, 6, [450, 450, 1300, 1100, 600, 600],
      async function (k, s, show, ms) {
        if (k === 0) T2.add(["", barLine(sCells, 0), sp("  elapsed 0s", MUT)]);
        show();
        await animate(ms, function (p) {
          T2.set(sBar, barLine(sCells, (k + p) / 6));
          T2.set(sBar + 1, sp("  elapsed " + Math.round(((k + p) / 6) * 113) + "s", MUT));
        });
      });
    T2.set(sBar, line("13-complete", 8)); T2.set(sBar + 1, line("13-complete", 9));
    T2.add(lines("13-complete", 10, 17).concat(lines("13-complete", 19, 25)));
    await sleep(1300);

    // 5 · the site answers on the new server
    await chapter(5);
    browser.classList.add("show");
    await sleep(fast() ? 0 : 700);
    browser.classList.add("loaded");
    if (stopAfter !== null) return; // reduced motion: stay on the result
    await sleep(9000);
    play(0);
  }

  // ---------- wiring ----------
  T1 = new Term(root.querySelector(".term-old"));
  T2 = new Term(root.querySelector(".term-new"));
  terms = [T1, T2];
  var browser = root.querySelector(".demo-browser");
  var steps = Array.prototype.slice.call(root.querySelectorAll("[data-step]"));
  var cap = root.querySelector(".demo-cap");
  var playBtn = root.querySelector(".demo-play");

  function play(from) {
    gen++;
    T1.reset(); T2.reset();
    browser.classList.remove("show", "loaded");
    skipTo = reduce ? Infinity : from;
    stopAfter = reduce ? from : null;
    step = 0;
    scenario().catch(function (e) { if (e !== STOP) throw e; });
  }
  steps.forEach(function (b, i) {
    b.addEventListener("click", function () { userPaused = false; syncPause(); play(i); });
  });
  if (playBtn) {
    if (reduce) playBtn.hidden = true;
    playBtn.addEventListener("click", function () {
      userPaused = !userPaused; syncPause();
      playBtn.setAttribute("aria-label", userPaused ? playBtn.dataset.play : playBtn.dataset.pause);
    });
  }
  document.addEventListener("visibilitychange", syncPause);
  if ("IntersectionObserver" in window) {
    new IntersectionObserver(function (e) { offscreen = !e[0].isIntersecting; syncPause(); }, { threshold: 0.15 }).observe(root);
  } else { offscreen = false; }
  syncPause();
  root.classList.add("is-live");
  var start = function () { play(reduce ? 5 : 0); };
  if (document.fonts && document.fonts.load) {
    Promise.all([document.fonts.load('400 12px "CM Term"'), document.fonts.load('700 12px "CM Term"')]).then(start, start);
  } else { start(); }
})();
