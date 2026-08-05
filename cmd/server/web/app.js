(() => {
  const TYPES = ["string", "number", "boolean", "date"];

  const state = {
    inputs: [{ label: "Age", typeRef: "number" }],
    outputs: [{ name: "Category", typeRef: "string" }],
    rules: [
      { inputEntries: ["< 18"], outputEntries: ["Minor"] },
      { inputEntries: ["-"], outputEntries: ["Adult"] },
    ],
  };

  const $ = (id) => document.getElementById(id);

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

  function render() {
    const table = $("dtable");
    table.innerHTML = "";

    const headRow1 = document.createElement("tr");
    const headRow2 = document.createElement("tr");

    state.inputs.forEach((input, i) => {
      const th1 = document.createElement("th");
      th1.className = "col-group-input";
      th1.appendChild(textInput(input.label, (v) => (state.inputs[i].label = v)));
      th1.appendChild(removeButton(() => {
        state.inputs.splice(i, 1);
        state.rules.forEach((r) => r.inputEntries.splice(i, 1));
        render();
      }, "Remove input column"));
      headRow1.appendChild(th1);

      const th2 = document.createElement("th");
      th2.className = "col-group-input";
      th2.appendChild(typeSelect(input.typeRef, (v) => (state.inputs[i].typeRef = v)));
      headRow2.appendChild(th2);
    });

    state.outputs.forEach((output, i) => {
      const th1 = document.createElement("th");
      th1.className = "col-group-output";
      th1.appendChild(textInput(output.name, (v) => (state.outputs[i].name = v)));
      th1.appendChild(removeButton(() => {
        state.outputs.splice(i, 1);
        state.rules.forEach((r) => r.outputEntries.splice(i, 1));
        render();
      }, "Remove output column"));
      headRow1.appendChild(th1);

      const th2 = document.createElement("th");
      th2.className = "col-group-output";
      th2.appendChild(typeSelect(output.typeRef, (v) => (state.outputs[i].typeRef = v)));
      headRow2.appendChild(th2);
    });

    const cornerTh = document.createElement("th");
    headRow1.appendChild(cornerTh);
    const cornerTh2 = document.createElement("th");
    headRow2.appendChild(cornerTh2);

    table.appendChild(headRow1);
    table.appendChild(headRow2);

    state.rules.forEach((rule, ri) => {
      const tr = document.createElement("tr");

      state.inputs.forEach((_, ci) => {
        const td = document.createElement("td");
        td.appendChild(
          textInput(rule.inputEntries[ci], (v) => (state.rules[ri].inputEntries[ci] = v))
        );
        tr.appendChild(td);
      });

      state.outputs.forEach((_, ci) => {
        const td = document.createElement("td");
        td.appendChild(
          textInput(rule.outputEntries[ci], (v) => (state.rules[ri].outputEntries[ci] = v))
        );
        tr.appendChild(td);
      });

      const tdRm = document.createElement("td");
      tdRm.appendChild(
        removeButton(() => {
          state.rules.splice(ri, 1);
          render();
        }, "Remove rule")
      );
      tr.appendChild(tdRm);

      table.appendChild(tr);
    });

    updateInputsTemplate();
  }

  function updateInputsTemplate() {
    const box = $("inputsJSON");
    if (box.dataset.userEdited === "true") return;
    const template = {};
    for (const input of state.inputs) {
      template[input.label] = input.typeRef === "number" ? 0 : input.typeRef === "boolean" ? false : "";
    }
    box.value = JSON.stringify(template, null, 2);
  }

  function collectSpec() {
    return {
      decisionName: $("decisionName").value || "Decision",
      hitPolicy: $("hitPolicy").value,
      aggregation: $("aggregation").value,
      inputs: state.inputs,
      outputs: state.outputs,
      rules: state.rules,
    };
  }

  function showResult(text, isError) {
    const el = $("result");
    el.textContent = text;
    el.className = "result" + (isError ? " error" : "");
  }

  async function runEval() {
    let inputs;
    try {
      inputs = JSON.parse($("inputsJSON").value || "{}");
    } catch (e) {
      showResult("Invalid inputs JSON: " + e.message, true);
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
        showResult(data.error || `HTTP ${res.status}`, true);
        return;
      }
      showResult(JSON.stringify(data.outputs, null, 2), false);
    } catch (e) {
      showResult("Request failed: " + e.message, true);
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
        showResult(data.error || `HTTP ${res.status}`, true);
        return;
      }
      $("xmlOut").value = data.xml;
    } catch (e) {
      showResult("Request failed: " + e.message, true);
    }
  }

  function downloadXML() {
    const xml = $("xmlOut").value;
    if (!xml) return;
    const blob = new Blob([xml], { type: "application/xml" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    const name = ($("decisionName").value || "decision").trim().replace(/[^a-zA-Z0-9]+/g, "_");
    a.download = `${name}.dmn`;
    a.click();
    URL.revokeObjectURL(url);
  }

  $("addInput").addEventListener("click", () => {
    state.inputs.push({ label: `Input ${state.inputs.length + 1}`, typeRef: "string" });
    state.rules.forEach((r) => r.inputEntries.push("-"));
    render();
  });

  $("addOutput").addEventListener("click", () => {
    state.outputs.push({ name: `Output ${state.outputs.length + 1}`, typeRef: "string" });
    state.rules.forEach((r) => r.outputEntries.push(""));
    render();
  });

  $("addRule").addEventListener("click", () => {
    state.rules.push({
      inputEntries: state.inputs.map(() => "-"),
      outputEntries: state.outputs.map(() => ""),
    });
    render();
  });

  $("hitPolicy").addEventListener("change", (e) => {
    $("aggregationLabel").classList.toggle("hidden", e.target.value !== "COLLECT");
  });

  $("inputsJSON").addEventListener("input", (e) => {
    e.target.dataset.userEdited = "true";
  });

  $("runEval").addEventListener("click", runEval);
  $("exportXML").addEventListener("click", exportXML);
  $("downloadXML").addEventListener("click", downloadXML);

  render();
})();
