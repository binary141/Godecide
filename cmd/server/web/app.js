(() => {
  const TYPES = ["string", "number", "boolean", "date"];

  let nodeCounter = 0;
  let governanceCounter = { knowledgeSources: 0, organizationUnits: 0, performanceIndicators: 0 };

  function newNode(overrides) {
    nodeCounter += 1;
    return Object.assign(
      {
        id: "n" + nodeCounter,
        decisionName: "Decision " + nodeCounter,
        hitPolicy: "UNIQUE",
        aggregation: "",
        inputs: [{ label: "Input 1", typeRef: "string", source: "" }],
        outputs: [{ name: "Output", typeRef: "string" }],
        rules: [{ inputEntries: ["-"], outputEntries: [""] }],
        authority: [],
        decisionMakers: [],
        decisionOwners: [],
        impactedPerformanceIndicators: [],
      },
      overrides
    );
  }

  const GOVERNANCE_KINDS = {
    knowledgeSources: { prefix: "ks", label: "Knowledge source", plural: "Knowledge sources" },
    organizationUnits: { prefix: "ou", label: "Organization unit", plural: "Organization units" },
    performanceIndicators: { prefix: "pi", label: "Performance indicator", plural: "Performance indicators" },
  };

  function newGovernanceEntry(kind) {
    governanceCounter[kind] += 1;
    const id = GOVERNANCE_KINDS[kind].prefix + governanceCounter[kind];
    const entry = { id, name: GOVERNANCE_KINDS[kind].label + " " + governanceCounter[kind] };
    if (kind === "knowledgeSources") entry.type = "";
    return entry;
  }

  const state = {
    nodes: [
      newNode({
        decisionName: "Age Check",
        inputs: [{ label: "Age", typeRef: "number", source: "" }],
        outputs: [{ name: "Category", typeRef: "string" }],
        rules: [
          { inputEntries: ["< 18"], outputEntries: ["Minor"] },
          { inputEntries: [">= 18"], outputEntries: ["Adult"] },
        ],
      }),
    ],
    governance: {
      knowledgeSources: [],
      organizationUnits: [],
      performanceIndicators: [],
    },
  };

  // removeGovernanceEntry drops an entry and un-links it from every node
  // that referenced it, the same way removeNode clears dangling sources.
  function removeGovernanceEntry(kind, id) {
    state.governance[kind] = state.governance[kind].filter((e) => e.id !== id);
    const nodeField = { knowledgeSources: "authority", organizationUnits: null, performanceIndicators: "impactedPerformanceIndicators" }[kind];
    for (const n of state.nodes) {
      if (nodeField) n[nodeField] = n[nodeField].filter((ref) => ref !== id);
      if (kind === "organizationUnits") {
        n.decisionMakers = n.decisionMakers.filter((ref) => ref !== id);
        n.decisionOwners = n.decisionOwners.filter((ref) => ref !== id);
      }
    }
    render();
  }

  const $ = (id) => document.getElementById(id);

  // editingDeployment, when set, identifies the deployment this graph was
  // loaded from via Cockpit's "Edit in Builder" (see loadSpec below). It
  // suppresses deploySpec's name-uniquifying so redeploying bumps that
  // deployment's own version series instead of creating an unrelated one.
  let editingDeployment = null;

  function renderEditingBanner() {
    const banner = $("builderEditingBanner");
    if (!banner) return;
    if (!editingDeployment) {
      banner.classList.add("hidden");
      banner.innerHTML = "";
      return;
    }
    banner.classList.remove("hidden");
    banner.innerHTML = "";
    banner.appendChild(
      el(
        "span",
        {},
        `Editing "${editingDeployment.name}" (currently v${editingDeployment.version}). Deploying will create a new version.`
      )
    );
    const stopBtn = el("button", { className: "secondary", textContent: "Stop editing" });
    stopBtn.addEventListener("click", () => {
      editingDeployment = null;
      renderEditingBanner();
    });
    banner.appendChild(stopBtn);
  }

  function nodeOutputVar(n) {
    return n.outputs.length === 1 ? n.outputs[0].name : n.decisionName;
  }

  function nodeOutputType(n) {
    return n.outputs.length === 1 ? n.outputs[0].typeRef : "";
  }

  function el(tag, props, ...children) {
    const e = document.createElement(tag);
    Object.assign(e, props);
    for (const c of children) e.append(c);
    return e;
  }

  function typeSelect(value, onChange) {
    const sel = document.createElement("select");
    for (const t of TYPES) {
      const opt = document.createElement("option");
      opt.value = t;
      opt.textContent = t;
      if (t === value) opt.selected = true;
      sel.appendChild(opt);
    }
    sel.addEventListener("change", () => onChange(sel.value));
    return sel;
  }

  function textInput(value, onChange) {
    const inp = document.createElement("input");
    inp.type = "text";
    inp.value = value ?? "";
    inp.addEventListener("input", () => onChange(inp.value));
    return inp;
  }

  function removeButton(onClick, title) {
    const btn = document.createElement("button");
    btn.className = "rm-btn";
    btn.textContent = "✕";
    btn.title = title || "Remove";
    btn.addEventListener("click", onClick);
    return btn;
  }

  // sourceSelect lets an input column bind to an external test input or to
  // another node's output. Changing it re-renders the whole node list since
  // switching source changes which controls that column shows.
  function sourceSelect(node, input) {
    const sel = document.createElement("select");
    sel.appendChild(el("option", { value: "", textContent: "External input" }));
    for (const other of state.nodes) {
      if (other.id === node.id) continue;
      const opt = el("option", {
        value: other.id,
        textContent: "From: " + (other.decisionName || other.id),
      });
      if (input.source === other.id) opt.selected = true;
      sel.appendChild(opt);
    }
    if (input.source === "") sel.value = "";
    sel.addEventListener("change", () => {
      input.source = sel.value;
      render();
    });
    return sel;
  }

  function removeNode(nodeId) {
    state.nodes = state.nodes.filter((n) => n.id !== nodeId);
    for (const n of state.nodes) {
      for (const input of n.inputs) {
        if (input.source === nodeId) input.source = "";
      }
    }
    render();
  }

  // checkboxList renders one checkbox per governance entry of a kind, toggling
  // membership of that entry's id in the given node array field.
  function checkboxList(kind, selectedIds, onToggle) {
    const entries = state.governance[kind];
    if (entries.length === 0) {
      return el("span", { className: "hint", textContent: "None defined - add one in the Governance panel above." });
    }
    const wrap = el("div", { className: "governance-checkboxes" });
    for (const entry of entries) {
      const id = "gov_" + kind + "_" + entry.id + "_" + Math.random().toString(36).slice(2, 7);
      const cb = el("input", { type: "checkbox", id, checked: selectedIds.includes(entry.id) });
      cb.addEventListener("change", () => onToggle(entry.id, cb.checked));
      wrap.appendChild(el("label", { className: "governance-check" }, cb, el("span", { textContent: entry.name || entry.id })));
    }
    return wrap;
  }

  // renderNodeGovernance renders the "Governance links" section of a node
  // panel: which knowledge sources back it, which org units make/own it, and
  // which performance indicators it impacts. Purely metadata - never
  // evaluated - so it's rendered and collected independently of the table.
  function renderNodeGovernance(node) {
    const box = el("div", { className: "node-governance-editor" });
    box.appendChild(el("div", { className: "hint", textContent: "Governance (read-only metadata, not evaluated):" }));

    const row = (labelText, kind, field) =>
      el(
        "div",
        { className: "governance-field" },
        el("strong", { className: "governance-field-label", textContent: labelText }),
        checkboxList(kind, node[field], (id, checked) => {
          if (checked) node[field].push(id);
          else node[field] = node[field].filter((x) => x !== id);
        })
      );

    box.appendChild(row("Authority (knowledge sources)", "knowledgeSources", "authority"));
    box.appendChild(row("Decision maker (org units)", "organizationUnits", "decisionMakers"));
    box.appendChild(row("Decision owner (org units)", "organizationUnits", "decisionOwners"));
    box.appendChild(row("Impacted performance indicators", "performanceIndicators", "impactedPerformanceIndicators"));

    return box;
  }

  function renderGovernancePanel() {
    const panel = el("div", { className: "panel" });
    panel.appendChild(el("h2", { textContent: "Governance" }));
    panel.appendChild(
      el("p", {
        className: "hint",
        textContent: "Knowledge sources, organization units, and performance indicators are DRD metadata: link them to a decision node below to render authorityRequirement / decisionMaker / decisionOwner / impactedPerformanceIndicator in the exported DMN.",
      })
    );

    for (const [kind, meta] of Object.entries(GOVERNANCE_KINDS)) {
      const section = el("div", { className: "governance-section" });
      section.appendChild(el("h3", { textContent: meta.plural }));

      for (const entry of state.governance[kind]) {
        const rowChildren = [
          textInput(entry.name, (v) => (entry.name = v)),
        ];
        if (kind === "knowledgeSources") {
          rowChildren.push(textInput(entry.type, (v) => (entry.type = v)));
          rowChildren[rowChildren.length - 1].placeholder = "Type (optional)";
        }
        rowChildren.push(removeButton(() => removeGovernanceEntry(kind, entry.id), "Remove " + meta.label.toLowerCase()));
        section.appendChild(el("div", { className: "row governance-entry-row" }, ...rowChildren));
      }

      const addBtn = el("button", { className: "secondary", textContent: "+ " + meta.label });
      addBtn.addEventListener("click", () => {
        state.governance[kind].push(newGovernanceEntry(kind));
        render();
      });
      section.appendChild(addBtn);
      panel.appendChild(section);
    }

    return panel;
  }

  function renderNodeTable(node) {
    const table = document.createElement("table");
    const headRow1 = document.createElement("tr");
    const headRow2 = document.createElement("tr");

    node.inputs.forEach((input, i) => {
      const th1 = document.createElement("th");
      th1.className = "col-group-input";
      th1.appendChild(sourceSelect(node, input));

      if (input.source === "") {
        th1.appendChild(textInput(input.label, (v) => (input.label = v)));
      } else {
        const upstream = state.nodes.find((n) => n.id === input.source);
        const varName = upstream ? nodeOutputVar(upstream) : "?";
        th1.appendChild(el("div", { className: "derived", textContent: "= " + varName }));
      }

      th1.appendChild(
        removeButton(() => {
          node.inputs.splice(i, 1);
          node.rules.forEach((r) => r.inputEntries.splice(i, 1));
          render();
        }, "Remove input column")
      );
      headRow1.appendChild(th1);

      const th2 = document.createElement("th");
      th2.className = "col-group-input";
      if (input.source === "") {
        th2.appendChild(typeSelect(input.typeRef, (v) => (input.typeRef = v)));
      } else {
        const upstream = state.nodes.find((n) => n.id === input.source);
        const typeRef = upstream ? nodeOutputType(upstream) || input.typeRef : input.typeRef;
        th2.appendChild(el("div", { className: "derived", textContent: typeRef || "(type)" }));
      }
      headRow2.appendChild(th2);
    });

    node.outputs.forEach((output, i) => {
      const th1 = document.createElement("th");
      th1.className = "col-group-output";
      th1.appendChild(textInput(output.name, (v) => (output.name = v)));
      th1.appendChild(
        removeButton(() => {
          node.outputs.splice(i, 1);
          node.rules.forEach((r) => r.outputEntries.splice(i, 1));
          render();
        }, "Remove output column")
      );
      headRow1.appendChild(th1);

      const th2 = document.createElement("th");
      th2.className = "col-group-output";
      th2.appendChild(typeSelect(output.typeRef, (v) => (output.typeRef = v)));
      headRow2.appendChild(th2);
    });

    headRow1.appendChild(document.createElement("th"));
    headRow2.appendChild(document.createElement("th"));

    table.appendChild(headRow1);
    table.appendChild(headRow2);

    node.rules.forEach((rule, ri) => {
      const tr = document.createElement("tr");

      node.inputs.forEach((_, ci) => {
        const td = document.createElement("td");
        td.appendChild(
          textInput(rule.inputEntries[ci], (v) => (rule.inputEntries[ci] = v))
        );
        tr.appendChild(td);
      });

      node.outputs.forEach((_, ci) => {
        const td = document.createElement("td");
        td.appendChild(
          textInput(rule.outputEntries[ci], (v) => (rule.outputEntries[ci] = v))
        );
        tr.appendChild(td);
      });

      const tdRm = document.createElement("td");
      tdRm.appendChild(
        removeButton(() => {
          node.rules.splice(ri, 1);
          render();
        }, "Remove rule")
      );
      tr.appendChild(tdRm);

      table.appendChild(tr);
    });

    return table;
  }

  function renderNodePanel(node) {
    const panel = document.createElement("div");
    panel.className = "node-panel";

    const header = el(
      "div",
      { className: "row node-header" },
      el(
        "label",
        {},
        "Decision name",
        textInput(node.decisionName, (v) => (node.decisionName = v))
      ),
      el(
        "label",
        {},
        "Hit policy",
        (() => {
          const sel = document.createElement("select");
          for (const hp of ["UNIQUE", "FIRST", "ANY", "PRIORITY", "RULE ORDER", "OUTPUT ORDER", "COLLECT"]) {
            const opt = el("option", { value: hp, textContent: hp });
            if (hp === node.hitPolicy) opt.selected = true;
            sel.appendChild(opt);
          }
          sel.addEventListener("change", () => {
            node.hitPolicy = sel.value;
            render();
          });
          return sel;
        })()
      )
    );

    if (node.hitPolicy === "COLLECT") {
      header.appendChild(
        el(
          "label",
          {},
          "Aggregation",
          (() => {
            const sel = document.createElement("select");
            for (const agg of ["", "SUM", "MIN", "MAX", "COUNT"]) {
              const opt = el("option", { value: agg, textContent: agg || "(none)" });
              if (agg === node.aggregation) opt.selected = true;
              sel.appendChild(opt);
            }
            sel.addEventListener("change", () => (node.aggregation = sel.value));
            return sel;
          })()
        )
      );
    }

    if (state.nodes.length > 1) {
      const rm = el("button", { className: "secondary", textContent: "Remove node" });
      rm.addEventListener("click", () => removeNode(node.id));
      header.appendChild(rm);
    }

    panel.appendChild(header);

    const toolbar = el("div", { className: "toolbar" });
    const addInput = el("button", { textContent: "+ Input column" });
    addInput.addEventListener("click", () => {
      node.inputs.push({ label: `Input ${node.inputs.length + 1}`, typeRef: "string", source: "" });
      node.rules.forEach((r) => r.inputEntries.push("-"));
      render();
    });
    const addOutput = el("button", { textContent: "+ Output column" });
    addOutput.addEventListener("click", () => {
      node.outputs.push({ name: `Output ${node.outputs.length + 1}`, typeRef: "string" });
      node.rules.forEach((r) => r.outputEntries.push(""));
      render();
    });
    const addRule = el("button", { textContent: "+ Rule" });
    addRule.addEventListener("click", () => {
      node.rules.push({
        inputEntries: node.inputs.map(() => "-"),
        outputEntries: node.outputs.map(() => ""),
      });
      render();
    });
    toolbar.append(addInput, addOutput, addRule);
    panel.appendChild(toolbar);

    const scroll = el("div", { className: "table-scroll" });
    scroll.appendChild(renderNodeTable(node));
    panel.appendChild(scroll);

    panel.appendChild(renderNodeGovernance(node));

    return panel;
  }

  function render() {
    const govContainer = $("governance");
    govContainer.innerHTML = "";
    govContainer.appendChild(renderGovernancePanel());

    const container = $("nodes");
    container.innerHTML = "";
    for (const node of state.nodes) {
      container.appendChild(renderNodePanel(node));
    }
    updateInputsTemplate();
  }

  function updateInputsTemplate() {
    const box = $("inputsJSON");
    if (box.dataset.userEdited === "true") return;
    const template = {};
    for (const node of state.nodes) {
      for (const input of node.inputs) {
        if (input.source !== "") continue;
        template[input.label] = input.typeRef === "number" ? 0 : input.typeRef === "boolean" ? false : "";
      }
    }
    box.value = JSON.stringify(template, null, 2);
  }

  // loadSpec replaces the whole graph with the given GraphSpec (the same
  // shape collectSpec produces), for Cockpit's "Edit in Builder" flow.
  // deploymentInfo, when given, marks this graph as editing that deployment
  // (see editingDeployment above).
  function loadSpec(spec, deploymentInfo) {
    nodeCounter = 0;
    governanceCounter = { knowledgeSources: 0, organizationUnits: 0, performanceIndicators: 0 };

    state.nodes = (spec.nodes || []).map((n) =>
      newNode({
        id: n.id,
        decisionName: n.decisionName,
        hitPolicy: n.hitPolicy || "UNIQUE",
        aggregation: n.aggregation || "",
        inputs: n.inputs && n.inputs.length ? n.inputs : [{ label: "Input 1", typeRef: "string", source: "" }],
        outputs: n.outputs && n.outputs.length ? n.outputs : [{ name: "Output", typeRef: "string" }],
        rules:
          n.rules && n.rules.length
            ? n.rules
            : [{ inputEntries: (n.inputs || []).map(() => "-"), outputEntries: (n.outputs || []).map(() => "") }],
        authority: n.authority || [],
        decisionMakers: n.decisionMakers || [],
        decisionOwners: n.decisionOwners || [],
        impactedPerformanceIndicators: n.impactedPerformanceIndicators || [],
      })
    );
    if (state.nodes.length === 0) state.nodes.push(newNode());

    state.governance = {
      knowledgeSources: (spec.governance && spec.governance.knowledgeSources) || [],
      organizationUnits: (spec.governance && spec.governance.organizationUnits) || [],
      performanceIndicators: (spec.governance && spec.governance.performanceIndicators) || [],
    };

    editingDeployment = deploymentInfo || null;
    renderEditingBanner();

    $("inputsJSON").dataset.userEdited = "false";
    render();
  }

  window.Builder = { loadSpec };

  function collectSpec() {
    return {
      nodes: state.nodes.map((n) => ({
        id: n.id,
        decisionName: n.decisionName || "Decision",
        hitPolicy: n.hitPolicy,
        aggregation: n.aggregation,
        inputs: n.inputs,
        outputs: n.outputs,
        rules: n.rules,
        authority: n.authority,
        decisionMakers: n.decisionMakers,
        decisionOwners: n.decisionOwners,
        impactedPerformanceIndicators: n.impactedPerformanceIndicators,
      })),
      governance: state.governance,
    };
  }

  function showError(text) {
    const el = $("result");
    el.textContent = text;
    el.className = "result error";
  }

  function showOutputs(outputs) {
    const el = $("result");
    const lines = state.nodes.map((n) => `${n.decisionName}: ${JSON.stringify(outputs[n.id])}`);
    el.textContent = lines.join("\n");
    el.className = "result";
  }

  async function runEval() {
    let inputs;
    try {
      inputs = JSON.parse($("inputsJSON").value || "{}");
    } catch (e) {
      showError("Invalid inputs JSON: " + e.message);
      return;
    }

    try {
      const res = await fetch("/api/evaluate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ spec: collectSpec(), inputs }),
      });
      const data = await res.json();
      if (!res.ok || data.error) {
        showError(data.error || `HTTP ${res.status}`);
        return;
      }
      showOutputs(data.outputs || {});
    } catch (e) {
      showError("Request failed: " + e.message);
    }
  }

  async function exportXML() {
    try {
      const res = await fetch("/api/export", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(collectSpec()),
      });
      const data = await res.json();
      if (!res.ok || data.error) {
        $("xmlOut").value = "";
        showError(data.error || `HTTP ${res.status}`);
        return null;
      }
      $("xmlOut").value = data.xml;
      return data.xml;
    } catch (e) {
      showError("Request failed: " + e.message);
      return null;
    }
  }

  async function downloadXML() {
    const xml = await exportXML();
    if (!xml) return;
    const blob = new Blob([xml], { type: "application/xml" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    const name = (state.nodes[0]?.decisionName || "decision").trim().replace(/[^a-zA-Z0-9]+/g, "_");
    a.download = `${name}.dmn`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function showDeployResult(text, isError) {
    const el = $("deployResult");
    el.textContent = text;
    el.className = isError ? "result error" : "result";
  }

  // deploySpec clones collectSpec() and gives the first node's decisionName
  // a unique suffix. The deployed decision name is also its versioning key
  // (see db.CreateDeployment): redeploying the same name bumps its version
  // instead of creating a separate decision, which is right for the
  // programmatic API but not for the builder's "Deploy" button, where an
  // unchanged name is usually just a forgotten rename, not an intentional
  // redeploy. Uniquifying here makes every builder deploy its own new
  // decision - unless this graph came from Cockpit's "Edit in Builder"
  // (editingDeployment set), where an unchanged name IS the intentional
  // redeploy: it's what bumps the edited deployment's own version.
  function deploySpec() {
    const spec = collectSpec();
    if (!editingDeployment && spec.nodes.length > 0) {
      const suffix = new Date().toISOString().replace(/[-:TZ.]/g, "").slice(0, 14);
      spec.nodes[0] = { ...spec.nodes[0], decisionName: `${spec.nodes[0].decisionName}-${suffix}` };
    }
    return spec;
  }

  async function deployTable() {
    showDeployResult("Deploying…", false);
    try {
      const exportRes = await fetch("/api/export", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(deploySpec()),
      });
      const exportData = await exportRes.json();
      if (!exportRes.ok || exportData.error) {
        showDeployResult(exportData.error || `HTTP ${exportRes.status}`, true);
        return;
      }

      const res = await fetch("/api/deployments", {
        method: "POST",
        headers: { "Content-Type": "application/xml" },
        body: exportData.xml,
      });
      const data = await res.json();
      if (!res.ok || data.error) {
        showDeployResult(data.error || `HTTP ${res.status}`, true);
        return;
      }
      if (editingDeployment) {
        showDeployResult(`Deployed "${data.name}" as v${data.version} (id ${data.id}).`, false);
        editingDeployment = { id: data.id, name: data.name, version: data.version };
        renderEditingBanner();
      } else {
        showDeployResult(`Deployed "${data.name}" as a new decision (id ${data.id}).`, false);
      }
    } catch (e) {
      showDeployResult("Request failed: " + e.message, true);
    }
  }

  $("addNode").addEventListener("click", () => {
    state.nodes.push(newNode());
    render();
  });

  $("inputsJSON").addEventListener("input", (e) => {
    e.target.dataset.userEdited = "true";
  });

  $("runEval").addEventListener("click", runEval);
  $("exportXML").addEventListener("click", exportXML);
  $("downloadXML").addEventListener("click", downloadXML);
  $("deployTable").addEventListener("click", deployTable);

  render();
})();
