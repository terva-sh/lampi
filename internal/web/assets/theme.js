// Applies a remembered theme before the page paints. It loads without
// defer, so it is kept to this; lake.js wires the toggle.
(function () {
  try {
    var t = localStorage.getItem("lampi-theme");
    if (t === "light" || t === "dark") document.documentElement.dataset.theme = t;
  } catch (e) {}
})();
