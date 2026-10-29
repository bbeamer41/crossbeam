/* Copyright (C) 2026 K5LEB */
/* SPDX-License-Identifier: GPL-3.0-or-later */
(function () {
  var form = document.getElementById("compose");
  if (!form || !form.elements) {
    return;
  }
  var packet = document.getElementById("packet");
  var micron = document.getElementById("micron");
  var status = document.getElementById("preview-status");
  var timer = 0;
  var previewGen = 0;

  function field(name) {
    var el = form.elements.namedItem(name);
    if (!el || typeof el.value !== "string") {
      return "";
    }
    return el.value;
  }

  function preview() {
    var gen = ++previewGen;
    var payload = {
      title: field("title"),
      author: field("author"),
      id: field("id"),
      created: field("created"),
      body: field("body")
    };
    fetch("/api/preview", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-Crossbeam-Request": "1"
      },
      body: JSON.stringify(payload)
    }).then(function (res) {
      return res.json().then(function (data) {
        return { ok: res.ok, data: data };
      });
    }).then(function (result) {
      if (gen !== previewGen) {
        return;
      }
      if (!result.ok) {
        if (status) {
          status.textContent = (result.data && result.data.error) || "Preview failed.";
        }
        return;
      }
      if (status) {
        status.textContent = "";
      }
      if (packet) {
        packet.textContent = result.data.packet || "";
      }
      if (micron) {
        micron.textContent = result.data.micron || "";
      }
      var created = form.elements.namedItem("created");
      if (created && !created.value && result.data.created) {
        created.value = result.data.created;
      }
    }).catch(function () {
      if (gen !== previewGen) {
        return;
      }
      if (status) {
        status.textContent = "Preview failed.";
      }
    });
  }

  form.addEventListener("input", function () {
    window.clearTimeout(timer);
    timer = window.setTimeout(preview, 250);
  });
})();
