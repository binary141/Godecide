(() => {
  const TYPES = ["string", "number", "boolean", "date"];

  let nodeCounter = 0;

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
      },
      overrides
    );
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
  };

  const $ = (id) => document.getElementById(id);

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

    return panel;
  }

  function render() {
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
      })),
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
        return;
      }
      $("xmlOut").value = data.xml;
    } catch (e) {
      showError("Request failed: " + e.message);
    }
  }

  function downloadXML() {
    const xml = $("xmlOut").value;
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

  render();
})();
