// Cockpit view: a Camunda-cockpit-style browser over deployed DMNs — every
// deployed name grouped by version, and per-deployment definition,
// evaluation and history panels. Talks to the same /api/deployments* API
// the builder's "Deploy" flow feeds.
(() => {
  const $ = (id) => document.getElementById(id);

  const state = {
    groups: [],       // [{ name, versions: [deployment...] }], versions desc
    loaded: false,
    offset: 0,
    total: 0,
    filter: "",
    expanded: new Set(), // group names currently expanded in the sidebar
    selectedId: null,
    selected: null,      // full deployment row (with xml) for selectedId
    decisions: null,      // /decisions payload for selectedId
    history: null,        // { evaluations, total, limit, offset }
    tab: "definition",    // definition | evaluate | history
    evalInputs: "",
    evalUserEdited: false,
  };

  const PAGE_SIZE = 100;

  function fmtDate(iso) {
    const d = new Date(iso);
    return isNaN(d) ? iso : d.toLocaleString();
  }

  function el(tag, props, ...children) {
    const e = document.createElement(tag);
    Object.assign(e, props);
    for (const c of children) {
      if (c == null) continue;
      e.append(c);
    }
    return e;
  }

  async function getJSON(url, opts) {
    const res = await fetch(url, opts);
    let data = null;
    try {
      data = await res.json();
    } catch (_) {
      /* no body */
    }
    if (!res.ok) {
      throw new Error((data && data.error) || `HTTP ${res.status}`);
    }
    return data;
  }

  function regroup(deployments) {
    const byName = new Map();
    for (const d of deployments) {
      if (!byName.has(d.name)) byName.set(d.name, []);
      byName.get(d.name).push(d);
    }
    const groups = [...byName.entries()].map(([name, versions]) => ({
      name,
      versions: versions.sort((a, b) => b.version - a.version),
    }));
    groups.sort((a, b) => a.name.localeCompare(b.name));
    return groups;
  }

  async function loadList(reset) {
    if (reset) {
      state.offset = 0;
      state.groups = [];
    }
    const data = await getJSON(`/api/deployments?limit=${PAGE_SIZE}&offset=${state.offset}`);
    state.total = data.total;
    state.offset += data.deployments.length;
    const all = reset ? data.deployments : [...flatten(state.groups), ...data.deployments];
    state.groups = regroup(all);
    state.loaded = true;
    renderSidebar();
  }

  function flatten(groups) {
    return groups.flatMap((g) => g.versions);
  }

  function renderSidebar() {
    const list = $("cockpitList");
    list.innerHTML = "";

    const filtered = state.groups
      .map((g) => g)
      .filter((g) => g.name.toLowerCase().includes(state.filter.toLowerCase()));

    if (filtered.length === 0) {
      list.appendChild(el("p", { className: "hint", textContent: state.loaded ? "No deployments found." : "Loading…" }));
    }

    for (const group of filtered) {
      const latest = group.versions[0];
      const isExpanded = state.expanded.has(group.name);
      const groupEl = el("div", { className: "cockpit-group" });

      const header = el(
        "div",
        { className: "cockpit-group-header" },
        el("span", { className: "cockpit-caret", textContent: isExpanded ? "▾" : "▸" }),
        el("span", { className: "cockpit-name", textContent: group.name }),
        el("span", { className: "badge", textContent: "v" + latest.version }),
        el("span", { className: "cockpit-count", textContent: group.versions.length > 1 ? `${group.versions.length} versions` : "" })
      );
      header.addEventListener("click", () => {
        if (state.expanded.has(group.name)) state.expanded.delete(group.name);
        else state.expanded.add(group.name);
        renderSidebar();
      });
      groupEl.appendChild(header);

      const versionsToShow = isExpanded ? group.versions : group.versions.slice(0, 1);
      const versionList = el("div", { className: "cockpit-versions" });
      for (const dep of versionsToShow) {
        const row = el(
          "div",
          {
            className: "cockpit-version-row" + (dep.id === state.selectedId ? " selected" : ""),
          },
          el("span", { className: "badge badge-outline", textContent: "v" + dep.version }),
          el("span", { className: "cockpit-version-date", textContent: fmtDate(dep.createdAt) })
        );
        row.addEventListener("click", (e) => {
          e.stopPropagation();
          selectDeployment(dep.id);
        });
        versionList.appendChild(row);
      }
      groupEl.appendChild(versionList);
      list.appendChild(groupEl);
    }

    $("cockpitLoadMore").classList.toggle("hidden", flatten(state.groups).length >= state.total);
  }

  async function selectDeployment(id) {
    state.selectedId = id;
    state.selected = null;
    state.decisions = null;
    state.history = null;
    state.tab = "definition";
    state.evalUserEdited = false;
    renderSidebar();
    renderMain();

    try {
      const [dep, decisions] = await Promise.all([
        getJSON(`/api/deployments/${id}`),
        getJSON(`/api/deployments/${id}/decisions`),
      ]);
      state.selected = dep;
      state.decisions = decisions;
      buildEvalTemplate();
      renderMain();
    } catch (e) {
      $("cockpitMain").innerHTML = "";
      $("cockpitMain").appendChild(el("p", { className: "result error", textContent: "Failed to load deployment: " + e.message }));
    }
  }

  function buildEvalTemplate() {
    if (state.evalUserEdited || !state.decisions) return;
    const template = {};
    for (const input of state.decisions.inputData || []) {
      const t = input.Variable && input.Variable.TypeRef;
      template[input.Name] = t === "number" ? 0 : t === "boolean" ? false : "";
    }
    state.evalInputs = JSON.stringify(template, null, 2);
  }

  async function loadHistory(offset) {
    offset = offset || 0;
    const data = await getJSON(`/api/deployments/${state.selectedId}/evaluations?limit=20&offset=${offset}`);
    state.history = data;
    renderMain();
  }

  // --- rendering: main panel ---

  function renderMain() {
    const main = $("cockpitMain");
    main.innerHTML = "";

    if (!state.selectedId) {
      main.appendChild(el("p", { className: "hint", textContent: "Select a deployment on the left to inspect it." }));
      return;
    }
    if (!state.selected || !state.decisions) {
      main.appendChild(el("p", { className: "hint", textContent: "Loading…" }));
      return;
    }

    const dep = state.selected;
    main.appendChild(
      el(
        "div",
        { className: "cockpit-header" },
        el("h2", { textContent: dep.name }),
        el("span", { className: "badge", textContent: "v" + dep.version }),
        el("span", { className: "hint", textContent: `Deployed ${fmtDate(dep.createdAt)} · DMN ${dep.dmnVersion || "?"}` }),
        el(
          "button",
          { className: "secondary danger", textContent: "Delete this version" },
          )
      )
    );
    main.lastChild.lastChild.addEventListener("click", () => deleteSelected());

    const tabs = el("div", { className: "toolbar cockpit-tabs" });
    for (const [id, label] of [["definition", "Definition"], ["evaluate", "Evaluate"], ["history", "History"]]) {
      const b = el("button", {
        className: "secondary" + (state.tab === id ? " active" : ""),
        textContent: label,
      });
      b.addEventListener("click", () => {
        state.tab = id;
        if (id === "history" && !state.history) loadHistory(0);
        renderMain();
      });
      tabs.appendChild(b);
    }
    main.appendChild(tabs);

    if (state.tab === "definition") main.appendChild(renderDefinitionTab());
    else if (state.tab === "evaluate") main.appendChild(renderEvaluateTab());
    else main.appendChild(renderHistoryTab());
  }

  async function deleteSelected() {
    if (!confirm(`Delete ${state.selected.name} v${state.selected.version}? This cannot be undone.`)) return;
    try {
      await fetch(`/api/deployments/${state.selectedId}`, { method: "DELETE" });
      state.selectedId = null;
      state.selected = null;
      await loadList(true);
      renderMain();
    } catch (e) {
      alert("Delete failed: " + e.message);
    }
  }

  function renderDefinitionTab() {
    const wrap = el("div", {});
    const decisions = state.decisions.decisions || [];
    const byId = Object.fromEntries(decisions.map((d) => [d.id, d]));

    for (const d of decisions) {
      const card = el("div", { className: "node-panel" });
      const reqBadges = (d.requires || []).map((r) =>
        el("span", {
          className: "badge badge-outline",
          textContent: r.type === "input" ? "in: " + r.ref : "needs: " + (byId[r.ref] ? byId[r.ref].name : r.ref),
        })
      );
      card.appendChild(
        el(
          "div",
          { className: "row node-header" },
          el("strong", { textContent: d.name || d.id }),
          el("span", { className: "hint", textContent: d.variable && d.variable.Name ? "→ " + d.variable.Name : "" }),
          ...reqBadges
        )
      );

      for (const table of d.tables || []) {
        card.appendChild(renderReadOnlyTable(table));
      }
      wrap.appendChild(card);
    }

    if (decisions.length === 0) {
      wrap.appendChild(el("p", { className: "hint", textContent: "This deployment has no decision tables to display." }));
    }
    return wrap;
  }

  function renderReadOnlyTable(table) {
    const scroll = el("div", { className: "table-scroll" });
    const t = document.createElement("table");
    const head = document.createElement("tr");
    for (const input of table.Inputs || []) {
      head.appendChild(
        el("th", { className: "col-group-input" }, el("div", {}, input.Label || (input.InputExpression || {}).Text || ""), el("div", { className: "derived", textContent: (input.InputExpression || {}).TypeRef || "" }))
      );
    }
    for (const output of table.Output || []) {
      head.appendChild(el("th", { className: "col-group-output", textContent: output.Name }));
    }
    const policy = table.HitPolicy || "UNIQUE";
    head.appendChild(
      el("th", { textContent: policy + (table.Aggregation ? " (" + table.Aggregation + ")" : "") })
    );
    t.appendChild(head);

    for (const rule of table.Rules || []) {
      const tr = document.createElement("tr");
      for (const ie of rule.InputEntries || []) {
        tr.appendChild(el("td", { textContent: ie.Text }));
      }
      for (const oe of rule.OutputEntries || []) {
        tr.appendChild(el("td", { textContent: oe.Text }));
      }
      tr.appendChild(el("td", {}));
      t.appendChild(tr);
    }

    scroll.appendChild(t);
    return scroll;
  }

  function renderEvaluateTab() {
    const wrap = el("div", {});
    wrap.appendChild(el("p", { className: "hint", textContent: "Provide one JSON value per external input and evaluate against this deployed version." }));
    const textarea = el("textarea", { rows: 8, value: state.evalInputs });
    textarea.addEventListener("input", () => {
      state.evalUserEdited = true;
      state.evalInputs = textarea.value;
    });
    wrap.appendChild(textarea);

    const toolbar = el("div", { className: "toolbar" });
    const runBtn = el("button", { textContent: "Evaluate" });
    const result = el("pre", { className: "result" });
    runBtn.addEventListener("click", async () => {
      let inputs;
      try {
        inputs = JSON.parse(state.evalInputs || "{}");
      } catch (e) {
        result.className = "result error";
        result.textContent = "Invalid inputs JSON: " + e.message;
        return;
      }
      runBtn.disabled = true;
      try {
        const res = await fetch(`/api/deployments/${state.selectedId}/evaluate`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ inputs }),
        });
        const data = await res.json();
        if (!res.ok && !data.trace) {
          result.className = "result error";
          result.textContent = data.error || `HTTP ${res.status}`;
        } else {
          const lines = [];
          if (data.error) lines.push("Error: " + data.error);
          if (data.outputs) lines.push("Outputs:\n" + JSON.stringify(data.outputs, null, 2));
          if (data.trace && data.trace.length) {
            lines.push(
              "\nMatched rules:\n" +
                data.trace
                  .map((t) => `${t.decisionName || t.decisionId}: rule(s) ${t.matchedRules.map((r) => "#" + r.ruleIndex).join(", ") || "(none)"}`)
                  .join("\n")
            );
          }
          result.className = "result" + (data.error ? " error" : "");
          result.textContent = lines.join("\n");
        }
        if (state.tab === "history") loadHistory(0);
        state.history = null; // invalidate so History tab refetches next visit
      } catch (e) {
        result.className = "result error";
        result.textContent = "Request failed: " + e.message;
      } finally {
        runBtn.disabled = false;
      }
    });
    toolbar.appendChild(runBtn);
    wrap.appendChild(toolbar);
    wrap.appendChild(result);
    return wrap;
  }

  function renderHistoryTab() {
    const wrap = el("div", {});
    if (!state.history) {
      wrap.appendChild(el("p", { className: "hint", textContent: "Loading…" }));
      return wrap;
    }
    const { evaluations, total, limit, offset } = state.history;
    wrap.appendChild(el("p", { className: "hint", textContent: `${total} evaluation(s) recorded` }));

    if (evaluations.length === 0) {
      wrap.appendChild(el("p", { className: "hint", textContent: "No evaluations recorded yet for this version." }));
      return wrap;
    }

    const table = document.createElement("table");
    table.appendChild(
      el("tr", {}, el("th", { textContent: "When" }), el("th", { textContent: "Inputs" }), el("th", { textContent: "Result" }), el("th", { textContent: "" }))
    );

    for (const ev of evaluations) {
      const tr = document.createElement("tr");
      tr.appendChild(el("td", { textContent: fmtDate(ev.createdAt) }));
      tr.appendChild(el("td", { textContent: truncate(JSON.stringify(ev.inputs)) }));
      const resultCell = el("td", {});
      if (ev.error) {
        resultCell.appendChild(el("span", { className: "badge badge-error", textContent: "error" }));
        resultCell.append(" " + truncate(ev.error));
      } else {
        resultCell.appendChild(el("span", { className: "badge badge-ok", textContent: "ok" }));
        resultCell.append(" " + truncate(JSON.stringify(ev.outputs)));
      }
      tr.appendChild(resultCell);

      const expandTd = el("td", {});
      const expandBtn = el("button", { className: "secondary", textContent: "Details" });
      expandTd.appendChild(expandBtn);
      tr.appendChild(expandTd);
      table.appendChild(tr);

      const detailRow = el("tr", { className: "hidden cockpit-detail-row" });
      const detailTd = el("td", { colSpan: 4 });
      const pre = el("pre", { className: "result" });
      pre.textContent = JSON.stringify(ev, null, 2);
      detailTd.appendChild(pre);
      detailRow.appendChild(detailTd);
      table.appendChild(detailRow);

      expandBtn.addEventListener("click", () => detailRow.classList.toggle("hidden"));
    }
    wrap.appendChild(el("div", { className: "table-scroll" }, table));

    const nav = el("div", { className: "toolbar" });
    if (offset > 0) {
      const prev = el("button", { className: "secondary", textContent: "◀ Newer" });
      prev.addEventListener("click", () => loadHistory(Math.max(0, offset - limit)));
      nav.appendChild(prev);
    }
    if (offset + evaluations.length < total) {
      const next = el("button", { className: "secondary", textContent: "Older ▶" });
      next.addEventListener("click", () => loadHistory(offset + limit));
      nav.appendChild(next);
    }
    wrap.appendChild(nav);
    return wrap;
  }

  function truncate(s, n) {
    n = n || 80;
    if (s == null) return "";
    return s.length > n ? s.slice(0, n) + "…" : s;
  }

  $("cockpitSearch").addEventListener("input", (e) => {
    state.filter = e.target.value;
    renderSidebar();
  });
  $("cockpitRefresh").addEventListener("click", () => loadList(true));
  $("cockpitLoadMore").addEventListener("click", () => loadList(false));

  window.Cockpit = {
    onShow() {
      if (!state.loaded) loadList(true);
    },
  };
})();
