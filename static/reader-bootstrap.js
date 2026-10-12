(() => {
  const appRoot = document.getElementById("app");
  const status = document.getElementById("focus-bootstrap");
  const retry = document.getElementById("focus-bootstrap-retry");
  retry.addEventListener("click", () => window.location.reload());
  try {
    if (!window.__plaudParityReady) throw new Error("reader extension not ready");
    // Reveal and render in the same task: no intermediate legacy layout can
    // be painted. The V3 renderer shows its own bounded data-loading state.
    appRoot.hidden = false;
    render();
    status.remove();
  } catch (error) {
    appRoot.hidden = true;
    document.getElementById("focus-bootstrap-message").textContent =
      window.plaudI18n?.locale === "en"
        ? "Focus Space could not open. Please reload."
        : "焦点空间未能打开，请重新加载。";
    retry.textContent = window.plaudI18n?.locale === "en" ? "Reload" : "重新加载";
    retry.hidden = false;
    console.error("Focus Space initialization failed", error);
  }
})();
