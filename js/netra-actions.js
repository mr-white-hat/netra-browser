// netra-actions.js — versioned JS primitives for netra-browser agents.
//
// Inject once per page via the bridge's browser_eval; helpers then live at
// window.__netra.*. Self-contained, no dependencies. Idempotent: re-injecting
// the bundle replaces the namespace cleanly without breaking in-flight code.
//
// Single-file v0; bundler/release pipeline deferred.
(function () {
  "use strict";

  const VERSION = "0.1.0";

  // ------------------------------------------------------------------
  // extractTable(selector)
  //   Pull rows out of a <table>, returning each row as an object keyed by
  //   the header column text (or by index if no <thead>).
  // ------------------------------------------------------------------
  function extractTable(selector) {
    const table = document.querySelector(selector);
    if (!table) return { ok: false, error: "no element matches " + selector };
    if (table.tagName !== "TABLE") {
      return { ok: false, error: "matched element is <" + table.tagName.toLowerCase() + ">, not <table>" };
    }
    const headerCells = table.querySelectorAll("thead th, thead td");
    const headers = [];
    headerCells.forEach(function (h) { headers.push((h.innerText || "").trim()); });

    const bodyRows = table.querySelectorAll("tbody tr");
    const rowsList = bodyRows.length ? bodyRows : table.querySelectorAll("tr");
    const out = [];
    rowsList.forEach(function (tr) {
      // skip rows that are all-th (header row not in <thead>)
      const cells = tr.querySelectorAll("td, th");
      if (!cells.length) return;
      const isHeader = Array.prototype.every.call(cells, function (c) { return c.tagName === "TH"; });
      if (isHeader && headers.length) return;
      const obj = {};
      cells.forEach(function (c, i) {
        const key = headers[i] || ("col_" + i);
        obj[key] = (c.innerText || "").trim();
      });
      out.push(obj);
    });
    return { ok: true, rows: out, headers: headers };
  }

  // ------------------------------------------------------------------
  // scrollToBottom({timeoutMs, settleMs})
  //   Smooth-scroll to the bottom; if document height keeps growing
  //   (infinite scroll), keep scrolling until height stabilizes for
  //   settleMs OR timeoutMs is hit. Returns the final height.
  // ------------------------------------------------------------------
  function scrollToBottom(opts) {
    opts = opts || {};
    const timeoutMs = opts.timeoutMs || 5000;
    const settleMs = opts.settleMs || 500;
    const start = Date.now();
    let lastHeight = -1;
    let lastChange = Date.now();
    return new Promise(function (resolve) {
      function step() {
        const h = document.documentElement.scrollHeight;
        window.scrollTo(0, h);
        const now = Date.now();
        if (h !== lastHeight) {
          lastHeight = h;
          lastChange = now;
        }
        if (now - lastChange >= settleMs) {
          resolve({ ok: true, final_height: h, elapsed_ms: now - start });
          return;
        }
        if (now - start >= timeoutMs) {
          resolve({ ok: true, final_height: h, elapsed_ms: now - start, timed_out: true });
          return;
        }
        setTimeout(step, 100);
      }
      step();
    });
  }

  // ------------------------------------------------------------------
  // formAutoFill({email, password, ...})
  //   Heuristic field-fill: walks visible <input>/<textarea> elements,
  //   matches each value to the most likely field by (type, name,
  //   autocomplete, placeholder, label text). Returns which fields it
  //   filled so the caller can verify.
  // ------------------------------------------------------------------
  function formAutoFill(values) {
    values = values || {};
    const filled = [];
    const skipped = [];
    const inputs = document.querySelectorAll("input, textarea");

    function fieldHints(el) {
      const hints = [el.type, el.name, el.id, el.autocomplete, el.placeholder].filter(Boolean).map(function (s) { return String(s).toLowerCase(); });
      // Find associated label.
      let lbl = "";
      if (el.id) {
        const l = document.querySelector('label[for="' + CSS.escape(el.id) + '"]');
        if (l) lbl = l.innerText || "";
      }
      if (!lbl && el.parentElement) {
        const l = el.parentElement.closest("label");
        if (l) lbl = l.innerText || "";
      }
      if (lbl) hints.push(lbl.toLowerCase());
      return hints.join(" ");
    }

    function matchesAny(hint, needles) {
      return needles.some(function (n) { return hint.indexOf(n) !== -1; });
    }

    const RULES = {
      email: ["email", "e-mail"],
      password: ["password", "passwd"],
      username: ["username", "user", "login", "userid"],
      first_name: ["first name", "firstname", "given"],
      last_name: ["last name", "lastname", "surname", "family"],
      phone: ["phone", "mobile", "tel"],
      address: ["address", "street"],
      city: ["city", "town"],
      zip: ["zip", "postal", "postcode"],
      country: ["country"],
    };

    function setVal(el, v) {
      const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, "value").set;
      setter.call(el, v);
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
    }

    inputs.forEach(function (el) {
      const t = (el.type || "").toLowerCase();
      if (t === "submit" || t === "button" || t === "hidden" || t === "checkbox" || t === "radio" || t === "file") return;
      if (el.disabled || el.readOnly) return;
      if (el.offsetParent === null && t !== "password") return; // hidden — but allow password (sometimes off-screen)
      const hints = fieldHints(el);

      // Direct field-type matches.
      if (t === "email" && values.email !== undefined) {
        setVal(el, values.email);
        filled.push({ key: "email", selector_hint: el.name || el.id, value_was: values.email });
        return;
      }
      if (t === "password" && values.password !== undefined) {
        setVal(el, values.password);
        filled.push({ key: "password", selector_hint: el.name || el.id, value_was: "***" });
        return;
      }

      // Hint-based matching.
      for (const key in RULES) {
        if (!Object.prototype.hasOwnProperty.call(values, key)) continue;
        if (matchesAny(hints, RULES[key])) {
          setVal(el, values[key]);
          filled.push({ key: key, selector_hint: el.name || el.id, value_was: key === "password" ? "***" : values[key] });
          return;
        }
      }
      skipped.push({ selector_hint: el.name || el.id, hints: hints });
    });

    return { ok: true, filled: filled, skipped_count: skipped.length };
  }

  // ------------------------------------------------------------------
  // detectFrameworks()
  //   Heuristic framework detection. Returns flags + version where guessable.
  // ------------------------------------------------------------------
  function detectFrameworks() {
    const r = {};
    r.react = !!(window.React || document.querySelector("[data-reactroot], [data-reactid]") ||
                 (document.querySelector("*") && Object.keys(document.querySelector("*")).some(function (k) { return k.startsWith("__reactFiber") || k.startsWith("__reactInternalInstance"); })));
    r.next = !!document.getElementById("__next") || !!window.__NEXT_DATA__;
    r.vue = !!window.Vue || !!document.querySelector("[data-v-]") || !!document.querySelector("[data-v-app]");
    r.nuxt = !!window.__NUXT__;
    r.angular = !!window.ng || !!document.querySelector("[ng-version]");
    r.svelte = !!document.querySelector("[class*='svelte-']");
    r.jquery = !!window.jQuery;
    r.htmx = !!window.htmx || !!document.querySelector("[hx-get], [hx-post]");
    r.alpine = !!window.Alpine;
    r.gatsby = !!document.getElementById("___gatsby");
    r.remix = !!window.__remixContext;
    r.solid = !!window._$HY;
    return { ok: true, frameworks: r };
  }

  // ------------------------------------------------------------------
  // openShadowRoots()
  //   Walk the DOM tree (including any open shadow roots) and return a
  //   summary: count of shadow hosts found, plus innerText of every
  //   shadow root concatenated. Closed shadow roots are unreachable from
  //   page-context JS — that's a browser-level limitation.
  // ------------------------------------------------------------------
  function openShadowRoots() {
    const roots = [];
    const seen = new WeakSet();
    function walk(node) {
      if (!node || seen.has(node)) return;
      seen.add(node);
      if (node.shadowRoot) {
        roots.push({
          host_tag: node.tagName.toLowerCase(),
          host_id: node.id || null,
          text: (node.shadowRoot.textContent || "").trim().slice(0, 4000),
        });
        // Recurse INTO the shadow root tree (one level — children of the
        // shadow root may themselves host nested shadow roots).
        for (let i = 0; i < node.shadowRoot.children.length; i++) walk(node.shadowRoot.children[i]);
      }
      if (node.children) {
        for (let i = 0; i < node.children.length; i++) walk(node.children[i]);
      }
    }
    walk(document.documentElement);
    return { ok: true, count: roots.length, roots: roots };
  }

  // Idempotent install — replace any prior namespace.
  window.__netra = {
    version: VERSION,
    extractTable: extractTable,
    scrollToBottom: scrollToBottom,
    formAutoFill: formAutoFill,
    detectFrameworks: detectFrameworks,
    openShadowRoots: openShadowRoots,
  };
  return window.__netra.version;
})();
