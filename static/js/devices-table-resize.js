(() => {
  const STORAGE_KEY = "nms_devices_table_col_widths_v1";

  function loadWidths() {
    try {
      const raw = window.localStorage.getItem(STORAGE_KEY);
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch (_) {
      return [];
    }
  }

  function saveWidths(widths) {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(widths));
    } catch (_) {}
  }

  function applyWidths(cols, headers, widths) {
    for (let i = 0; i < cols.length; i++) {
      const w = widths[i];
      if (typeof w === "number" && w > 0) {
        cols[i].style.width = `${w}px`;
        headers[i].style.width = `${w}px`;
      }
    }
  }

  function initDevicesTableResize() {
    const root = document.getElementById("devices-table-root");
    if (!root) return;
    const table = root.querySelector("table");
    if (!table) return;

    const cols = table.querySelectorAll("colgroup col");
    const headers = table.querySelectorAll("thead th");
    if (!cols.length || cols.length !== headers.length) return;

    const widths = loadWidths();
    applyWidths(cols, headers, widths);

    if (table.dataset.colResizeReady === "1") return;
    table.dataset.colResizeReady = "1";

    const minWidth = 72;
    Array.prototype.forEach.call(headers, (th, idx) => {
      th.style.position = "relative";
      th.style.cursor = "default";

      const handle = document.createElement("span");
      handle.setAttribute("aria-hidden", "true");
      handle.title = "Изменить ширину столбца";
      handle.style.position = "absolute";
      handle.style.top = "0";
      handle.style.right = "-6px";
      handle.style.width = "12px";
      handle.style.height = "100%";
      handle.style.cursor = "col-resize";
      handle.style.userSelect = "none";
      handle.style.touchAction = "none";
      handle.style.zIndex = "10";
      handle.style.background =
        "linear-gradient(to right, transparent 45%, rgba(148,163,184,0.55) 45%, rgba(148,163,184,0.55) 55%, transparent 55%)";

      handle.addEventListener("pointerdown", (e) => {
        e.preventDefault();
        try {
          handle.setPointerCapture(e.pointerId);
        } catch (_) {}
        const startX = e.clientX;
        const startWidth = th.getBoundingClientRect().width;
        table.style.tableLayout = "fixed";

        const onMove = (ev) => {
          const next = Math.max(minWidth, Math.round(startWidth + (ev.clientX - startX)));
          cols[idx].style.width = `${next}px`;
          th.style.width = `${next}px`;
          widths[idx] = next;
        };

        const onUp = (ev) => {
          window.removeEventListener("pointermove", onMove);
          window.removeEventListener("pointerup", onUp);
          try {
            handle.releasePointerCapture(ev.pointerId);
          } catch (_) {}
          saveWidths(widths);
        };

        window.addEventListener("pointermove", onMove);
        window.addEventListener("pointerup", onUp);
      });

      handle.addEventListener("mousedown", (e) => {
        e.preventDefault();
        const startX = e.clientX;
        const startWidth = th.getBoundingClientRect().width;
        table.style.tableLayout = "fixed";

        const onMove = (ev) => {
          const next = Math.max(minWidth, Math.round(startWidth + (ev.clientX - startX)));
          cols[idx].style.width = `${next}px`;
          th.style.width = `${next}px`;
          widths[idx] = next;
        };

        const onUp = () => {
          window.removeEventListener("mousemove", onMove);
          window.removeEventListener("mouseup", onUp);
          saveWidths(widths);
        };

        window.addEventListener("mousemove", onMove);
        window.addEventListener("mouseup", onUp);
      });

      th.appendChild(handle);
    });
  }

  document.addEventListener("DOMContentLoaded", initDevicesTableResize);
  document.body.addEventListener("htmx:afterSwap", (evt) => {
    if (evt && evt.target && evt.target.id === "devices-table-wrap") {
      initDevicesTableResize();
    }
  });
})();
