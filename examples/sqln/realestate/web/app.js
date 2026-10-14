// Search page: one text box, results from GET /api/search.
//
// Rendering rule: every value is written as text (textContent), except
// titleHtml and snippetHtml, which the server builds with fts.Highlight.HTML
// — the listing text escaped, with only <mark> added around the matches.
"use strict";

(() => {
  const $ = (id) => document.getElementById(id);
  const form = $("search");
  const input = $("q");
  const clear = $("clear");
  const els = {
    intro: $("intro"), results: $("results"), progress: $("progress"), stats: $("stats"),
    corrected: $("corrected"), understood: $("understood"), chips: $("chips"),
    approximate: $("approximate"), list: $("list"), empty: $("empty"), error: $("error"), pager: $("pager"),
  };

  const DEBOUNCE_MS = 350;
  let timer = 0;
  let controller = null;
  let current = { q: "", page: 0 };

  // ── Helpers ──────────────────────────────────────────────────────────

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;
  const area = (m2) => `${Number(m2).toLocaleString("pt-BR", { maximumFractionDigits: 0 })} m²`;

  // ── Search ───────────────────────────────────────────────────────────

  async function search(q, page, push) {
    q = q.trim();
    current = { q, page };
    syncURL(q, page, push);
    if (!q) {
      showHome();
      return;
    }
    document.body.classList.replace("home", "searched");
    els.intro.hidden = true;
    els.results.hidden = false;
    els.progress.hidden = false;
    els.results.setAttribute("aria-busy", "true");

    controller?.abort();
    controller = new AbortController();
    const started = performance.now();
    try {
      const url = `/api/search?${new URLSearchParams({ q, page: String(page) })}`;
      const res = await fetch(url, { signal: controller.signal, headers: { Accept: "application/json" } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      render(data, (performance.now() - started) / 1000);
      if (push) window.scrollTo({ top: 0 });
    } catch (err) {
      if (err.name === "AbortError") return;
      els.error.hidden = false;
      els.list.replaceChildren();
      els.pager.hidden = true;
    } finally {
      els.progress.hidden = true;
      els.results.removeAttribute("aria-busy");
    }
  }

  function showHome() {
    document.body.classList.replace("searched", "home");
    els.intro.hidden = false;
    els.results.hidden = true;
    document.title = "listaimovel.com";
  }

  function syncURL(q, page, push) {
    const params = new URLSearchParams();
    if (q) params.set("q", q);
    if (page > 0) params.set("page", String(page));
    const url = params.size ? `/?${params}` : "/";
    if (url === location.pathname + location.search) return;
    history[push ? "pushState" : "replaceState"]({ q, page }, "", url);
  }

  // ── Rendering ────────────────────────────────────────────────────────

  function render(data, seconds) {
    document.title = `${data.query} – listaimovel.com`;
    els.error.hidden = true;

    const total = data.total;
    els.stats.textContent = total
      ? `${plural(total, "imóvel encontrado", "imóveis encontrados")} (${seconds.toFixed(2).replace(".", ",")} segundos)`
      : "";

    els.corrected.hidden = !data.corrected;
    if (data.corrected) {
      const fixed = el("strong", "", data.corrected);
      els.corrected.replaceChildren("Mostrando resultados para ", fixed);
    }

    els.chips.replaceChildren(...data.understood.map((c) => el("li", `chip chip-${c.kind}`, c.label)));
    els.understood.hidden = data.understood.length === 0;

    els.approximate.hidden = !data.approximate;
    els.approximate.textContent = data.approximate || "";

    els.list.replaceChildren(...data.results.map(card));
    els.empty.hidden = total > 0;
    renderPager(data.page, data.pages);
  }

  function card(r) {
    const li = el("li", "result");

    const crumb = el("p", "crumb", [r.neighborhood, `${r.city}/${r.uf}`, r.type, r.offer].join(" · "));

    const title = el("h2");
    title.innerHTML = r.titleHtml; // escaped by the server (fts.Highlight.HTML)

    const snippet = el("p", "snippet");
    snippet.innerHTML = r.snippetHtml; // escaped by the server (fts.Highlight.HTML)

    const facts = el("ul", "facts");
    const add = (text) => facts.append(el("li", "", text));
    if (r.bedrooms) add(plural(r.bedrooms, "quarto", "quartos"));
    if (r.suites) add(plural(r.suites, "suíte", "suítes"));
    if (r.bathrooms) add(plural(r.bathrooms, "banheiro", "banheiros"));
    add(r.parking ? plural(r.parking, "vaga", "vagas") : "sem vaga");
    add(area(r.area));

    const deal = el("div", "deal");
    deal.append(el("span", "price", r.priceLabel));
    deal.append(el("span", `badge badge-${r.offerCode}`, r.offer));
    deal.append(el("span", "badge", r.condition));
    deal.append(el("span", "owner", `${r.owner} · ${r.ownerType}`));

    li.append(crumb, title, snippet, facts, deal);
    if (r.features.length) {
      const features = el("ul", "features");
      features.append(...r.features.map((f) => el("li", "", f)));
      li.append(features);
    }
    return li;
  }

  function renderPager(page, pages) {
    els.pager.hidden = pages <= 1;
    if (pages <= 1) return;
    const button = (label, target, opts = {}) => {
      const b = el("button", "", label);
      b.type = "button";
      if (opts.current) b.setAttribute("aria-current", "page");
      if (opts.label) b.setAttribute("aria-label", opts.label);
      b.disabled = target < 0 || target >= pages || opts.current;
      b.addEventListener("click", () => search(current.q, target, true));
      return b;
    };
    const first = Math.max(0, Math.min(page - 4, pages - 9));
    const last = Math.min(pages, first + 9);
    const buttons = [button("‹ Anterior", page - 1, { label: "Página anterior" })];
    for (let p = first; p < last; p++) {
      buttons.push(button(String(p + 1), p, { current: p === page, label: `Página ${p + 1}` }));
    }
    buttons.push(button("Próxima ›", page + 1, { label: "Próxima página" }));
    els.pager.replaceChildren(...buttons);
  }

  // ── Events ───────────────────────────────────────────────────────────

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    clearTimeout(timer);
    search(input.value, 0, true);
  });

  // Search as you type, after a pause.
  input.addEventListener("input", () => {
    clear.hidden = input.value === "";
    clearTimeout(timer);
    const q = input.value.trim();
    if (q.length >= 3 || (q === "" && current.q !== "")) {
      timer = setTimeout(() => search(q, 0, false), DEBOUNCE_MS);
    }
  });

  clear.addEventListener("click", () => {
    input.value = "";
    clear.hidden = true;
    input.focus();
    search("", 0, true);
  });

  $("examples").addEventListener("click", (e) => {
    const b = e.target.closest("button[data-q]");
    if (!b) return;
    input.value = b.dataset.q;
    clear.hidden = false;
    search(input.value, 0, true);
  });

  $("brand").addEventListener("click", (e) => {
    e.preventDefault();
    input.value = "";
    clear.hidden = true;
    search("", 0, true);
  });

  // "/" focuses the box, as in search engines.
  document.addEventListener("keydown", (e) => {
    if (e.key === "/" && document.activeElement !== input) {
      e.preventDefault();
      input.focus();
      input.select();
    }
  });

  window.addEventListener("popstate", () => fromURL());

  function fromURL() {
    const params = new URLSearchParams(location.search);
    const q = params.get("q") || "";
    const page = Number.parseInt(params.get("page") || "0", 10) || 0;
    input.value = q;
    clear.hidden = q === "";
    search(q, page, false);
  }

  fromURL();
  if (!input.value) input.focus();
})();
