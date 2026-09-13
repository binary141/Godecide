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
    tab: "definition",    // definition | evaluate | batch | history
    evalInputs: "",
    evalUserEdited: false,
    batchInputs: "",
    batchResults: null,
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
    state.batchInputs = "";
    state.batchResults = null;
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
    if (!state.batchInputs) {
      state.batchInputs = JSON.stringify([template], null, 2);
    }
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
    for (const [id, label] of [["diagram", "Diagram"], ["definition", "Definition"], ["evaluate", "Evaluate"], ["batch", "Batch"], ["history", "History"]]) {
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

    if (state.tab === "diagram") main.appendChild(renderDiagramTab());
    else if (state.tab === "definition") main.appendChild(renderDefinitionTab());
    else if (state.tab === "evaluate") main.appendChild(renderEvaluateTab());
    else if (state.tab === "batch") main.appendChild(renderBatchTab());
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

  const SVG_NS = "http://www.w3.org/2000/svg";

  function svgEl(tag, attrs, ...children) {
    const e = document.createElementNS(SVG_NS, tag);
    for (const k in attrs || {}) e.setAttribute(k, attrs[k]);
    for (const c of children) {
      if (c == null) continue;
      e.append(c);
    }
    return e;
  }

  function truncateLabel(s, n) {
    n = n || 24;
    if (s == null) return "";
    return s.length > n ? s.slice(0, n - 1) + "…" : s;
  }

  // Assigns each input/decision node a layer: external inputs (and any
  // decision with no requirements) sit at layer 0; every other decision's
  // layer is one more than the deepest layer of anything it requires. This
  // gives a simple, dependency-ordered DRD layout without a full graph
  // layout library.
  function computeDrdLayers(inputData, decisions) {
    const decisionById = new Map(decisions.map((d) => [d.id, d]));
    const inputIds = new Set(inputData.map((i) => i.ID));
    const layers = new Map();

    function layerOf(id, stack) {
      if (layers.has(id)) return layers.get(id);
      if (inputIds.has(id)) {
        layers.set(id, 0);
        return 0;
      }
      const d = decisionById.get(id);
      if (!d || stack.has(id)) {
        layers.set(id, 0);
        return 0;
      }
      stack.add(id);
      let maxDep = -1;
      for (const r of d.requires || []) {
        maxDep = Math.max(maxDep, layerOf(r.ref, stack));
      }
      stack.delete(id);
      const layer = maxDep + 1;
      layers.set(id, layer);
      return layer;
    }

    for (const d of decisions) layerOf(d.id, new Set());
    for (const i of inputData) layers.set(i.ID, 0);
    return layers;
  }

  function renderDiagramTab() {
    const inputData = state.decisions.inputData || [];
    const decisions = state.decisions.decisions || [];

    if (inputData.length === 0 && decisions.length === 0) {
      return el("p", { className: "hint", textContent: "This deployment has no decision requirements graph to display." });
    }

    const layerMap = computeDrdLayers(inputData, decisions);
    const rows = [];
    for (const i of inputData) {
      const l = layerMap.get(i.ID) || 0;
      (rows[l] = rows[l] || []).push({ id: i.ID, kind: "input", label: i.Name || i.ID });
    }
    for (const d of decisions) {
      const l = layerMap.get(d.id) || 0;
      (rows[l] = rows[l] || []).push({ id: d.id, kind: "decision", label: d.name || d.id });
    }
    for (const row of rows) if (row) row.sort((a, b) => a.label.localeCompare(b.label));

    const NW = 170, NH = 56, HGAP = 30, VGAP = 70, MARGIN = 30;
    const maxLayer = rows.length - 1;
    const rowWidths = rows.map((row) => (row ? row.length * NW + (row.length - 1) * HGAP : 0));
    const canvasWidth = Math.max(...rowWidths) + MARGIN * 2;
    const canvasHeight = (maxLayer + 1) * (NH + VGAP) - VGAP + MARGIN * 2;

    const positions = new Map();
    rows.forEach((row, li) => {
      if (!row) return;
      const rowWidth = rowWidths[li];
      let x = MARGIN + (canvasWidth - MARGIN * 2 - rowWidth) / 2;
      const y = MARGIN + (maxLayer - li) * (NH + VGAP);
      for (const node of row) {
        positions.set(node.id, { ...node, x, y, w: NW, h: NH });
        x += NW + HGAP;
      }
    });

    const svg = svgEl("svg", {
      class: "drd-svg",
      viewBox: `0 0 ${canvasWidth} ${canvasHeight}`,
      width: canvasWidth,
      height: canvasHeight,
    });
    svg.appendChild(
      svgEl(
        "defs",
        {},
        svgEl(
          "marker",
          { id: "drdArrow", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" },
          svgEl("path", { d: "M0,0 L10,5 L0,10 z", class: "drd-arrowhead" })
        )
      )
    );

    const edgesG = svgEl("g", { class: "drd-edges" });
    for (const d of decisions) {
      const dst = positions.get(d.id);
      if (!dst) continue;
      for (const r of d.requires || []) {
        const src = positions.get(r.ref);
        if (!src) continue;
        const x1 = src.x + src.w / 2, y1 = src.y;
        const x2 = dst.x + dst.w / 2, y2 = dst.y + dst.h;
        const midY = (y1 + y2) / 2;
        edgesG.appendChild(
          svgEl("path", {
            class: "drd-edge",
            d: `M${x1},${y1} C${x1},${midY} ${x2},${midY} ${x2},${y2}`,
            "marker-end": "url(#drdArrow)",
          })
        );
      }
    }
    svg.appendChild(edgesG);

    const nodesG = svgEl("g", { class: "drd-nodes" });
    for (const node of positions.values()) {
      const g = svgEl("g", { class: "drd-node drd-node-" + node.kind, transform: `translate(${node.x},${node.y})` });
      g.appendChild(
        svgEl("rect", {
          class: "drd-node-shape",
          width: node.w,
          height: node.h,
          rx: node.kind === "input" ? node.h / 2 : 6,
        })
      );
      const text = svgEl("text", { class: "drd-node-label", x: node.w / 2, y: node.h / 2 });
      text.textContent = truncateLabel(node.label);
      g.appendChild(svgEl("title", {}, node.label));
      g.appendChild(text);
      nodesG.appendChild(g);
    }
    svg.appendChild(nodesG);

    const wrap = el("div", { className: "table-scroll drd-wrap" }, svg);
    return wrap;
  }

  // Governance/business-context lookups shared by the definition tab: a
  // knowledgeSource, performanceIndicator, or organizationUnit id resolves
  // to its display name via these maps, falling back to the raw id.
  function governanceNameMaps() {
    const ks = Object.fromEntries((state.decisions.knowledgeSources || []).map((k) => [k.ID, k.Name || k.ID]));
    const pi = Object.fromEntries((state.decisions.performanceIndicators || []).map((p) => [p.ID, p.Name || p.ID]));
    const ou = Object.fromEntries((state.decisions.organizationUnits || []).map((o) => [o.ID, o.Name || o.ID]));
    return { ks, pi, ou };
  }

  function renderDefinitionTab() {
    const wrap = el("div", {});
    const decisions = state.decisions.decisions || [];
    const byId = Object.fromEntries(decisions.map((d) => [d.id, d]));
    const { ks, pi, ou } = governanceNameMaps();

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

      const govBadges = [
        ...(d.authority || []).map((a) =>
          el("span", { className: "badge badge-outline badge-governance", textContent: "authority: " + (ks[a.ref] || a.ref) })
        ),
        ...(d.decisionMakers || []).map((id) => el("span", { className: "badge badge-outline badge-governance", textContent: "maker: " + (ou[id] || id) })),
        ...(d.decisionOwners || []).map((id) => el("span", { className: "badge badge-outline badge-governance", textContent: "owner: " + (ou[id] || id) })),
        ...(d.impactedPerformanceIndicators || []).map((id) =>
          el("span", { className: "badge badge-outline badge-governance", textContent: "impacts: " + (pi[id] || id) })
        ),
      ];
      if (govBadges.length > 0) {
        card.appendChild(el("div", { className: "row node-governance" }, ...govBadges));
      }

      for (const table of d.tables || []) {
        card.appendChild(renderReadOnlyTable(table, null));
      }
      wrap.appendChild(card);
    }

    if (decisions.length === 0) {
      wrap.appendChild(el("p", { className: "hint", textContent: "This deployment has no decision tables to display." }));
    }

    const govPanel = renderGovernancePanel();
    if (govPanel) wrap.appendChild(govPanel);

    return wrap;
  }

  // Renders top-level governance DRG elements (knowledgeSource,
  // organizationUnit, performanceIndicator) that don't necessarily attach to
  // any single decision card above - e.g. an organizationUnit is itself the
  // list of decisions it makes/owns, not something a decision points back at.
  function renderGovernancePanel() {
    const knowledgeSources = state.decisions.knowledgeSources || [];
    const orgUnits = state.decisions.organizationUnits || [];
    const perfIndicators = state.decisions.performanceIndicators || [];
    if (knowledgeSources.length === 0 && orgUnits.length === 0 && perfIndicators.length === 0) return null;

    const decisionsById = Object.fromEntries((state.decisions.decisions || []).map((d) => [d.id, d.name || d.id]));
    const { ks } = governanceNameMaps();
    const panel = el("div", { className: "node-panel" });
    panel.appendChild(el("div", { className: "row node-header" }, el("strong", { textContent: "Governance" })));

    for (const k of knowledgeSources) {
      const badges = (k.AuthorityRequirements || []).map((a) => {
        const ref = a.RequiredAuthority ? a.RequiredAuthority.Href : a.RequiredDecision ? a.RequiredDecision.Href : a.RequiredInput ? a.RequiredInput.Href : "";
        const id = (ref || "").replace(/^#/, "");
        return el("span", { className: "badge badge-outline badge-governance", textContent: "depends on: " + (ks[id] || decisionsById[id] || id) });
      });
      panel.appendChild(
        el(
          "div",
          { className: "row governance-row" },
          el("span", { className: "badge badge-outline", textContent: "knowledge source" }),
          el("strong", { textContent: k.Name || k.ID }),
          k.Type ? el("span", { className: "hint", textContent: k.Type }) : null,
          ...badges
        )
      );
    }

    for (const o of orgUnits) {
      const made = (o.DecisionsMade || []).map((r) => decisionsById[r.Href.replace(/^#/, "")] || r.Href);
      const owned = (o.DecisionsOwned || []).map((r) => decisionsById[r.Href.replace(/^#/, "")] || r.Href);
      panel.appendChild(
        el(
          "div",
          { className: "row governance-row" },
          el("span", { className: "badge badge-outline", textContent: "organization unit" }),
          el("strong", { textContent: o.Name || o.ID }),
          ...made.map((name) => el("span", { className: "badge badge-outline badge-governance", textContent: "makes: " + name })),
          ...owned.map((name) => el("span", { className: "badge badge-outline badge-governance", textContent: "owns: " + name }))
        )
      );
    }

    for (const p of perfIndicators) {
      const impacting = (p.ImpactingDecisions || []).map((r) => decisionsById[r.Href.replace(/^#/, "")] || r.Href);
      panel.appendChild(
        el(
          "div",
          { className: "row governance-row" },
          el("span", { className: "badge badge-outline", textContent: "performance indicator" }),
          el("strong", { textContent: p.Name || p.ID }),
          ...impacting.map((name) => el("span", { className: "badge badge-outline badge-governance", textContent: "impacted by: " + name }))
        )
      );
    }

    return panel;
  }

  // matchedRuleIndices, when given, is a Set of 1-based rule indices that
  // fired for this table on the most recent evaluation; those rows get a
  // "matched" highlight and marker instead of just being listed as text.
  function renderReadOnlyTable(table, matchedRuleIndices) {
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

    (table.Rules || []).forEach((rule, i) => {
      const matched = !!(matchedRuleIndices && matchedRuleIndices.has(i + 1));
      const tr = el("tr", { className: matched ? "matched-rule" : "" });
      for (const ie of rule.InputEntries || []) {
        tr.appendChild(el("td", { textContent: ie.Text }));
      }
      for (const oe of rule.OutputEntries || []) {
        tr.appendChild(el("td", { textContent: oe.Text }));
      }
      tr.appendChild(el("td", { className: "matched-marker" }, matched ? el("span", { className: "badge badge-ok", textContent: "matched" }) : null));
      t.appendChild(tr);
    });

    scroll.appendChild(t);
    return scroll;
  }

  // Renders one card per decision in the evaluation trace, showing its
  // decision table(s) with the rule(s) that actually fired highlighted
  // in place, rather than just naming rule indices as text.
  function renderTracePanel(trace) {
    const wrap = el("div", {});
    wrap.appendChild(el("h3", { textContent: "Matched rules", className: "trace-heading" }));
    const decisionsById = Object.fromEntries((state.decisions.decisions || []).map((d) => [d.id, d]));

    for (const t of trace) {
      const decision = decisionsById[t.decisionId];
      const matchedSet = new Set((t.matchedRules || []).map((r) => r.ruleIndex));
      const card = el("div", { className: "node-panel" });
      card.appendChild(
        el(
          "div",
          { className: "row node-header" },
          el("strong", { textContent: t.decisionName || t.decisionId }),
          matchedSet.size === 0
            ? el("span", { className: "badge badge-outline", textContent: "no rule matched" })
            : null
        )
      );
      if (decision) {
        for (const table of decision.tables || []) {
          card.appendChild(renderReadOnlyTable(table, matchedSet));
        }
      } else {
        card.appendChild(el("p", { className: "hint", textContent: "rule(s) " + [...matchedSet].map((i) => "#" + i).join(", ") }));
      }
      wrap.appendChild(card);
    }
    return wrap;
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
    const tracePanel = el("div", {});
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
      tracePanel.innerHTML = "";
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
          result.className = "result" + (data.error ? " error" : "");
          result.textContent = lines.join("\n");
          if (data.trace && data.trace.length) {
            tracePanel.appendChild(renderTracePanel(data.trace));
          }
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
    wrap.appendChild(tracePanel);
    return wrap;
  }

  function renderBatchTab() {
    const wrap = el("div", {});
    wrap.appendChild(
      el("p", {
        className: "hint",
        textContent:
          "Provide a JSON array of input objects, one per row, to evaluate them all against this deployed version. Each row is recorded to history individually.",
      })
    );
    const textarea = el("textarea", { rows: 10, value: state.batchInputs });
    textarea.addEventListener("input", () => {
      state.batchInputs = textarea.value;
    });
    wrap.appendChild(textarea);

    const toolbar = el("div", { className: "toolbar" });
    const runBtn = el("button", { textContent: "Run batch" });
    const status = el("span", { className: "hint" });
    toolbar.appendChild(runBtn);
    toolbar.appendChild(status);
    wrap.appendChild(toolbar);

    const resultsWrap = el("div", {});
    if (state.batchResults) resultsWrap.appendChild(renderBatchResults(state.batchResults));
    wrap.appendChild(resultsWrap);

    runBtn.addEventListener("click", async () => {
      let rows;
      try {
        rows = JSON.parse(state.batchInputs || "[]");
        if (!Array.isArray(rows) || rows.length === 0) {
          throw new Error("expected a non-empty JSON array of input objects");
        }
      } catch (e) {
        status.className = "result error";
        status.textContent = "Invalid rows JSON: " + e.message;
        return;
      }

      runBtn.disabled = true;
      status.className = "hint";
      status.textContent = `Running ${rows.length} row(s)…`;
      try {
        const data = await getJSON(`/api/deployments/${state.selectedId}/evaluate/batch`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ rows }),
        });
        state.batchResults = data.results;
        const errCount = data.results.filter((r) => r.error).length;
        status.className = "hint";
        status.textContent = `${data.results.length} row(s) evaluated, ${errCount} error(s).`;
        resultsWrap.innerHTML = "";
        resultsWrap.appendChild(renderBatchResults(state.batchResults));
        state.history = null; // invalidate so History tab refetches next visit
      } catch (e) {
        status.className = "result error";
        status.textContent = "Request failed: " + e.message;
      } finally {
        runBtn.disabled = false;
      }
    });

    return wrap;
  }

  function renderBatchResults(results) {
    const wrap = el("div", {});
    const downloadBtn = el("button", { className: "secondary", textContent: "Download CSV" });
    downloadBtn.addEventListener("click", () => downloadBatchCSV(results));
    wrap.appendChild(el("div", { className: "toolbar" }, downloadBtn));

    const table = document.createElement("table");
    table.appendChild(
      el("tr", {}, el("th", { textContent: "#" }), el("th", { textContent: "Inputs" }), el("th", { textContent: "Result" }))
    );
    results.forEach((r, i) => {
      const tr = document.createElement("tr");
      tr.appendChild(el("td", { textContent: String(i + 1) }));
      tr.appendChild(el("td", { textContent: truncate(JSON.stringify(r.inputs)) }));
      const resultCell = el("td", {});
      if (r.error) {
        resultCell.appendChild(el("span", { className: "badge badge-error", textContent: "error" }));
        resultCell.append(" " + truncate(r.error));
      } else {
        resultCell.appendChild(el("span", { className: "badge badge-ok", textContent: "ok" }));
        resultCell.append(" " + truncate(JSON.stringify(r.outputs)));
      }
      tr.appendChild(resultCell);
      table.appendChild(tr);
    });
    wrap.appendChild(el("div", { className: "table-scroll" }, table));
    return wrap;
  }

  function downloadBatchCSV(results) {
    const inputKeys = new Set();
    const outputKeys = new Set();
    for (const r of results) {
      for (const k of Object.keys(r.inputs || {})) inputKeys.add(k);
      for (const k of Object.keys(r.outputs || {})) outputKeys.add(k);
    }
    const headers = [...inputKeys, ...outputKeys, "error"];
    const csvEscape = (v) => {
      if (v == null) return "";
      const s = typeof v === "string" ? v : JSON.stringify(v);
      return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
    };
    const lines = [headers.map(csvEscape).join(",")];
    for (const r of results) {
      const row = [
        ...[...inputKeys].map((k) => csvEscape((r.inputs || {})[k])),
        ...[...outputKeys].map((k) => csvEscape((r.outputs || {})[k])),
        csvEscape(r.error || ""),
      ];
      lines.push(row.join(","));
    }
    const blob = new Blob([lines.join("\n")], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = el("a", { href: url, download: `batch-results-${state.selectedId}.csv` });
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
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
